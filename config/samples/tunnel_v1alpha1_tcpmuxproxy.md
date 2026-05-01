# TCPMuxProxy

## Purpose

TCPMuxProxy enables TCP multiplexing over a single frp tunnel connection using the HTTP CONNECT method. Multiple clients can share one tunnel by connecting through an HTTP CONNECT proxy, with routing determined by the target domain.

## Prerequisites

- A NexusClient in the same namespace and in `Running` phase
- DNS (or client configuration) pointing `customDomains` to the NexusServer's external address
- Clients that support HTTP CONNECT proxy protocol

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient in this namespace |
| `spec.localPort` | `integer` | Yes | — | Port of the local service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.customDomains` | `[]string` | No | — | Domain names for HTTP CONNECT routing |
| `spec.subdomain` | `string` | No | — | Subdomain prefix for frps subdomain routing |
| `spec.multiplexer` | `string` | No | `httpconnect` | Multiplexer type; only `httpconnect` is supported |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_tcpmuxproxy.yaml
```

## Verify

```bash
kubectl get tcpmuxproxy tcpmuxproxy-sample
kubectl describe tcpmuxproxy tcpmuxproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Proxy config written; waiting for frpc hot-reload |
| `Running` | frpc confirmed the proxy is active |
| `Failed` | frpc reported an error; check `status.conditions` |

## Real-World Example

Set up an HTTP CONNECT proxy for outbound traffic from the hub cluster routed through the spoke:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: TCPMuxProxy
metadata:
  name: http-connect-tunnel
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 3128          # local HTTP CONNECT proxy (e.g. Squid) on the spoke
  customDomains:
    - proxy.example.com
  multiplexer: httpconnect
```

Configure clients to use `proxy.example.com` as their HTTP CONNECT proxy.

## Troubleshooting

- **CONNECT tunnel not established**: Ensure the client sends a proper `CONNECT host:port HTTP/1.1` request with `Host: proxy.example.com`.
- **Wrong `multiplexer` value**: Only `httpconnect` is supported. Any other value will cause frp to reject the proxy configuration.
- **Local service not responding**: Verify the HTTP CONNECT proxy (or target service) is running on `spec.localIP:spec.localPort`.

## Related Resources

- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [NexusServer](tunnel_v1alpha1_nexusserver.md) — hub where connections arrive
- [HTTPProxy](tunnel_v1alpha1_httpproxy.md) — simpler HTTP virtual host without multiplexing
