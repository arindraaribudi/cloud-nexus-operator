# TCPProxy

## Purpose

TCPProxy tunnels a raw TCP port from the spoke side through frp to the hub side. Any TCP-based service (SSH, databases, custom applications) can be exposed without modifying the service itself.

## Prerequisites

- A NexusClient in the same namespace and in `Running` phase
- The local service listening on `spec.localPort` on the spoke node

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient in this namespace |
| `spec.localPort` | `integer` | Yes | — | Port of the local service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.remotePort` | `integer` | No | auto-assigned | Port frps exposes on the hub side |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_tcpproxy.yaml
```

## Verify

```bash
kubectl get tcpproxy tcpproxy-sample
kubectl describe tcpproxy tcpproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Operator has written the proxy config; waiting for frpc hot-reload |
| `Running` | frpc confirmed the proxy is active |
| `Failed` | frpc reported an error; check `status.conditions` |

## Real-World Example

Expose SSH (port 22) on the spoke side at port 6000 on the hub:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: TCPProxy
metadata:
  name: ssh-tunnel
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 22
  remotePort: 6000
```

Connect from the hub cluster:
```bash
ssh -p 6000 user@<nexusserver-status-address>
```

## Troubleshooting

- **Phase stays `Pending`**: Check that the NexusClient is `Running`. Verify the operator reconciled the proxy: `kubectl describe tcpproxy <name>` and look at conditions.
- **Connection refused on remote port**: Confirm `spec.remotePort` is not in use by another proxy. Check the NexusServer logs.
- **`localIP` service unreachable**: Ensure the local service is running on `spec.localIP:spec.localPort` inside the frpc pod's network namespace. Use `localIP: <ClusterIP>` if the service is a Kubernetes Service.
- **Bandwidth throttling needed**: Add `bandwidthLimit = "1MB"` to `spec.extraConfig`.

## Related Resources

- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [NexusServer](tunnel_v1alpha1_nexusserver.md) — hub where the remote port is exposed
- [STCPProxy](tunnel_v1alpha1_stcpproxy.md) — secure peer-to-peer TCP without exposing a public port
