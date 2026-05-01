# HTTPProxy

## Purpose

HTTPProxy tunnels HTTP traffic from the spoke side to the hub using frp's virtual host routing. Incoming HTTP requests on the NexusServer's `vhostHTTPPort` are routed to the correct spoke service based on the `Host` header matching `customDomains` or `subdomain`.

## Prerequisites

- A NexusClient in the same namespace and in `Running` phase
- The NexusServer has `spec.vhostHTTPPort` set and that port exposed in `spec.service.ports`
- DNS (or `/etc/hosts`) pointing `customDomains` entries to the NexusServer's external address

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient in this namespace |
| `spec.localPort` | `integer` | Yes | — | Port of the local HTTP service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.remotePort` | `integer` | No | — | Rarely needed; frps routes HTTP by virtual host via `vhostHTTPPort` |
| `spec.customDomains` | `[]string` | No | — | Fully-qualified domain names routed to this proxy by `Host` header |
| `spec.subdomain` | `string` | No | — | Subdomain prefix for frps subdomain routing (alternative to `customDomains`) |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_httpproxy.yaml
```

## Verify

```bash
kubectl get httpproxy httpproxy-sample
kubectl describe httpproxy httpproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Proxy config written; waiting for frpc hot-reload |
| `Running` | frpc confirmed the proxy is active |
| `Failed` | frpc reported an error; check `status.conditions` |

Test routing (replace address and domain):
```bash
curl -H "Host: app.example.com" http://<nexusserver-address>:8080/
```

## Real-World Example

Expose a web application running on port 3000 on the spoke, accessible via `app.example.com` on the hub:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: HTTPProxy
metadata:
  name: webapp-tunnel
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 3000
  customDomains:
    - app.example.com
  extraConfig: |
    hostHeaderRewrite = "localhost"
```

## Troubleshooting

- **404 or connection refused from hub**: Confirm `vhostHTTPPort` is set on NexusServer and the port is in `spec.service.ports`. Verify DNS resolves `customDomains` to the NexusServer address.
- **Wrong service responding**: Ensure `customDomains` entries are unique across all HTTPProxy resources on the same NexusServer.
- **frpc hot-reload fails**: Check the NexusClient admin API is reachable (see NexusClient troubleshooting).
- **Host header issues**: Use `hostHeaderRewrite` in `extraConfig` to rewrite the Host header before it reaches the backend service.

## Related Resources

- [NexusServer](tunnel_v1alpha1_nexusserver.md) — must have `vhostHTTPPort` configured
- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [HTTPSProxy](tunnel_v1alpha1_httpsproxy.md) — TLS/HTTPS equivalent
- [TCPMuxProxy](tunnel_v1alpha1_tcpmuxproxy.md) — HTTP CONNECT multiplexer alternative
