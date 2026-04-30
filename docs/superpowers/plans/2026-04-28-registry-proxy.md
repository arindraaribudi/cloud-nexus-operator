# Registry Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `RegistryProxy` CRD that deploys a cloud-auth proxy on the spoke, tunnels it via FRP, and automatically creates a hub-side Service so hub CI/CD can `docker pull reg-1-docker/<upstream>/<image>:tag` without credentials.

**Architecture:** `RegistryProxy` controller deploys a `/registry-proxy` binary (bundled in the crd-tunnel image) that strips the upstream hostname from the image path and injects cloud credentials (GCP ADC / AWS IRSA) before forwarding. The FRP tunnel exposes it at a remote port on frps; frps fires an `httpPlugin` webhook to the operator which creates a ClusterIP Service named `<name>-docker` on the hub side.

**Tech Stack:** Go 1.25, controller-runtime v0.23.3, `golang.org/x/oauth2/google`, `github.com/aws/aws-sdk-go-v2/{config,service/ecr}`, `net/http/httputil`, FRP v0.61.0 httpPlugin protocol.

---

## File Map

| File | Action |
|---|---|
| `api/v1alpha1/registryproxy_types.go` | Create — RegistryProxy CRD types + finalizer const |
| `api/v1alpha1/frpserver_types.go` | Modify — add `EnableOperatorPlugin`, `OperatorWebhookAddr` |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerate via `make generate` |
| `config/crd/bases/` | Regenerate via `make manifests` |
| `internal/config/server.go` | Modify — add httpPlugin to frps template; add `namespace` param to `RenderFrpsToml` |
| `internal/config/server_test.go` | Modify — update all `RenderFrpsToml` call sites to pass `""` as namespace |
| `internal/controller/frpserver_controller.go` | Modify — pass `server.Namespace` to `RenderFrpsToml` |
| `internal/webhook/frps_events.go` | Create — frps plugin HTTP handler + Runnable server |
| `internal/webhook/frps_events_test.go` | Create — handler unit tests with httptest |
| `cmd/registry-proxy/main.go` | Create — registry auth proxy binary |
| `cmd/registry-proxy/main_test.go` | Create — path parsing + auth routing unit tests |
| `internal/controller/registryproxy_controller.go` | Create — RegistryProxy reconciler |
| `internal/controller/registryproxy_controller_test.go` | Create — envtest controller tests |
| `cmd/main.go` | Modify — register RegistryProxy controller + start webhook server |
| `config/default/frps_plugin_service.yaml` | Create — ClusterIP Service for operator webhook port |
| `config/default/kustomization.yaml` | Modify — add `frps_plugin_service.yaml` |
| `config/manager/manager.yaml` | Modify — expose port 9090 on manager container |
| `config/samples/tunnelv1alpha1_registryproxy.yaml` | Create — sample CR |
| `docker/Dockerfile.frp` | Modify — add proxy-builder stage for `/registry-proxy` binary |

---

## Known Limitation

FRP proxy TOML names are generated as `<clientRef.name>-<type>` (existing behavior in `internal/config/client.go:113`). If a FrpClient already has a TCP FrpProxy, adding a RegistryProxy for the same FrpClient creates a second `<client>-tcp` entry which conflicts. In that case, run a dedicated FrpClient per RegistryProxy. Do not change the naming logic in this PR to avoid breaking existing deployments.

---

## Task 1: RegistryProxy CRD types

**Files:**
- Create: `api/v1alpha1/registryproxy_types.go`
- Modify: `api/v1alpha1/frpserver_types.go`

- [ ] **Step 1: Create `api/v1alpha1/registryproxy_types.go`**

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// RegistryProxyCleanupFinalizer is added to RegistryProxy resources.
	// Deletion removes owned FrpProxy/Deployment/Service before the CR is removed.
	RegistryProxyCleanupFinalizer = "tunnel.io/registryproxy-cleanup"
)

// RegistryProxySpec defines the desired state of RegistryProxy.
type RegistryProxySpec struct {
	// ClientRef references the FrpClient (spoke) that owns the tunnel.
	// +required
	ClientRef LocalObjectRef `json:"clientRef"`

	// RemotePort is the port frps will bind on the hub side.
	// Must not conflict with other FrpProxy remotePort values on the same FrpClient.
	// +required
	RemotePort int32 `json:"remotePort"`

	// ServiceAccountName is the Kubernetes SA annotated with WI/IRSA for cloud auth.
	// +required
	ServiceAccountName string `json:"serviceAccountName"`

	// ProxyPort is the port the registry-proxy binary listens on inside the Pod.
	// Default: 5000
	// +optional
	ProxyPort *int32 `json:"proxyPort,omitempty"`

	// Image overrides the proxy container image.
	// Defaults to the image used by the referenced FrpClient.
	// +optional
	Image *ImageSpec `json:"image,omitempty"`
}

