# FRP Operator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Kubernetes operator that manages FRP hub-and-spoke tunnel infrastructure via three CRDs (FrpServer, FrpClient, FrpProxy) with multi-platform Docker images and hot reload.

**Architecture:** A single Go operator binary with three controllers (kubebuilder/controller-runtime). FrpProxy changes trigger a config re-render and live admin API reload on the frpc pod via an internal ClusterIP Service — no pod restarts on proxy changes. Docker images are built from FRP source using multi-stage Dockerfiles with Docker Buildx producing unified multi-platform manifest lists.

**Tech Stack:** Go 1.22, kubebuilder v4, controller-runtime v0.18, Ginkgo v2/Gomega, Kustomize, Docker Buildx

---

## Prerequisites

- `go` 1.22+
- `kubebuilder` v4 (`brew install kubebuilder` or download from https://github.com/kubernetes-sigs/kubebuilder/releases)
- `controller-gen` (installed via `go install` in Task 1)
- `docker` with `buildx` plugin
- `kubectl` + access to a cluster for e2e (optional)

---

## File Map

| File | Purpose |
|------|---------|
| `docker/Dockerfile.frps` | Multi-stage multi-arch frps image |
| `docker/Dockerfile.frpc` | Multi-stage multi-arch frpc image |
| `api/v1alpha1/types_shared.go` | Shared types: ImageSpec, SecretKeyRef, ServiceSpec |
| `api/v1alpha1/frpserver_types.go` | FrpServer CRD spec/status |
| `api/v1alpha1/frpclient_types.go` | FrpClient CRD spec/status |
| `api/v1alpha1/frpproxy_types.go` | FrpProxy CRD spec/status + finalizer const |
| `internal/config/server.go` | Renders frps.toml from FrpServerSpec |
| `internal/config/server_test.go` | Unit tests for server config renderer |
| `internal/config/client.go` | Renders frpc.toml + proxy blocks from FrpClientSpec + []FrpProxySpec |
| `internal/config/client_test.go` | Unit tests for client config renderer |
| `internal/reload/client.go` | HTTP client for frpc admin API reload |
| `internal/reload/client_test.go` | Unit tests for reload client |
| `controllers/frpserver_controller.go` | FrpServer reconciler |
| `controllers/frpclient_controller.go` | FrpClient reconciler |
| `controllers/frpproxy_controller.go` | FrpProxy reconciler (hot reload owner) |
| `controllers/suite_test.go` | envtest setup shared by all controller tests |
| `controllers/frpserver_controller_test.go` | FrpServer controller envtest tests |
| `controllers/frpclient_controller_test.go` | FrpClient controller envtest tests |
| `controllers/frpproxy_controller_test.go` | FrpProxy controller envtest tests |
| `main.go` | Operator entrypoint, registers all three controllers |
| `config/crd/kustomization.yaml` | CRD kustomize overlay |
| `config/rbac/service_account.yaml` | Operator ServiceAccount |
| `config/rbac/cluster_role.yaml` | Operator ClusterRole |
| `config/rbac/cluster_role_binding.yaml` | Binds ClusterRole to ServiceAccount |
| `config/rbac/kustomization.yaml` | RBAC kustomize overlay |
| `config/manager/deployment.yaml` | Operator Deployment |
| `config/manager/kustomization.yaml` | Manager kustomize overlay |
| `config/default/namespace.yaml` | frp-system namespace |
| `config/default/kustomization.yaml` | Root kustomize overlay (composes all) |
| `config/samples/frpserver-sample.yaml` | Basic FrpServer example |
| `config/samples/frpserver-gcp-nlb.yaml` | FrpServer with GCP NLB annotations |
| `config/samples/frpserver-aws-nlb.yaml` | FrpServer with AWS NLB annotations |
| `config/samples/frpclient-sample.yaml` | Basic FrpClient example |
| `config/samples/frpproxy-sample.yaml` | FrpProxy TCP tunnel example |
| `Makefile` | Build, generate, deploy, docker targets |

---

## Task 1: Scaffold project with kubebuilder

**Files:**
- Create: `go.mod`, `main.go`, `api/v1alpha1/groupversion_info.go` (generated)
- Create: `controllers/frpserver_controller.go` scaffold (generated)
- Create: `controllers/frpclient_controller.go` scaffold (generated)
- Create: `controllers/frpproxy_controller.go` scaffold (generated)
- Create: `config/` directory tree (generated)

- [ ] **Step 1: Verify kubebuilder is installed**

```bash
kubebuilder version
```
Expected: version line like `Version: main.XXXX, ...`. If not found: `brew install kubebuilder`.

- [ ] **Step 2: Initialize kubebuilder project at repo root**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
kubebuilder init --domain tunnel.io --repo tunnel.io/frp-operator
```
Expected: `go.mod`, `main.go`, `Makefile`, `PROJECT`, `config/` created.

- [ ] **Step 3: Create FrpServer API**

```bash
kubebuilder create api --group tunnel --version v1alpha1 --kind FrpServer --resource --controller
```
When prompted `Create Resource [y/n]`: y. `Create Controller [y/n]`: y.

- [ ] **Step 4: Create FrpClient API**

```bash
kubebuilder create api --group tunnel --version v1alpha1 --kind FrpClient --resource --controller
```

- [ ] **Step 5: Create FrpProxy API**

```bash
kubebuilder create api --group tunnel --version v1alpha1 --kind FrpProxy --resource --controller
```

- [ ] **Step 6: Create internal package directories**

```bash
mkdir -p internal/config internal/reload docker
```

- [ ] **Step 7: Verify scaffold compiles**

```bash
go build ./...
```
Expected: no errors (generated scaffold is valid Go).

- [ ] **Step 8: Commit scaffold**

```bash
git add -A
git commit -m "chore: kubebuilder scaffold for FrpServer, FrpClient, FrpProxy"
```

---

## Task 2: Dockerfile for frps (server)

**Files:**
- Create: `docker/Dockerfile.frps`

- [ ] **Step 1: Write Dockerfile.frps**

```dockerfile
# docker/Dockerfile.frps
ARG FRP_VERSION=v0.61.0

FROM golang:1.22-alpine AS builder
ARG FRP_VERSION
ARG TARGETOS
ARG TARGETARCH
RUN apk add --no-cache git
WORKDIR /build
RUN git clone --depth 1 --branch ${FRP_VERSION} https://github.com/fatedier/frp .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o frps ./cmd/frps

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /build/frps /frps
EXPOSE 7000 80 443 7500
ENTRYPOINT ["/frps"]
CMD ["-c", "/etc/frp/frps.toml"]
```

- [ ] **Step 2: Verify build locally (single-platform test)**

```bash
docker build --build-arg FRP_VERSION=v0.61.0 \
  --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 \
  -t frps:test -f docker/Dockerfile.frps .
```
Expected: image built successfully.

- [ ] **Step 3: Commit**

```bash
git add docker/Dockerfile.frps
git commit -m "feat: add multi-stage Dockerfile for frps"
```

---

## Task 3: Dockerfile for frpc (client)

**Files:**
- Create: `docker/Dockerfile.frpc`

- [ ] **Step 1: Write Dockerfile.frpc**

```dockerfile
# docker/Dockerfile.frpc
ARG FRP_VERSION=v0.61.0

FROM golang:1.22-alpine AS builder
ARG FRP_VERSION
ARG TARGETOS
ARG TARGETARCH
RUN apk add --no-cache git
WORKDIR /build
RUN git clone --depth 1 --branch ${FRP_VERSION} https://github.com/fatedier/frp .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o frpc ./cmd/frpc

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /build/frpc /frpc
EXPOSE 7400
ENTRYPOINT ["/frpc"]
CMD ["-c", "/etc/frp/frpc.toml"]
```

- [ ] **Step 2: Verify build locally (single-platform test)**

```bash
docker build --build-arg FRP_VERSION=v0.61.0 \
  --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 \
  -t frpc:test -f docker/Dockerfile.frpc .
```
Expected: image built successfully.

- [ ] **Step 3: Commit**

```bash
git add docker/Dockerfile.frpc
git commit -m "feat: add multi-stage Dockerfile for frpc"
```

---

## Task 4: Shared types + FrpServer CRD types

**Files:**
- Create: `api/v1alpha1/types_shared.go`
- Modify: `api/v1alpha1/frpserver_types.go`

- [ ] **Step 1: Write shared types**

```go
// api/v1alpha1/types_shared.go
package v1alpha1

import corev1 "k8s.io/api/core/v1"

// ImageSpec defines the container image for frps or frpc.
type ImageSpec struct {
	// +kubebuilder:default:="your.registry.io/frps"
	Repository string `json:"repository,omitempty"`
	// +kubebuilder:default:="v0.61.0"
	Tag string `json:"tag,omitempty"`
}

// SecretKeyRef references a key inside a Kubernetes Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ServicePort defines a port exposed by the FrpServer Service.
type ServicePort struct {
	Name string `json:"name"`
	// +kubebuilder:default:=TCP
	Protocol corev1.Protocol `json:"protocol,omitempty"`
	Port     int32           `json:"port"`
}

// ServiceSpec defines the Kubernetes Service created for an FrpServer.
type ServiceSpec struct {
	// +kubebuilder:default:=LoadBalancer
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	Type corev1.ServiceType `json:"type,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Ports []ServicePort `json:"ports,omitempty"`
}

// Phase represents the lifecycle phase of a tunnel resource.
// +kubebuilder:validation:Enum=Pending;Running;Failed;Reloading
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseFailed    Phase = "Failed"
	PhaseReloading Phase = "Reloading"
)
```

- [ ] **Step 2: Write FrpServer types**

```go
// api/v1alpha1/frpserver_types.go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FrpServerSpec defines the desired state of FrpServer.
type FrpServerSpec struct {
	// Image is the frps container image. Defaults are set in the CRD schema.
	// +optional
	Image ImageSpec `json:"image,omitempty"`
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas,omitempty"`
	// +kubebuilder:default:=7000
	BindPort int32 `json:"bindPort,omitempty"`
	// +optional
	VhostHTTPPort *int32 `json:"vhostHTTPPort,omitempty"`
	// +optional
	VhostHTTPSPort *int32 `json:"vhostHTTPSPort,omitempty"`
	// +optional
	DashboardPort *int32 `json:"dashboardPort,omitempty"`
	// +optional
	DashboardUser string `json:"dashboardUser,omitempty"`
	// +optional
	DashboardPasswordSecret *SecretKeyRef `json:"dashboardPasswordSecret,omitempty"`
	// AuthTokenSecret is the shared token frpc clients must present.
	AuthTokenSecret SecretKeyRef `json:"authTokenSecret"`
	// ExtraConfig is raw TOML appended verbatim to frps.toml.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
	// Service defines the Kubernetes Service for this server.
	// +optional
	Service ServiceSpec `json:"service,omitempty"`
}

// FrpServerStatus defines the observed state of FrpServer.
type FrpServerStatus struct {
	// +optional
	Phase Phase `json:"phase,omitempty"`
	// Address is the externally-reachable IP/hostname of the server.
	// +optional
	Address string `json:"address,omitempty"`
	// +optional
	Port int32 `json:"port,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.status.address`
// +kubebuilder:printcolumn:name="Port",type=integer,JSONPath=`.status.port`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type FrpServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec   FrpServerSpec   `json:"spec,omitempty"`
	Status FrpServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type FrpServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FrpServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrpServer{}, &FrpServerList{})
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./api/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add api/v1alpha1/types_shared.go api/v1alpha1/frpserver_types.go
git commit -m "feat: add FrpServer CRD types and shared types"
```

---

## Task 5: FrpClient CRD types

**Files:**
- Modify: `api/v1alpha1/frpclient_types.go`

- [ ] **Step 1: Write FrpClient types**

```go
// api/v1alpha1/frpclient_types.go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServerRef resolves the address of the parent FrpServer.
// Exactly one of Name or Address must be set.
type ServerRef struct {
	// Name of the FrpServer resource in the same namespace.
	// +optional
	Name string `json:"name,omitempty"`
	// Address is the explicit hostname/IP of the frps server.
	// +optional
	Address string `json:"address,omitempty"`
	// +kubebuilder:default:=7000
	Port int32 `json:"port,omitempty"`
}

// AdminSpec configures the frpc admin (hot reload) API.
type AdminSpec struct {
	// +kubebuilder:default:=7400
	Port int32 `json:"port,omitempty"`
	// TokenSecret references the Secret holding the admin API token.
	TokenSecret SecretKeyRef `json:"tokenSecret"`
}

// FrpClientSpec defines the desired state of FrpClient.
type FrpClientSpec struct {
	// Image is the frpc container image. Defaults are set in the CRD schema.
	// +optional
	Image ImageSpec `json:"image,omitempty"`
	// ServerRef resolves the frps server address.
	ServerRef ServerRef `json:"serverRef"`
	// AuthTokenSecret is the shared token to authenticate with frps.
	AuthTokenSecret SecretKeyRef `json:"authTokenSecret"`
	// Admin configures the frpc admin API used for hot reload.
	Admin AdminSpec `json:"admin"`
	// ExtraConfig is raw TOML appended verbatim to frpc.toml.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// FrpClientStatus defines the observed state of FrpClient.
type FrpClientStatus struct {
	// +optional
	Phase Phase `json:"phase,omitempty"`
	// ServerAddress is the resolved frps address (host:port).
	// +optional
	ServerAddress string `json:"serverAddress,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Server",type=string,JSONPath=`.status.serverAddress`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type FrpClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec   FrpClientSpec   `json:"spec,omitempty"`
	Status FrpClientStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type FrpClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FrpClient `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrpClient{}, &FrpClientList{})
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./api/...
```

- [ ] **Step 3: Commit**

```bash
git add api/v1alpha1/frpclient_types.go
git commit -m "feat: add FrpClient CRD types"
```

---

## Task 6: FrpProxy CRD types

**Files:**
- Modify: `api/v1alpha1/frpproxy_types.go`

- [ ] **Step 1: Write FrpProxy types**

```go
// api/v1alpha1/frpproxy_types.go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProxyFinalizer is set on every FrpProxy so deletion triggers a reload.
const ProxyFinalizer = "tunnel.io/proxy-cleanup"

// ClientRef references the parent FrpClient.
type ClientRef struct {
	Name string `json:"name"`
}

// FrpProxySpec defines the desired state of FrpProxy.
type FrpProxySpec struct {
	// ClientRef references the parent FrpClient.
	ClientRef ClientRef `json:"clientRef"`
	// Type is the tunnel protocol.
	// +kubebuilder:validation:Enum=tcp;udp;http;https;stcp;xtcp;tcpmux
	Type string `json:"type"`
	// +optional
	// +kubebuilder:default:="127.0.0.1"
	LocalIP string `json:"localIP,omitempty"`
	// +optional
	LocalPort int32 `json:"localPort,omitempty"`
	// RemotePort is the port exposed on the frps server (tcp/udp only).
	// +optional
	RemotePort int32 `json:"remotePort,omitempty"`
	// CustomDomains for http/https proxy types.
	// +optional
	CustomDomains []string `json:"customDomains,omitempty"`
	// SecretKey for stcp/xtcp proxy types.
	// +optional
	SecretKey string `json:"secretKey,omitempty"`
	// ExtraConfig is raw TOML appended verbatim to this proxy block.
	// +optional
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// FrpProxyStatus defines the observed state of FrpProxy.
type FrpProxyStatus struct {
	// +optional
	Phase Phase `json:"phase,omitempty"`
	// LastReloadTime is when the last successful hot reload occurred.
	// +optional
	LastReloadTime *metav1.Time `json:"lastReloadTime,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Client",type=string,JSONPath=`.spec.clientRef.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="LastReload",type=date,JSONPath=`.status.lastReloadTime`
type FrpProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec   FrpProxySpec   `json:"spec,omitempty"`
	Status FrpProxyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type FrpProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FrpProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrpProxy{}, &FrpProxyList{})
}
```

- [ ] **Step 2: Verify compile + generate deepcopy**

```bash
go build ./api/...
make generate
```
Expected: `api/v1alpha1/zz_generated.deepcopy.go` updated, no errors.

- [ ] **Step 3: Commit**

```bash
git add api/v1alpha1/frpproxy_types.go api/v1alpha1/zz_generated.deepcopy.go
git commit -m "feat: add FrpProxy CRD types with finalizer constant"
```

---

## Task 7: Generate CRD manifests

**Files:**
- Create: `config/crd/bases/tunnel.io_frpservers.yaml` (generated)
- Create: `config/crd/bases/tunnel.io_frpclients.yaml` (generated)
- Create: `config/crd/bases/tunnel.io_frpproxies.yaml` (generated)

- [ ] **Step 1: Generate CRD manifests from kubebuilder markers**

```bash
make manifests
```
Expected: `config/crd/bases/` populated with three YAML files.

- [ ] **Step 2: Verify CRDs are valid**

```bash
kubectl apply --dry-run=client -f config/crd/bases/
```
Expected: `... created (dry run)` for each file, no validation errors.

- [ ] **Step 3: Commit**

```bash
git add config/crd/
git commit -m "chore: generate CRD manifests from type markers"
```

---

## Task 8: Server config renderer + tests

**Files:**
- Create: `internal/config/server.go`
- Create: `internal/config/server_test.go`

- [ ] **Step 1: Write the failing test first**

```go
// internal/config/server_test.go
package config_test

import (
	"strings"
	"testing"

	"tunnel.io/frp-operator/internal/config"
)

func TestRenderServer_BasicBindPort(t *testing.T) {
	cfg := config.ServerParams{
		BindPort:  7000,
		AuthToken: "secret123",
	}
	out, err := config.RenderServer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "bindPort = 7000") {
		t.Errorf("expected bindPort = 7000 in output, got:\n%s", out)
	}
	if !strings.Contains(out, `auth.token = "secret123"`) {
		t.Errorf("expected auth.token in output, got:\n%s", out)
	}
}

func TestRenderServer_DashboardBlock(t *testing.T) {
	port := int32(7500)
	cfg := config.ServerParams{
		BindPort:          7000,
		AuthToken:         "tok",
		DashboardPort:     &port,
		DashboardUser:     "admin",
		DashboardPassword: "pass",
	}
	out, err := config.RenderServer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[webServer]") {
		t.Errorf("expected [webServer] block, got:\n%s", out)
	}
	if !strings.Contains(out, "port = 7500") {
		t.Errorf("expected port = 7500, got:\n%s", out)
	}
}

