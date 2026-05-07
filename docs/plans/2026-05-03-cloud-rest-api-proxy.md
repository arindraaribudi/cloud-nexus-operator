# CloudRestApiProxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `CloudRestApiProxy` CRD + `cloud-proxy` binary that lets hub workloads call AWS/GCP/Azure REST APIs via a spoke-side credential-injecting proxy, and migrate `RegistryProxy` from TCPProxy to HTTPProxy so both share a single hub gateway Service.

**Architecture:** Each spoke deploys a `cloud-proxy` pod (new binary) and an owned `HTTPProxy` CRD. FRP routes hub requests to the right spoke by matching `customDomains` + `locations` on `vhostHTTPPort`. The `cloud-proxy` strips the spoke/type prefix, detects the cloud provider from the FQDN, signs the request (AWS SigV4 via IRSA, GCP/Azure Bearer via Workload Identity), and forwards to `https://{cloudFQDN}{apiPath}`. `RegistryProxy` is migrated in parallel to the same HTTPProxy-based pattern so both share `{nexusserver}-gateway` Service.

**Tech Stack:** Go 1.25, controller-runtime, kubebuilder markers, AWS SDK v2 (`github.com/aws/aws-sdk-go-v2`), `golang.org/x/oauth2/google`, `github.com/Azure/azure-sdk-for-go/sdk/azidentity`, Ginkgo/Gomega for controller tests, standard `testing` for binary tests.

---

## File Map

```
New files:
  api/v1alpha1/cloudrestapiproxy_types.go
  internal/controller/cloudrestapiproxy_controller.go
  internal/controller/cloudrestapiproxy_controller_test.go
  cmd/cloud-proxy/main.go
  cmd/cloud-proxy/main_test.go
  config/samples/tunnel_v1alpha1_cloudrestapiproxy.yaml

Modified files:
  api/v1alpha1/nexusserver_types.go              (+GatewayServiceType field)
  api/v1alpha1/zz_generated.deepcopy.go          (regenerated via make generate)
  internal/controller/nexusserver_controller.go  (+reconcileGatewayService)
  internal/controller/nexusserver_controller_test.go  (+gateway Service tests)
  internal/controller/registryproxy_controller.go     (TCPProxy→HTTPProxy migration)
  internal/controller/registryproxy_controller_test.go (update assertions)
  cmd/registry-proxy/main.go                     (+STRIP_PREFIX support)
  cmd/registry-proxy/main_test.go                (+STRIP_PREFIX test)
  cmd/main.go                                    (+CloudRestApiProxy controller registration)
  config/samples/tunnel_v1alpha1_nexusserver.yaml (+vhostHTTPPort + gatewayServiceType)
```

---

## Task 1: CloudRestApiProxy CRD types

**Files:**
- Create: `api/v1alpha1/cloudrestapiproxy_types.go`
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (via `make generate`)

- [ ] **Step 1: Create the types file**

```go
// api/v1alpha1/cloudrestapiproxy_types.go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// CloudRestApiProxyCleanupFinalizer is added to CloudRestApiProxy resources.
	CloudRestApiProxyCleanupFinalizer = "tunnel.io/cloudrestapiproxy-cleanup"
)

// CloudRestApiProxySpec defines the desired state of CloudRestApiProxy.
type CloudRestApiProxySpec struct {
	// ClientRef references the NexusClient (spoke) that owns the tunnel.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`

	// ServiceAccountName is the Kubernetes SA annotated with cloud credentials
	// (IRSA for AWS, Workload Identity for GCP, Azure Workload Identity for Azure).
	// +required
	ServiceAccountName string `json:"serviceAccountName"`

	// ProxyPort is the port the cloud-proxy pod listens on inside the spoke.
	// +optional
	// +kubebuilder:default=8080
	ProxyPort int32 `json:"proxyPort,omitempty"`

	// AllowedDomains is an optional allowlist of upstream cloud FQDNs.
	// Supports wildcards: ["*.amazonaws.com", "*.googleapis.com"].
	// If empty, all domains are permitted.
	// +optional
	AllowedDomains []string `json:"allowedDomains,omitempty"`

	// Image overrides the default cloud-proxy container image.
	// +optional
	Image *ImageSpec `json:"image,omitempty"`
}

// CloudRestApiProxyStatus defines the observed state of CloudRestApiProxy.
type CloudRestApiProxyStatus struct {
	// Phase is Pending, Running, or Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// GatewayURL is the base URL on the hub gateway for this proxy.
	// e.g. http://hub-gateway.default.svc.cluster.local:8080/spoke1/cloud
	// +optional
	GatewayURL string `json:"gatewayURL,omitempty"`

	// Conditions reports detailed lifecycle state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="GatewayURL",type="string",JSONPath=".status.gatewayURL"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CloudRestApiProxy is the Schema for the cloudrestapiproxies API.
type CloudRestApiProxy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec CloudRestApiProxySpec `json:"spec"`

	// +optional
	Status CloudRestApiProxyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CloudRestApiProxyList contains a list of CloudRestApiProxy.
type CloudRestApiProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CloudRestApiProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudRestApiProxy{}, &CloudRestApiProxyList{})
}
```

- [ ] **Step 2: Regenerate deepcopy and CRD manifests**

```bash
make generate manifests
```

Expected: `api/v1alpha1/zz_generated.deepcopy.go` updated with `DeepCopyObject` and `DeepCopyInto` for `CloudRestApiProxy` and `CloudRestApiProxyList`. New file `config/crd/bases/tunnel.io_cloudrestapiproxies.yaml` created.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./api/...
```

Expected: exits 0, no errors.

- [ ] **Step 4: Commit**

```bash
git add api/v1alpha1/cloudrestapiproxy_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/bases/ config/rbac/
git commit -m "feat(api): add CloudRestApiProxy CRD types and generated manifests"
```

---

## Task 2: NexusServer gateway Service

**Files:**
- Modify: `api/v1alpha1/nexusserver_types.go`
- Modify: `internal/controller/nexusserver_controller.go`
- Modify: `internal/controller/nexusserver_controller_test.go`

- [ ] **Step 1: Write the failing test**

Add this `Describe` block to `internal/controller/nexusserver_controller_test.go` (inside the existing test file, after existing tests):

