# HTTPSProxy

## Purpose

HTTPSProxy tunnels HTTPS (TLS) traffic from the spoke side to the hub using frp's virtual host routing. Incoming HTTPS requests on the NexusServer's `vhostHTTPSPort` are routed to the correct spoke service based on SNI (Server Name Indication) matching `customDomains` or `subdomain`. TLS is terminated at the spoke service.

## Prerequisites

- A NexusClient in the same namespace and in `Running` phase
- The NexusServer has `spec.vhostHTTPSPort` set and that port exposed in `spec.service.ports`
- DNS (or `/etc/hosts`) pointing `customDomains` to the NexusServer's external address
- The local service serves TLS on `spec.localPort`

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient in this namespace |
| `spec.localPort` | `integer` | Yes | — | Port of the local HTTPS service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.remotePort` | `integer` | No | — | Rarely needed; frps routes HTTPS by SNI via `vhostHTTPSPort` |
| `spec.customDomains` | `[]string` | No | — | Fully-qualified domain names matched by SNI |
| `spec.subdomain` | `string` | No | — | Subdomain prefix for frps subdomain routing |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_httpsproxy.yaml
```

## Verify

```bash
kubectl get httpsproxy httpsproxy-sample
kubectl describe httpsproxy httpsproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Proxy config written; waiting for frpc hot-reload |
| `Running` | frpc confirmed the proxy is active |
| `Failed` | frpc reported an error; check `status.conditions` |

Test routing:
```bash
curl https://secure.example.com:<vhostHTTPSPort>/
```

## Real-World Example

Expose a TLS-enabled API running on port 8443 on the spoke via `api.example.com` on the hub:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: HTTPSProxy
metadata:
  name: api-tls-tunnel
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 8443
  customDomains:
    - api.example.com
```

## Troubleshooting

- **TLS handshake failure**: The local service must handle TLS itself. frps passes the TLS stream through without terminating it. Verify the local service has valid TLS certs for `customDomains`.
- **SNI not matching**: Ensure the client sends the correct SNI (matching `customDomains`) when connecting.
- **`vhostHTTPSPort` not configured**: Set it on the NexusServer and add it to `spec.service.ports`.
- **Certificate mismatch**: The certificate served by `spec.localPort` must be valid for the `customDomains` entry.

## Related Resources

- [NexusServer](tunnel_v1alpha1_nexusserver.md) — must have `vhostHTTPSPort` configured
- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [HTTPProxy](tunnel_v1alpha1_httpproxy.md) — plaintext HTTP equivalent
