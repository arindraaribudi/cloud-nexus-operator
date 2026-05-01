# RegistryProxy

## Purpose

RegistryProxy tunnels access to a private container registry through the frp tunnel. It deploys a registry-proxy sidecar on the spoke side and creates a hub-side Kubernetes Service, allowing pods in the hub cluster to pull images via a synthetic Docker registry endpoint backed by cloud credentials.

## Prerequisites

- cloud-nexus-operator installed in the cluster
- A NexusClient in the same namespace
- A NexusServer with `enableOperatorPlugin: true`
- A Kubernetes ServiceAccount annotated for Workload Identity (GKE) or IRSA (EKS) with read access to the target registry

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient (spoke) that owns the tunnel |
| `spec.remotePort` | `integer` | Yes | — | Port frps will bind on the hub side; must not conflict with other proxy remote ports |
| `spec.serviceAccountName` | `string` | Yes | — | Kubernetes ServiceAccount annotated with WI/IRSA for cloud registry auth |
| `spec.proxyPort` | `integer` | No | `5000` | Port the registry-proxy binary listens on inside the Pod |
| `spec.image.repository` | `string` | No | inherited from NexusClient | Container image repository override |
| `spec.image.tag` | `string` | No | inherited from NexusClient | Container image tag override |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_registryproxy.yaml
```

## Verify

```bash
kubectl get registryproxy registryproxy-sample
kubectl describe registryproxy registryproxy-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Operator is creating the registry-proxy Deployment and TCPProxy |
| `Running` | Both the registry-proxy Deployment and tunnel are healthy |
| `Failed` | One or both components failed; check `status.conditions` |

`status.hubService` contains the FQDN of the hub-side Service once created, e.g.:
```
registryproxy-sample-docker.tunnels.svc.cluster.local
```

Use this address as a Docker registry endpoint:
```bash
docker pull registryproxy-sample-docker.tunnels.svc.cluster.local/my-image:tag
```

Check individual condition types:
```bash
kubectl get registryproxy registryproxy-sample -o jsonpath='{.status.conditions}' | jq .
```

| Condition | Meaning |
|-----------|---------|
| `ProxyReady` | The registry-proxy Deployment is healthy |
| `TunnelConnected` | The owned TCPProxy is in `Running` phase |

## Real-World Example

Mirror a GCP Artifact Registry through the tunnel to a separate hub cluster:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: RegistryProxy
metadata:
  name: artifact-registry-mirror
  namespace: tunnels
spec:
  clientRef:
    name: spoke-client
  remotePort: 15000
  serviceAccountName: artifact-registry-sa  # annotated: iam.gke.io/gcp-service-account=...
  proxyPort: 5000
```

After `status.phase` reaches `Running`, configure your hub-cluster nodes or image pull policy to use `artifact-registry-mirror-docker.tunnels.svc.cluster.local` as the registry.

## Troubleshooting

- **Phase stays `Pending`**: Ensure the NexusServer has `enableOperatorPlugin: true`. Check `kubectl describe registryproxy <name>` for condition messages.
- **`ProxyReady` is False**: The registry-proxy Deployment failed to start. Check `kubectl logs -n <namespace> deploy/<registryproxy-name>`.
- **`TunnelConnected` is False**: The underlying TCPProxy is not running. Check `kubectl get tcpproxy -n <namespace>` for the owned proxy and its phase.
- **Hub-side Service not appearing**: The operator plugin must be reachable. Check `spec.operatorWebhookAddr` on the NexusServer or leave it empty for auto-derivation.
- **Cloud auth failing**: Ensure the ServiceAccount is correctly annotated for WI/IRSA and the IAM binding grants `roles/artifactregistry.reader` (GCP) or equivalent (AWS).

## Related Resources

- [NexusServer](tunnel_v1alpha1_nexusserver.md) — must have `enableOperatorPlugin: true`
- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [TCPProxy](tunnel_v1alpha1_tcpproxy.md) — RegistryProxy creates and owns a TCPProxy internally