```go
var _ = Describe("NexusServer gateway Service", func() {
	const (
		gwServerName = "hub-with-vhost"
		gwNs         = "default"
	)
	ctx := context.Background()
	nsName := func(name string) types.NamespacedName {
		return types.NamespacedName{Namespace: gwNs, Name: name}
	}

	AfterEach(func() {
		srv := &tunnelv1alpha1.NexusServer{}
		if err := k8sClient.Get(ctx, nsName(gwServerName), srv); err == nil {
			_ = k8sClient.Delete(ctx, srv)
		}
	})

	It("creates gateway Service when vhostHTTPPort is set", func() {
		srv := &tunnelv1alpha1.NexusServer{
			ObjectMeta: metav1.ObjectMeta{Name: gwServerName, Namespace: gwNs},
			Spec: tunnelv1alpha1.NexusServerSpec{
				BindPort:           7000,
				VhostHTTPPort:      8080,
				GatewayServiceType: corev1.ServiceTypeClusterIP,
			},
		}
		Expect(k8sClient.Create(ctx, srv)).To(Succeed())

		r := &NexusServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsName(gwServerName)})
		Expect(err).NotTo(HaveOccurred())

		var gw corev1.Service
		Expect(k8sClient.Get(ctx, nsName(gwServerName+"-gateway"), &gw)).To(Succeed())
		Expect(gw.Spec.Ports[0].Port).To(Equal(int32(8080)))
		Expect(gw.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
	})

	It("does not create gateway Service when vhostHTTPPort is 0", func() {
		srv := &tunnelv1alpha1.NexusServer{
			ObjectMeta: metav1.ObjectMeta{Name: gwServerName, Namespace: gwNs},
			Spec:       tunnelv1alpha1.NexusServerSpec{BindPort: 7000},
		}
		Expect(k8sClient.Create(ctx, srv)).To(Succeed())

		r := &NexusServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsName(gwServerName)})
		Expect(err).NotTo(HaveOccurred())

		var gw corev1.Service
		Expect(k8sClient.Get(ctx, nsName(gwServerName+"-gateway"), &gw)).To(
			Satisfy(errors.IsNotFound))
	})
})
```

- [ ] **Step 2: Run to confirm it fails**

```bash
go test ./internal/controller/... -run "TestControllers/NexusServer_gateway" -v 2>&1 | tail -20
```

Expected: FAIL — `GatewayServiceType` field does not exist yet.

- [ ] **Step 3: Add GatewayServiceType field to NexusServerSpec**

In `api/v1alpha1/nexusserver_types.go`, add the import for `corev1` and the new field. Change the import block from:

```go
import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

To:

```go
import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

Add the field after `EnableOperatorPlugin` in `NexusServerSpec`:

```go
	// GatewayServiceType is the K8s Service type for the unified HTTP gateway.
	// The gateway Service is only created when vhostHTTPPort is non-zero.
	// +optional
	// +kubebuilder:default=ClusterIP
	GatewayServiceType corev1.ServiceType `json:"gatewayServiceType,omitempty"`
```

- [ ] **Step 4: Add reconcileGatewayService to nexusserver_controller.go**

In `internal/controller/nexusserver_controller.go`, add the call in `Reconcile` after `r.reconcileService`:

```go
	if err := r.reconcileGatewayService(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}
```

Then add the method at the bottom of the file (before `SetupWithManager`):

```go
func (r *NexusServerReconciler) reconcileGatewayService(ctx context.Context, server *tunnelv1alpha1.NexusServer) error {
	if server.Spec.VhostHTTPPort == 0 {
		return nil
	}
	svcType := server.Spec.GatewayServiceType
	if svcType == "" {
		svcType = corev1.ServiceTypeClusterIP
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-gateway",
			Namespace: server.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(server, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = svcType
		svc.Spec.Selector = map[string]string{"frpserver": server.Name}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "vhost-http",
				Port:       server.Spec.VhostHTTPPort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(server.Spec.VhostHTTPPort),
			},
		}
		return nil
	})
	return err
}
```

- [ ] **Step 5: Regenerate deepcopy**

```bash
make generate manifests
```

- [ ] **Step 6: Run the tests**

```bash
go test ./internal/controller/... -v 2>&1 | tail -30
```

Expected: all tests PASS including the two new gateway Service tests.

- [ ] **Step 7: Commit**

```bash
git add api/v1alpha1/nexusserver_types.go api/v1alpha1/zz_generated.deepcopy.go \
        internal/controller/nexusserver_controller.go \
        internal/controller/nexusserver_controller_test.go \
        config/crd/bases/ config/rbac/
git commit -m "feat(nexusserver): create gateway Service when vhostHTTPPort is set"
```

---

## Task 3: cloud-proxy binary — core parsing and provider detection

**Files:**
- Create: `cmd/cloud-proxy/main.go`
- Create: `cmd/cloud-proxy/main_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/cloud-proxy/main_test.go`:

