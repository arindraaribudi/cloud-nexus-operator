# XTCPProxy

## Purpose

XTCPProxy (Extended TCP) provides a peer-to-peer TCP tunnel with NAT traversal. Both sides connect outbound through frps to establish a direct peer-to-peer connection, bypassing the server for data transfer. Requires a shared secret key like STCPProxy but attempts hole-punching for direct connectivity.

## Prerequisites

- A NexusClient in the same namespace acting as the **server side** (this proxy)
- A separate frpc instance acting as the **visitor side** with matching `secretKey`
- The local service running on `spec.localPort`
- Both sides must be able to initiate outbound UDP connections (for NAT traversal)

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient (server side) in this namespace |
| `spec.localPort` | `integer` | Yes | — | Port of the local service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.secretKey` | `string` | No | — | Shared secret; visitors must present this to connect |
| `spec.allowUsers` | `[]string` | No | allow all | frpc instance names permitted to connect as visitors |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_xtcpproxy.yaml
```

## Verify

```bash
kubectl get xtcpproxy xtcpproxy-sample
kubectl describe xtcpproxy xtcpproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Proxy config written; waiting for frpc hot-reload |
| `Running` | frpc registered the XTCP server proxy |
| `Failed` | frpc reported an error; check `status.conditions` |

## Real-World Example

Low-latency direct connection between two nodes behind NAT:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: XTCPProxy
metadata:
  name: ssh-xtcp-server
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 22
  secretKey: "p2p-secret-key"
  allowUsers:
    - hub-visitor-client
```

## Troubleshooting

- **NAT traversal failing**: XTCP requires outbound UDP. Strict NAT or firewalls that block UDP will prevent hole-punching. Fall back to STCPProxy in that case.
- **Visitor cannot connect**: Verify `secretKey` matches and the visitor's frpc `name` is in `allowUsers` if set.
- **High latency**: If NAT traversal fails, frp falls back to relaying traffic through frps, increasing latency. Check frpc logs for "xtcp: fallback to stcp" messages.

## Related Resources

- [STCPProxy](tunnel_v1alpha1_stcpproxy.md) — similar but without NAT traversal; more reliable through strict firewalls
- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [TCPProxy](tunnel_v1alpha1_tcpproxy.md) — simpler public TCP exposure
