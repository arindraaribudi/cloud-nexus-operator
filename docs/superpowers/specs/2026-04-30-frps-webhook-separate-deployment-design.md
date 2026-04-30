# Design: Separate frps-webhook Deployment + frp v0.68.1 Upgrade

**Date**: 2026-04-30
**Branch**: feat/crd-tunnel
**Status**: Approved

---

## Problem Statement

Two production issues identified during investigation of `proxy [] not found` log spam:

1. **Webhook availability**: The frps httpPlugin webhook (port 9090) runs inside the `controller-manager` pod. Any operator restart or upgrade makes the webhook unavailable. frps then rejects all named proxy registrations during that window. frpc retries ~5 times (~33s each) then stops. Tunnels break until frpc is manually restarted.

2. **Dead config**: `transport.poolCount = 0` in the frpc template is a no-op — frp's `Complete()` method uses `util.EmptyOr(c.PoolCount, 1)` which treats 0 as "unset" and replaces it with 1. This applies to both v0.61.0 and v0.68.1.

---

## Goals

- Decouple the frps webhook HTTP server from the operator lifecycle so it survives operator restarts/upgrades
- Upgrade frp binaries from v0.61.0 to v0.68.1
- Remove the dead `transport.poolCount = 0` config line

## Non-Goals

- Adding `failSafe` support (not present in frp v0.68.1)
- Fixing the `proxy [] not found` log spam (confirmed harmless; cannot be disabled)
- Modifying the webhook handler logic

---

## Architecture

### Before

```
controller-manager pod
├── FrpServer controller
├── FrpClient controller
├── FrpProxy controller
├── RegistryProxy controller
└── frps webhook HTTP server :9090   ← breaks on operator restart
```

### After

```
controller-manager pod               frps-webhook pod
├── FrpServer controller             └── frps webhook HTTP server :9090
├── FrpClient controller
├── FrpProxy controller
└── RegistryProxy controller
```

The existing `controller-manager-frps-plugin` Service selector is updated from `control-plane: controller-manager` to `app: frps-webhook`.

---

## Component: `cmd/frps-webhook`

New binary entrypoint at `cmd/frps-webhook/main.go`. Uses a controller-runtime Manager with no controllers registered — only the webhook Runnable is added. This provides:
- Health probe endpoints (`/healthz`, `/readyz`) via `--health-probe-bind-address`
- Structured logging (zap)
- Graceful shutdown on SIGTERM/SIGINT

```go
// pseudocode
mgr, _ := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    HealthProbeBindAddress: ":8082",
})
mgr.AddHealthzCheck("healthz", healthz.Ping)
mgr.AddReadyzCheck("readyz", healthz.Ping)
mgr.Add(webhook.NewHTTPServer(":9090", mgr.GetClient()))
mgr.Start(ctrl.SetupSignalHandler())
```

No changes to `internal/webhook/frps_events.go`.

---

## Component: `docker/Dockerfile.webhook`

Multi-stage build mirroring the operator Dockerfile:

```
Stage 1 (builder): golang:1.25-alpine
  - go build cmd/frps-webhook → /frps-webhook binary

Stage 2 (runtime): gcr.io/distroless/static:nonroot
  - COPY /frps-webhook
  - ENTRYPOINT ["/frps-webhook"]
```

---

## Component: Kustomize Resources

### New directory: `config/webhook-server/`

| File | Purpose |
|------|---------|
| `kustomization.yaml` | Lists all resources in this directory |
| `deployment.yaml` | Deployment for frps-webhook (1 replica, distroless, port 9090) |
| `service_account.yaml` | Dedicated ServiceAccount `frps-webhook` |
| `cluster_role.yaml` | ClusterRole: `services` create/update/patch/delete/get/list across all namespaces |
| `cluster_role_binding.yaml` | Binds ClusterRole to the webhook ServiceAccount |

### Modified: `config/default/frps_plugin_service.yaml`

Selector updated:
```yaml
# before
selector:
  control-plane: controller-manager
# after
selector:
  app: frps-webhook
```

### Modified: `config/default/kustomization.yaml`

Adds `- ../webhook-server` to resources.