```go
package main

import (
	"testing"
)

func TestParseCloudRequest(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		prefix      string
		wantFQDN    string
		wantAPIPath string
		wantErr     bool
	}{
		{
			name:        "AWS Secrets Manager",
			path:        "/spoke1/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp",
			prefix:      "/spoke1/cloud",
			wantFQDN:    "secretsmanager.us-east-1.amazonaws.com",
			wantAPIPath: "/v1/secret/myapp",
		},
		{
			name:        "GCP Secret Manager",
			path:        "/spoke1/cloud/secretmanager.googleapis.com/v1/projects/p/secrets/s/versions/latest:access",
			prefix:      "/spoke1/cloud",
			wantFQDN:    "secretmanager.googleapis.com",
			wantAPIPath: "/v1/projects/p/secrets/s/versions/latest:access",
		},
		{
			name:        "Azure Key Vault",
			path:        "/spoke2/cloud/myvault.vault.azure.net/secrets/myapp",
			prefix:      "/spoke2/cloud",
			wantFQDN:    "myvault.vault.azure.net",
			wantAPIPath: "/secrets/myapp",
		},
		{
			name:    "wrong prefix",
			path:    "/spoke1/registry/something",
			prefix:  "/spoke1/cloud",
			wantErr: true,
		},
		{
			name:    "no FQDN",
			path:    "/spoke1/cloud/",
			prefix:  "/spoke1/cloud",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fqdn, apiPath, err := parseCloudRequest(tc.path, tc.prefix)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fqdn != tc.wantFQDN {
				t.Errorf("FQDN: got %q, want %q", fqdn, tc.wantFQDN)
			}
			if apiPath != tc.wantAPIPath {
				t.Errorf("apiPath: got %q, want %q", apiPath, tc.wantAPIPath)
			}
		})
	}
}

func TestCloudProvider(t *testing.T) {
	tests := []struct {
		fqdn string
		want string
	}{
		{"secretsmanager.us-east-1.amazonaws.com", "aws"},
		{"ssm.eu-west-1.amazonaws.com", "aws"},
		{"secretmanager.googleapis.com", "gcp"},
		{"storage.googleapis.com", "gcp"},
		{"myvault.vault.azure.net", "azure"},
		{"mystore.azconfig.io", "azure"},
		{"management.azure.com", "azure"},
		{"example.com", ""},
		{"github.com", ""},
	}
	for _, tc := range tests {
		got := cloudProvider(tc.fqdn)
		if got != tc.want {
			t.Errorf("cloudProvider(%q) = %q, want %q", tc.fqdn, got, tc.want)
		}
	}
}

func TestIsAllowedDomain(t *testing.T) {
	tests := []struct {
		fqdn     string
		patterns []string
		want     bool
	}{
		{"secretsmanager.us-east-1.amazonaws.com", []string{"*.amazonaws.com"}, true},
		{"secretmanager.googleapis.com", []string{"*.amazonaws.com"}, false},
		{"secretmanager.googleapis.com", []string{"*.amazonaws.com", "*.googleapis.com"}, true},
		{"example.com", []string{}, true}, // empty = allow all
		{"myvault.vault.azure.net", []string{"*.vault.azure.net"}, true},
		{"myvault.vault.azure.net", []string{"*.azure.net"}, true},
	}
	for _, tc := range tests {
		got := isAllowedDomain(tc.fqdn, tc.patterns)
		if got != tc.want {
			t.Errorf("isAllowedDomain(%q, %v) = %v, want %v", tc.fqdn, tc.patterns, got, tc.want)
		}
	}
}

func TestParseAWSService(t *testing.T) {
	tests := []struct {
		fqdn        string
		wantService string
		wantRegion  string
		wantErr     bool
	}{
		{"secretsmanager.us-east-1.amazonaws.com", "secretsmanager", "us-east-1", false},
		{"ssm.eu-west-1.amazonaws.com", "ssm", "eu-west-1", false},
		{"kms.ap-southeast-1.amazonaws.com", "kms", "ap-southeast-1", false},
		{"badformat.amazonaws.com", "", "", true},
	}
	for _, tc := range tests {
		svc, region, err := parseAWSService(tc.fqdn)
		if tc.wantErr {
			if err == nil {
				t.Errorf("expected error for %q, got nil", tc.fqdn)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.fqdn, err)
		}
		if svc != tc.wantService {
			t.Errorf("service: got %q, want %q", svc, tc.wantService)
		}
		if region != tc.wantRegion {
			t.Errorf("region: got %q, want %q", region, tc.wantRegion)
		}
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

```bash
go test ./cmd/cloud-proxy/... 2>&1 | tail -5
```

Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Create cmd/cloud-proxy/main.go with parsing functions**

```go
// cmd/cloud-proxy/main.go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type handler struct {
	stripPrefix    string
	allowedDomains []string

	gcpTS oauth2.TokenSource
	gcpMu sync.Mutex
}

func main() {
	stripPrefix := os.Getenv("STRIP_PREFIX")
	if stripPrefix == "" {
		slog.Error("STRIP_PREFIX env var is required")
		os.Exit(1)
	}
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8080"
	}
	var allowed []string
	if raw := os.Getenv("ALLOWED_DOMAINS"); raw != "" {
		for _, d := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(d); t != "" {
				allowed = append(allowed, t)
			}
		}
	}

	h := &handler{stripPrefix: stripPrefix, allowedDomains: allowed}
	addr := ":" + port
	slog.Info("cloud-proxy starting", "addr", addr, "prefix", stripPrefix)
	if err := http.ListenAndServe(addr, h); err != nil {
		slog.Error("cloud-proxy exited", "error", err)
		os.Exit(1)
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cloudFQDN, apiPath, err := parseCloudRequest(r.URL.Path, h.stripPrefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !isAllowedDomain(cloudFQDN, h.allowedDomains) {
		http.Error(w, "domain not in allowedDomains", http.StatusForbidden)
		return
	}
	provider := cloudProvider(cloudFQDN)
	if provider == "" {
		http.Error(w, "unsupported cloud domain: "+cloudFQDN, http.StatusBadRequest)
		return
	}

	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}
	}

	upstreamURL := &url.URL{
		Scheme:   "https",
		Host:     cloudFQDN,
		Path:     apiPath,
		RawQuery: r.URL.RawQuery,
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	for k, vs := range r.Header {
		if strings.EqualFold(k, "host") || strings.EqualFold(k, "authorization") {
			continue
		}
		for _, v := range vs {
			outReq.Header.Add(k, v)
		}
	}
	outReq.Host = cloudFQDN
	outReq.ContentLength = int64(len(bodyBytes))

	switch provider {
	case "aws":
		if err := h.signAWS(r.Context(), outReq, bodyBytes, cloudFQDN); err != nil {
			writeJSONError(w, http.StatusBadGateway, "AWS auth error: "+err.Error())
			return
		}
	case "gcp":
		if err := h.signGCP(r.Context(), outReq); err != nil {
			writeJSONError(w, http.StatusBadGateway, "GCP auth error: "+err.Error())
			return
		}
	case "azure":
		if err := signAzure(r.Context(), outReq, cloudFQDN); err != nil {
			writeJSONError(w, http.StatusBadGateway, "Azure auth error: "+err.Error())
			return
		}
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(outReq)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "upstream error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.Error("copy response body", "error", err)
	}
}

func (h *handler) signAWS(ctx context.Context, req *http.Request, body []byte, fqdn string) error {
	service, region, err := parseAWSService(fqdn)
	if err != nil {
		return err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("AWS credentials: %w", err)
	}
	hash := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(hash[:])
	signer := v4.NewSigner()
	return signer.SignHTTP(ctx, creds, req, bodyHash, service, region, time.Now())
}

