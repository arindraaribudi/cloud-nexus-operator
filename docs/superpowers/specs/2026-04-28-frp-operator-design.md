# FRP Operator Design

**Date:** 2026-04-28
**Project:** crd-tunnel-service
**Source:** https://github.com/fatedier/frp

---

## Overview

A Kubernetes operator that manages FRP (Fast Reverse Proxy) hub-and-spoke tunnel infrastructure via Custom Resource Definitions. Users declare `FrpServer` (hub), `FrpClient` (spoke), and `FrpProxy` (tunnel rule) resources. The operator handles Deployments, ConfigMaps, Services, and hot-reloads proxy config changes without pod restarts.

---

## Repository Structure

```
crd-tunnel-service/
├── docker/
│   ├── Dockerfile.frps          # multi-stage, multi-arch server image
│   └── Dockerfile.frpc          # multi-stage, multi-arch client image
├── operator/
│   ├── main.go                  # entrypoint, registers controllers
│   ├── api/
│   │   └── v1alpha1/
│   │       ├── frpserver_types.go
│   │       ├── frpclient_types.go
│   │       ├── frpproxy_types.go
│   │       └── groupversion_info.go
│   ├── controllers/
│   │   ├── frpserver_controller.go
│   │   ├── frpclient_controller.go
│   │   └── frpproxy_controller.go
│   ├── internal/
│   │   ├── config/              # toml config renderer (frps.toml / frpc.toml)
│   │   └── reload/              # frpc admin API client (hot reload)
│   └── go.mod
├── config/
│   ├── crd/                     # generated CRD manifests
│   │   ├── bases/
│   │   │   ├── tunnel.io_frpservers.yaml
│   │   │   ├── tunnel.io_frpclients.yaml
│   │   │   └── tunnel.io_frpproxies.yaml
│   │   └── kustomization.yaml
│   ├── rbac/                    # ClusterRole, ServiceAccount, ClusterRoleBinding
│   ├── manager/                 # operator Deployment
│   ├── default/                 # composes crd + rbac + manager
│   └── samples/
│       ├── frpserver-sample.yaml
│       ├── frpserver-gcp-nlb.yaml
│       ├── frpserver-aws-nlb.yaml
│       ├── frpclient-sample.yaml
│       └── frpproxy-sample.yaml
└── Makefile
```

---

## Docker Images

### Build Strategy

Two images — `frps` (server) and `frpc` (client) — built from source using multi-stage Dockerfiles.

```
Stage 1 (builder): golang:1.22-alpine
  - Clone fatedier/frp at pinned tag
  - CGO_ENABLED=0 go build ./cmd/frps (or frpc)
  - GOARCH/GOOS injected via --build-arg (Buildx passes automatically)

Stage 2 (runtime): gcr.io/distroless/static
  - COPY binary from builder
  - COPY entrypoint.sh
  - EXPOSE relevant ports
```

Config is never baked into the image. The entrypoint runs `frps -c /etc/frp/frps.toml` (or `frpc -c /etc/frp/frpc.toml`). Config is always mounted via a Kubernetes ConfigMap.

### Multi-Platform

**Platforms:** `linux/amd64`, `linux/arm64`, `linux/arm/v7`

**Unified manifest list** — a single tag is an OCI manifest list containing all platform variants. Docker/containerd pulls the correct variant automatically.

```
registry/frps:v0.61.0    ← manifest list (amd64 + arm64 + arm/v7)
registry/frps:latest
registry/frpc:v0.61.0
registry/frpc:latest
```

Built with:
```bash
docker buildx build \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  --push \
  -t $(REGISTRY)/frps:$(FRP_VERSION) \
  -t $(REGISTRY)/frps:latest \
  -f docker/Dockerfile.frps .
```

### Makefile Targets

```makefile
make docker-build VERSION=v0.61.0 REGISTRY=your.registry.io
  # builds and pushes both frps and frpc multi-platform images

make generate        # run controller-gen, regenerate deepcopy + CRD manifests
make manifests       # generate CRD YAML from kubebuilder markers
make install         # kubectl apply -k config/crd
make deploy          # kubectl apply -k config/default
make undeploy        # kubectl delete -k config/default
make run             # run operator locally against current kubeconfig (dev)
```