func TestRenderServer_VhostPorts(t *testing.T) {
	httpPort := int32(80)
	httpsPort := int32(443)
	cfg := config.ServerParams{
		BindPort:       7000,
		AuthToken:      "tok",
		VhostHTTPPort:  &httpPort,
		VhostHTTPSPort: &httpsPort,
	}
	out, err := config.RenderServer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "vhostHTTPPort = 80") {
		t.Errorf("expected vhostHTTPPort = 80, got:\n%s", out)
	}
	if !strings.Contains(out, "vhostHTTPSPort = 443") {
		t.Errorf("expected vhostHTTPSPort = 443, got:\n%s", out)
	}
}

func TestRenderServer_ExtraConfig(t *testing.T) {
	cfg := config.ServerParams{
		BindPort:    7000,
		AuthToken:   "tok",
		ExtraConfig: "maxPoolCount = 5",
	}
	out, err := config.RenderServer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "maxPoolCount = 5") {
		t.Errorf("expected extra config in output, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./internal/config/... -run TestRenderServer -v
```
Expected: compile error — `config.ServerParams` and `config.RenderServer` not defined yet.

- [ ] **Step 3: Implement server config renderer**

```go
// internal/config/server.go
package config

import (
	"bytes"
	"text/template"
)

// ServerParams holds the values needed to render frps.toml.
type ServerParams struct {
	BindPort          int32
	VhostHTTPPort     *int32
	VhostHTTPSPort    *int32
	DashboardPort     *int32
	DashboardUser     string
	DashboardPassword string
	AuthToken         string
	ExtraConfig       string
}

var serverTemplate = template.Must(template.New("frps").Parse(`bindPort = {{ .BindPort }}
{{ if .VhostHTTPPort }}vhostHTTPPort = {{ deref .VhostHTTPPort }}
{{ end -}}
{{ if .VhostHTTPSPort }}vhostHTTPSPort = {{ deref .VhostHTTPSPort }}
{{ end -}}
{{ if .AuthToken }}auth.token = "{{ .AuthToken }}"
{{ end -}}
{{ if .DashboardPort }}
[webServer]
addr = "0.0.0.0"
port = {{ deref .DashboardPort }}
{{ if .DashboardUser }}user = "{{ .DashboardUser }}"
{{ end -}}
{{ if .DashboardPassword }}password = "{{ .DashboardPassword }}"
{{ end -}}
{{ end -}}
{{ if .ExtraConfig }}{{ .ExtraConfig }}
{{ end -}}
`))

func init() {
	serverTemplate.Funcs(template.FuncMap{
		"deref": func(p *int32) int32 {
			if p == nil {
				return 0
			}
			return *p
		},
	})
}

// RenderServer renders frps.toml content from the given params.
func RenderServer(p ServerParams) (string, error) {
	tmpl := template.Must(template.New("frps").Funcs(template.FuncMap{
		"deref": func(p *int32) int32 {
			if p == nil {
				return 0
			}
			return *p
		},
	}).Parse(`bindPort = {{ .BindPort }}
{{ if .VhostHTTPPort }}vhostHTTPPort = {{ deref .VhostHTTPPort }}
{{ end -}}
{{ if .VhostHTTPSPort }}vhostHTTPSPort = {{ deref .VhostHTTPSPort }}
{{ end -}}
{{ if .AuthToken }}auth.token = "{{ .AuthToken }}"
{{ end -}}
{{ if .DashboardPort }}
[webServer]
addr = "0.0.0.0"
port = {{ deref .DashboardPort }}
{{ if .DashboardUser }}user = "{{ .DashboardUser }}"
{{ end -}}
{{ if .DashboardPassword }}password = "{{ .DashboardPassword }}"
{{ end -}}
{{ end -}}
{{ if .ExtraConfig }}{{ .ExtraConfig }}
{{ end -}}
`))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./internal/config/... -run TestRenderServer -v
```
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/server.go internal/config/server_test.go
git commit -m "feat: server config renderer with tests"
```

---

## Task 9: Client config renderer + tests

**Files:**
- Create: `internal/config/client.go`
- Create: `internal/config/client_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/client_test.go
package config_test

import (
	"strings"
	"testing"

	"tunnel.io/frp-operator/internal/config"
)

func TestRenderClient_BaseConfig(t *testing.T) {
	cfg := config.ClientParams{
		ServerAddr: "10.0.0.1",
		ServerPort: 7000,
		AuthToken:  "secret",
		AdminPort:  7400,
		AdminToken: "admintoken",
	}
	out, err := config.RenderClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `serverAddr = "10.0.0.1"`) {
		t.Errorf("expected serverAddr, got:\n%s", out)
	}
	if !strings.Contains(out, "serverPort = 7000") {
		t.Errorf("expected serverPort, got:\n%s", out)
	}
	if !strings.Contains(out, "[webServer]") {
		t.Errorf("expected [webServer] block, got:\n%s", out)
	}
	if !strings.Contains(out, "port = 7400") {
		t.Errorf("expected admin port, got:\n%s", out)
	}
}

func TestRenderClient_TCPProxy(t *testing.T) {
	cfg := config.ClientParams{
		ServerAddr: "10.0.0.1",
		ServerPort: 7000,
		AuthToken:  "secret",
		AdminPort:  7400,
		AdminToken: "admintoken",
		Proxies: []config.ProxyParams{
			{
				Name:       "my-ssh",
				Type:       "tcp",
				LocalIP:    "127.0.0.1",
				LocalPort:  22,
				RemotePort: 6000,
			},
		},
	}
	out, err := config.RenderClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[[proxies]]") {
		t.Errorf("expected [[proxies]] block, got:\n%s", out)
	}
	if !strings.Contains(out, `name = "my-ssh"`) {
		t.Errorf("expected proxy name, got:\n%s", out)
	}
	if !strings.Contains(out, "remotePort = 6000") {
		t.Errorf("expected remotePort, got:\n%s", out)
	}
}

func TestRenderClient_HTTPProxy(t *testing.T) {
	cfg := config.ClientParams{
		ServerAddr: "10.0.0.1",
		ServerPort: 7000,
		AuthToken:  "secret",
		AdminPort:  7400,
		AdminToken: "admintoken",
		Proxies: []config.ProxyParams{
			{
				Name:          "my-web",
				Type:          "http",
				LocalIP:       "127.0.0.1",
				LocalPort:     8080,
				CustomDomains: []string{"web.example.com", "www.example.com"},
			},
		},
	}
	out, err := config.RenderClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `customDomains = ["web.example.com", "www.example.com"]`) {
		t.Errorf("expected customDomains, got:\n%s", out)
	}
}

func TestRenderClient_MultipleProxies(t *testing.T) {
	cfg := config.ClientParams{
		ServerAddr: "10.0.0.1",
		ServerPort: 7000,
		AuthToken:  "secret",
		AdminPort:  7400,
		AdminToken: "admintoken",
		Proxies: []config.ProxyParams{
			{Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6000},
			{Name: "web", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080, CustomDomains: []string{"web.example.com"}},
		},
	}
	out, err := config.RenderClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := strings.Count(out, "[[proxies]]")
	if count != 2 {
		t.Errorf("expected 2 [[proxies]] blocks, got %d in:\n%s", count, out)
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./internal/config/... -run TestRenderClient -v
```
Expected: compile error — `config.ClientParams` and `config.RenderClient` not defined.

- [ ] **Step 3: Implement client config renderer**

```go
// internal/config/client.go
package config

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// ProxyParams holds values for one [[proxies]] block in frpc.toml.
type ProxyParams struct {
	Name          string
	Type          string
	LocalIP       string
	LocalPort     int32
	RemotePort    int32
	CustomDomains []string
	SecretKey     string
	ExtraConfig   string
}

// ClientParams holds the values needed to render frpc.toml.
type ClientParams struct {
	ServerAddr  string
	ServerPort  int32
	AuthToken   string
	AdminPort   int32
	AdminToken  string
	ExtraConfig string
	Proxies     []ProxyParams
}

var clientTmpl = template.Must(template.New("frpc").Funcs(template.FuncMap{
	"joinDomains": func(domains []string) string {
		quoted := make([]string, len(domains))
		for i, d := range domains {
			quoted[i] = fmt.Sprintf("%q", d)
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	},
}).Parse(`serverAddr = "{{ .ServerAddr }}"
serverPort = {{ .ServerPort }}
{{ if .AuthToken }}auth.token = "{{ .AuthToken }}"
{{ end -}}
{{ if .ExtraConfig }}{{ .ExtraConfig }}
{{ end -}}

[webServer]
addr = "0.0.0.0"
port = {{ .AdminPort }}
{{ if .AdminToken }}token = "{{ .AdminToken }}"
{{ end -}}
{{ range .Proxies }}
[[proxies]]
name = "{{ .Name }}"
type = "{{ .Type }}"
{{ if .LocalIP }}localIP = "{{ .LocalIP }}"
{{ end -}}
{{ if .LocalPort }}localPort = {{ .LocalPort }}
{{ end -}}
{{ if .RemotePort }}remotePort = {{ .RemotePort }}
{{ end -}}
{{ if .CustomDomains }}customDomains = {{ joinDomains .CustomDomains }}
{{ end -}}
{{ if .SecretKey }}secretKey = "{{ .SecretKey }}"
{{ end -}}
{{ if .ExtraConfig }}{{ .ExtraConfig }}
{{ end -}}
{{ end -}}
`))

// RenderClient renders frpc.toml content from the given params.
func RenderClient(p ClientParams) (string, error) {
	var buf bytes.Buffer
	if err := clientTmpl.Execute(&buf, p); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./internal/config/... -v
```
Expected: all tests PASS (both server and client).

- [ ] **Step 5: Commit**

```bash
git add internal/config/client.go internal/config/client_test.go
git commit -m "feat: client config renderer with proxy blocks and tests"
```

---

## Task 10: Admin reload client + tests

**Files:**
- Create: `internal/reload/client.go`
- Create: `internal/reload/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/reload/client_test.go
package reload_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"tunnel.io/frp-operator/internal/reload"
)

func TestReload_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/reload" {
			t.Errorf("expected /api/reload, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mytoken" {
			t.Errorf("expected Bearer mytoken, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := reload.NewClient()
	err := c.Reload(context.Background(), srv.URL, "mytoken")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestReload_Non200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := reload.NewClient()
	err := c.Reload(context.Background(), srv.URL, "mytoken")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestReload_ConnectionRefused(t *testing.T) {
	c := reload.NewClient()
	err := c.Reload(context.Background(), "http://127.0.0.1:19999", "mytoken")
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./internal/reload/... -v
```
Expected: compile error — `reload.NewClient` and `reload.Client` not defined.

- [ ] **Step 3: Implement reload client**

```go
// internal/reload/client.go
package reload

import (
	"context"
	"fmt"
	"net/http"
)

// Client calls the frpc admin API to hot-reload tunnel config.
type Client struct {
	httpClient *http.Client
}

// NewClient returns a new reload Client.
func NewClient() *Client {
	return &Client{httpClient: &http.Client{}}
}

// Reload calls POST {adminBaseURL}/api/reload with the given token.
// adminBaseURL should be of the form "http://host:port" (no trailing slash).
func (c *Client) Reload(ctx context.Context, adminBaseURL, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adminBaseURL+"/api/reload", nil)
	if err != nil {
		return fmt.Errorf("building reload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling reload API at %s: %w", adminBaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reload API returned status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./internal/reload/... -v
```
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reload/client.go internal/reload/client_test.go
git commit -m "feat: frpc admin API reload client with tests"
```

---

## Task 11: envtest suite setup

**Files:**
- Modify: `controllers/suite_test.go`

- [ ] **Step 1: Write envtest suite**

```go
// controllers/suite_test.go
package controllers_test

import (
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = tunnelv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	_ = ctrl.SetupSignalHandler()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
```

- [ ] **Step 2: Install test dependencies**

```bash
go get github.com/onsi/ginkgo/v2
go get github.com/onsi/gomega
go mod tidy
```

- [ ] **Step 3: Verify suite compiles**

```bash
go test ./controllers/... -v -run TestControllers 2>&1 | head -20
```
Expected: suite starts (may fail on missing CRD paths — that's fine at this stage).

- [ ] **Step 4: Commit**

```bash
git add controllers/suite_test.go go.mod go.sum
git commit -m "test: add envtest suite for controller tests"
```

---

## Task 12: FrpServer controller + tests

**Files:**
- Modify: `controllers/frpserver_controller.go`
- Create: `controllers/frpserver_controller_test.go`

- [ ] **Step 1: Write the failing controller tests**

```go
// controllers/frpserver_controller_test.go
package controllers_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/controllers"
)

var _ = Describe("FrpServer controller", func() {
	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	BeforeEach(func() {
		// Create the auth token secret
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "frps-auth", Namespace: "default"},
			StringData: map[string]string{"token": "mytoken"},
		}
		_ = k8sClient.Create(ctx, secret)

		// Start the FrpServer controller
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		err = (&controllers.FrpServerReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr)
		Expect(err).NotTo(HaveOccurred())
		go func() { _ = mgr.Start(ctx) }()
	})

	It("creates a Deployment and Service for a new FrpServer", func() {
		server := &tunnelv1alpha1.FrpServer{
			ObjectMeta: metav1.ObjectMeta{Name: "hub", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpServerSpec{
				BindPort: 7000,
				AuthTokenSecret: tunnelv1alpha1.SecretKeyRef{
					Name: "frps-auth", Key: "token",
				},
				Service: tunnelv1alpha1.ServiceSpec{Type: "ClusterIP"},
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())

		// Deployment should be created
		deploy := &appsv1.Deployment{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "hub", Namespace: "default"}, deploy)
		}, timeout, interval).Should(Succeed())

		// Service should be created
		svc := &corev1.Service{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "hub", Namespace: "default"}, svc)
		}, timeout, interval).Should(Succeed())

		// ConfigMap should be created
		cm := &corev1.ConfigMap{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "hub-config", Namespace: "default"}, cm)
		}, timeout, interval).Should(Succeed())

		Expect(cm.Data["frps.toml"]).To(ContainSubstring("bindPort = 7000"))
	})
})
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./controllers/... -v 2>&1 | tail -20
```
Expected: compile error — `controllers.FrpServerReconciler` has no `SetupWithManager` matching or reconcile logic missing.

- [ ] **Step 3: Implement FrpServer controller**

```go
// controllers/frpserver_controller.go
package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/internal/config"
)