func (h *handler) signGCP(ctx context.Context, req *http.Request) error {
	h.gcpMu.Lock()
	defer h.gcpMu.Unlock()
	if h.gcpTS == nil {
		ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return fmt.Errorf("GCP token source: %w", err)
		}
		h.gcpTS = ts
	}
	token, err := h.gcpTS.Token()
	if err != nil {
		return fmt.Errorf("GCP token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return nil
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// parseCloudRequest strips the prefix from path and returns the cloud FQDN and API path.
// path:   /spoke1/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp
// prefix: /spoke1/cloud
// →       fqdn=secretsmanager.us-east-1.amazonaws.com  apiPath=/v1/secret/myapp
func parseCloudRequest(path, prefix string) (cloudFQDN, apiPath string, err error) {
	if !strings.HasPrefix(path, prefix) {
		return "", "", fmt.Errorf("path %q does not start with prefix %q", path, prefix)
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return "", "", fmt.Errorf("empty path after prefix")
	}
	slash := strings.Index(rest, "/")
	if slash == -1 {
		return rest, "/", nil
	}
	return rest[:slash], rest[slash:], nil
}

// cloudProvider returns "aws", "gcp", "azure", or "" for unrecognised domains.
func cloudProvider(fqdn string) string {
	switch {
	case strings.HasSuffix(fqdn, ".amazonaws.com"):
		return "aws"
	case strings.HasSuffix(fqdn, ".googleapis.com"):
		return "gcp"
	case strings.HasSuffix(fqdn, ".azure.net"),
		strings.HasSuffix(fqdn, ".azure.com"),
		strings.HasSuffix(fqdn, ".azconfig.io"):
		return "azure"
	default:
		return ""
	}
}

// isAllowedDomain reports whether fqdn matches any pattern in patterns.
// An empty patterns slice allows all domains.
func isAllowedDomain(fqdn string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if matchDomain(fqdn, p) {
			return true
		}
	}
	return false
}

// matchDomain matches fqdn against pattern, supporting leading "*." wildcards.
func matchDomain(fqdn, pattern string) bool {
	if pattern == fqdn {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(fqdn, pattern[1:])
	}
	return false
}

// parseAWSService extracts the service name and region from an AWS FQDN.
// e.g. secretsmanager.us-east-1.amazonaws.com → "secretsmanager", "us-east-1"
func parseAWSService(fqdn string) (service, region string, err error) {
	trimmed := strings.TrimSuffix(fqdn, ".amazonaws.com")
	parts := strings.SplitN(trimmed, ".", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", fmt.Errorf("unexpected AWS FQDN format: %s", fqdn)
	}
	return parts[0], parts[1], nil
}
```

Note: `signAzure` is referenced but not yet implemented — add a placeholder stub so the file compiles:

```go
func signAzure(_ context.Context, _ *http.Request, _ string) error {
	return fmt.Errorf("Azure support not yet implemented")
}
```

- [ ] **Step 4: Add Azure SDK dependency**

```bash
go get github.com/Azure/azure-sdk-for-go/sdk/azidentity@latest
go get github.com/Azure/azure-sdk-for-go/sdk/azcore@latest
go mod tidy
```

- [ ] **Step 5: Replace signAzure stub with real implementation**

Replace the stub with:

```go
import (
	// add to import block:
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

func signAzure(ctx context.Context, req *http.Request, cloudFQDN string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("Azure credential: %w", err)
	}
	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://" + cloudFQDN + "/.default"},
	})
	if err != nil {
		return fmt.Errorf("Azure token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	return nil
}
```

- [ ] **Step 6: Run the tests**

```bash
go test ./cmd/cloud-proxy/... -v 2>&1 | tail -20
```

Expected: all four `Test*` functions PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/cloud-proxy/ go.mod go.sum
git commit -m "feat(cloud-proxy): add binary with path parsing, provider detection, AWS/GCP/Azure signing"
```

---

## Task 4: CloudRestApiProxy controller

**Files:**
- Create: `internal/controller/cloudrestapiproxy_controller.go`
- Create: `internal/controller/cloudrestapiproxy_controller_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/controller/cloudrestapiproxy_controller_test.go`:

```go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

var _ = Describe("CloudRestApiProxy Controller", func() {
	const (
		crName     = "spoke1-cloud"
		crClient   = "spoke1-client"
		crServer   = "main-hub"
		ns         = "default"
		vhostPort  = int32(8080)
	)
	ctx := context.Background()
	nsName := func(name string) types.NamespacedName {
		return types.NamespacedName{Namespace: ns, Name: name}
	}
	reconciler := func() *CloudRestApiProxyReconciler {
		return &CloudRestApiProxyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}
	reconcile_ := func() {
		By("Reconciling CloudRestApiProxy")
		result, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(crName)})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reconcile error: %v", err))
		By(fmt.Sprintf("result: %+v", result))
	}

	BeforeEach(func() {
		// NexusServer with vhostHTTPPort set.
		srv := &tunnelv1alpha1.NexusServer{
			ObjectMeta: metav1.ObjectMeta{Name: crServer, Namespace: ns},
			Spec:       tunnelv1alpha1.NexusServerSpec{BindPort: 7000, VhostHTTPPort: vhostPort},
		}
		if err := k8sClient.Get(ctx, nsName(crServer), srv); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, srv)).To(Succeed())
		}
		// NexusClient referencing the server.
		spoke := &tunnelv1alpha1.NexusClient{
			ObjectMeta: metav1.ObjectMeta{Name: crClient, Namespace: ns},
			Spec: tunnelv1alpha1.NexusClientSpec{
				ServerRef: tunnelv1alpha1.ServerRef{Name: crServer},
				Image:     tunnelv1alpha1.ImageSpec{Repository: "registry.example.com/crd-tunnel", Tag: "1.0.0"},
			},
		}
		if err := k8sClient.Get(ctx, nsName(crClient), spoke); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, spoke)).To(Succeed())
		}
		// CloudRestApiProxy CR.
		cr := &tunnelv1alpha1.CloudRestApiProxy{
			ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: ns},
			Spec: tunnelv1alpha1.CloudRestApiProxySpec{
				ClientRef:          tunnelv1alpha1.LocalObjectRef{Name: crClient},
				ServiceAccountName: "cloud-proxy-sa",
				AllowedDomains:     []string{"*.amazonaws.com"},
			},
		}
		if err := k8sClient.Get(ctx, nsName(crName), cr); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		}
	})

	It("adds finalizer on first reconcile", func() {
		reconcile_()
		var cr tunnelv1alpha1.CloudRestApiProxy
		Expect(k8sClient.Get(ctx, nsName(crName), &cr)).To(Succeed())
		Expect(cr.Finalizers).To(ContainElement(tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer))
	})

	It("creates cloud-proxy Deployment with correct env vars", func() {
		reconcile_()
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(crName+"-cloud-proxy"), &dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal("cloud-proxy-sa"))
		envVars := dep.Spec.Template.Spec.Containers[0].Env
		envMap := map[string]string{}
		for _, e := range envVars {
			envMap[e.Name] = e.Value
		}
		Expect(envMap["STRIP_PREFIX"]).To(Equal("/"+crClient+"/cloud"))
		Expect(envMap["ALLOWED_DOMAINS"]).To(Equal("*.amazonaws.com"))
	})

	It("creates spoke-side ClusterIP Service", func() {
		reconcile_()
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, nsName(crName+"-cloud-proxy-svc"), &svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))
	})

	It("creates owned HTTPProxy with correct customDomains and locations", func() {
		reconcile_()
		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(crName+"-tunnel"), &hp)).To(Succeed())
		Expect(hp.Spec.ClientRef.Name).To(Equal(crClient))
		Expect(hp.Spec.CustomDomains).To(ContainElement(
			fmt.Sprintf("%s-gateway.%s.svc.cluster.local", crServer, ns),
		))
		Expect(hp.Spec.ExtraConfig).To(ContainSubstring("/"+crClient+"/cloud"))
	})

	It("removes owned HTTPProxy on deletion", func() {
		reconcile_()
		var cr tunnelv1alpha1.CloudRestApiProxy
		Expect(k8sClient.Get(ctx, nsName(crName), &cr)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &cr)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(crName)})
		Expect(err).NotTo(HaveOccurred())

		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(crName+"-tunnel"), &hp)).To(
			Satisfy(errors.IsNotFound))
	})
})
```

- [ ] **Step 2: Run to confirm it fails**

```bash
go test ./internal/controller/... -run "TestControllers/CloudRestApiProxy" -v 2>&1 | tail -10
```

Expected: FAIL — `CloudRestApiProxyReconciler` undefined.

- [ ] **Step 3: Create the controller**

Create `internal/controller/cloudrestapiproxy_controller.go`:

```go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/cloud-nexus-operator/api/v1alpha1"
)

const cloudRestApiProxyRequeueDelay = 15 * time.Second

// CloudRestApiProxyReconciler reconciles a CloudRestApiProxy object.
type CloudRestApiProxyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=cloudrestapiproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=cloudrestapiproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=cloudrestapiproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=httpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=nexusclients,verbs=get;list;watch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=nexusservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *CloudRestApiProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cr tunnelv1alpha1.CloudRestApiProxy
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !cr.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer) {
			if err := r.deleteOwnedResources(ctx, &cr); err != nil {
				log.Error(err, "cleanup failed")
				return ctrl.Result{RequeueAfter: cloudRestApiProxyRequeueDelay}, nil
			}
			controllerutil.RemoveFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer)
			return ctrl.Result{}, r.Update(ctx, &cr)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer) {
		controllerutil.AddFinalizer(&cr, tunnelv1alpha1.CloudRestApiProxyCleanupFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve NexusClient.
	var spoke tunnelv1alpha1.NexusClient
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cr.Namespace,
		Name:      cr.Spec.ClientRef.Name,
	}, &spoke); err != nil {
		if errors.IsNotFound(err) {
			log.Info("NexusClient not found, requeuing", "client", cr.Spec.ClientRef.Name)
			return ctrl.Result{RequeueAfter: cloudRestApiProxyRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve NexusServer for gateway FQDN and vhostHTTPPort.
	var server tunnelv1alpha1.NexusServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cr.Namespace,
		Name:      spoke.Spec.ServerRef.Name,
	}, &server); err != nil {
		if errors.IsNotFound(err) {
			log.Info("NexusServer not found, requeuing", "server", spoke.Spec.ServerRef.Name)
			return ctrl.Result{RequeueAfter: cloudRestApiProxyRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	proxyPort := cr.Spec.ProxyPort
	if proxyPort == 0 {
		proxyPort = 8080
	}
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	if cr.Spec.Image != nil && cr.Spec.Image.Repository != "" {
		image = cr.Spec.Image.Repository + ":" + cr.Spec.Image.Tag
	}
	spokeName := spoke.Name
	gatewayFQDN := fmt.Sprintf("%s-gateway.%s.svc.cluster.local", server.Name, cr.Namespace)

	if err := r.reconcileDeployment(ctx, &cr, image, proxyPort, spokeName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileService(ctx, &cr, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileHTTPProxy(ctx, &cr, proxyPort, spokeName, gatewayFQDN); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.syncStatus(ctx, &cr, server.Spec.VhostHTTPPort, spokeName, gatewayFQDN)
}

func (r *CloudRestApiProxyReconciler) reconcileDeployment(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, image string, proxyPort int32, spokeName string) error {
	replicas := int32(1)
	stripPrefix := "/" + spokeName + "/cloud"
	allowedDomains := strings.Join(cr.Spec.AllowedDomains, ",")

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-cloud-proxy",
			Namespace: cr.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetControllerReference(cr, dep, r.Scheme); err != nil {
			return err
		}
		labels := map[string]string{"app": "cloud-proxy", "cloudrestapiproxy": cr.Name}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				ServiceAccountName: cr.Spec.ServiceAccountName,
				Containers: []corev1.Container{
					{
						Name:  "cloud-proxy",
						Image: image,
						Command: []string{"/cloud-proxy"},
						Ports: []corev1.ContainerPort{
							{Name: "proxy", ContainerPort: proxyPort, Protocol: corev1.ProtocolTCP},
						},
						Env: []corev1.EnvVar{
							{Name: "STRIP_PREFIX", Value: stripPrefix},
							{Name: "PROXY_PORT", Value: fmt.Sprintf("%d", proxyPort)},
							{Name: "ALLOWED_DOMAINS", Value: allowedDomains},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}

func (r *CloudRestApiProxyReconciler) reconcileService(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, proxyPort int32) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-cloud-proxy-svc",
			Namespace: cr.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(cr, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"cloudrestapiproxy": cr.Name}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "proxy",
				Port:       proxyPort,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(proxyPort),
			},
		}
		return nil
	})
	return err
}

func (r *CloudRestApiProxyReconciler) reconcileHTTPProxy(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, proxyPort int32, spokeName, gatewayFQDN string) error {
	localIP := fmt.Sprintf("%s-cloud-proxy-svc.%s.svc.cluster.local", cr.Name, cr.Namespace)
	location := "/" + spokeName + "/cloud"

	hp := &tunnelv1alpha1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-tunnel",
			Namespace: cr.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, hp, func() error {
		if err := controllerutil.SetControllerReference(cr, hp, r.Scheme); err != nil {
			return err
		}
		if hp.Labels == nil {
			hp.Labels = make(map[string]string)
		}
		hp.Labels["cloudrestapiproxy"] = cr.Name
		hp.Spec.ClientRef = tunnelv1alpha1.LocalObjectRef{Name: cr.Spec.ClientRef.Name}
		hp.Spec.LocalIP = localIP
		hp.Spec.LocalPort = proxyPort
		hp.Spec.CustomDomains = []string{gatewayFQDN}
		hp.Spec.ExtraConfig = fmt.Sprintf("locations = [%q]", location)
		return nil
	})
	return err
}

func (r *CloudRestApiProxyReconciler) deleteOwnedResources(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy) error {
	hp := &tunnelv1alpha1.HTTPProxy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-tunnel"}, hp); err == nil {
		if err := r.Delete(ctx, hp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete HTTPProxy: %w", err)
		}
	}
	return nil
}

func (r *CloudRestApiProxyReconciler) syncStatus(ctx context.Context, cr *tunnelv1alpha1.CloudRestApiProxy, vhostPort int32, spokeName, gatewayFQDN string) error {
	var dep appsv1.Deployment
	proxyReady := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-cloud-proxy"}, &dep); err == nil {
		proxyReady = dep.Status.ReadyReplicas > 0
	}

	var hp tunnelv1alpha1.HTTPProxy
	tunnelConnected := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name + "-tunnel"}, &hp); err == nil {
		tunnelConnected = hp.Status.Phase == "Running"
	}

	updated := cr.DeepCopy()
	now := metav1.Now()

	setCondition := func(condType string, status metav1.ConditionStatus, reason, msg string) {
		cond := metav1.Condition{
			Type:               condType,
			Status:             status,
			Reason:             reason,
			Message:            msg,
			LastTransitionTime: now,
		}
		for i, c := range updated.Status.Conditions {
			if c.Type == condType {
				if c.Status == status {
					return
				}
				updated.Status.Conditions[i] = cond
				return
			}
		}
		updated.Status.Conditions = append(updated.Status.Conditions, cond)
	}

	if proxyReady {
		setCondition("ProxyReady", metav1.ConditionTrue, "DeploymentReady", "")
	} else {
		setCondition("ProxyReady", metav1.ConditionFalse, "DeploymentNotReady", "waiting for cloud-proxy pod")
	}
	if tunnelConnected {
		setCondition("TunnelConnected", metav1.ConditionTrue, "HTTPProxyRunning", "")
		updated.Status.GatewayURL = fmt.Sprintf("http://%s:%d/%s/cloud", gatewayFQDN, vhostPort, spokeName)
	} else {
		setCondition("TunnelConnected", metav1.ConditionFalse, "HTTPProxyNotRunning", "waiting for FRP tunnel")
		updated.Status.GatewayURL = ""
	}

	if proxyReady && tunnelConnected {
		updated.Status.Phase = "Running"
	} else {
		updated.Status.Phase = "Pending"
	}

	return r.Status().Update(ctx, updated)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudRestApiProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.CloudRestApiProxy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(
			&tunnelv1alpha1.HTTPProxy{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				crName, ok := obj.GetLabels()["cloudrestapiproxy"]
				if !ok || crName == "" {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      crName,
					},
				}}
			}),
		).
		Named("cloudrestapiproxy").
		Complete(r)
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/controller/... -v 2>&1 | tail -30
```

Expected: all CloudRestApiProxy tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/cloudrestapiproxy_controller.go \
        internal/controller/cloudrestapiproxy_controller_test.go
git commit -m "feat(controller): add CloudRestApiProxy reconciler"
```

---

## Task 5: Register CloudRestApiProxy controller in cmd/main.go

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add the registration block**

In `cmd/main.go`, after the RegistryProxy registration block (after line `}).SetupWithManager(mgr); err != nil {`), add:

```go
	if err := (&controller.CloudRestApiProxyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "CloudRestApiProxy")
		os.Exit(1)
	}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/...