---

## CRD Specifications

### API Group: `tunnel.io/v1alpha1`

### Image Version Defaults

Default image repository and tag are defined as **CRD schema defaults** (OpenAPI `default` values), set at install time. Users need zero image config for the common case. Per-resource overrides are optional.

**Resolution order:**
```
spec.image.tag (user-set)  →  CRD schema default (set at CRD install time)
```

Kubebuilder marker: `// +kubebuilder:default:="v0.61.0"` on struct fields.

---

### FrpServer (hub)

```yaml
apiVersion: tunnel.io/v1alpha1
kind: FrpServer
metadata:
  name: hub
spec:
  # image is optional — CRD schema defaults apply if omitted
  image:
    repository: your.registry.io/frps   # default set in CRD schema
    tag: v0.61.0                         # default set in CRD schema
  replicas: 1
  bindPort: 7000
  vhostHTTPPort: 80        # optional
  vhostHTTPSPort: 443      # optional
  dashboardPort: 7500      # optional
  dashboardUser: admin
  dashboardPasswordSecret:
    name: frps-dashboard-secret
    key: password
  authTokenSecret:          # shared auth token frpc must present
    name: frps-auth-secret
    key: token
  extraConfig: |            # raw toml appended verbatim to frps.toml
    maxPoolCount = 5
  service:
    type: LoadBalancer      # LoadBalancer | NodePort | ClusterIP
    annotations:            # map[string]string, passed verbatim to Service
      # GCP External NLB (Passthrough)
      networking.gke.io/load-balancer-type: "External"
      # GCP Internal NLB
      # networking.gke.io/load-balancer-type: "Internal"
      # AWS NLB
      # service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      # Azure
      # service.beta.kubernetes.io/azure-load-balancer-internal: "false"
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

---

### FrpClient (spoke)

```yaml
apiVersion: tunnel.io/v1alpha1
kind: FrpClient
metadata:
  name: spoke-1
spec:
  # image is optional — CRD schema defaults apply if omitted
  image:
    repository: your.registry.io/frpc   # default set in CRD schema
    tag: v0.61.0                         # default set in CRD schema
  serverRef:
    name: hub               # resolves server address from FrpServer.status
    # or use explicit address:
    # address: frps.example.com
    # port: 7000
  authTokenSecret:
    name: frps-auth-secret
    key: token
  admin:
    port: 7400              # local admin API port for hot reload
    tokenSecret:
      name: frpc-admin-secret
      key: token
  extraConfig: |
    loginFailExit = false
```

---

### FrpProxy (tunnel rule)

```yaml
apiVersion: tunnel.io/v1alpha1
kind: FrpProxy
metadata:
  name: my-ssh-tunnel
spec:
  clientRef:
    name: spoke-1           # parent FrpClient
  type: tcp                 # tcp | udp | http | https | stcp | xtcp | tcpmux
  localIP: 127.0.0.1
  localPort: 22
  remotePort: 6000          # for tcp/udp
  # for http/https:
  customDomains:
    - ssh.example.com
  # for stcp/xtcp:
  secretKey: mysecret
  extraConfig: |            # raw toml for this proxy block
    useEncryption = true
```

---

## Reconciler Logic

### FrpServer Controller

1. Render `frps.toml` from spec → create/update ConfigMap
2. Create Deployment (mounting ConfigMap at `/etc/frp/frps.toml`)
3. Create Service (type + annotations from `spec.service`)
4. Write resolved address to `status.address`, `status.port`
5. Config change → update ConfigMap + rolling restart (frps has no hot reload API)

### FrpClient Controller

1. Resolve server address from `spec.serverRef.name` (reads `FrpServer.status`) or explicit address
2. Ensure admin token Secret exists
3. Render base `frpc.toml` (no proxy blocks) → create/update ConfigMap
4. Create Deployment (mounting ConfigMap at `/etc/frp/frpc.toml`)
5. Write `status.ready`, `status.adminPort`

### FrpProxy Controller (hot reload owner)

1. On create/update/delete: fetch all `FrpProxy` resources for the parent `FrpClient`
2. Re-render full `frpc.toml` (base config + all proxy blocks)
3. Update the ConfigMap
4. Call frpc admin API: `POST http://localhost:{adminPort}/api/reload`
   - Auth: `Authorization: Bearer {token}` (token from Secret)
   - Called via in-pod exec through controller-runtime