// FrpServerReconciler reconciles FrpServer objects.
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
type FrpServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *FrpServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var server tunnelv1alpha1.FrpServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Read auth token
	authToken, err := r.readSecretKey(ctx, server.Namespace, server.Spec.AuthTokenSecret)
	if err != nil {
		return r.setFailedStatus(ctx, &server, "SecretNotFound", err)
	}

	// Optionally read dashboard password
	var dashboardPassword string
	if server.Spec.DashboardPasswordSecret != nil {
		dashboardPassword, err = r.readSecretKey(ctx, server.Namespace, *server.Spec.DashboardPasswordSecret)
		if err != nil {
			return r.setFailedStatus(ctx, &server, "DashboardSecretNotFound", err)
		}
	}

	// Render frps.toml
	toml, err := config.RenderServer(config.ServerParams{
		BindPort:          server.Spec.BindPort,
		VhostHTTPPort:     server.Spec.VhostHTTPPort,
		VhostHTTPSPort:    server.Spec.VhostHTTPSPort,
		DashboardPort:     server.Spec.DashboardPort,
		DashboardUser:     server.Spec.DashboardUser,
		DashboardPassword: dashboardPassword,
		AuthToken:         authToken,
		ExtraConfig:       server.Spec.ExtraConfig,
	})
	if err != nil {
		return r.setFailedStatus(ctx, &server, "ConfigRenderFailed", err)
	}

	if err := r.reconcileConfigMap(ctx, &server, toml); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileDeployment(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileService(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}

	// Update status
	server.Status.Phase = tunnelv1alpha1.PhaseRunning
	server.Status.Port = server.Spec.BindPort
	if err := r.Status().Update(ctx, &server); err != nil {
		logger.Error(err, "failed to update FrpServer status")
	}
	return ctrl.Result{}, nil
}