```

Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add cmd/main.go
git commit -m "feat(main): register CloudRestApiProxy controller"
```

---

## Task 6: Migrate RegistryProxy controller from TCPProxy to HTTPProxy

**Files:**
- Modify: `internal/controller/registryproxy_controller.go`
- Modify: `internal/controller/registryproxy_controller_test.go`

- [ ] **Step 1: Update the test — replace TCPProxy assertions with HTTPProxy**

In `internal/controller/registryproxy_controller_test.go`, update the `"removes owned TCPProxy on deletion"` test to:

```go
	It("removes owned HTTPProxy on deletion", func() {
		reconcile_() // create resources

		var rp tunnelv1alpha1.RegistryProxy
		Expect(k8sClient.Get(ctx, nsName(rpName), &rp)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &rp)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred())

		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(rpName+"-registry-proxy"), &hp)).To(
			Satisfy(errors.IsNotFound))
	})
```

Add a new test verifying HTTPProxy is created with correct fields:

```go
	It("creates owned HTTPProxy with correct customDomains and location", func() {
		reconcile_()
		var hp tunnelv1alpha1.HTTPProxy
		Expect(k8sClient.Get(ctx, nsName(rpName+"-registry-proxy"), &hp)).To(Succeed())
		Expect(hp.Spec.ClientRef.Name).To(Equal(clientName))
		Expect(hp.Spec.CustomDomains).To(ContainElement(
			fmt.Sprintf("%s-gateway.%s.svc.cluster.local", serverName, ns),
		))
		Expect(hp.Spec.ExtraConfig).To(ContainSubstring("/"+clientName+"/registry"))
	})

	It("creates Deployment with STRIP_PREFIX env var", func() {
		reconcile_()
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy"), &dep)).To(Succeed())
		envMap := map[string]string{}
		for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
			envMap[e.Name] = e.Value
		}
		Expect(envMap["STRIP_PREFIX"]).To(Equal("/"+clientName+"/registry"))
	})
```