5. On reload failure → requeue after 10s, set `Synced=False`
6. On success → set `status.phase=Running`, `status.lastReloadTime`

**The frpc pod is never restarted on proxy changes.** Only ConfigMap + admin API reload.

### Finalizer

`FrpProxy` resources carry finalizer `tunnel.io/proxy-cleanup`. Deletion triggers a reload (removing the proxy block from frpc config) before the resource is removed — preventing stale proxy entries in a running frpc process.

---

## Status Fields

### FrpServer
```yaml
status:
  phase: Running | Pending | Failed
  address: "10.0.0.1"
  port: 7000
  conditions:
    - type: Ready
      status: "True"
      reason: DeploymentReady
```

### FrpClient
```yaml
status:
  phase: Running | Pending | Failed
  serverAddress: "10.0.0.1:7000"
  conditions:
    - type: Ready
      status: "True"
    - type: ServerResolved
      status: "True"
```

### FrpProxy
```yaml
status:
  phase: Running | Pending | Failed | Reloading
  lastReloadTime: "2026-04-28T10:00:00Z"
  conditions:
    - type: Synced
      status: "True"
      reason: ReloadSucceeded
```

---

## Error Handling

| Scenario | Behaviour |
|---|---|
| `FrpServer` not found when resolving `serverRef` | Requeue with backoff, set `ServerResolved=False` |
| Admin API call fails (pod not ready) | Requeue after 10s, set `Synced=False` with message |
| ConfigMap render error (bad toml) | Set `Failed`, do not requeue until spec changes |
| Pod crashloops | Reflected via Deployment status → `phase=Failed` |
| `FrpProxy` deleted | Re-render toml without that proxy, reload, remove finalizer |

---

## Kustomize Deployment

```
config/
├── crd/                     # CRD manifests only
├── rbac/                    # ServiceAccount, ClusterRole, ClusterRoleBinding
│                            # Permissions: watch/get/list/update CRDs,
│                            #   Deployments, ConfigMaps, Services, Secrets, Pods/exec
├── manager/                 # operator Deployment
├── default/                 # composes crd + rbac + manager + namespace
└── samples/                 # ready-to-use example CRs
```

**Install commands:**
```bash
# Full install
kubectl apply -k config/default

# CRDs only (GitOps split)
kubectl apply -k config/crd

# Uninstall
kubectl delete -k config/default
```

---

## Testing Strategy

### Unit Tests (`operator/internal/`)
- Config renderer: given a spec, assert correct toml output
- Reload client: mock HTTP server, assert correct request (method, path, auth header)

### Controller Tests (envtest)
- `FrpServer` reconcile: creates Deployment, ConfigMap, Service with correct spec
- `FrpClient` reconcile: resolves `serverRef` from `FrpServer.status`, creates resources
- `FrpProxy` reconcile: adds/removes proxy blocks, calls mock reload endpoint
- Finalizer: deleting `FrpProxy` triggers reload before removal
- Error paths: missing `FrpServer`, failed reload → correct status conditions

### E2E Tests (optional, real cluster)
- Deploy operator, create all three CRDs, assert tunnel works end-to-end
- Hot reload: add second `FrpProxy`, assert no pod restart, assert new tunnel active

### Tooling
- `github.com/onsi/ginkgo/v2` + `gomega`
- `sigs.k8s.io/controller-runtime/pkg/envtest`
- `net/http/httptest` for mocking frpc admin API

---

## Technology Stack

| Component | Technology |
|---|---|
| Operator framework | kubebuilder + controller-runtime |
| Language | Go 1.22+ |
| CRD generation | controller-gen |
| Deployment | Kustomize |
| Docker build | Docker Buildx (multi-platform manifest list) |
| Runtime image | gcr.io/distroless/static |
| Test framework | Ginkgo v2 + Gomega + envtest |