func (r *FrpServerReconciler) readSecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &secret); err != nil {
		return "", fmt.Errorf("secret %s/%s: %w", ns, ref.Name, err)
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", ref.Key, ns, ref.Name)
	}
	return string(val), nil
}

func (r *FrpServerReconciler) setFailedStatus(ctx context.Context, server *tunnelv1alpha1.FrpServer, reason string, err error) (ctrl.Result, error) {
	server.Status.Phase = tunnelv1alpha1.PhaseFailed
	_ = r.Status().Update(ctx, server)
	return ctrl.Result{}, err
}

func (r *FrpServerReconciler) reconcileConfigMap(ctx context.Context, server *tunnelv1alpha1.FrpServer, toml string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      server.Name + "-config",
			Namespace: server.Namespace,
		},
	}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{"frps.toml": toml}
		return ctrl.SetControllerReference(server, cm, r.Scheme)
	})
	return err
}

func (r *FrpServerReconciler) reconcileDeployment(ctx context.Context, server *tunnelv1alpha1.FrpServer) error {
	replicas := server.Spec.Replicas
	image := server.Spec.Image.Repository + ":" + server.Spec.Image.Tag
	labels := map[string]string{
		"app.kubernetes.io/name":     "frps",
		"app.kubernetes.io/instance": server.Name,
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
	}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "frps",
						Image: image,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/etc/frp",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: server.Name + "-config"},
							},
						},
					}},
				},
			},
		}
		return ctrl.SetControllerReference(server, deploy, r.Scheme)
	})
	return err
}