### Modified: `config/manager/manager.yaml`

Removes the `frps-plugin` container port (9090) from the controller-manager Deployment.

---

## Component: Operator Changes

### `cmd/main.go`

Remove:
```go
if err := mgr.Add(webhook.NewHTTPServer(":9090", mgr.GetClient())); err != nil {
    setupLog.Error(err, "Failed to register frps plugin webhook server")
    os.Exit(1)
}
```

The webhook package import is removed if it becomes unused (it won't be needed in main.go anymore).

---

## Component: frp Upgrade to v0.68.1

### `Makefile`
```makefile
FRP_VERSION ?= v0.68.1   # was v0.61.0
TUNNEL_VERSION ?= 1.0.3  # was 1.0.2 (frp binary bump)
OPERATOR_VERSION ?= 1.0.12  # was 1.0.11 (operator changes)
WEBHOOK_VERSION ?= 1.0.0    # new
```

### `docker/Dockerfile.frp`
```dockerfile
ARG FRP_VERSION=v0.68.1  # was v0.61.0
```

### New Makefile target: `docker-build-webhook`
```makefile
docker-build-webhook: docker-buildx-setup
    $(CONTAINER_TOOL) buildx build \
        --builder frp-builder \
        --platform $(PLATFORMS) \
        --push \
        -t $(REGISTRY)/crd-tunnel-webhook:$(WEBHOOK_VERSION) \
        -f docker/Dockerfile.webhook .
```

`docker-build` updated to include `docker-build-webhook`.

---

## Component: Config Template Cleanup

### `internal/config/client.go`

Remove `transport.poolCount = 0` from `frpcBaseTemplate`. This line was a no-op: frp's `Complete()` uses `util.EmptyOr(c.PoolCount, 1)` which replaces 0 with 1, so setting it had no effect and only created misleading ConfigMap content.

### `internal/config/client_test.go`

Remove the assertion that checked for `transport.poolCount = 0`.

---

## RBAC Design

The webhook needs to create/update/delete Services in **any namespace** (FrpProxy CRs can be in any namespace). It does not need access to CRDs, ConfigMaps, Deployments, or Secrets.

**ClusterRole** (`frps-webhook-role`):
```yaml
rules:
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "create", "update", "patch", "delete"]
```

The operator's existing ClusterRole retains its own service permissions for controller reconciliation.

---

## Deployment Spec Notes

- `replicas: 1` — single replica is sufficient; frps already retries on webhook failure
- `resources`: minimal — `requests: 10m CPU / 32Mi`, `limits: 100m CPU / 64Mi`
- `securityContext`: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`
- Liveness probe: `httpGet /healthz :8082`
- Readiness probe: `httpGet /readyz :8082`

---

## Go Module Dependencies

The frp binaries are compiled inside Docker and are **not** Go module dependencies — `go.mod` does not import frp packages. No `go.mod` changes are required for the frp version upgrade.

---

## Files Changed Summary

| File | Change |
|------|--------|
| `cmd/frps-webhook/main.go` | **New** — webhook-only manager binary |
| `docker/Dockerfile.webhook` | **New** — builds frps-webhook image |
| `config/webhook-server/kustomization.yaml` | **New** |
| `config/webhook-server/deployment.yaml` | **New** |
| `config/webhook-server/service_account.yaml` | **New** |
| `config/webhook-server/cluster_role.yaml` | **New** |
| `config/webhook-server/cluster_role_binding.yaml` | **New** |
| `config/default/frps_plugin_service.yaml` | **Modified** — selector updated |
| `config/default/kustomization.yaml` | **Modified** — add webhook-server resource |
| `config/manager/manager.yaml` | **Modified** — remove port 9090 |
| `cmd/main.go` | **Modified** — remove webhook.NewHTTPServer call |
| `Makefile` | **Modified** — version bumps, new docker-build-webhook target |
| `docker/Dockerfile.frp` | **Modified** — FRP_VERSION default bump |
| `internal/config/client.go` | **Modified** — remove transport.poolCount = 0 |
| `internal/config/client_test.go` | **Modified** — remove poolCount assertion |
