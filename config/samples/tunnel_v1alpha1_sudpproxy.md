# SUDPProxy

## Purpose

SUDPProxy (Secret UDP) provides a secure peer-to-peer UDP tunnel authenticated by a shared secret key. Like STCPProxy but for UDP traffic — no port is exposed publicly on the hub. Only visitors that present the correct secret key can access the proxied UDP service.

## Prerequisites

- A NexusClient in the same namespace acting as the **server side** (this proxy)
- A separate frpc instance acting as the **visitor side** with matching `secretKey`
- The local UDP service running on `spec.localPort`

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient (server side) in this namespace |
| `spec.localPort` | `integer` | Yes | — | UDP port of the local service on the spoke side |
| `spec.localIP` | `string` | No | `127.0.0.1` | IP of the local service |
| `spec.secretKey` | `string` | No | — | Shared secret; visitors must present this to connect |
| `spec.allowUsers` | `[]string` | No | allow all | frpc instance names permitted to connect as visitors |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to this proxy block |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_sudpproxy.yaml
```

## Verify

```bash
kubectl get sudpproxy sudpproxy-sample
kubectl describe sudpproxy sudpproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Proxy config written; waiting for frpc hot-reload |
| `Running` | frpc registered the SUDP server proxy |
| `Failed` | frpc reported an error; check `status.conditions` |

## Real-World Example

Expose a mDNS or DNS-SD service running on the spoke, accessible only to a trusted visitor:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: SUDPProxy
metadata:
  name: mdns-sudp-server
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  localPort: 5353
  secretKey: "mdns-secret-key"
  allowUsers:
    - hub-visitor-client
```

## Troubleshooting

- **Visitor cannot connect**: Verify `secretKey` matches exactly. Check that the visitor's frpc `name` is in `allowUsers` if the list is set.
- **UDP packets not arriving**: UDP tunneling through frp adds overhead. Ensure neither side has a firewall blocking the frp control port.
- **Public UDP port needed instead**: Use UDPProxy to expose a public UDP port on the hub without secret key authentication.

## Related Resources

- [UDPProxy](tunnel_v1alpha1_udpproxy.md) — public UDP port exposure without secret key
- [STCPProxy](tunnel_v1alpha1_stcpproxy.md) — secret TCP equivalent
- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