- [ ] **Step 2: Run to confirm new tests fail**

```bash
go test ./internal/controller/... -run "TestControllers/RegistryProxy" -v 2>&1 | tail -20
```

Expected: new assertions fail — no HTTPProxy, no STRIP_PREFIX env.

- [ ] **Step 3: Replace reconcileTCPProxy with reconcileHTTPProxy in registryproxy_controller.go**

Replace the entire `reconcileTCPProxy` function with:

```go
func (r *RegistryProxyReconciler) reconcileHTTPProxy(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, proxyPort int32, spokeName, gatewayFQDN string) error {
	localIP := fmt.Sprintf("%s-proxy-svc.%s.svc.cluster.local", rp.Name, rp.Namespace)
	location := "/" + spokeName + "/registry"

	hp := &tunnelv1alpha1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-registry-proxy",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, hp, func() error {
		if err := controllerutil.SetControllerReference(rp, hp, r.Scheme); err != nil {
			return err
		}
		if hp.Labels == nil {
			hp.Labels = make(map[string]string)
		}
		hp.Labels["registryproxy"] = rp.Name
		hp.Spec.ClientRef = tunnelv1alpha1.LocalObjectRef{Name: rp.Spec.ClientRef.Name}
		hp.Spec.LocalIP = localIP
		hp.Spec.LocalPort = proxyPort
		hp.Spec.CustomDomains = []string{gatewayFQDN}
		hp.Spec.ExtraConfig = fmt.Sprintf("locations = [%q]", location)
		return nil
	})
	return err
}
```

