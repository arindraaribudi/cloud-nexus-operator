# frps-webhook Separate Deployment + frp v0.68.1 Upgrade — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the frps httpPlugin webhook server from the controller-manager pod into its own dedicated Deployment + image so it survives operator restarts, and upgrade frp binaries from v0.61.0 to v0.68.1.

**Architecture:** A new `cmd/frps-webhook/main.go` binary runs a controller-runtime Manager with zero controllers — only `internal/webhook.HTTPServer` registered as a Runnable. This is built into a dedicated `crd-tunnel-webhook` image. New kustomize resources in `config/webhook-server/` deploy it with its own ServiceAccount and ClusterRole. The existing `controller-manager-frps-plugin` Service selector is updated to point at the new pods. The operator's `cmd/main.go` drops the `mgr.Add(webhook.NewHTTPServer(...))` call entirely.

**Tech Stack:** Go 1.25, controller-runtime v0.23.3, kustomize v5, Docker buildx multi-platform, frp v0.68.1

---

## File Map

| File | Action |
|------|--------|
| `internal/config/client.go` | Modify — remove `transport.poolCount = 0` from template |
| `internal/config/client_test.go` | Modify — remove poolCount assertion |
| `cmd/frps-webhook/main.go` | Create — webhook-only manager binary |
| `docker/Dockerfile.webhook` | Create — builds crd-tunnel-webhook image |
| `config/webhook-server/kustomization.yaml` | Create — lists webhook-server resources |
| `config/webhook-server/deployment.yaml` | Create — frps-webhook Deployment |
| `config/webhook-server/service_account.yaml` | Create — frps-webhook ServiceAccount |
| `config/webhook-server/cluster_role.yaml` | Create — ClusterRole for Services CRUD |
| `config/webhook-server/cluster_role_binding.yaml` | Create — binds role to webhook SA |
| `config/default/frps_plugin_service.yaml` | Modify — selector: `app: frps-webhook` |
| `config/default/kustomization.yaml` | Modify — add `- ../webhook-server` resource |
| `config/manager/manager.yaml` | Modify — remove port 9090 from container |
| `cmd/main.go` | Modify — remove webhook import + mgr.Add call |
| `Makefile` | Modify — WEBHOOK_VERSION, docker-build-webhook target, version bumps |
| `docker/Dockerfile.frp` | Modify — ARG FRP_VERSION=v0.68.1 |

---

### Task 1: Remove dead `transport.poolCount = 0` config

**Files:**
- Modify: `internal/config/client_test.go:48-50`
- Modify: `internal/config/client.go:29`

- [ ] **Step 1: Remove the poolCount assertion from the test**

In `internal/config/client_test.go`, delete lines 48-50:
```go
	if !strings.Contains(out, "transport.poolCount = 0") {
		t.Errorf("expected transport.poolCount = 0 in output, got:\n%s", out)
	}
```

The function `TestRenderFrpcToml_BaseConfig` should end after the `webServer.port` check.

- [ ] **Step 2: Run tests — should pass (poolCount still in template)**

```bash
go test ./internal/config/... -v -run TestRenderFrpcToml_BaseConfig
```