// RegistryProxyStatus defines the observed state of RegistryProxy.
type RegistryProxyStatus struct {
	// Phase is the current lifecycle phase: Pending | Running | Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// HubService is the FQDN of the hub-side Service once created.
	// Example: reg-1-docker.tunnels.svc.cluster.local
	// +optional
	HubService string `json:"hubService,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Conditions:
	//   ProxyReady      - registry-proxy Deployment is healthy
	//   TunnelConnected - owned FrpProxy phase is Running (tunnel is active)
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="HubService",type="string",JSONPath=".status.hubService"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RegistryProxy is the Schema for the registryproxies API.
type RegistryProxy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec RegistryProxySpec `json:"spec"`

	// +optional
	Status RegistryProxyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// RegistryProxyList contains a list of RegistryProxy.
type RegistryProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []RegistryProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RegistryProxy{}, &RegistryProxyList{})
}
```

- [ ] **Step 2: Add `EnableOperatorPlugin` and `OperatorWebhookAddr` to `api/v1alpha1/frpserver_types.go`**

Append these two fields at the end of `FrpServerSpec`, just before the closing `}`:

```go
	// EnableOperatorPlugin enables the frps httpPlugin that notifies the
	// operator when proxies connect/disconnect. Required for RegistryProxy.
	// Default: false
	// +optional
	EnableOperatorPlugin bool `json:"enableOperatorPlugin,omitempty"`

	// OperatorWebhookAddr is the address of the operator frps-plugin HTTP server.
	// Only used when EnableOperatorPlugin is true.
	// Example: "crd-tunnel-service-controller-manager-frps-plugin.crd-tunnel-service-system.svc.cluster.local:9090"
	// If empty, defaults to "controller-manager-frps-plugin.<namespace>.svc.cluster.local:9090"
	// where <namespace> is the FrpServer's namespace.
	// +optional
	OperatorWebhookAddr string `json:"operatorWebhookAddr,omitempty"`
```

- [ ] **Step 3: Run `make generate` to regenerate deepcopy**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
make generate
```

Expected: `zz_generated.deepcopy.go` is updated with `DeepCopyInto`/`DeepCopy` methods for `RegistryProxy`, `RegistryProxyList`, `RegistryProxySpec`, `RegistryProxyStatus`. No errors.

- [ ] **Step 4: Run `make manifests` to regenerate CRD YAML**

```bash
make manifests
```

Expected: `config/crd/bases/tunnel.tunnel.io_registryproxies.yaml` is created. No errors.

- [ ] **Step 5: Verify build compiles**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add api/v1alpha1/registryproxy_types.go api/v1alpha1/frpserver_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/
git commit -m "feat: add RegistryProxy CRD types and EnableOperatorPlugin on FrpServer"
```

---

## Task 2: Update frps.toml template to support httpPlugin

**Files:**
- Modify: `internal/config/server.go`
- Modify: `internal/config/server_test.go`
- Modify: `internal/controller/frpserver_controller.go`

- [ ] **Step 1: Write failing test for plugin block**

Add to `internal/config/server_test.go` (all existing tests need namespace `""` added as 4th arg — update those first, then add new test):

Update the **four existing** `config.RenderFrpsToml(spec, ...)` calls from 3 args to 4 args by appending `""`:

```go
// Before:
out, err := config.RenderFrpsToml(spec, "", "")
// After:
out, err := config.RenderFrpsToml(spec, "", "", "")
```

Then append this new test:

```go
func TestRenderFrpsToml_OperatorPlugin(t *testing.T) {
	spec := tunnelv1alpha1.FrpServerSpec{
		BindPort:             7000,
		EnableOperatorPlugin: true,
	}
	out, err := config.RenderFrpsToml(spec, "", "", "mynamespace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `[[httpPlugins]]`) {
		t.Errorf("expected [[httpPlugins]] in output, got:\n%s", out)
	}
	if !strings.Contains(out, `controller-manager-frps-plugin.mynamespace.svc.cluster.local:9090`) {
		t.Errorf("expected webhook addr in output, got:\n%s", out)
	}
	if !strings.Contains(out, `failSafe = true`) {
		t.Errorf("expected failSafe in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_OperatorPluginCustomAddr(t *testing.T) {
	spec := tunnelv1alpha1.FrpServerSpec{
		BindPort:             7000,
		EnableOperatorPlugin: true,
		OperatorWebhookAddr:  "my-custom-svc.ns.svc.cluster.local:9090",
	}
	out, err := config.RenderFrpsToml(spec, "", "", "mynamespace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `my-custom-svc.ns.svc.cluster.local:9090`) {
		t.Errorf("expected custom addr in output, got:\n%s", out)
	}
}

func TestRenderFrpsToml_NoPlugin(t *testing.T) {
	spec := tunnelv1alpha1.FrpServerSpec{BindPort: 7000}
	out, err := config.RenderFrpsToml(spec, "", "", "mynamespace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, `[[httpPlugins]]`) {
		t.Errorf("expected no httpPlugins when disabled, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/config/...
```

Expected: compilation error (`too many arguments in call to config.RenderFrpsToml`) before the new tests even run. This confirms the test is ahead of the implementation.

- [ ] **Step 3: Update `internal/config/server.go`**

Replace the entire file content:

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

// Package config renders TOML configuration files for frps and frpc.
package config

import (
	"bytes"
	"fmt"
	"text/template"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
)

var frpsTemplate = template.Must(template.New("frps").Parse(`bindPort = {{ .BindPort }}
{{- if .VhostHTTPPort }}
vhostHTTPPort = {{ .VhostHTTPPort }}
{{- end }}
{{- if .VhostHTTPSPort }}
vhostHTTPSPort = {{ .VhostHTTPSPort }}
{{- end }}
{{- if .DashboardPort }}
webServer.port = {{ .DashboardPort }}
{{- end }}
{{- if .DashboardUser }}
webServer.user = "{{ .DashboardUser }}"
{{- end }}
{{- if .DashboardPassword }}
webServer.password = "{{ .DashboardPassword }}"
{{- end }}
{{- if .AuthToken }}
auth.method = "token"
auth.token = "{{ .AuthToken }}"
{{- end }}
{{ .ExtraConfig }}
{{- if .EnableOperatorPlugin }}

[[httpPlugins]]
name     = "operator-notifier"
addr     = "{{ .OperatorWebhookAddr }}"
path     = "/frps/events"
ops      = ["NewProxy", "CloseProxy"]
failSafe = true
{{- end }}
`))

type frpsData struct {
	BindPort             int32
	VhostHTTPPort        int32
	VhostHTTPSPort       int32
	DashboardPort        int32
	DashboardUser        string
	DashboardPassword    string
	AuthToken            string
	ExtraConfig          string
	EnableOperatorPlugin bool
	OperatorWebhookAddr  string
}

// RenderFrpsToml renders the frps.toml content from the given spec and resolved secrets.
// namespace is the FrpServer's Kubernetes namespace, used to construct the default
// operator webhook address when spec.OperatorWebhookAddr is empty.
func RenderFrpsToml(spec tunnelv1alpha1.FrpServerSpec, authToken, dashPassword, namespace string) (string, error) {
	webhookAddr := spec.OperatorWebhookAddr
	if webhookAddr == "" && spec.EnableOperatorPlugin {
		webhookAddr = fmt.Sprintf("controller-manager-frps-plugin.%s.svc.cluster.local:9090", namespace)
	}

	data := frpsData{
		BindPort:             spec.BindPort,
		VhostHTTPPort:        spec.VhostHTTPPort,
		VhostHTTPSPort:       spec.VhostHTTPSPort,
		DashboardPort:        spec.DashboardPort,
		DashboardUser:        spec.DashboardUser,
		DashboardPassword:    dashPassword,
		AuthToken:            authToken,
		ExtraConfig:          spec.ExtraConfig,
		EnableOperatorPlugin: spec.EnableOperatorPlugin,
		OperatorWebhookAddr:  webhookAddr,
	}

	if data.BindPort == 0 {
		data.BindPort = 7000
	}

	var buf bytes.Buffer
	if err := frpsTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render frps.toml: %w", err)
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Update call site in `internal/controller/frpserver_controller.go`**

On line 82, change:
```go
toml, err := config.RenderFrpsToml(server.Spec, authToken, dashPassword)
```
to:
```go
toml, err := config.RenderFrpsToml(server.Spec, authToken, dashPassword, server.Namespace)
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
go test ./internal/config/... ./internal/controller/...
```

Expected: `ok  tunnel.io/frp-operator/internal/config` and controller tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/server.go internal/config/server_test.go internal/controller/frpserver_controller.go
git commit -m "feat: add httpPlugin support to frps.toml template"
```

---

## Task 3: Add cloud SDK dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add AWS SDK v2 modules**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
go get github.com/aws/aws-sdk-go-v2/config@latest
go get github.com/aws/aws-sdk-go-v2/service/ecr@latest
```

Expected: `go.mod` updated with new `require` entries for `github.com/aws/aws-sdk-go-v2`.

- [ ] **Step 2: Make `golang.org/x/oauth2` a direct dependency**

It is already in `go.mod` as indirect. After importing it in Task 4 code, `go mod tidy` will promote it. For now just verify it's present:

```bash
grep "golang.org/x/oauth2" go.mod
```

Expected: `golang.org/x/oauth2 v0.30.0` (or newer) present.

- [ ] **Step 3: Tidy**

```bash
go mod tidy
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add AWS SDK v2 dependencies for registry-proxy auth"
```

---

## Task 4: `registry-proxy` binary

**Files:**
- Create: `cmd/registry-proxy/main.go`
- Create: `cmd/registry-proxy/main_test.go`

- [ ] **Step 1: Write failing unit tests**

Create `cmd/registry-proxy/main_test.go`:

```go
package main

import (
	"testing"
)

func TestParseUpstream_Valid(t *testing.T) {
	tests := []struct {
		path     string
		wantHost string
		wantRest string
		wantOK   bool
	}{
		{
			path:     "/v2/asia-southeast3-docker.pkg.dev/my-project/my-app/manifests/latest",
			wantHost: "asia-southeast3-docker.pkg.dev",
			wantRest: "my-project/my-app/manifests/latest",
			wantOK:   true,
		},
		{
			path:     "/v2/123456789.dkr.ecr.us-east-1.amazonaws.com/my-image/blobs/sha256:abc",
			wantHost: "123456789.dkr.ecr.us-east-1.amazonaws.com",
			wantRest: "my-image/blobs/sha256:abc",
			wantOK:   true,
		},
		{
			path:     "/v2/",
			wantHost: "",
			wantRest: "",
			wantOK:   false,
		},
		{
			path:     "/v2/nohost/manifests/tag",
			wantHost: "",
			wantRest: "",
			wantOK:   false,
		},
	}
	for _, tc := range tests {
		host, rest, ok := parseUpstream(tc.path)
		if ok != tc.wantOK {
			t.Errorf("path=%q: got ok=%v, want %v", tc.path, ok, tc.wantOK)
		}
		if host != tc.wantHost {
			t.Errorf("path=%q: got host=%q, want %q", tc.path, host, tc.wantHost)
		}
		if rest != tc.wantRest {
			t.Errorf("path=%q: got rest=%q, want %q", tc.path, rest, tc.wantRest)
		}
	}
}

func TestAuthScheme(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"asia-southeast3-docker.pkg.dev", "gcp"},
		{"gcr.io", "gcp"},
		{"us.gcr.io", "gcp"},
		{"123456.dkr.ecr.us-east-1.amazonaws.com", "ecr"},
		{"ghcr.io", ""},
		{"registry-1.docker.io", ""},
		{"example.com", ""},
	}
	for _, tc := range tests {
		got := authScheme(tc.host)
		if got != tc.want {
			t.Errorf("host=%q: got %q, want %q", tc.host, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./cmd/registry-proxy/...
```

Expected: `build failed` — `parseUpstream` and `authScheme` are not defined yet.

- [ ] **Step 3: Write implementation `cmd/registry-proxy/main.go`**

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

package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"golang.org/x/oauth2/google"
)

func main() {
	port := flag.Int("port", 5000, "port the registry proxy listens on")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", handleRequest)
	addr := fmt.Sprintf(":%d", *port)
	slog.Info("registry-proxy starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("registry-proxy exited", "error", err)
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// /v2/ ping — return 200 so Docker clients don't hit the auth challenge.
	if r.URL.Path == "/v2/" || r.URL.Path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}

	upstreamHost, imagePath, ok := parseUpstream(r.URL.Path)
	if !ok {
		http.Error(w, "registry-proxy: cannot parse upstream host from path", http.StatusBadRequest)
		return
	}

	authHeader, err := buildAuthHeader(r.Context(), upstreamHost)
	if err != nil {
		slog.Error("auth failed", "upstream", upstreamHost, "error", err)
		http.Error(w, "registry-proxy: auth error", http.StatusBadGateway)
		return
	}

	target := &url.URL{
		Scheme:   "https",
		Host:     upstreamHost,
		Path:     "/v2/" + imagePath,
		RawQuery: r.URL.RawQuery,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = target
			req.Host = upstreamHost
			req.Header.Del("Authorization")
			if authHeader != "" {
				req.Header.Set("Authorization", authHeader)
			}
		},
	}
	proxy.ServeHTTP(w, r)
}