func (r *FrpServerReconciler) reconcileService(ctx context.Context, server *tunnelv1alpha1.FrpServer) error {
	labels := map[string]string{
		"app.kubernetes.io/name":     "frps",
		"app.kubernetes.io/instance": server.Name,
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
	}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Annotations = server.Spec.Service.Annotations
		svc.Spec.Type = server.Spec.Service.Type
		svc.Spec.Selector = labels
		ports := []corev1.ServicePort{{
			Name:     "frp",
			Port:     server.Spec.BindPort,
			Protocol: corev1.ProtocolTCP,
		}}
		for _, p := range server.Spec.Service.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = corev1.ProtocolTCP
			}
			ports = append(ports, corev1.ServicePort{Name: p.Name, Port: p.Port, Protocol: proto})
		}
		svc.Spec.Ports = ports
		return ctrl.SetControllerReference(server, svc, r.Scheme)
	})
	return err
}

// reconcileServiceAddress reads the Service and populates FrpServer.status.address.
func (r *FrpServerReconciler) reconcileServiceAddress(ctx context.Context, server *tunnelv1alpha1.FrpServer) {
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: server.Name, Namespace: server.Namespace}, &svc); err != nil {
		return
	}
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) > 0 {
		ing := svc.Status.LoadBalancer.Ingress[0]
		if ing.IP != "" {
			server.Status.Address = ing.IP
		} else {
			server.Status.Address = ing.Hostname
		}
	}
	if svc.Spec.Type == corev1.ServiceTypeClusterIP {
		server.Status.Address = svc.Spec.ClusterIP
	}
	_ = errors.IsNotFound(nil) // keep import
}

func (r *FrpServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.FrpServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./controllers/... -v -run "FrpServer" 2>&1 | tail -30
```
Expected: `PASS` — Deployment, Service, ConfigMap created.

- [ ] **Step 5: Commit**

```bash
git add controllers/frpserver_controller.go controllers/frpserver_controller_test.go
git commit -m "feat: FrpServer controller with envtest tests"
```

---

## Task 13: FrpClient controller + tests

**Files:**
- Modify: `controllers/frpclient_controller.go`
- Create: `controllers/frpclient_controller_test.go`

- [ ] **Step 1: Write failing test**

```go
// controllers/frpclient_controller_test.go
package controllers_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/controllers"
)

var _ = Describe("FrpClient controller", func() {
	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	BeforeEach(func() {
		// Create necessary secrets
		for _, s := range []*corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "frpc-auth", Namespace: "default"}, StringData: map[string]string{"token": "authtoken"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "frpc-admin", Namespace: "default"}, StringData: map[string]string{"token": "admintoken"}},
		} {
			_ = k8sClient.Create(ctx, s)
		}

		// Create a FrpServer so serverRef.name can resolve
		server := &tunnelv1alpha1.FrpServer{
			ObjectMeta: metav1.ObjectMeta{Name: "hub", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpServerSpec{
				BindPort: 7000,
				AuthTokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "frpc-auth", Key: "token"},
				Service: tunnelv1alpha1.ServiceSpec{Type: "ClusterIP"},
			},
		}
		_ = k8sClient.Create(ctx, server)
		// Manually patch FrpServer status so FrpClient can resolve the address
		server.Status.Address = "10.0.0.1"
		server.Status.Port = 7000
		_ = k8sClient.Status().Update(ctx, server)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		err = (&controllers.FrpClientReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr)
		Expect(err).NotTo(HaveOccurred())
		go func() { _ = mgr.Start(ctx) }()
	})

	It("creates Deployment, ConfigMap, and admin Service for a new FrpClient with serverRef.name", func() {
		spoke := &tunnelv1alpha1.FrpClient{
			ObjectMeta: metav1.ObjectMeta{Name: "spoke-1", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpClientSpec{
				ServerRef: tunnelv1alpha1.ServerRef{Name: "hub"},
				AuthTokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "frpc-auth", Key: "token"},
				Admin: tunnelv1alpha1.AdminSpec{
					Port:        7400,
					TokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "frpc-admin", Key: "token"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, spoke)).To(Succeed())

		deploy := &appsv1.Deployment{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "spoke-1", Namespace: "default"}, deploy)
		}, timeout, interval).Should(Succeed())

		cm := &corev1.ConfigMap{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "spoke-1-config", Namespace: "default"}, cm)
		}, timeout, interval).Should(Succeed())
		Expect(cm.Data["frpc.toml"]).To(ContainSubstring(`serverAddr = "10.0.0.1"`))

		adminSvc := &corev1.Service{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "spoke-1-admin", Namespace: "default"}, adminSvc)
		}, timeout, interval).Should(Succeed())
	})

	It("uses explicit serverRef address when Name is empty", func() {
		spoke := &tunnelv1alpha1.FrpClient{
			ObjectMeta: metav1.ObjectMeta{Name: "spoke-explicit", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpClientSpec{
				ServerRef: tunnelv1alpha1.ServerRef{Address: "frps.example.com", Port: 7001},
				AuthTokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "frpc-auth", Key: "token"},
				Admin: tunnelv1alpha1.AdminSpec{
					Port:        7400,
					TokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "frpc-admin", Key: "token"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, spoke)).To(Succeed())

		cm := &corev1.ConfigMap{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "spoke-explicit-config", Namespace: "default"}, cm)
		}, timeout, interval).Should(Succeed())
		Expect(cm.Data["frpc.toml"]).To(ContainSubstring(`serverAddr = "frps.example.com"`))
		Expect(cm.Data["frpc.toml"]).To(ContainSubstring("serverPort = 7001"))
	})
})
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./controllers/... -v -run "FrpClient" 2>&1 | tail -20
```
Expected: compile error — `controllers.FrpClientReconciler` not implemented.

- [ ] **Step 3: Implement FrpClient controller**

```go
// controllers/frpclient_controller.go
package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/internal/config"
)

