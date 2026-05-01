# NexusClient

## Purpose

NexusClient deploys and manages an frpc (Fast Reverse Proxy client) instance as a Kubernetes Deployment, acting as the **spoke** in a hub-and-spoke tunnel topology. It connects outbound to a NexusServer and keeps the tunnel alive.

## Prerequisites

- cloud-nexus-operator installed in the cluster
- A NexusServer (or explicit server address/port) reachable from this cluster

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.image.repository` | `string` | No | `asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel` | Container image repository for frpc |
| `spec.image.tag` | `string` | No | `1.0.0` | Container image tag |
| `spec.serverRef.name` | `string` | No* | — | Name of a NexusServer in the same namespace; operator resolves address from its status |
| `spec.serverRef.address` | `string` | No* | — | Explicit frps host or IP (used when `name` is empty) |
| `spec.serverRef.port` | `integer` | No* | — | Explicit frps port (used when `name` is empty) |
| `spec.authTokenSecret.name` | `string` | No | auto-inherited | Name of the Secret containing the auth token |
| `spec.authTokenSecret.key` | `string` | No | auto-inherited | Key within the Secret |
| `spec.admin.port` | `integer` | No | `7400` | Local admin API port inside the frpc pod (used for hot-reload) |
| `spec.admin.tokenSecret.name` | `string` | No | auto-generated | Name of the Secret for the admin API token |
| `spec.admin.tokenSecret.key` | `string` | No | auto-generated | Key within the Secret |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to frpc.toml |

\* `spec.serverRef` is required. Provide either `name` (preferred) or `address`+`port`.

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_nexusclient.yaml
```

## Verify

```bash
kubectl get nexusclient nexusclient-sample
kubectl describe nexusclient nexusclient-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Operator is creating the frpc Deployment |
| `Running` | Deployment is ready and connected to the server; `status.serverAddress` is populated |
| `Failed` | Deployment could not reach ready state; check `status.conditions` |

`status.serverAddress` shows the resolved `host:port` the frpc pod is connecting to.

## Real-World Example

Spoke client connecting to a hub server with explicit auth token and custom admin:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: NexusClient
metadata:
  name: spoke-client
  namespace: tunnels
spec:
  serverRef:
    name: hub-server             # references a NexusServer in the same namespace
  authTokenSecret:
    name: shared-auth-token
    key: token
  admin:
    port: 7400
    tokenSecret:
      name: spoke-admin-token
      key: token
  extraConfig: |
    [log]
    to = "console"
    level = "info"
```

## Troubleshooting

- **Phase stays `Pending`**: Run `kubectl describe nexusclient <name>` and check `Conditions`. Check `kubectl get events -n <namespace>`.
- **Phase is `Failed`**: The frpc pod may be crash-looping. Run `kubectl logs -n <namespace> deploy/<nexusclient-name>` to see frpc output.
- **Cannot resolve NexusServer**: Ensure the referenced NexusServer is in the same namespace and its phase is `Running`.
- **Auth token mismatch**: Ensure `spec.authTokenSecret` points to the same token used by the NexusServer's `spec.authTokenSecret`.
- **Proxy hot-reload not working**: The operator uses the admin API for reload. Verify `spec.admin.port` is not conflicting with another process and the admin token secret exists.

## Related Resources

- [NexusServer](tunnel_v1alpha1_nexusserver.md) — the hub this client connects to
- [TCPProxy](tunnel_v1alpha1_tcpproxy.md) — TCP tunnel proxies using this client
- [HTTPProxy](tunnel_v1alpha1_httpproxy.md) — HTTP virtual host proxies using this client
- [RegistryProxy](tunnel_v1alpha1_registryproxy.md) — Docker registry proxy using this client
