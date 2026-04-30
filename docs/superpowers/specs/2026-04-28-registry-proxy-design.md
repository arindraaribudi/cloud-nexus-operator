# Registry Proxy Design

**Date:** 2026-04-28
**Status:** Approved
**Scope:** Extend the crd-tunnel-service operator to support a Docker registry auth proxy, tunneled through FRP, so hub-side CI/CD can pull images without cloud credentials.

---

## Problem

The hub cluster's CI/CD tooling needs to pull container images from private cloud registries (GCP Artifact Registry, AWS ECR, etc.). Today this requires running `docker login` with cloud credentials on every hub machine or pipeline. The spoke cluster already has the appropriate cloud identity (Workload Identity / Pod Identity). The goal is to expose an authenticated registry proxy from the spoke to the hub through the existing FRP tunnel, so the hub just uses a plain internal Service URL with no credentials.

---

## Design Goals

- Hub CI/CD does `docker pull reg-1-docker/asia-southeast3-docker.pkg.dev/my-image:tag` — no login
- One proxy handles multiple upstream registries (upstream host encoded in the image path)
- Auth uses cloud-native identity (GKE Workload Identity, AWS IRSA/Pod Identity) — no static secrets
- No kubeconfig of hub stored in spoke — the FRP tunnel connection itself signals the hub
- No changes to existing FrpServer, FrpClient, FrpProxy controllers
- Additive: existing deployments unaffected

---

## Architecture

```
Spoke cluster                                Hub cluster
──────────────────────────────────           ─────────────────────────────────────
RegistryProxy CR (reg-1)
        │
        ▼
[RegistryProxy controller]
  ├─ Deployment: reg-1-proxy
  │    image: crd-tunnel (same as frpc)
  │    cmd: /registry-proxy --port=5000
  │    serviceAccount: registry-proxy-sa (annotated WI/IRSA)
  │
  ├─ Service: reg-1-proxy-svc (ClusterIP:5000)
  │
  └─ FrpProxy: reg-1-registry-proxy
       type: tcp
       localSvc: reg-1-proxy-svc:5000
       remotePort: 15000
       metadatas:
         svcName: reg-1-docker
         namespace: tunnels
         remotePort: 15000
                │
                │  frpc registers proxy with frps
                ▼
              frps fires NewProxy webhook
                │
                ▼
        hub operator webhook server (:9090)
          reads metadatas
          creates Service reg-1-docker:
            selector: frpserver=main-hub
            port: 80 → targetPort: 15000

Hub CI/CD:
  docker pull reg-1-docker/asia-southeast3-docker.pkg.dev/my-image:tag
       │
       └─► reg-1-docker.tunnels.svc:80
             └─► frps:15000 (FRP tunnel)
                   └─► reg-1-proxy-svc:5000 (spoke)
                         └─► /registry-proxy binary
                               extracts upstream: asia-southeast3-docker.pkg.dev
                               gets GCP ADC token
                               forwards to: https://asia-southeast3-docker.pkg.dev/v2/my-image/...
                               returns response
```

---

## New CRD: RegistryProxy

**API:** `tunnel.tunnel.io/v1alpha1`
**Kind:** `RegistryProxy`
**Applied to:** Spoke cluster

### Spec

```go
type RegistryProxySpec struct {
    // ClientRef references the FrpClient (spoke) that owns the tunnel.
    ClientRef LocalObjectReference `json:"clientRef"`

    // RemotePort is the port frps will bind on the hub side.
    // Must not conflict with other FrpProxy remotePort values on the same FrpClient.
    RemotePort int32 `json:"remotePort"`

    // ServiceAccountName is the Kubernetes SA annotated with WI/IRSA for cloud auth.
    ServiceAccountName string `json:"serviceAccountName"`

    // ProxyPort is the port the registry-proxy binary listens on inside the Pod.
    // Default: 5000
    // +optional
    ProxyPort *int32 `json:"proxyPort,omitempty"`

    // Image overrides the proxy container image.
    // Default: inherits from the referenced FrpClient's image.
    // +optional
    Image *ImageSpec `json:"image,omitempty"`
}
```

### Status

```go
type RegistryProxyStatus struct {
    Phase      string `json:"phase"` // Pending | Running | Failed
    HubService string `json:"hubService,omitempty"` // FQDN once created on hub

    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Conditions:
    //   ProxyReady      - registry-proxy Deployment is healthy
    //   TunnelConnected - frps fired NewProxy; hub Service exists
}
```

### Example CR

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: RegistryProxy
metadata:
  name: reg-1
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  remotePort: 15000
  serviceAccountName: registry-proxy-sa
```

After reconciliation the hub gets:
```
reg-1-docker.tunnels.svc.cluster.local:80
```

---

## New Binary: `registry-proxy` (`cmd/registry-proxy/main.go`)

Bundled into the existing `crd-tunnel` Docker image as `/registry-proxy`. No new image required.

### Startup

```
/registry-proxy --port=5000
```

### Request Routing

Implements the OCI Distribution Spec `/v2/` API as a pure reverse proxy (no local storage).

**Path parsing:**

```
Incoming:  GET /v2/asia-southeast3-docker.pkg.dev/my-app/manifests/latest
                   └──────────────┬──────────────┘ └──────────────────────┘
                          upstream host                   actual image path