// parseUpstream extracts the upstream registry hostname from a Docker registry
// API path. The first segment after /v2/ that contains a "." is treated as the
// upstream host.
//
// /v2/asia-southeast3-docker.pkg.dev/project/image/manifests/latest
//     → host="asia-southeast3-docker.pkg.dev", rest="project/image/manifests/latest"
func parseUpstream(path string) (host, rest string, ok bool) {
	trimmed := strings.TrimPrefix(path, "/v2/")
	if trimmed == "" || trimmed == path {
		return "", "", false
	}
	idx := strings.Index(trimmed, "/")
	if idx < 0 {
		return "", "", false
	}
	candidate := trimmed[:idx]
	if !strings.Contains(candidate, ".") {
		return "", "", false
	}
	return candidate, trimmed[idx+1:], true
}

// authScheme returns "gcp", "ecr", or "" depending on the upstream host.
func authScheme(host string) string {
	if host == "gcr.io" || strings.HasSuffix(host, ".gcr.io") || strings.HasSuffix(host, ".pkg.dev") {
		return "gcp"
	}
	if strings.Contains(host, ".dkr.ecr.") && strings.HasSuffix(host, ".amazonaws.com") {
		return "ecr"
	}
	return ""
}

func buildAuthHeader(ctx context.Context, host string) (string, error) {
	switch authScheme(host) {
	case "gcp":
		return gcpBearerHeader(ctx)
	case "ecr":
		return ecrBasicHeader(ctx, host)
	default:
		return "", nil
	}
}