- [ ] **Step 4: Update Reconcile to resolve NexusServer and call reconcileHTTPProxy**

Replace the section in `Reconcile` that sets `frpServerName` and calls `reconcileTCPProxy` with:

```go
	// Resolve NexusServer for gateway FQDN.
	var server tunnelv1alpha1.NexusServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      spoke.Spec.ServerRef.Name,
	}, &server); err != nil {
		if errors.IsNotFound(err) {
			log.Info("NexusServer not found, requeuing", "server", spoke.Spec.ServerRef.Name)
			return ctrl.Result{RequeueAfter: registryProxyRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	gatewayFQDN := fmt.Sprintf("%s-gateway.%s.svc.cluster.local", server.Name, rp.Namespace)
	spokeName := spoke.Name

	if err := r.reconcileProxyDeployment(ctx, &rp, image, proxyPort, spokeName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileProxyService(ctx, &rp, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileHTTPProxy(ctx, &rp, proxyPort, spokeName, gatewayFQDN); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.syncStatus(ctx, &rp, server.Spec.VhostHTTPPort, spokeName, gatewayFQDN)
```

- [ ] **Step 5: Add spokeName and STRIP_PREFIX to reconcileProxyDeployment**

Change the signature to `reconcileProxyDeployment(ctx, rp, image, proxyPort, spokeName)` and add the `STRIP_PREFIX` env to the container:

```go
func (r *RegistryProxyReconciler) reconcileProxyDeployment(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, image string, proxyPort int32, spokeName string) error {
	replicas := int32(1)
	stripPrefix := "/" + spokeName + "/registry"
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-proxy",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetControllerReference(rp, dep, r.Scheme); err != nil {
			return err
		}
		labels := map[string]string{"app": "registry-proxy", "registryproxy": rp.Name}
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				ServiceAccountName: rp.Spec.ServiceAccountName,
				Containers: []corev1.Container{
					{
						Name:    "registry-proxy",
						Image:   image,
						Command: []string{"/registry-proxy", fmt.Sprintf("--port=%d", proxyPort)},
						Ports: []corev1.ContainerPort{
							{Name: "proxy", ContainerPort: proxyPort, Protocol: corev1.ProtocolTCP},
						},
						Env: []corev1.EnvVar{
							{Name: "STRIP_PREFIX", Value: stripPrefix},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}
```

- [ ] **Step 6: Update deleteOwnedResources to delete HTTPProxy**

Replace the TCPProxy deletion with:

```go
func (r *RegistryProxyReconciler) deleteOwnedResources(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy) error {
	hp := &tunnelv1alpha1.HTTPProxy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, hp); err == nil {
		if err := r.Delete(ctx, hp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete HTTPProxy: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 7: Update syncStatus to use HTTPProxy phase and gateway URL**

Replace the `syncStatus` function:

```go
func (r *RegistryProxyReconciler) syncStatus(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, vhostPort int32, spokeName, gatewayFQDN string) error {
	var dep appsv1.Deployment
	proxyReady := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-proxy"}, &dep); err == nil {
		proxyReady = dep.Status.ReadyReplicas > 0
	}

	var hp tunnelv1alpha1.HTTPProxy
	tunnelConnected := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, &hp); err == nil {
		tunnelConnected = hp.Status.Phase == "Running"
	}

	updated := rp.DeepCopy()
	now := metav1.Now()

	setCondition := func(condType string, status metav1.ConditionStatus, reason, msg string) {
		cond := metav1.Condition{
			Type:               condType,
			Status:             status,
			Reason:             reason,
			Message:            msg,
			LastTransitionTime: now,
		}
		for i, c := range updated.Status.Conditions {
			if c.Type == condType {
				if c.Status == status {
					return
				}
				updated.Status.Conditions[i] = cond
				return
			}
		}
		updated.Status.Conditions = append(updated.Status.Conditions, cond)
	}

	if proxyReady {
		setCondition("ProxyReady", metav1.ConditionTrue, "DeploymentReady", "")
	} else {
		setCondition("ProxyReady", metav1.ConditionFalse, "DeploymentNotReady", "waiting for registry-proxy pod")
	}
	if tunnelConnected {
		setCondition("TunnelConnected", metav1.ConditionTrue, "HTTPProxyRunning", "")
		updated.Status.HubService = fmt.Sprintf("http://%s:%d/%s/registry", gatewayFQDN, vhostPort, spokeName)
	} else {
		setCondition("TunnelConnected", metav1.ConditionFalse, "HTTPProxyNotRunning", "waiting for FRP tunnel")
		updated.Status.HubService = ""
	}

	if proxyReady && tunnelConnected {
		updated.Status.Phase = "Running"
	} else {
		updated.Status.Phase = "Pending"
	}

	return r.Status().Update(ctx, updated)
}
```

- [ ] **Step 8: Update SetupWithManager to watch HTTPProxy instead of TCPProxy**

Replace the `Watches` call in `SetupWithManager`:

```go
		Watches(
			&tunnelv1alpha1.HTTPProxy{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				rpName, ok := obj.GetLabels()["registryproxy"]
				if !ok || rpName == "" {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      rpName,
					},
				}}
			}),
		).
```

Also add the RBAC annotation for nexusservers and change tcpproxies to httpproxies:

```go
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=httpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=nexusservers,verbs=get;list;watch
```

(Remove the old `tcpproxies` rbac line.)

- [ ] **Step 9: Run the tests**

```bash
go test ./internal/controller/... -v 2>&1 | tail -30
```

Expected: all RegistryProxy tests PASS with updated assertions.

- [ ] **Step 10: Commit**

```bash
git add internal/controller/registryproxy_controller.go \
        internal/controller/registryproxy_controller_test.go
