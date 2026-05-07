# CloudRestApiProxy — Design Spec

**Date:** 2026-05-03
**Status:** Approved
**Branch:** feat/initial

---

## Overview

Add a `CloudRestApiProxy` CRD that enables workloads on the hub cluster to call cloud provider REST APIs (AWS, GCP, Azure) whose credentials live only in a spoke cluster. The spoke injects the appropriate auth token (AWS SigV4, GCP/Azure Bearer) before forwarding to the real cloud endpoint.

This spec also covers migrating `RegistryProxy` from `TCPProxy` to `HTTPProxy` so both share a single unified hub-side gateway Service routed by path prefix.

---

## Goals

- Hub workloads call cloud APIs without holding any cloud credentials.
- Path structure on the hub encodes both the target spoke and the cloud service FQDN — no per-service CRD configuration required.
- A single Kubernetes Service on the hub routes all spoke/service combinations.
- Existing `RegistryProxy` users move to the same gateway endpoint.
- Works for any `*.amazonaws.com`, `*.googleapis.com`, and `*.azure.net`/`*.azure.com` endpoint without operator changes.

## Non-Goals

- Mutual TLS between hub and spoke (handled by FRP's existing auth token).
- Cross-cloud credential federation (each spoke uses its own cloud identity).
- gRPC proxying (HTTP/1.1 only; gRPC requires HTTP/2 which FRP vhost does not support).
- Rate limiting or request transformation beyond auth header injection.

---

## Architecture

### Traffic Flow

```
Hub caller
  │
  │  GET /spoke1/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp
  ▼
{nexusserver}-gateway K8s Service :8080
  │  (FRP vhostHTTPPort — layer-7 HTTP routing)
  │
  │  match Host: gateway-svc  AND  location: /spoke1/cloud
  ▼
HTTPProxy CRD  (owned by CloudRestApiProxy CR)
  │
  │  [FRP tunnel — frpc ← frps]
  ▼
cloud-proxy pod :8080  (in spoke1)
  │  1. strip /spoke1/cloud
  │  2. parse cloudFQDN = secretsmanager.us-east-1.amazonaws.com
  │  3. parse apiPath   = /v1/secret/myapp
  │  4. detect AWS → SigV4 sign via IRSA
  ▼
https://secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp
```

### Path Convention

| Segment | Example | Meaning |
|---|---|---|
| `/{spokeName}` | `/spoke1` | Identifies the spoke; FRP routes to that spoke's tunnel |
| `/{serviceType}` | `/cloud` or `/registry` | Identifies the proxy type on the spoke |
| `/{cloudFQDN}` | `/secretsmanager.us-east-1.amazonaws.com` | Full cloud service hostname; encodes provider, service, region |
| `/{apiPath...}` | `/v1/secret/myapp` | Passed verbatim to the upstream cloud API |

### Hub-Side Routing (FRP vhost + locations)

All HTTPProxy CRDs for a given NexusServer share the same `customDomains` value — the FQDN of the `{nexusserver}-gateway` Service. Each proxy registration uses a unique `locations` path prefix, set via `extraConfig`:

```toml
locations = ["/spoke1/cloud"]
```

FRP routes based on both the Host header (gateway FQDN) and the location prefix. Multiple spokes and multiple service types are all registered simultaneously with no port conflicts.

### Spoke-Side Deployments

Each spoke runs two independent proxy pods:

| Pod | Binary | Port | Manages |
|---|---|---|---|
| `registry-proxy` | `cmd/registry-proxy` | 5000 | Docker registry auth (existing, modified) |
| `cloud-proxy` | `cmd/cloud-proxy` | 8080 | Cloud REST API auth (new) |

Both pods are given a `STRIP_PREFIX` env var so they can remove the `/{spokeName}/{serviceType}` segments before processing the request.

---

## Component Designs

### 1. NexusServer — Gateway Service

**New field** on `NexusServerSpec`:

```go
// GatewayServiceType is the K8s Service type for the unified HTTP gateway.
// Only created when vhostHTTPPort is non-zero.
// Defaults to ClusterIP.
// +optional
// +kubebuilder:default=ClusterIP
GatewayServiceType corev1.ServiceType `json:"gatewayServiceType,omitempty"`
```

**Controller change:** When `spec.vhostHTTPPort != 0`, the NexusServer controller creates and owns a Service:

```yaml
metadata:
  name: {nexusserver-name}-gateway
  namespace: {nexusserver-namespace}
spec:
  type: {gatewayServiceType}    # ClusterIP or LoadBalancer
  selector:
    app: {nexusserver-name}
  ports:
    - name: vhost-http
      port: {vhostHTTPPort}
      targetPort: {vhostHTTPPort}
```

The gateway Service FQDN is:
```
{nexusserver-name}-gateway.{nexusserver-namespace}.svc.cluster.local
```

This FQDN is what RegistryProxy and CloudRestApiProxy controllers write into `customDomains` on their owned HTTPProxy CRDs. The controllers resolve it via: `clientRef → NexusClient → spec.serverRef → NexusServer`.

---

### 2. CloudRestApiProxy CRD

**File:** `api/v1alpha1/cloudrestapiproxy_types.go`

```go
type CloudRestApiProxySpec struct {
    // ClientRef references the NexusClient (spoke) that owns the tunnel.
    ClientRef LocalObjectRef `json:"clientRef"`

    // ServiceAccountName is the Kubernetes SA annotated with cloud credentials
    // (IRSA for AWS, Workload Identity for GCP, Azure Workload Identity for Azure).
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

type CloudRestApiProxyStatus struct {
    // Phase is Pending, Running, or Failed.
    Phase string `json:"phase,omitempty"`

    // GatewayURL is the full base URL on the hub gateway for this proxy.
    // e.g. http://{nexusserver}-gateway.{ns}.svc:8080/{spokeName}/cloud
    GatewayURL string `json:"gatewayURL,omitempty"`

    // Conditions contains standard K8s condition objects.
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

**Kubebuilder markers:**
```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="GatewayURL",type="string",JSONPath=".status.gatewayURL"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
```

**Sample CR:**
```yaml
apiVersion: tunnel.io/v1alpha1
kind: CloudRestApiProxy
metadata:
  name: spoke1-cloud
  namespace: spoke1-ns
spec:
  clientRef:
    name: spoke1-client
  serviceAccountName: cloud-proxy-sa
  proxyPort: 8080
  allowedDomains:
    - "*.amazonaws.com"
    - "*.googleapis.com"
```

---

### 3. CloudRestApiProxy Controller

**File:** `internal/controller/cloudrestapiproxy_controller.go`

Follows the same reconcile pattern as `RegistryProxyReconciler`. Owns two child resources:

**Child 1 — Deployment:**
```
name:      {cr-name}-cloud-proxy
namespace: {cr-namespace}   # spoke namespace
image:     cloud-proxy image (from spec.image or default)
serviceAccountName: spec.serviceAccountName
env:
  STRIP_PREFIX:    /{spokeName}/cloud
  PROXY_PORT:      {spec.proxyPort}
  ALLOWED_DOMAINS: comma-joined spec.allowedDomains (empty = allow all)
```

The spoke name is `NexusClient.metadata.name` resolved via `spec.clientRef`.

**Child 2 — HTTPProxy CR:**
```
name:      {cr-name}-tunnel
namespace: {cr-namespace}
spec:
  clientRef:    {spec.clientRef}
  localIP:      127.0.0.1
  localPort:    {spec.proxyPort}
  customDomains:
    - {nexusserver}-gateway.{hub-ns}.svc
  extraConfig: |
    locations = ["/{spokeName}/cloud"]
```

**Status reconciliation:**
- `Phase: Pending` — Deployment not yet ready or HTTPProxy not connected.
- `Phase: Running` — Deployment ready AND HTTPProxy phase is Running.
- `Phase: Failed` — Deployment in crash loop or HTTPProxy failed.
- `GatewayURL` is set once HTTPProxy reaches Running: `http://{gateway-fqdn}:{vhostHTTPPort}/{spokeName}/cloud`

**Finalizer:** `tunnel.io/cloudrestapiproxy-cleanup` — triggers HTTPProxy deletion and waits for tunnel teardown before removing the CR.

---

### 4. cloud-proxy Binary

**File:** `cmd/cloud-proxy/main.go`

Single HTTP server. All routing logic is in the request handler.

**Environment variables:**

| Var | Required | Description |
|---|---|---|
| `STRIP_PREFIX` | Yes | Path prefix to strip before processing, e.g. `/spoke1/cloud` |
| `PROXY_PORT` | No | Listening port, default `8080` |
| `ALLOWED_DOMAINS` | No | Comma-separated domain patterns. Empty = allow all |

**Request handler logic:**

```
1. Strip STRIP_PREFIX from request path.
   Error 400 if path does not start with STRIP_PREFIX.

2. Extract first path segment as cloudFQDN.
   Remaining path becomes apiPath.
   Error 400 if cloudFQDN is empty.

3. Check ALLOWED_DOMAINS (wildcard match).
   Error 403 if cloudFQDN not in allowlist.

4. Detect cloud provider:
   *.amazonaws.com                        → AWS
   *.googleapis.com                       → GCP
   *.azure.net | *.azure.com | *.azconfig.io → Azure
   Other                                  → Error 400 "unsupported cloud domain"

5. Acquire credentials:
   AWS:   AWS SDK v2, credential chain honours IRSA env vars
          (AWS_WEB_IDENTITY_TOKEN_FILE, AWS_ROLE_ARN).
          SigV4-sign the full request (method, URL, headers, body).
          Service name = first label of cloudFQDN.
          Region      = second label of cloudFQDN.

   GCP:   golang.org/x/oauth2/google default credentials
          (honours GOOGLE_APPLICATION_CREDENTIALS and metadata server).
          Add: Authorization: Bearer {token}

   Azure: Azure SDK DefaultAzureCredential
          (honours AZURE_CLIENT_ID, projected token, metadata server).
          Resource = https://{cloudFQDN}
          Add: Authorization: Bearer {token}

6. Build upstream URL: https://{cloudFQDN}{apiPath}
   Copy original query string.
   Copy original headers (except Host, Authorization).

7. Execute request with httputil.ReverseProxy (streaming).
   Forward response status, headers, and body verbatim.

8. On upstream error: return 502 with JSON error body.
```

---

### 5. RegistryProxy Migration

**Controller change** (`internal/controller/registryproxy_controller.go`):

- Replace owned `TCPProxy` child with owned `HTTPProxy` child.
- HTTPProxy spec mirrors CloudRestApiProxy's pattern: `customDomains` set to gateway FQDN, `extraConfig` sets `locations = ["/{spokeName}/registry"]`.
- Add `STRIP_PREFIX=/{spokeName}/registry` env var to the registry-proxy Deployment.
- Remove webhook-driven hub Service creation for RegistryProxy (the gateway Service is now owned by NexusServer controller).

**Binary change** (`cmd/registry-proxy/main.go`):

Read `STRIP_PREFIX` env var and apply `http.StripPrefix` before the existing `/v2/` handler registration. When `STRIP_PREFIX` is empty the binary behaves identically to today (backward-compatible for direct use).

**Status field change:**

`RegistryProxy.status.hubService` changes from a bare `host:port` to a full URL path:

```
Before: spoke1-registry.hub-ns.svc:6000
After:  http://{nexusserver}-gateway.hub-ns.svc:8080/spoke1/registry
```

**Docker client reconfiguration required:** Existing docker daemon `registry-mirrors` or `insecureRegistries` entries pointing to the old dedicated port must be updated to the new gateway URL path. This is a breaking change and must be called out in the release notes.

---

## Supported Cloud Services Reference

### AWS — SigV4 via IRSA

Service name and region auto-parsed from FQDN: `{service}.{region}.amazonaws.com`.

| Service | FQDN |
|---|---|
| Secrets Manager | `secretsmanager.{region}.amazonaws.com` |
| SSM Parameter Store | `ssm.{region}.amazonaws.com` |
| KMS | `kms.{region}.amazonaws.com` |
| S3 | `s3.{region}.amazonaws.com` |
| DynamoDB | `dynamodb.{region}.amazonaws.com` |
| Any AWS service | `{service}.{region}.amazonaws.com` |

### GCP — OAuth2 Bearer via Workload Identity

| Service | FQDN |
|---|---|
| Secret Manager | `secretmanager.googleapis.com` |
| Parameter Manager | `parametermanager.googleapis.com` |
| Cloud KMS | `cloudkms.googleapis.com` |
| Cloud Storage | `storage.googleapis.com` |
| Any GCP API | `{service}.googleapis.com` |

### Azure — Azure AD Bearer via Workload Identity

| Service | FQDN |
|---|---|
| Key Vault (secrets/keys) | `{vault-name}.vault.azure.net` |
| App Configuration | `{store}.azconfig.io` |
| Any Azure service | `*.azure.net` / `*.azure.com` |

---

## Error Handling

| Scenario | Response |
|---|---|
| Path does not start with STRIP_PREFIX | 400 Bad Request |
| No cloudFQDN segment in path | 400 Bad Request |
| cloudFQDN not in AllowedDomains | 403 Forbidden |
| Unrecognised cloud domain (not AWS/GCP/Azure) | 400 Bad Request |
| Credential acquisition failure (IRSA/WI not configured) | 502 Bad Gateway + JSON error |
| Upstream cloud API returns error | Response forwarded verbatim (4xx/5xx pass-through) |
| Upstream connection timeout | 504 Gateway Timeout |

---

## Testing Plan

| Layer | What |
|---|---|
| Unit | cloud-proxy handler: path stripping, FQDN parsing, provider detection, domain allowlist |
| Unit | registry-proxy: STRIP_PREFIX stripping leaves existing path logic unchanged |
| Unit | CloudRestApiProxy controller: owned resource generation (Deployment env vars, HTTPProxy customDomains and extraConfig) |
| Unit | NexusServer controller: gateway Service created only when vhostHTTPPort non-zero |
| Integration | CloudRestApiProxy CR → HTTPProxy CR created with correct locations |
| Integration | RegistryProxy CR → HTTPProxy CR (not TCPProxy) created |
| E2E | Hub caller hits gateway URL, cloud-proxy signs request, mock AWS/GCP endpoint validates signature |

---

## Open Questions

None — all design decisions resolved during brainstorming session.

---

## File Map

```
New:
  api/v1alpha1/cloudrestapiproxy_types.go
  internal/controller/cloudrestapiproxy_controller.go
  cmd/cloud-proxy/main.go
  config/crd/bases/tunnel.io_cloudrestapiproxies.yaml   (generated)
  config/samples/tunnel_v1alpha1_cloudrestapiproxy.yaml
  config/rbac/cloudrestapiproxy_editor_role.yaml        (generated)
  config/rbac/cloudrestapiproxy_viewer_role.yaml        (generated)
  docs/superpowers/specs/2026-05-03-cloud-rest-api-proxy-design.md

Modified:
  api/v1alpha1/nexusserver_types.go
  api/v1alpha1/zz_generated.deepcopy.go
  internal/controller/nexusserver_controller.go
  internal/controller/registryproxy_controller.go
  cmd/registry-proxy/main.go
  cmd/main.go
  config/samples/tunnel_v1alpha1_nexusserver.yaml
```
