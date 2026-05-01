# UDPProxy

## Purpose

UDPProxy tunnels a UDP port from the spoke side through frp to the hub side. Useful for DNS servers, game servers, and other UDP-based services.

## Prerequisites

- A NexusClient in the same namespace and in `Running` phase
- The local UDP service listening on `spec.localPort` on the spoke node

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient in this namespace |
| `spec.localPort` | `integer` | Yes | — | UDP port of the local service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.remotePort` | `integer` | No | auto-assigned | UDP port frps exposes on the hub side |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_udpproxy.yaml
```

## Verify

```bash
kubectl get udpproxy udpproxy-sample
kubectl describe udpproxy udpproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Operator has written the proxy config; waiting for frpc hot-reload |
| `Running` | frpc confirmed the proxy is active |
| `Failed` | frpc reported an error; check `status.conditions` |

## Real-World Example

Expose a DNS resolver running on the spoke at UDP port 53, accessible on the hub at port 6053:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: UDPProxy
metadata:
  name: dns-tunnel
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 53
  remotePort: 6053
```

Test from the hub:
```bash
dig @<nexusserver-status-address> -p 6053 example.com
```

## Troubleshooting

- **Phase stays `Pending`**: Verify the NexusClient is `Running`. Check conditions with `kubectl describe udpproxy <name>`.
- **No response on remote port**: UDP tunneling may have higher latency than TCP. Verify the local service is responding to UDP queries locally first.
- **Port conflict**: Ensure `spec.remotePort` is not used by another proxy on the same NexusClient.

## Related Resources

- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [NexusServer](tunnel_v1alpha1_nexusserver.md) — hub where the remote port is exposed
- [TCPProxy](tunnel_v1alpha1_tcpproxy.md) — TCP equivalent for TCP-based services