// FrpClientReconciler reconciles FrpClient objects.
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpclients/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
type FrpClientReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *FrpClientReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var spoke tunnelv1alpha1.FrpClient
	if err := r.Get(ctx, req.NamespacedName, &spoke); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve server address
	serverAddr, serverPort, err := r.resolveServerAddr(ctx, &spoke)
	if err != nil {
		spoke.Status.Phase = tunnelv1alpha1.PhaseFailed
		_ = r.Status().Update(ctx, &spoke)
		return ctrl.Result{RequeueAfter: requeueBackoff}, err
	}

	// Read auth token
	authToken, err := r.readSecretKey(ctx, spoke.Namespace, spoke.Spec.AuthTokenSecret)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Read admin token
	adminToken, err := r.readSecretKey(ctx, spoke.Namespace, spoke.Spec.Admin.TokenSecret)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Render frpc.toml (no proxies yet — FrpProxy controller adds them)
	toml, err := config.RenderClient(config.ClientParams{
		ServerAddr:  serverAddr,
		ServerPort:  serverPort,
		AuthToken:   authToken,
		AdminPort:   spoke.Spec.Admin.Port,
		AdminToken:  adminToken,
		ExtraConfig: spoke.Spec.ExtraConfig,
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileConfigMap(ctx, &spoke, toml); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileDeployment(ctx, &spoke); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileAdminService(ctx, &spoke); err != nil {
		return ctrl.Result{}, err
	}

	spoke.Status.Phase = tunnelv1alpha1.PhaseRunning
	spoke.Status.ServerAddress = fmt.Sprintf("%s:%d", serverAddr, serverPort)
	if err := r.Status().Update(ctx, &spoke); err != nil {
		logger.Error(err, "failed to update FrpClient status")
	}
	return ctrl.Result{}, nil
}

func (r *FrpClientReconciler) resolveServerAddr(ctx context.Context, spoke *tunnelv1alpha1.FrpClient) (string, int32, error) {
	ref := spoke.Spec.ServerRef
	if ref.Name != "" {
		var server tunnelv1alpha1.FrpServer
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: spoke.Namespace}, &server); err != nil {
			return "", 0, fmt.Errorf("FrpServer %q not found: %w", ref.Name, err)
		}
		if server.Status.Address == "" {
			return "", 0, fmt.Errorf("FrpServer %q has no status.address yet", ref.Name)
		}
		return server.Status.Address, server.Status.Port, nil
	}
	if ref.Address != "" {
		return ref.Address, ref.Port, nil
	}
	return "", 0, fmt.Errorf("spec.serverRef must have either name or address")
}

func (r *FrpClientReconciler) readSecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &secret); err != nil {
		return "", fmt.Errorf("secret %s/%s: %w", ns, ref.Name, err)
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", ref.Key, ns, ref.Name)
	}
	return string(val), nil
}

func (r *FrpClientReconciler) reconcileConfigMap(ctx context.Context, spoke *tunnelv1alpha1.FrpClient, toml string) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: spoke.Name + "-config", Namespace: spoke.Namespace}}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{"frpc.toml": toml}
		return ctrl.SetControllerReference(spoke, cm, r.Scheme)
	})
	return err
}

func (r *FrpClientReconciler) reconcileDeployment(ctx context.Context, spoke *tunnelv1alpha1.FrpClient) error {
	replicas := int32(1)
	image := spoke.Spec.Image.Repository + ":" + spoke.Spec.Image.Tag
	labels := map[string]string{
		"app.kubernetes.io/name":     "frpc",
		"app.kubernetes.io/instance": spoke.Name,
	}
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: spoke.Name, Namespace: spoke.Namespace}}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "frpc",
						Image: image,
						VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/frp"}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: spoke.Name + "-config"},
							},
						},
					}},
				},
			},
		}
		return ctrl.SetControllerReference(spoke, deploy, r.Scheme)
	})
	return err
}

func (r *FrpClientReconciler) reconcileAdminService(ctx context.Context, spoke *tunnelv1alpha1.FrpClient) error {
	labels := map[string]string{
		"app.kubernetes.io/name":     "frpc",
		"app.kubernetes.io/instance": spoke.Name,
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: spoke.Name + "-admin", Namespace: spoke.Namespace}}
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = labels
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:     "admin",
			Port:     spoke.Spec.Admin.Port,
			Protocol: corev1.ProtocolTCP,
		}}
		return ctrl.SetControllerReference(spoke, svc, r.Scheme)
	})
	return err
}

func (r *FrpClientReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.FrpClient{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./controllers/... -v -run "FrpClient" 2>&1 | tail -30
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controllers/frpclient_controller.go controllers/frpclient_controller_test.go
git commit -m "feat: FrpClient controller with serverRef resolution and envtest tests"
```

---

## Task 14: FrpProxy controller + tests

**Files:**
- Modify: `controllers/frpproxy_controller.go`
- Create: `controllers/frpproxy_controller_test.go`

- [ ] **Step 1: Add requeueBackoff constant to shared file**

Create `controllers/constants.go`:

```go
// controllers/constants.go
package controllers

import "time"

const requeueBackoff = 10 * time.Second
```

- [ ] **Step 2: Write failing test**

```go
// controllers/frpproxy_controller_test.go
package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/controllers"
	"tunnel.io/frp-operator/internal/reload"
)

var _ = Describe("FrpProxy controller", func() {
	const timeout = 10 * time.Second
	const interval = 250 * time.Millisecond
	ctx := context.Background()

	var reloadCalled bool
	var mockAdminServer *httptest.Server

	BeforeEach(func() {
		reloadCalled = false
		mockAdminServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/reload" && r.Method == http.MethodPost {
				reloadCalled = true
			}
			w.WriteHeader(http.StatusOK)
		}))

		for _, s := range []*corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "proxy-auth", Namespace: "default"}, StringData: map[string]string{"token": "authtoken"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "proxy-admin", Namespace: "default"}, StringData: map[string]string{"token": "admintoken"}},
		} {
			_ = k8sClient.Create(ctx, s)
		}

		// Pre-create FrpClient with status
		spoke := &tunnelv1alpha1.FrpClient{
			ObjectMeta: metav1.ObjectMeta{Name: "spoke-proxy-test", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpClientSpec{
				ServerRef:       tunnelv1alpha1.ServerRef{Address: "10.0.0.1", Port: 7000},
				AuthTokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "proxy-auth", Key: "token"},
				Admin: tunnelv1alpha1.AdminSpec{
					Port:        7400,
					TokenSecret: tunnelv1alpha1.SecretKeyRef{Name: "proxy-admin", Key: "token"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, spoke)).To(Succeed())
		spoke.Status.Phase = tunnelv1alpha1.PhaseRunning
		_ = k8sClient.Status().Update(ctx, spoke)

		// Pre-create ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "spoke-proxy-test-config", Namespace: "default"},
			Data:       map[string]string{"frpc.toml": `serverAddr = "10.0.0.1"` + "\nserverPort = 7000\n"},
		}
		_ = k8sClient.Create(ctx, cm)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		reloadClient := reload.NewClient()
		err = (&controllers.FrpProxyReconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			ReloadClient: reloadClient,
			AdminBaseURLFunc: func(spokeName, namespace string, adminPort int32) string {
				return mockAdminServer.URL
			},
		}).SetupWithManager(mgr)
		Expect(err).NotTo(HaveOccurred())
		go func() { _ = mgr.Start(ctx) }()
	})

	AfterEach(func() {
		mockAdminServer.Close()
	})

	It("updates the ConfigMap and calls reload when a proxy is created", func() {
		proxy := &tunnelv1alpha1.FrpProxy{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ssh", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpProxySpec{
				ClientRef:  tunnelv1alpha1.ClientRef{Name: "spoke-proxy-test"},
				Type:       "tcp",
				LocalIP:    "127.0.0.1",
				LocalPort:  22,
				RemotePort: 6000,
			},
		}
		Expect(k8sClient.Create(ctx, proxy)).To(Succeed())

		cm := &corev1.ConfigMap{}
		Eventually(func() string {
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "spoke-proxy-test-config", Namespace: "default"}, cm)
			return cm.Data["frpc.toml"]
		}, timeout, interval).Should(ContainSubstring("remotePort = 6000"))

		Eventually(func() bool { return reloadCalled }, timeout, interval).Should(BeTrue())
	})

	It("adds a finalizer to FrpProxy on creation", func() {
		proxy := &tunnelv1alpha1.FrpProxy{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ssh-fin", Namespace: "default"},
			Spec: tunnelv1alpha1.FrpProxySpec{
				ClientRef: tunnelv1alpha1.ClientRef{Name: "spoke-proxy-test"},
				Type:      "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6001,
			},
		}
		Expect(k8sClient.Create(ctx, proxy)).To(Succeed())

		Eventually(func() []string {
			p := &tunnelv1alpha1.FrpProxy{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "my-ssh-fin", Namespace: "default"}, p)
			return p.Finalizers
		}, timeout, interval).Should(ContainElement(tunnelv1alpha1.ProxyFinalizer))
	})
})
```

- [ ] **Step 3: Run test — verify it fails**

```bash
go test ./controllers/... -v -run "FrpProxy" 2>&1 | tail -20
```
Expected: compile error — `controllers.FrpProxyReconciler` not implemented.

- [ ] **Step 4: Implement FrpProxy controller**

```go
// controllers/frpproxy_controller.go
package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/internal/config"
	"tunnel.io/frp-operator/internal/reload"
)

// AdminBaseURLFunc builds the admin API base URL for a given FrpClient.
// Injected so tests can override with a mock server URL.
type AdminBaseURLFunc func(spokeName, namespace string, adminPort int32) string