Expected: PASS (the assertion is gone, so the presence of poolCount doesn't matter yet)

- [ ] **Step 3: Remove `transport.poolCount = 0` from the frpc template**

In `internal/config/client.go`, change `frpcBaseTemplate` from:
```go
var frpcBaseTemplate = template.Must(template.New("frpc-base").Parse(`serverAddr = "{{ .ServerAddr }}"
serverPort = {{ .ServerPort }}
transport.poolCount = 0
{{- if .AuthToken }}
```
to:
```go
var frpcBaseTemplate = template.Must(template.New("frpc-base").Parse(`serverAddr = "{{ .ServerAddr }}"
serverPort = {{ .ServerPort }}
{{- if .AuthToken }}
```

- [ ] **Step 4: Run all config tests**

```bash
go test ./internal/config/... -v
```

Expected: all PASS, no mention of `transport.poolCount`

- [ ] **Step 5: Commit**

```bash
git add internal/config/client.go internal/config/client_test.go
git commit -m "fix: remove no-op transport.poolCount=0 from frpc template

frp's Complete() uses util.EmptyOr(c.PoolCount, 1) which replaces 0
with 1, making the config line silently ignored.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Create `cmd/frps-webhook/main.go`

**Files:**
- Create: `cmd/frps-webhook/main.go`

- [ ] **Step 1: Create the directory and file**

Create `cmd/frps-webhook/main.go` with this content:

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
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"tunnel.io/frp-operator/internal/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	var probeAddr string
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	if err := mgr.Add(webhook.NewHTTPServer(":9090", mgr.GetClient())); err != nil {
		setupLog.Error(err, "Failed to register frps plugin webhook server")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting frps-webhook server")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run frps-webhook")
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/frps-webhook/...
```

Expected: exits 0, no output, produces `frps-webhook` binary in current dir (clean up: `rm -f frps-webhook`)

- [ ] **Step 3: Commit**

```bash
git add cmd/frps-webhook/main.go
git commit -m "feat: add cmd/frps-webhook standalone binary

Runs a controller-runtime Manager with only the frps HTTPServer
Runnable — no controllers. Survives operator restarts independently.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Create `docker/Dockerfile.webhook`

**Files:**
- Create: `docker/Dockerfile.webhook`

- [ ] **Step 1: Create the Dockerfile**

Create `docker/Dockerfile.webhook`:

```dockerfile
# Build the frps-webhook binary
FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o frps-webhook cmd/frps-webhook/main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/frps-webhook .
USER 65532:65532

ENTRYPOINT ["/frps-webhook"]
```

- [ ] **Step 2: Verify it builds locally (single platform)**

```bash
docker build -t crd-tunnel-webhook:local-test -f docker/Dockerfile.webhook .
```

Expected: Build succeeds, image tagged `crd-tunnel-webhook:local-test`

- [ ] **Step 3: Commit**

```bash
git add docker/Dockerfile.webhook
git commit -m "feat: add Dockerfile.webhook for standalone frps-webhook image

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Create kustomize resources for `config/webhook-server/`

**Files:**
- Create: `config/webhook-server/kustomization.yaml`
- Create: `config/webhook-server/service_account.yaml`
- Create: `config/webhook-server/cluster_role.yaml`
- Create: `config/webhook-server/cluster_role_binding.yaml`
- Create: `config/webhook-server/deployment.yaml`

- [ ] **Step 1: Create `config/webhook-server/kustomization.yaml`**

```yaml
resources:
- service_account.yaml
- cluster_role.yaml
- cluster_role_binding.yaml
- deployment.yaml
```

- [ ] **Step 2: Create `config/webhook-server/service_account.yaml`**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  labels:
    app.kubernetes.io/name: crd-tunnel-service
    app.kubernetes.io/managed-by: kustomize
  name: frps-webhook
  namespace: system
```

- [ ] **Step 3: Create `config/webhook-server/cluster_role.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: frps-webhook-role
rules:
- apiGroups:
  - ""
  resources:
  - services
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
```

- [ ] **Step 4: Create `config/webhook-server/cluster_role_binding.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  labels:
    app.kubernetes.io/name: crd-tunnel-service
    app.kubernetes.io/managed-by: kustomize
  name: frps-webhook-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: frps-webhook-role
subjects:
- kind: ServiceAccount
  name: frps-webhook
  namespace: system
```

- [ ] **Step 5: Create `config/webhook-server/deployment.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frps-webhook
  namespace: system
  labels:
    app: frps-webhook
    app.kubernetes.io/name: crd-tunnel-service
    app.kubernetes.io/managed-by: kustomize
spec:
  selector:
    matchLabels:
      app: frps-webhook
  replicas: 1
  template:
    metadata:
      labels:
        app: frps-webhook
    spec:
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
      - command:
        - /frps-webhook
        args:
          - --health-probe-bind-address=:8082
        image: asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel-webhook:1.0.0
        name: frps-webhook
        ports:
        - containerPort: 9090
          name: frps-plugin
          protocol: TCP
        securityContext:
          readOnlyRootFilesystem: true
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - "ALL"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8082
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8082
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          limits:
            cpu: 100m
            memory: 64Mi
          requests:
            cpu: 10m
            memory: 32Mi
        volumeMounts: []
      volumes: []
      serviceAccountName: frps-webhook
      terminationGracePeriodSeconds: 10
```

- [ ] **Step 6: Update `config/default/frps_plugin_service.yaml` selector**

The selector currently matches the controller-manager pod. Replace it so it selects the webhook pod instead:

```yaml
# Service that exposes the frps-webhook server endpoint.
# frps calls POST /frps/events when proxies connect/disconnect.
apiVersion: v1
kind: Service
metadata:
  name: controller-manager-frps-plugin
  namespace: system
spec:
  selector:
    app: frps-webhook
  ports:
    - name: frps-plugin
      port: 9090
      targetPort: 9090
      protocol: TCP
```

Note: The Service name stays `controller-manager-frps-plugin` (becomes `crd-tunnel-service-controller-manager-frps-plugin` after kustomize namePrefix). The frps config renders this address as `controller-manager-frps-plugin.<namespace>.svc.cluster.local:9090` — no address change required.

- [ ] **Step 7: Add `webhook-server` to `config/default/kustomization.yaml`**

In `config/default/kustomization.yaml`, add `- ../webhook-server` after the `- ../manager` line:

```yaml
resources:
- ../crd
- ../rbac
- ../manager
- ../webhook-server
# [WEBHOOK] To enable webhook, uncomment all the sections with [WEBHOOK] prefix including the one in
```

- [ ] **Step 8: Verify kustomize build succeeds**

```bash
bin/kustomize build config/default 2>&1 | head -40
```

Expected: Valid YAML output, no errors. You should see both a `Deployment` named `crd-tunnel-service-controller-manager` and one named `crd-tunnel-service-frps-webhook`.

If `bin/kustomize` is not present, run `make kustomize` first.

- [ ] **Step 9: Commit**

```bash
git add config/webhook-server/ config/default/frps_plugin_service.yaml config/default/kustomization.yaml
git commit -m "feat: add config/webhook-server kustomize resources

Adds Deployment, ServiceAccount, ClusterRole, and ClusterRoleBinding
for the standalone frps-webhook. Updates frps_plugin_service.yaml
selector from controller-manager to frps-webhook pod label.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Remove webhook from controller-manager

**Files:**
- Modify: `cmd/main.go:40,212-215`
- Modify: `config/manager/manager.yaml:69-71`

- [ ] **Step 1: Remove the webhook import and mgr.Add call from `cmd/main.go`**

Remove line 40 (the webhook import):
```go
	"tunnel.io/frp-operator/internal/webhook"
```

Remove lines 212-215 (the mgr.Add call):
```go
	if err := mgr.Add(webhook.NewHTTPServer(":9090", mgr.GetClient())); err != nil {
		setupLog.Error(err, "Failed to register frps plugin webhook server")
		os.Exit(1)
	}
```

The blank line after `// +kubebuilder:scaffold:builder` (line 210) that previously separated the builder section from the webhook.Add call can also be removed.

- [ ] **Step 2: Remove port 9090 from `config/manager/manager.yaml`**

Remove the frps-plugin port from the container ports section. The ports block:
```yaml
        ports:
        - containerPort: 9090
          name: frps-plugin
          protocol: TCP
```
should be deleted entirely from the manager container spec (it will have no ports declared, which is fine — metrics uses a patch).

- [ ] **Step 3: Verify operator still compiles**

```bash
go build ./cmd/...
```

Expected: exits 0, no "imported and not used" errors, no reference to `webhook` package in the binary entrypoint

- [ ] **Step 4: Run all tests**

```bash
go test ./... 2>&1 | tail -20
```

Expected: all packages PASS (or skip with no failures). The webhook package tests (`internal/webhook/...`) still compile and pass since the package itself is unchanged.

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go config/manager/manager.yaml
git commit -m "feat: remove frps webhook from controller-manager

The webhook now runs as a dedicated frps-webhook Deployment. This
decouples its availability from the operator lifecycle — operator
restarts no longer interrupt frps proxy registration.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Version bumps, new Makefile target, frp v0.68.1

**Files:**
- Modify: `Makefile:6-8`
- Modify: `docker/Dockerfile.frp:1`

- [ ] **Step 1: Update `Makefile` — version variables**

Change lines 6-8:
```makefile
FRP_VERSION ?= v0.61.0
TUNNEL_VERSION ?= 1.0.2
OPERATOR_VERSION ?= 1.0.11
```
to:
```makefile
FRP_VERSION ?= v0.68.1
TUNNEL_VERSION ?= 1.0.3
OPERATOR_VERSION ?= 1.0.12
WEBHOOK_VERSION ?= 1.0.0
```

- [ ] **Step 2: Add `docker-build-webhook` target to `Makefile`**

After the `docker-build-operator` target (line 146), add:

```makefile
.PHONY: docker-build-webhook
docker-build-webhook: docker-buildx-setup ## Build and push the crd-tunnel-webhook image.
	$(CONTAINER_TOOL) buildx build \
		--builder frp-builder \
		--platform $(PLATFORMS) \
		--push \
		-t $(REGISTRY)/crd-tunnel-webhook:$(WEBHOOK_VERSION) \
		-f docker/Dockerfile.webhook .
```

- [ ] **Step 3: Update `docker-build` to include webhook**

Change:
```makefile
.PHONY: docker-build
docker-build: docker-build-frp docker-build-operator ## Build and push both the FRP and operator images.
```
to:
```makefile
.PHONY: docker-build
docker-build: docker-build-frp docker-build-operator docker-build-webhook ## Build and push all images (FRP, operator, webhook).
```

- [ ] **Step 4: Update `docker/Dockerfile.frp` — frp version default**

Change line 1:
```dockerfile
ARG FRP_VERSION=v0.61.0
```
to:
```dockerfile
ARG FRP_VERSION=v0.68.1
```

- [ ] **Step 5: Verify `make help` shows the new target**

```bash
make help 2>&1 | grep webhook
```

Expected output includes:
```
  docker-build-webhook  Build and push the crd-tunnel-webhook image.
```

- [ ] **Step 6: Commit**

```bash
git add Makefile docker/Dockerfile.frp
git commit -m "feat: upgrade frp to v0.68.1, add docker-build-webhook target

- FRP_VERSION: v0.61.0 → v0.68.1
- TUNNEL_VERSION: 1.0.2 → 1.0.3 (frp binary bump)
- OPERATOR_VERSION: 1.0.11 → 1.0.12 (operator changes)
- WEBHOOK_VERSION: 1.0.0 (new dedicated webhook image)

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -count=1
```

Expected: all packages PASS

- [ ] **Step 2: Verify kustomize output has expected resources**

```bash
bin/kustomize build config/default | grep "^kind:\|^  name:"
```

Expected output includes (after namePrefix `crd-tunnel-service-`):
```
kind: Namespace
kind: ServiceAccount
  name: crd-tunnel-service-controller-manager
kind: ServiceAccount
  name: crd-tunnel-service-frps-webhook
kind: ClusterRole
  name: crd-tunnel-service-frps-webhook-role
  name: crd-tunnel-service-manager-role
kind: ClusterRoleBinding
  name: crd-tunnel-service-frps-webhook-rolebinding
  name: crd-tunnel-service-manager-rolebinding
kind: Deployment
  name: crd-tunnel-service-controller-manager
kind: Deployment
  name: crd-tunnel-service-frps-webhook
kind: Service
  name: crd-tunnel-service-controller-manager-frps-plugin
```

- [ ] **Step 3: Verify both binaries build cleanly**

```bash
go build ./cmd/main.go && go build ./cmd/frps-webhook/main.go && echo "both binaries OK" && rm -f main frps-webhook
```

Expected: `both binaries OK`

- [ ] **Step 4: Verify frpc ConfigMap no longer contains poolCount**

After deploying and operator reconciling, run:
```bash
KUBECONFIG=/Users/arindra/crd-fleet-manager.kubeconfig kubectl --context crd-devops-prod \
  -n frp-system get configmap spoke-1-config -o jsonpath='{.data.frpc\.toml}'
```

Expected: output does NOT contain `transport.poolCount`