func gcpBearerHeader(ctx context.Context) (string, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("GCP ADC token source: %w", err)
	}
	token, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("GCP token: %w", err)
	}
	return "Bearer " + token.AccessToken, nil
}

func ecrBasicHeader(ctx context.Context, host string) (string, error) {
	// Host format: <account>.dkr.ecr.<region>.amazonaws.com
	parts := strings.Split(host, ".")
	// parts: [account, dkr, ecr, region, amazonaws, com] → region is index 3
	if len(parts) < 6 {
		return "", fmt.Errorf("cannot parse region from ECR host %q", host)
	}
	region := parts[3]

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", fmt.Errorf("AWS config: %w", err)
	}
	client := ecr.NewFromConfig(cfg)
	result, err := client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", fmt.Errorf("ECR GetAuthorizationToken: %w", err)
	}
	if len(result.AuthorizationData) == 0 {
		return "", fmt.Errorf("ECR returned empty authorization data")
	}
	// AuthorizationToken is base64(username:password) — use directly as Basic auth value.
	encoded := *result.AuthorizationData[0].AuthorizationToken
	// Validate it decodes correctly.
	if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
		return "", fmt.Errorf("ECR token is not valid base64: %w", err)
	}
	return "Basic " + encoded, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./cmd/registry-proxy/...
```

Expected: `ok  tunnel.io/frp-operator/cmd/registry-proxy`.

- [ ] **Step 5: Run `go mod tidy` to promote oauth2 to direct**

```bash
go mod tidy
```

Expected: `golang.org/x/oauth2` moves from `// indirect` to a direct `require` in `go.mod`.

- [ ] **Step 6: Commit**

```bash
git add cmd/registry-proxy/ go.mod go.sum
git commit -m "feat: add registry-proxy binary with GCP ADC and AWS ECR auth"
```

---

## Task 5: frps webhook HTTP handler

**Files:**
- Create: `internal/webhook/frps_events.go`
- Create: `internal/webhook/frps_events_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/webhook/frps_events_test.go`:

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

package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"tunnel.io/frp-operator/internal/webhook"
)

func newHandler(t *testing.T) (*webhook.FrpsEventHandler, *runtime.Scheme) {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	return webhook.NewFrpsEventHandler(c), s
}

