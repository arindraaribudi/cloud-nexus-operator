# STCPProxy

## Purpose

STCPProxy (Secret TCP) provides a secure peer-to-peer TCP tunnel where both sides connect through the frps server using a shared secret key. Unlike TCPProxy, no port is exposed publicly on the hub — only visitors that know the secret key can access the proxied service.

## Prerequisites

- A NexusClient in the same namespace acting as the **server side** (this proxy)
- A separate frpc instance acting as the **visitor side** with matching `secretKey`
- The local service running on `spec.localPort`

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
kubectl apply -f tunnel_v1alpha1_stcpproxy.yaml
```

## Verify

```bash
kubectl get stcpproxy stcpproxy-sample
kubectl describe stcpproxy stcpproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Proxy config written; waiting for frpc hot-reload |
| `Running` | frpc registered the STCP server proxy |
| `Failed` | frpc reported an error; check `status.conditions` |

## Real-World Example

Expose an internal database on the spoke only to a specific trusted visitor frpc instance:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: STCPProxy
metadata:
  name: db-stcp-server
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 5432                # PostgreSQL on the spoke
  secretKey: "s3cr3t-key-db"
  allowUsers:
    - hub-visitor-client
```

The visitor frpc must be configured with:
```toml
[[visitors]]
name = "hub-visitor-client"
type = "stcp"
serverName = "db-stcp-server"
secretKey = "s3cr3t-key-db"
bindAddr = "127.0.0.1"
bindPort = 15432
```

## Troubleshooting

- **Visitor cannot connect**: Ensure `secretKey` matches exactly between the STCPProxy and the visitor frpc config. Check `allowUsers` — if set, the visitor's frpc `name` must be in the list.
- **Proxy not showing as Running**: Confirm the NexusClient is `Running` and the proxy name is unique within the NexusClient's tunnel.
- **No public port exposure needed**: STCP intentionally does not expose a port on the hub. If you need a publicly accessible port, use TCPProxy instead.

## Related Resources

- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [XTCPProxy](tunnel_v1alpha1_xtcpproxy.md) — extended P2P TCP with NAT traversal (visitor side)
- [TCPProxy](tunnel_v1alpha1_tcpproxy.md) — public TCP exposure without secret key