// DefaultAdminBaseURL returns the in-cluster URL for the frpc admin Service.
func DefaultAdminBaseURL(spokeName, namespace string, adminPort int32) string {
	return fmt.Sprintf("http://%s-admin.%s.svc.cluster.local:%d", spokeName, namespace, adminPort)
}

// FrpProxyReconciler reconciles FrpProxy objects.
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tunnel.tunnel.io,resources=frpproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
type FrpProxyReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ReloadClient     *reload.Client
	AdminBaseURLFunc AdminBaseURLFunc
}

func (r *FrpProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var proxy tunnelv1alpha1.FrpProxy
	if err := r.Get(ctx, req.NamespacedName, &proxy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !proxy.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &proxy)
	}

	// Ensure finalizer
	if !controllerutil.ContainsFinalizer(&proxy, tunnelv1alpha1.ProxyFinalizer) {
		controllerutil.AddFinalizer(&proxy, tunnelv1alpha1.ProxyFinalizer)
		if err := r.Update(ctx, &proxy); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Fetch parent FrpClient
	var spoke tunnelv1alpha1.FrpClient
	if err := r.Get(ctx, types.NamespacedName{Name: proxy.Spec.ClientRef.Name, Namespace: proxy.Namespace}, &spoke); err != nil {
		return ctrl.Result{RequeueAfter: requeueBackoff}, err
	}

	// Collect all FrpProxy resources for this client
	proxies, err := r.listProxiesForClient(ctx, proxy.Namespace, spoke.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Re-render full frpc.toml
	if err := r.rebuildAndReload(ctx, &spoke, proxies, logger); err != nil {
		proxy.Status.Phase = tunnelv1alpha1.PhaseFailed
		_ = r.Status().Update(ctx, &proxy)
		return ctrl.Result{RequeueAfter: requeueBackoff}, err
	}

	now := metav1.Now()
	proxy.Status.Phase = tunnelv1alpha1.PhaseRunning
	proxy.Status.LastReloadTime = &now
	_ = r.Status().Update(ctx, &proxy)
	return ctrl.Result{}, nil
}

func (r *FrpProxyReconciler) handleDeletion(ctx context.Context, proxy *tunnelv1alpha1.FrpProxy) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(proxy, tunnelv1alpha1.ProxyFinalizer) {
		return ctrl.Result{}, nil
	}

	var spoke tunnelv1alpha1.FrpClient
	if err := r.Get(ctx, types.NamespacedName{Name: proxy.Spec.ClientRef.Name, Namespace: proxy.Namespace}, &spoke); err == nil {
		// Collect proxies excluding the one being deleted
		proxies, _ := r.listProxiesForClient(ctx, proxy.Namespace, spoke.Name)
		remaining := make([]tunnelv1alpha1.FrpProxy, 0, len(proxies))
		for _, p := range proxies {
			if p.Name != proxy.Name {
				remaining = append(remaining, p)
			}
		}
		_ = r.rebuildAndReload(ctx, &spoke, remaining, nil)
	}

	controllerutil.RemoveFinalizer(proxy, tunnelv1alpha1.ProxyFinalizer)
	return ctrl.Result{}, r.Update(ctx, proxy)
}

func (r *FrpProxyReconciler) listProxiesForClient(ctx context.Context, ns, clientName string) ([]tunnelv1alpha1.FrpProxy, error) {
	var list tunnelv1alpha1.FrpProxyList
	if err := r.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	var result []tunnelv1alpha1.FrpProxy
	for _, p := range list.Items {
		if p.Spec.ClientRef.Name == clientName && p.DeletionTimestamp.IsZero() {
			result = append(result, p)
		}
	}
	return result, nil
}

func (r *FrpProxyReconciler) rebuildAndReload(ctx context.Context, spoke *tunnelv1alpha1.FrpClient, proxies []tunnelv1alpha1.FrpProxy, logger interface{ Error(error, string, ...interface{}) }) error {
	authToken, err := r.readSecretKey(ctx, spoke.Namespace, spoke.Spec.AuthTokenSecret)
	if err != nil {
		return err
	}
	adminToken, err := r.readSecretKey(ctx, spoke.Namespace, spoke.Spec.Admin.TokenSecret)
	if err != nil {
		return err
	}

	serverAddr := spoke.Spec.ServerRef.Address
	serverPort := spoke.Spec.ServerRef.Port
	if spoke.Spec.ServerRef.Name != "" {
		var server tunnelv1alpha1.FrpServer
		if err := r.Get(ctx, types.NamespacedName{Name: spoke.Spec.ServerRef.Name, Namespace: spoke.Namespace}, &server); err == nil {
			serverAddr = server.Status.Address
			serverPort = server.Status.Port
		}
	}

	proxyParams := make([]config.ProxyParams, 0, len(proxies))
	for _, p := range proxies {
		proxyParams = append(proxyParams, config.ProxyParams{
			Name:          p.Name,
			Type:          p.Spec.Type,
			LocalIP:       p.Spec.LocalIP,
			LocalPort:     p.Spec.LocalPort,
			RemotePort:    p.Spec.RemotePort,
			CustomDomains: p.Spec.CustomDomains,
			SecretKey:     p.Spec.SecretKey,
			ExtraConfig:   p.Spec.ExtraConfig,
		})
	}

	toml, err := config.RenderClient(config.ClientParams{
		ServerAddr:  serverAddr,
		ServerPort:  serverPort,
		AuthToken:   authToken,
		AdminPort:   spoke.Spec.Admin.Port,
		AdminToken:  adminToken,
		ExtraConfig: spoke.Spec.ExtraConfig,
		Proxies:     proxyParams,
	})
	if err != nil {
		return fmt.Errorf("rendering frpc.toml: %w", err)
	}

	// Update ConfigMap
	var cm corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{Name: spoke.Name + "-config", Namespace: spoke.Namespace}, &cm); err != nil {
		return fmt.Errorf("getting configmap: %w", err)
	}
	cm.Data = map[string]string{"frpc.toml": toml}
	if err := r.Update(ctx, &cm); err != nil {
		return fmt.Errorf("updating configmap: %w", err)
	}

	// Call admin API reload
	adminURL := r.AdminBaseURLFunc(spoke.Name, spoke.Namespace, spoke.Spec.Admin.Port)
	if err := r.ReloadClient.Reload(ctx, adminURL, adminToken); err != nil {
		return fmt.Errorf("reload API: %w", err)
	}
	return nil
}

func (r *FrpProxyReconciler) readSecretKey(ctx context.Context, ns string, ref tunnelv1alpha1.SecretKeyRef) (string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &secret); err != nil {
		return "", fmt.Errorf("secret %s/%s: %w", ns, ref.Name, err)
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", ref.Key, ns, ref.Name)
	}
	return string(val), nil
}

func (r *FrpProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunnelv1alpha1.FrpProxy{}).
		Complete(r)
}
```

- [ ] **Step 5: Run all controller tests**

```bash
go test ./controllers/... -v 2>&1 | tail -40
```
Expected: all controller tests PASS.

- [ ] **Step 6: Commit**

```bash
git add controllers/frpproxy_controller.go controllers/frpproxy_controller_test.go controllers/constants.go
git commit -m "feat: FrpProxy controller with hot reload and finalizer"
```

---

## Task 15: Wire main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Replace scaffolded main.go**

```go
// main.go
package main