func postEvent(t *testing.T, h *webhook.FrpsEventHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/frps/events", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestFrpsEventHandler_NewProxy_CreatesService(t *testing.T) {
	h, s := newHandler(t)
	rr := postEvent(t, h, map[string]any{
		"version": "0.1.0",
		"op":      "NewProxy",
		"content": map[string]any{
			"proxy_name":  "reg-1-registry-proxy",
			"proxy_type":  "tcp",
			"remote_port": 15000,
			"metas": map[string]string{
				"svcName":       "reg-1-docker",
				"namespace":     "tunnels",
				"remotePort":    "15000",
				"frpServerName": "main-hub",
			},
		},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if reject, _ := resp["reject"].(bool); reject {
		t.Errorf("expected reject=false")
	}

	// Verify the Service was created.
	c := fake.NewClientBuilder().WithScheme(s).Build()
	h2 := webhook.NewFrpsEventHandler(c)
	_ = h2 // Check via the handler's client — re-use h's client via reflection is brittle.
	// Instead re-post using the original handler and verify via the fake client embedded.
	var svc corev1.Service
	if err := h.Client().Get(context.Background(), types.NamespacedName{
		Namespace: "tunnels",
		Name:      "reg-1-docker",
	}, &svc); err != nil {
		t.Fatalf("Service not created: %v", err)
	}
	if svc.Spec.Selector["frpserver"] != "main-hub" {
		t.Errorf("expected selector frpserver=main-hub, got %v", svc.Spec.Selector)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 80 {
		t.Errorf("expected port 80, got %v", svc.Spec.Ports)
	}
	if int(svc.Spec.Ports[0].TargetPort.IntVal) != 15000 {
		t.Errorf("expected targetPort 15000, got %v", svc.Spec.Ports[0].TargetPort)
	}
}

func TestFrpsEventHandler_CloseProxy_DeletesService(t *testing.T) {
	h, _ := newHandler(t)

	// Pre-create a Service.
	existing := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reg-1-docker",
			Namespace: "tunnels",
		},
	}
	if err := h.Client().Create(context.Background(), existing); err != nil {
		t.Fatalf("pre-create service: %v", err)
	}

	rr := postEvent(t, h, map[string]any{
		"version": "0.1.0",
		"op":      "CloseProxy",
		"content": map[string]any{
			"proxy_name": "reg-1-registry-proxy",
			"metas": map[string]string{
				"svcName":   "reg-1-docker",
				"namespace": "tunnels",
			},
		},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var svc corev1.Service
	if err := h.Client().Get(context.Background(), types.NamespacedName{
		Namespace: "tunnels",
		Name:      "reg-1-docker",
	}, &svc); err == nil {
		t.Error("expected Service to be deleted but it still exists")
	}
}

func TestFrpsEventHandler_NoSvcName_Passthrough(t *testing.T) {
	h, _ := newHandler(t)
	rr := postEvent(t, h, map[string]any{
		"version": "0.1.0",
		"op":      "NewProxy",
		"content": map[string]any{
			"proxy_name": "some-other-proxy",
			"metas":      map[string]string{},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if reject, _ := resp["reject"].(bool); reject {
		t.Errorf("expected reject=false for non-registry proxy")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/webhook/...
```

Expected: `build failed` — package does not exist yet.

- [ ] **Step 3: Create `internal/webhook/frps_events.go`**

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

// Package webhook provides the frps httpPlugin event handler.
package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// frps httpPlugin request/response types (frp plugin protocol v0.1.0).

type pluginRequest struct {
	Version string        `json:"version"`
	Op      string        `json:"op"`
	Content pluginContent `json:"content"`
}

type pluginContent struct {
	ProxyName string            `json:"proxy_name"`
	ProxyType string            `json:"proxy_type"`
	Metas     map[string]string `json:"metas"`
}

type pluginResponse struct {
	Reject       bool   `json:"reject"`
	RejectReason string `json:"reject_reason,omitempty"`
	Unchange     bool   `json:"unchange"`
}

// FrpsEventHandler handles POST /frps/events from frps httpPlugin callbacks.
type FrpsEventHandler struct {
	client client.Client
}

// NewFrpsEventHandler constructs a handler using the given controller-runtime client.
func NewFrpsEventHandler(c client.Client) *FrpsEventHandler {
	return &FrpsEventHandler{client: c}
}

// Client exposes the underlying client for testing.
func (h *FrpsEventHandler) Client() client.Client { return h.client }

func (h *FrpsEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := logf.FromContext(r.Context())

	var req pluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "failed to decode frps plugin request")
		writeReject(w, "invalid request body")
		return
	}

	switch req.Op {
	case "NewProxy":
		h.handleNewProxy(r.Context(), req.Content, w)
	case "CloseProxy":
		h.handleCloseProxy(r.Context(), req.Content, w)
	default:
		writeAllow(w)
	}
}

func (h *FrpsEventHandler) handleNewProxy(ctx context.Context, content pluginContent, w http.ResponseWriter) {
	log := logf.FromContext(ctx)

	svcName := content.Metas["svcName"]
	if svcName == "" {
		writeAllow(w)
		return
	}

	namespace := content.Metas["namespace"]
	frpServerName := content.Metas["frpServerName"]
	remotePortStr := content.Metas["remotePort"]
	remotePort, err := strconv.ParseInt(remotePortStr, 10, 32)
	if err != nil {
		log.Error(err, "invalid remotePort in metas", "value", remotePortStr)
		writeAllow(w) // failSafe: allow proxy even if we can't create the Service
		return
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, h.client, svc, func() error {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"frpserver": frpServerName}
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "registry",
				Port:       80,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt32(int32(remotePort)),
			},
		}
		return nil
	})
	if err != nil {
		log.Error(err, "failed to create hub Service", "name", svcName, "namespace", namespace)
		writeAllow(w) // failSafe
		return
	}

	log.Info("created hub registry Service", "name", svcName, "namespace", namespace)
	writeAllow(w)
}

func (h *FrpsEventHandler) handleCloseProxy(ctx context.Context, content pluginContent, w http.ResponseWriter) {
	log := logf.FromContext(ctx)

	svcName := content.Metas["svcName"]
	if svcName == "" {
		writeAllow(w)
		return
	}

	namespace := content.Metas["namespace"]
	svc := &corev1.Service{}
	if err := h.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: svcName}, svc); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "failed to get hub Service for deletion", "name", svcName)
		}
		writeAllow(w)
		return
	}

	if err := h.client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "failed to delete hub Service", "name", svcName)
	} else {
		log.Info("deleted hub registry Service", "name", svcName, "namespace", namespace)
	}
	writeAllow(w)
}

func writeAllow(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pluginResponse{Reject: false, Unchange: false})
}

func writeReject(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // frps expects 200 even for reject
	_ = json.NewEncoder(w).Encode(pluginResponse{Reject: true, RejectReason: reason})
}

// HTTPServer wraps FrpsEventHandler as a controller-runtime Runnable.
type HTTPServer struct {
	addr    string
	handler *FrpsEventHandler
}

// NewHTTPServer returns an HTTPServer that implements manager.Runnable.
func NewHTTPServer(addr string, c client.Client) *HTTPServer {
	return &HTTPServer{addr: addr, handler: NewFrpsEventHandler(c)}
}

// Start implements manager.Runnable. Blocks until ctx is cancelled.
func (s *HTTPServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/frps/events", s.handler)
	srv := &http.Server{Addr: s.addr, Handler: mux}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background()) //nolint:contextcheck
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/webhook/...
```

Expected: `ok  tunnel.io/frp-operator/internal/webhook`.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/
git commit -m "feat: add frps httpPlugin event handler for hub Service lifecycle"
```

---

## Task 6: RegistryProxy controller

**Files:**
- Create: `internal/controller/registryproxy_controller.go`
- Create: `internal/controller/registryproxy_controller_test.go`

- [ ] **Step 1: Write failing controller tests**

Create `internal/controller/registryproxy_controller_test.go`:

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
)

var _ = Describe("RegistryProxy Controller", func() {
	const (
		rpName    = "reg-1"
		clientName = "spoke-client"
		serverName = "main-hub"
		ns        = "default"
	)
	ctx := context.Background()

	// Helpers.
	nsName := func(name string) types.NamespacedName { return types.NamespacedName{Namespace: ns, Name: name} }
	reconciler := func() *RegistryProxyReconciler {
		return &RegistryProxyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	}
	reconcile_ := func() {
		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		// Create prerequisite FrpServer.
		srv := &tunnelv1alpha1.FrpServer{
			ObjectMeta: metav1.ObjectMeta{Name: serverName, Namespace: ns},
			Spec:       tunnelv1alpha1.FrpServerSpec{BindPort: 7000},
		}
		_ = k8sClient.Create(ctx, srv)

		// Create prerequisite FrpClient.
		spoke := &tunnelv1alpha1.FrpClient{
			ObjectMeta: metav1.ObjectMeta{Name: clientName, Namespace: ns},
			Spec: tunnelv1alpha1.FrpClientSpec{
				ServerRef: tunnelv1alpha1.ServerRef{Name: serverName},
				Image:     tunnelv1alpha1.ImageSpec{Repository: "registry.example.com/crd-tunnel", Tag: "1.0.0"},
			},
		}
		Expect(k8sClient.Create(ctx, spoke)).To(Succeed())

		// Create RegistryProxy.
		rp := &tunnelv1alpha1.RegistryProxy{
			ObjectMeta: metav1.ObjectMeta{Name: rpName, Namespace: ns},
			Spec: tunnelv1alpha1.RegistryProxySpec{
				ClientRef:          tunnelv1alpha1.LocalObjectRef{Name: clientName},
				RemotePort:         15000,
				ServiceAccountName: "registry-proxy-sa",
			},
		}
		Expect(k8sClient.Create(ctx, rp)).To(Succeed())
	})

	AfterEach(func() {
		for _, obj := range []client.Object{
			&tunnelv1alpha1.RegistryProxy{ObjectMeta: metav1.ObjectMeta{Name: rpName, Namespace: ns}},
			&tunnelv1alpha1.FrpClient{ObjectMeta: metav1.ObjectMeta{Name: clientName, Namespace: ns}},
			&tunnelv1alpha1.FrpServer{ObjectMeta: metav1.ObjectMeta{Name: serverName, Namespace: ns}},
		} {
			_ = k8sClient.Delete(ctx, obj)
		}
	})

	It("adds finalizer on first reconcile", func() {
		reconcile_()
		var rp tunnelv1alpha1.RegistryProxy
		Expect(k8sClient.Get(ctx, nsName(rpName), &rp)).To(Succeed())
		Expect(rp.Finalizers).To(ContainElement(tunnelv1alpha1.RegistryProxyCleanupFinalizer))
	})

	It("creates proxy Deployment with correct image and service account", func() {
		reconcile_()
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy"), &dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal("registry-proxy-sa"))
		Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("registry.example.com/crd-tunnel:1.0.0"))
		Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("/registry-proxy"))
	})

	It("creates proxy Service on default port 5000", func() {
		reconcile_()
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, nsName(rpName+"-proxy-svc"), &svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(5000)))
	})

	It("creates FrpProxy with correct metadata", func() {
		reconcile_()
		var fp tunnelv1alpha1.FrpProxy
		Expect(k8sClient.Get(ctx, nsName(rpName+"-registry-proxy"), &fp)).To(Succeed())
		Expect(fp.Spec.ClientRef.Name).To(Equal(clientName))
		Expect(fp.Spec.Type).To(Equal("tcp"))
		Expect(fp.Spec.RemotePort).To(Equal(int32(15000)))
		Expect(fp.Spec.ExtraConfig).To(ContainSubstring(`metadatas.svcName = "reg-1-docker"`))
		Expect(fp.Spec.ExtraConfig).To(ContainSubstring(`metadatas.frpServerName = "main-hub"`))
	})

	It("removes owned resources on deletion", func() {
		reconcile_() // create resources

		// Delete RegistryProxy.
		var rp tunnelv1alpha1.RegistryProxy
		Expect(k8sClient.Get(ctx, nsName(rpName), &rp)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &rp)).To(Succeed())

		// Reconcile deletion path.
		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred())

		// FrpProxy should be gone.
		var fp tunnelv1alpha1.FrpProxy
		Expect(k8sClient.Get(ctx, nsName(rpName+"-registry-proxy"), &fp)).To(
			Satisfy(errors.IsNotFound))
	})

	It("requeues when FrpClient not found", func() {
		// Delete FrpClient first.
		var spoke tunnelv1alpha1.FrpClient
		Expect(k8sClient.Get(ctx, nsName(clientName), &spoke)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &spoke)).To(Succeed())

		result, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nsName(rpName)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(15 * time.Second))
	})
})
```

Note: Add `"time"` to the imports at the top of the file.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/controller/... 2>&1 | head -20
```

Expected: `build failed` — `RegistryProxyReconciler` is not defined yet.

- [ ] **Step 3: Create `internal/controller/registryproxy_controller.go`**

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

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
)

const registryProxyRequeueDelay = 15 * time.Second

// RegistryProxyReconciler reconciles a RegistryProxy object.
type RegistryProxyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=registryproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *RegistryProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rp tunnelv1alpha1.RegistryProxy
	if err := r.Get(ctx, req.NamespacedName, &rp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !rp.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&rp, tunnelv1alpha1.RegistryProxyCleanupFinalizer) {
			if err := r.deleteOwnedResources(ctx, &rp); err != nil {
				log.Error(err, "cleanup failed")
				return ctrl.Result{RequeueAfter: registryProxyRequeueDelay}, nil
			}
			controllerutil.RemoveFinalizer(&rp, tunnelv1alpha1.RegistryProxyCleanupFinalizer)
			return ctrl.Result{}, r.Update(ctx, &rp)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&rp, tunnelv1alpha1.RegistryProxyCleanupFinalizer) {
		controllerutil.AddFinalizer(&rp, tunnelv1alpha1.RegistryProxyCleanupFinalizer)
		if err := r.Update(ctx, &rp); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve FrpClient.
	var spoke tunnelv1alpha1.FrpClient
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      rp.Spec.ClientRef.Name,
	}, &spoke); err != nil {
		if errors.IsNotFound(err) {
			log.Info("FrpClient not found, requeuing", "client", rp.Spec.ClientRef.Name)
			return ctrl.Result{RequeueAfter: registryProxyRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve image: use spec.Image if set, else inherit from FrpClient.
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	if rp.Spec.Image != nil && rp.Spec.Image.Repository != "" {
		image = rp.Spec.Image.Repository + ":" + rp.Spec.Image.Tag
	}
	if image == ":" {
		image = "asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel:1.0.0"
	}

	// Resolve proxy port.
	proxyPort := int32(5000)
	if rp.Spec.ProxyPort != nil {
		proxyPort = *rp.Spec.ProxyPort
	}

	// Resolve frpServerName for metadata (used by hub webhook to create Service selector).
	frpServerName := spoke.Spec.ServerRef.Name // may be "" for explicit-address mode

	if err := r.reconcileProxyDeployment(ctx, &rp, image, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileProxyService(ctx, &rp, proxyPort); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileFrpProxy(ctx, &rp, proxyPort, frpServerName); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.syncStatus(ctx, &rp)
}

func (r *RegistryProxyReconciler) reconcileProxyDeployment(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, image string, proxyPort int32) error {
	replicas := int32(1)
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
					},
				},
			},
		}
		return nil
	})
	return err
}

func (r *RegistryProxyReconciler) reconcileProxyService(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, proxyPort int32) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-proxy-svc",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(rp, svc, r.Scheme); err != nil {
			return err
		}
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = map[string]string{"registryproxy": rp.Name}
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

func (r *RegistryProxyReconciler) reconcileFrpProxy(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy, proxyPort int32, frpServerName string) error {
	localIP := fmt.Sprintf("%s-proxy-svc.%s.svc.cluster.local", rp.Name, rp.Namespace)
	extraConfig := fmt.Sprintf(
		"metadatas.svcName = %q\nmetadatas.namespace = %q\nmetadatas.remotePort = %q\nmetadatas.frpServerName = %q",
		rp.Name+"-docker",
		rp.Namespace,
		fmt.Sprintf("%d", rp.Spec.RemotePort),
		frpServerName,
	)

	fp := &tunnelv1alpha1.FrpProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.Name + "-registry-proxy",
			Namespace: rp.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, fp, func() error {
		fp.Spec.ClientRef = tunnelv1alpha1.LocalObjectRef{Name: rp.Spec.ClientRef.Name}
		fp.Spec.Type = "tcp"
		fp.Spec.LocalIP = localIP
		fp.Spec.LocalPort = proxyPort
		fp.Spec.RemotePort = rp.Spec.RemotePort
		fp.Spec.ExtraConfig = extraConfig
		return nil
	})
	return err
}

func (r *RegistryProxyReconciler) deleteOwnedResources(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy) error {
	// Delete FrpProxy first so frpc hot-reloads before we remove the proxy pod.
	fp := &tunnelv1alpha1.FrpProxy{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, fp); err == nil {
		if err := r.Delete(ctx, fp); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete FrpProxy: %w", err)
		}
	}
	// Owned Deployment and Service are garbage-collected via ownerReferences.
	return nil
}

func (r *RegistryProxyReconciler) syncStatus(ctx context.Context, rp *tunnelv1alpha1.RegistryProxy) error {
	// Check proxy Deployment readiness.
	var dep appsv1.Deployment
	proxyReady := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-proxy"}, &dep); err == nil {
		proxyReady = dep.Status.ReadyReplicas > 0
	}

	// Check tunnel connectivity via FrpProxy phase.
	var fp tunnelv1alpha1.FrpProxy
	tunnelConnected := false
	if err := r.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name + "-registry-proxy"}, &fp); err == nil {
		tunnelConnected = fp.Status.Phase == "Running"
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
					return // no change
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
		setCondition("TunnelConnected", metav1.ConditionTrue, "FrpProxyRunning", "")
		updated.Status.HubService = fmt.Sprintf("%s-docker.%s.svc.cluster.local", rp.Name, rp.Namespace)
	} else {
		setCondition("TunnelConnected", metav1.ConditionFalse, "FrpProxyNotRunning", "waiting for FRP tunnel")
		updated.Status.HubService = ""
	}

	if proxyReady && tunnelConnected {
		updated.Status.Phase = "Running"
	} else {
		updated.Status.Phase = "Pending"
	}

	if err := r.Status().Update(ctx, updated); err != nil && !errors.IsConflict(err) {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RegistryProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.RegistryProxy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		// React to FrpProxy status changes (named <rp.Name>-registry-proxy by convention).
		Watches(
			&tunnelv1alpha1.FrpProxy{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				name := obj.GetName()
				if !strings.HasSuffix(name, "-registry-proxy") {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      strings.TrimSuffix(name, "-registry-proxy"),
					},
				}}
			}),
		).
		Named("registryproxy").
		Complete(r)
}
```

- [ ] **Step 4: Fix missing `time` import in the test file**

The test file references `time.Second`. Ensure the import block at the top of `registryproxy_controller_test.go` includes:

```go
import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
)
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/controller/... -v 2>&1 | tail -30
```

Expected: `PASS` for RegistryProxy Controller suite. The envtest-based tests create/reconcile the CR and verify owned resources exist. All new Describe blocks should show `[PASS]`.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/registryproxy_controller.go internal/controller/registryproxy_controller_test.go
git commit -m "feat: add RegistryProxy controller with Deployment/Service/FrpProxy reconciliation"
```

---

## Task 7: Wire `cmd/main.go`

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Register `RegistryProxyReconciler` and start webhook server**

In `cmd/main.go`, after the existing `FrpProxyReconciler` block (around line 200), add:

```go
	if err := (&controller.RegistryProxyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "RegistryProxy")
		os.Exit(1)
	}
```

Then after the `+kubebuilder:scaffold:builder` comment but before `mgr.AddHealthzCheck`, add:

```go
	if err := mgr.Add(webhook.NewHTTPServer(":9090", mgr.GetClient())); err != nil {
		setupLog.Error(err, "Failed to register frps plugin webhook server")
		os.Exit(1)
	}
```

Add the import at the top of `cmd/main.go`:

```go
	"tunnel.io/frp-operator/internal/webhook"
```

- [ ] **Step 2: Verify build**

```bash
go build ./cmd/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add cmd/main.go
git commit -m "feat: register RegistryProxy controller and frps plugin webhook server"
```

---

## Task 8: Kustomize, RBAC, and sample CR

**Files:**
- Create: `config/default/frps_plugin_service.yaml`
- Modify: `config/default/kustomization.yaml`
- Modify: `config/manager/manager.yaml`
- Create: `config/samples/tunnelv1alpha1_registryproxy.yaml`

- [ ] **Step 1: Create `config/default/frps_plugin_service.yaml`**

```yaml
# Service that exposes the operator's frps-plugin webhook endpoint.
# frps calls POST /frps/events when proxies connect/disconnect.
apiVersion: v1
kind: Service
metadata:
  name: controller-manager-frps-plugin
  namespace: system
spec:
  selector:
    control-plane: controller-manager
    app.kubernetes.io/name: crd-tunnel-service
  ports:
    - name: frps-plugin
      port: 9090
      targetPort: 9090
      protocol: TCP
```

- [ ] **Step 2: Add to `config/default/kustomization.yaml`**

In the `resources:` list, after `- metrics_service.yaml`, add:

```yaml
- frps_plugin_service.yaml
```

- [ ] **Step 3: Expose port 9090 in `config/manager/manager.yaml`**

Change `ports: []` in the manager container to:

```yaml
        ports:
        - containerPort: 9090
          name: frps-plugin
          protocol: TCP
```

- [ ] **Step 4: Create sample CR `config/samples/tunnelv1alpha1_registryproxy.yaml`**

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: RegistryProxy
metadata:
  name: reg-1
  namespace: tunnels
  labels:
    app.kubernetes.io/name: crd-tunnel-service
    app.kubernetes.io/managed-by: kustomize
spec:
  clientRef:
    name: spoke-client           # Name of an existing FrpClient in this namespace.
  remotePort: 15000              # Port frps will expose on the hub side.
  serviceAccountName: registry-proxy-sa  # SA annotated with WI/IRSA.
  # proxyPort: 5000              # Optional; default 5000.
  # image:                       # Optional; inherits from FrpClient image.
  #   repository: asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel
  #   tag: "1.0.0"

# After applying:
#   docker pull reg-1-docker.tunnels.svc.cluster.local/asia-southeast3-docker.pkg.dev/my-image:tag
#
# The FrpServer must have enableOperatorPlugin: true.
# Example FrpServer patch:
#   spec:
#     enableOperatorPlugin: true
#     operatorWebhookAddr: "crd-tunnel-service-controller-manager-frps-plugin.crd-tunnel-service-system.svc.cluster.local:9090"
```

- [ ] **Step 5: Regenerate RBAC from kubebuilder markers**

```bash
make manifests
```

Expected: `config/rbac/role.yaml` is updated to include `registryproxies` resources.

- [ ] **Step 6: Commit**

```bash
git add config/default/frps_plugin_service.yaml config/default/kustomization.yaml config/manager/manager.yaml config/samples/tunnelv1alpha1_registryproxy.yaml config/rbac/
git commit -m "feat: add kustomize Service for frps plugin webhook and sample RegistryProxy CR"
```

---

## Task 9: Update `docker/Dockerfile.frp` to include `registry-proxy`

**Files:**
- Modify: `docker/Dockerfile.frp`

- [ ] **Step 1: Read current Makefile docker build target**

```bash
grep -A5 "docker-build-frp" /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service/Makefile
```

Note the current `docker buildx build` command context path. The build context must be the repo root (`.`) for the proxy stage to access the Go source.

- [ ] **Step 2: Update `docker/Dockerfile.frp`**

Replace the entire file with:

```dockerfile
ARG FRP_VERSION=v0.61.0

# Stage 1: Build frps and frpc from the FRP upstream source.
FROM golang:1.22-alpine AS frp-builder
ARG FRP_VERSION
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN apk add --no-cache git
WORKDIR /build
RUN git clone --depth 1 --branch ${FRP_VERSION} https://github.com/fatedier/frp .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o frps ./cmd/frps
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o frpc ./cmd/frpc

# Stage 2: Build registry-proxy from this repository's source.
FROM golang:1.25 AS proxy-builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o registry-proxy ./cmd/registry-proxy

# Stage 3: Final minimal runtime image.
FROM gcr.io/distroless/static:nonroot
COPY --from=frp-builder /build/frps /frps
COPY --from=frp-builder /build/frpc /frpc
COPY --from=proxy-builder /src/registry-proxy /registry-proxy
# No ENTRYPOINT/CMD — controllers set explicit Command per role
EXPOSE 7000 7400 80 443 7500
```

- [ ] **Step 3: Update Makefile docker build context if needed**

Find the `docker-build-frp` target in the Makefile. If the build context is currently `docker/` (just the docker directory), change it to `.` (repo root) so the `COPY . .` in stage 2 picks up the Go source:

```makefile
# Before (if context is docker/ or similar):
docker buildx build --platform $(PLATFORMS) -t $(IMG_FRP) -f docker/Dockerfile.frp docker/

# After:
docker buildx build --platform $(PLATFORMS) -t $(IMG_FRP) -f docker/Dockerfile.frp .
```

- [ ] **Step 4: Test single-platform build locally**

```bash
docker build -t crd-tunnel:test -f docker/Dockerfile.frp .
```

Expected: build succeeds. Verify binary exists:

```bash
docker run --rm crd-tunnel:test ls /registry-proxy /frps /frpc
```

Expected:
```
/frpc
/frps
/registry-proxy
```

- [ ] **Step 5: Commit**

```bash
git add docker/Dockerfile.frp Makefile
git commit -m "feat: build registry-proxy binary into crd-tunnel Docker image"
```

---

## Self-Review Checklist

Spec coverage verification:

| Spec requirement | Covered by |
|---|---|
| RegistryProxy CRD | Task 1 |
| Universal registry router (upstream host in path) | Task 4 `parseUpstream` |
| GCP ADC auth | Task 4 `gcpBearerHeader` |
| AWS IRSA/Pod Identity auth | Task 4 `ecrBasicHeader` |
| No hub kubeconfig in spoke | Tasks 5+6 — hub Service created by hub operator via frps webhook |
| frps httpPlugin for NewProxy/CloseProxy | Task 5 `FrpsEventHandler` |
| FrpServer `EnableOperatorPlugin` + template | Task 2 |
| FrpProxy with metadata | Task 6 `reconcileFrpProxy` |
| `TunnelConnected` via FrpProxy status | Task 6 `syncStatus` |
| `ProxyReady` via Deployment readyReplicas | Task 6 `syncStatus` |
| Hub Service `<name>-docker` port 80→remotePort | Task 5 `handleNewProxy` |
| Hub Service deleted on CloseProxy | Task 5 `handleCloseProxy` |
| Finalizer + owned resource cleanup | Task 6 `deleteOwnedResources` |
| Same image as FrpClient (no bootstrap problem) | Task 6 `reconcileProxyDeployment` |
| Binary bundled in crd-tunnel image | Task 9 |
| Kustomize Service for webhook | Task 8 |
| Sample CR | Task 8 |