Outgoing:  GET https://asia-southeast3-docker.pkg.dev/v2/my-app/manifests/latest
           Authorization: Bearer <fresh token>
```

The first path segment after `/v2/` that looks like a hostname (contains a `.`) is extracted as the upstream registry. The remainder is the image name passed verbatim to the upstream.

### Auth Routing

| Upstream pattern | Auth mechanism |
|---|---|
| `*.pkg.dev`, `gcr.io`, `*.gcr.io` | GCP ADC (`golang.org/x/oauth2/google`) |
| `*.dkr.ecr.*.amazonaws.com` | AWS SDK v2 default credential chain |
| `*.azurecr.io` | Azure SDK default credential chain |
| `ghcr.io`, `registry-1.docker.io` | No auth injection (public or passthrough) |
| anything else | No auth injection |

Tokens are fetched fresh per-request. Cloud SDKs cache and refresh internally. No state on disk.

### Token Challenge Handling

If the upstream returns `401 WWW-Authenticate: Bearer realm=...`, the proxy fetches a token from the realm endpoint using the cloud credential and retries the request transparently. This handles registries that require the two-step Docker auth flow.

### What it does NOT do

- No image layer caching (pure proxy)
- No TLS termination (HTTP within the cluster; FRP handles the tunnel)
- No auth for non-cloud registries (callers handle their own credentials)

---

## New Controller: RegistryProxy Controller

**File:** `internal/controller/registryproxy_controller.go`
**Watches:** `RegistryProxy` CR

### Reconciliation

1. Fetch `RegistryProxy`. If not found, return nil (already deleted).
2. Add finalizer `tunnel.io/registryproxy-cleanup`.
3. Fetch referenced `FrpClient`. If not found or not ready, requeue.
4. Resolve proxy image: use `spec.image` if set, else inherit from `FrpClient.spec.image`.
5. **Reconcile Deployment** `<name>-proxy`:
   - Image: resolved above
   - Command: `/registry-proxy --port=<proxyPort>`
   - ServiceAccountName: `spec.serviceAccountName`
   - Labels: `app: registry-proxy`, `registryproxy: <name>`
6. **Reconcile Service** `<name>-proxy-svc`:
   - ClusterIP, port `proxyPort`
   - Selector: `registryproxy: <name>`
7. **Reconcile FrpProxy** `<name>-registry-proxy`:
   - `clientRef.name`: from `spec.clientRef`
   - `type: tcp`
   - `localIP`: `<name>-proxy-svc.<namespace>.svc.cluster.local`
   - `localPort`: `spec.proxyPort`
   - `remotePort`: `spec.remotePort`
   - `extraConfig` (inline metadatas, valid within a `[[proxies]]` TOML block):
     ```toml
     metadatas.svcName = "<name>-docker"
     metadatas.namespace = "<namespace>"
     metadatas.remotePort = "<remotePort>"
     metadatas.frpServerName = "<frpServerName>"
     ```
     `frpServerName` is resolved from `FrpClient.spec.serverRef.name` so the hub webhook knows which FrpServer pod selector to use.
8. Update `ProxyReady` condition based on Deployment ready replicas.
9. Watch owned FrpProxy status: when `FrpProxy.status.phase == Running`, set `TunnelConnected=True` on the RegistryProxy; when FrpProxy phase leaves Running, set `TunnelConnected=False`. This avoids needing the hub operator to reach the spoke API.

### Deletion

When `DeletionTimestamp` is set:
1. Delete owned FrpProxy (triggers FrpProxy controller to hot-reload frpc and remove the tunnel).
2. Delete owned Service and Deployment.
3. The `CloseProxy` webhook on the hub deletes the hub-side Service automatically.
4. Remove finalizer.

---

## Hub Operator: Webhook Server

### Overview

A new HTTP server started in `cmd/main.go` alongside the existing controller manager. Handles frps plugin callbacks.

**Port:** 9090 (internal, not exposed externally)
**New Service added to kustomize:** `tunnel-operator-webhook` (ClusterIP, port 9090)

### Endpoint: `POST /frps/events`

frps sends JSON per the [frps plugin protocol](https://github.com/fatedier/frp/blob/dev/doc/plugin_usage.md):

```json
{
  "version": "0.1.0",
  "op": "NewProxy",
  "content": {
    "user": { "name": "spoke-client", ... },
    "proxy_name": "reg-1-registry-proxy",
    "proxy_type": "tcp",
    "remote_port": 15000,
    "metas": {
      "svcName": "reg-1-docker",
      "namespace": "tunnels",
      "remotePort": "15000"
    }
  }
}
```

**`NewProxy` handling:**
1. Check `metas.svcName` is set; if not, return `{"reject": false}` (not a registry proxy event).
2. Create (or update) Service in `metas.namespace`:
   ```yaml
   name: <metas.svcName>        # reg-1-docker
   selector:
     frpserver: <metas.frpServerName>
   ports:
     - port: 80
       targetPort: <metas.remotePort>
   ```
   `metas.frpServerName` is set by the RegistryProxy controller (from FrpClient.spec.serverRef.name) so the Service selector targets the correct frps pod.
3. Return `{"reject": false}`.

Note: the webhook handler does NOT patch RegistryProxy status — the RegistryProxy CR lives in the spoke cluster, which the hub operator has no API access to. `TunnelConnected` status is derived by the spoke's RegistryProxy controller from the owned FrpProxy status (see controller section step 9).

**`CloseProxy` handling:**
1. Check `metas.svcName`; if not set, return.
2. Delete Service `<metas.svcName>` in `<metas.namespace>` (ignore not-found).
3. Return `{"reject": false}`.

**On error:** Log and return `{"reject": false}` — `failSafe=true` on the plugin means frps does not block proxy connections when the webhook fails.

---

## FrpServer Config Changes

### New spec field

```go
type FrpServerSpec struct {
    // ... existing fields ...

    // EnableOperatorPlugin enables the frps httpPlugin that notifies the
    // operator when proxies connect/disconnect (required for RegistryProxy).
    // Default: false
    // +optional
    EnableOperatorPlugin bool `json:"enableOperatorPlugin,omitempty"`
}
```

### Updated frps.toml template (`internal/config/server.go`)

When `EnableOperatorPlugin` is true, append:

```toml
[[httpPlugins]]
name     = "operator-notifier"
addr     = "tunnel-operator-webhook.{{ .Namespace }}.svc.cluster.local:9090"
path     = "/frps/events"
ops      = ["NewProxy", "CloseProxy"]
failSafe = true
```

`failSafe = true` ensures frpc proxy registrations are not blocked if the operator webhook is temporarily unavailable (e.g., during operator restart).

---

## Error Handling

| Scenario | Behavior |
|---|---|
| FrpClient not found or not ready | `RegistryProxy` requeues with `15s` backoff |
| Proxy Deployment not ready | `ProxyReady=False`; requeue `15s` |
| FRP tunnel not yet connected | `TunnelConnected=False`; informational; hub Service does not exist yet |
| Hub webhook unavailable (`failSafe=true`) | FRP proxy still connects; Service created when operator recovers |
| Cloud credentials unavailable at request time | Per-request `502` returned to caller; proxy Pod stays running |
| `remotePort` conflict with existing FrpProxy | RegistryProxy controller sets `phase=Failed`, error condition; no FrpProxy created |
| RegistryProxy deleted | Owned Deployment/Service/FrpProxy deleted; `CloseProxy` webhook cleans up hub Service |
| Hub Service already exists (duplicate CR) | `CreateOrUpdate` — idempotent |

---

## File Changes Summary

| File | Change |
|---|---|
| `api/v1alpha1/registryproxy_types.go` | New — RegistryProxy CRD types |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerated |
| `internal/controller/registryproxy_controller.go` | New — RegistryProxy reconciler |
| `internal/webhook/frps_events.go` | New — frps plugin HTTP handler |
| `cmd/registry-proxy/main.go` | New — registry auth proxy binary |
| `cmd/main.go` | Start webhook server on :9090 alongside controller manager |
| `api/v1alpha1/frpserver_types.go` | Add `EnableOperatorPlugin bool` field |
| `internal/config/server.go` | Add httpPlugin block to frps.toml template |
| `docker/Dockerfile.frp` | Copy `/registry-proxy` binary into image |
| `config/crd/bases/` | Regenerated CRD manifests |
| `config/rbac/` | Add RBAC for RegistryProxy resources |
| `config/manager/manager.yaml` | Expose port 9090 on operator container |
| `config/default/` | Add `tunnel-operator-webhook` Service |
| `config/samples/` | Add sample RegistryProxy CR |
| `internal/controller/registryproxy_controller_test.go` | New — envtest tests |
| `internal/webhook/frps_events_test.go` | New — webhook handler tests |

---

## Testing Strategy

**Unit tests:**
- `cmd/registry-proxy`: path parsing (upstream host extraction), auth routing table, token challenge handling
- `internal/webhook/frps_events.go`: NewProxy/CloseProxy handler logic with fake k8s client

**Controller tests (envtest):**
- `RegistryProxy` reconciler: creates Deployment, Service, FrpProxy with correct fields
- Deletion: removes owned resources, removes finalizer
- FrpClient not found: requeues

**E2E (Kind):**
- Deploy FrpServer + FrpClient + RegistryProxy in a single-cluster Kind setup
- Verify `reg-1-docker` Service is created after tunnel connects
- Verify `docker pull reg-1-docker/<upstream>/<image>` succeeds (using a local registry as upstream)
- Verify Service is deleted after RegistryProxy is deleted