import (
	"flag"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	tunnelv1alpha1 "tunnel.io/frp-operator/api/v1alpha1"
	"tunnel.io/frp-operator/controllers"
	"tunnel.io/frp-operator/internal/reload"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tunnelv1alpha1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "tunnel.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.FrpServerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create FrpServer controller")
		os.Exit(1)
	}

	if err = (&controllers.FrpClientReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create FrpClient controller")
		os.Exit(1)
	}

	if err = (&controllers.FrpProxyReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ReloadClient:     reload.NewClient(),
		AdminBaseURLFunc: controllers.DefaultAdminBaseURL,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create FrpProxy controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify full build**

```bash
go build ./...
```
Expected: binary produced, no errors.

- [ ] **Step 3: Run all tests**

```bash
go test ./... 2>&1 | tail -20
```
Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: wire all three controllers in main.go"
```

---

## Task 16: Kustomize config

**Files:**
- Create: `config/rbac/service_account.yaml`
- Create: `config/rbac/cluster_role.yaml`
- Create: `config/rbac/cluster_role_binding.yaml`
- Create: `config/rbac/kustomization.yaml`
- Create: `config/manager/deployment.yaml`
- Create: `config/manager/kustomization.yaml`
- Create: `config/default/namespace.yaml`
- Create: `config/default/kustomization.yaml`
- Modify: `config/crd/kustomization.yaml`

- [ ] **Step 1: Re-generate RBAC from kubebuilder markers**

```bash
make manifests
```
Expected: `config/rbac/` files updated from `+kubebuilder:rbac` markers.

- [ ] **Step 2: Write ServiceAccount**

```yaml
# config/rbac/service_account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: frp-operator
  namespace: frp-system
```

- [ ] **Step 3: Write ClusterRole**

```yaml
# config/rbac/cluster_role.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: frp-operator-role
rules:
  - apiGroups: ["tunnel.tunnel.io"]
    resources: ["frpservers", "frpclients", "frpproxies"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["tunnel.tunnel.io"]
    resources: ["frpservers/status", "frpclients/status", "frpproxies/status"]
    verbs: ["get", "update", "patch"]
  - apiGroups: ["tunnel.tunnel.io"]
    resources: ["frpproxies/finalizers"]
    verbs: ["update"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["configmaps", "services", "secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
```

- [ ] **Step 4: Write ClusterRoleBinding**

```yaml
# config/rbac/cluster_role_binding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: frp-operator-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: frp-operator-role
subjects:
  - kind: ServiceAccount
    name: frp-operator
    namespace: frp-system
```

- [ ] **Step 5: Write rbac kustomization**

```yaml
# config/rbac/kustomization.yaml
resources:
  - service_account.yaml
  - cluster_role.yaml
  - cluster_role_binding.yaml
```

- [ ] **Step 6: Write operator Deployment**

```yaml
# config/manager/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frp-operator
  namespace: frp-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: frp-operator
  template:
    metadata:
      labels:
        app: frp-operator
    spec:
      serviceAccountName: frp-operator
      containers:
        - name: manager
          image: your.registry.io/frp-operator:latest
          args:
            - --leader-elect
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8081
            initialDelaySeconds: 15
            periodSeconds: 20
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8081
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            limits:
              cpu: 200m
              memory: 128Mi
            requests:
              cpu: 10m
              memory: 64Mi
      terminationGracePeriodSeconds: 10
```

- [ ] **Step 7: Write manager kustomization**

```yaml
# config/manager/kustomization.yaml
resources:
  - deployment.yaml
```

- [ ] **Step 8: Write namespace**

```yaml
# config/default/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: frp-system
```

- [ ] **Step 9: Write default kustomization**

```yaml
# config/default/kustomization.yaml
resources:
  - namespace.yaml
  - ../crd
  - ../rbac
  - ../manager
```

- [ ] **Step 10: Write CRD kustomization**

```yaml
# config/crd/kustomization.yaml
resources:
  - bases/tunnel.io_frpservers.yaml
  - bases/tunnel.io_frpclients.yaml
  - bases/tunnel.io_frpproxies.yaml
```

- [ ] **Step 11: Verify kustomize renders without error**

```bash
kubectl kustomize config/default
```
Expected: full YAML manifest printed to stdout, no errors.

- [ ] **Step 12: Commit**

```bash
git add config/
git commit -m "feat: kustomize config for CRDs, RBAC, operator deployment"
```

---

## Task 17: Sample CRs

**Files:**
- Create: `config/samples/frpserver-sample.yaml`
- Create: `config/samples/frpserver-gcp-nlb.yaml`
- Create: `config/samples/frpserver-aws-nlb.yaml`
- Create: `config/samples/frpclient-sample.yaml`
- Create: `config/samples/frpproxy-sample.yaml`

- [ ] **Step 1: Write frpserver-sample.yaml**

```yaml
# config/samples/frpserver-sample.yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: FrpServer
metadata:
  name: hub
  namespace: default
spec:
  bindPort: 7000
  authTokenSecret:
    name: frps-auth-token
    key: token
  service:
    type: LoadBalancer
    ports:
      - name: frp
        port: 7000
        protocol: TCP
```

- [ ] **Step 2: Write frpserver-gcp-nlb.yaml**

```yaml
# config/samples/frpserver-gcp-nlb.yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: FrpServer
metadata:
  name: hub-gcp
  namespace: default
spec:
  bindPort: 7000
  vhostHTTPPort: 80
  vhostHTTPSPort: 443
  authTokenSecret:
    name: frps-auth-token
    key: token
  service:
    type: LoadBalancer
    annotations:
      networking.gke.io/load-balancer-type: "External"
      cloud.google.com/load-balancer-type: "External"
    ports:
      - name: frp
        port: 7000
        protocol: TCP
      - name: vhost-http
        port: 80
        protocol: TCP
      - name: vhost-https
        port: 443
        protocol: TCP
```

- [ ] **Step 3: Write frpserver-aws-nlb.yaml**

```yaml
# config/samples/frpserver-aws-nlb.yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: FrpServer
metadata:
  name: hub-aws
  namespace: default
spec:
  bindPort: 7000
  authTokenSecret:
    name: frps-auth-token
    key: token
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
    ports:
      - name: frp
        port: 7000
        protocol: TCP
```

- [ ] **Step 4: Write frpclient-sample.yaml**

```yaml
# config/samples/frpclient-sample.yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: FrpClient
metadata:
  name: spoke-1
  namespace: default
spec:
  serverRef:
    name: hub
  authTokenSecret:
    name: frps-auth-token
    key: token
  admin:
    port: 7400
    tokenSecret:
      name: frpc-admin-token
      key: token
```

- [ ] **Step 5: Write frpproxy-sample.yaml**

```yaml
# config/samples/frpproxy-sample.yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: FrpProxy
metadata:
  name: my-ssh
  namespace: default
spec:
  clientRef:
    name: spoke-1
  type: tcp
  localIP: 127.0.0.1
  localPort: 22
  remotePort: 6000
---
apiVersion: tunnel.tunnel.io/v1alpha1
kind: FrpProxy
metadata:
  name: my-web
  namespace: default
spec:
  clientRef:
    name: spoke-1
  type: http
  localIP: 127.0.0.1
  localPort: 8080
  customDomains:
    - web.example.com
```

- [ ] **Step 6: Commit**

```bash
git add config/samples/
git commit -m "docs: add sample CRs for FrpServer, FrpClient, FrpProxy"
```

---

## Task 18: Makefile docker targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Append docker targets to the generated Makefile**

Add these targets to the bottom of the existing `Makefile` (after the existing kubebuilder-generated targets):

```makefile
# --- Docker multi-platform build targets ---

REGISTRY ?= your.registry.io
FRP_VERSION ?= v0.61.0
OPERATOR_VERSION ?= latest
PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7

.PHONY: docker-build docker-build-frps docker-build-frpc docker-build-operator docker-buildx-setup

docker-buildx-setup: ## Ensure buildx builder with multi-platform support exists
	docker buildx inspect frp-builder > /dev/null 2>&1 || \
	docker buildx create --name frp-builder --use --bootstrap

docker-build-frps: docker-buildx-setup ## Build and push multi-platform frps image
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg FRP_VERSION=$(FRP_VERSION) \
		--push \
		-t $(REGISTRY)/frps:$(FRP_VERSION) \
		-t $(REGISTRY)/frps:latest \
		-f docker/Dockerfile.frps .

docker-build-frpc: docker-buildx-setup ## Build and push multi-platform frpc image
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg FRP_VERSION=$(FRP_VERSION) \
		--push \
		-t $(REGISTRY)/frpc:$(FRP_VERSION) \
		-t $(REGISTRY)/frpc:latest \
		-f docker/Dockerfile.frpc .

docker-build-operator: docker-buildx-setup ## Build and push multi-platform operator image
	docker buildx build \
		--platform $(PLATFORMS) \
		--push \
		-t $(REGISTRY)/frp-operator:$(OPERATOR_VERSION) \
		.

docker-build: docker-build-frps docker-build-frpc ## Build and push all FRP images (frps + frpc)
	@echo "All FRP images pushed: $(REGISTRY)/frps:$(FRP_VERSION) and $(REGISTRY)/frpc:$(FRP_VERSION)"
```

- [ ] **Step 2: Verify make targets are listed**

```bash
make help 2>/dev/null || make --print-data-base 2>/dev/null | grep "^docker-"
```
Expected: `docker-build`, `docker-build-frps`, `docker-build-frpc`, `docker-build-operator` listed.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: add multi-platform docker build targets to Makefile"
```

---

## Final verification

- [ ] **Run all tests**

```bash
go test ./... -v 2>&1 | tail -30
```
Expected: all unit and controller tests PASS.

- [ ] **Build operator binary**

```bash
go build -o bin/frp-operator .
```
Expected: `bin/frp-operator` binary produced, no errors.

- [ ] **Dry-run install CRDs**

```bash
kubectl apply --dry-run=client -k config/crd
```
Expected: all three CRDs `created (dry run)`.

- [ ] **Dry-run full deploy**

```bash
kubectl apply --dry-run=client -k config/default
```
Expected: all resources `created (dry run)`.