git commit -m "feat(registryproxy): migrate from TCPProxy to HTTPProxy with STRIP_PREFIX and gateway routing"
```

---

## Task 7: Add STRIP_PREFIX support to registry-proxy binary

**Files:**
- Modify: `cmd/registry-proxy/main.go`
- Modify: `cmd/registry-proxy/main_test.go`

- [ ] **Step 1: Write the failing test**

Add to `cmd/registry-proxy/main_test.go`:

```go
func TestStripPrefix_Applied(t *testing.T) {
	// With STRIP_PREFIX=/spoke1/registry, path /spoke1/registry/v2/host.io/img/manifests/tag
	// should be routed as if path was /v2/host.io/img/manifests/tag.
	// We test parseUpstream on the stripped path.
	raw := "/spoke1/registry/v2/asia-southeast3-docker.pkg.dev/proj/img/manifests/latest"
	prefix := "/spoke1/registry"
	stripped := strings.TrimPrefix(raw, prefix)
	// stripped = /v2/asia-southeast3-docker.pkg.dev/proj/img/manifests/latest
	host, rest, ok := parseUpstream(stripped)
	if !ok {
		t.Fatal("expected ok=true after stripping prefix")
	}
	if host != "asia-southeast3-docker.pkg.dev" {
		t.Errorf("host: got %q, want %q", host, "asia-southeast3-docker.pkg.dev")
	}
	if rest != "proj/img/manifests/latest" {
		t.Errorf("rest: got %q, want %q", rest, "proj/img/manifests/latest")
	}
}

func TestStripPrefix_EmptyPrefix(t *testing.T) {
	// When STRIP_PREFIX is empty, /v2/host.io/img/... works unchanged.
	raw := "/v2/asia-southeast3-docker.pkg.dev/proj/img/manifests/latest"
	host, _, ok := parseUpstream(raw)
	if !ok {
		t.Fatal("expected ok=true with no prefix")
	}
	if host != "asia-southeast3-docker.pkg.dev" {
		t.Errorf("host: got %q", host)
	}
}
```

Add `"strings"` to the import if not already present.

- [ ] **Step 2: Run to confirm tests pass already (they test existing parseUpstream, not the new handler)**

```bash
go test ./cmd/registry-proxy/... -v 2>&1
```

Expected: existing tests and new tests PASS (they only call `parseUpstream` which already exists — this verifies the strip-then-parse logic works).

- [ ] **Step 3: Update main() to read STRIP_PREFIX and register handler with stripped path**

In `cmd/registry-proxy/main.go`, change `main()`:

```go
func main() {
	port := flag.Int("port", 5000, "port the registry proxy listens on")
	flag.Parse()

	stripPrefix := os.Getenv("STRIP_PREFIX")

	h := &handler{}
	mux := http.NewServeMux()

	pattern := "/v2/"
	if stripPrefix != "" {
		pattern = stripPrefix + "/v2/"
	}
	mux.Handle(pattern, http.StripPrefix(stripPrefix, http.HandlerFunc(h.handleRequest)))

	addr := fmt.Sprintf(":%d", *port)
	slog.Info("registry-proxy starting", "addr", addr, "stripPrefix", stripPrefix)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("registry-proxy exited", "error", err)
	}
}
```

Add `"os"` to imports.

- [ ] **Step 4: Run full test suite**

```bash
go test ./cmd/registry-proxy/... -v 2>&1
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/registry-proxy/main.go cmd/registry-proxy/main_test.go
git commit -m "feat(registry-proxy): support STRIP_PREFIX env var for unified gateway routing"
```

---

## Task 8: Sample configs

**Files:**
- Create: `config/samples/tunnel_v1alpha1_cloudrestapiproxy.yaml`
- Modify: `config/samples/tunnel_v1alpha1_nexusserver.yaml`
- Modify: `config/samples/kustomization.yaml`

- [ ] **Step 1: Create CloudRestApiProxy sample**

Create `config/samples/tunnel_v1alpha1_cloudrestapiproxy.yaml`:

```yaml
apiVersion: tunnel.io/v1alpha1
kind: CloudRestApiProxy
metadata:
  name: spoke1-cloud
  namespace: default
  labels:
    app.kubernetes.io/name: cloud-nexus-operator
    app.kubernetes.io/managed-by: kustomize
spec:
  clientRef:
    name: spoke1-client         # references an existing NexusClient
  serviceAccountName: cloud-proxy-sa  # SA with IRSA or Workload Identity annotation
  proxyPort: 8080
  allowedDomains:
    - "*.amazonaws.com"
    - "*.googleapis.com"
    - "*.azure.net"
    - "*.azconfig.io"
  # image:
  #   repository: your-registry/cloud-proxy
  #   tag: "1.0.0"
```

- [ ] **Step 2: Update NexusServer sample to include vhostHTTPPort and gatewayServiceType**

Open `config/samples/tunnel_v1alpha1_nexusserver.yaml` and add `vhostHTTPPort` and `gatewayServiceType` fields to spec:

```yaml
spec:
  bindPort: 7000
  vhostHTTPPort: 8080          # enables FRP HTTP routing and creates {name}-gateway Service
  gatewayServiceType: ClusterIP  # ClusterIP (hub-internal) or LoadBalancer
```

- [ ] **Step 3: Add CloudRestApiProxy to kustomization**

In `config/samples/kustomization.yaml`, add:

```yaml
  - tunnel_v1alpha1_cloudrestapiproxy.yaml
```

- [ ] **Step 4: Run full test suite one final time**

```bash
go test ./... 2>&1 | tail -20
```

Expected: all packages PASS.

- [ ] **Step 5: Final commit**

```bash
git add config/samples/tunnel_v1alpha1_cloudrestapiproxy.yaml \
        config/samples/tunnel_v1alpha1_nexusserver.yaml \
        config/samples/kustomization.yaml
git commit -m "docs(samples): add CloudRestApiProxy sample and update NexusServer sample with gateway fields"
```

---

## Self-Review Checklist

- [x] Spec §1 NexusServer gateway Service → Task 2
- [x] Spec §2 CloudRestApiProxy CRD → Task 1
- [x] Spec §3 CloudRestApiProxy controller (Deployment + HTTPProxy + status + finalizer) → Task 4
- [x] Spec §4 cloud-proxy binary (AWS/GCP/Azure, AllowedDomains, path parsing) → Task 3
- [x] Spec §5 RegistryProxy migration (TCPProxy→HTTPProxy, STRIP_PREFIX) → Tasks 6 & 7
- [x] Gateway Service FQDN resolved via clientRef→NexusClient→serverRef→NexusServer → Tasks 4 & 6
- [x] `STRIP_PREFIX` env injected by both controllers; binary reads it → Tasks 3, 6, 7
- [x] status.GatewayURL populated once HTTPProxy.status.phase=Running → Tasks 4 & 6
- [x] Azure SDK dependency added → Task 3
- [x] Breaking change (RegistryProxy hubService URL format) noted in Task 6
- [x] All controller tests use Ginkgo/Gomega + envtest (matching suite_test.go pattern)
- [x] All binary tests use standard `testing` table tests (matching existing main_test.go pattern)
