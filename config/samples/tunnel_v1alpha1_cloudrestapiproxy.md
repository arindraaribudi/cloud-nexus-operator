# CloudRestApiProxy

## Purpose

CloudRestApiProxy lets hub workloads call AWS, GCP, and Azure REST APIs through an FRP tunnel without embedding cloud credentials in the hub cluster. It deploys a `cloud-proxy` sidecar on the spoke side that intercepts outbound cloud API calls, signs them with the spoke's cloud identity (AWS SigV4 via IRSA, GCP Bearer via Workload Identity, Azure Bearer via Workload Identity), and forwards the signed request to the real cloud endpoint. Hub workloads reach cloud APIs through the shared hub gateway Service created by the NexusServer.

## Prerequisites

- cloud-nexus-operator installed in the cluster
- A NexusClient in the same namespace
- A NexusServer with `spec.vhostHTTPPort` set (creates the shared `{name}-gateway` Service)
- A Kubernetes ServiceAccount on the spoke annotated with cloud credentials:
  - **AWS**: `eks.amazonaws.com/role-arn` annotation (IRSA)
  - **GCP**: `iam.gke.io/gcp-service-account` annotation (Workload Identity)
  - **Azure**: `azure.workload.identity/client-id` annotation (Workload Identity)
  - **Tencent Cloud**: Set `TENCENTCLOUD_SECRET_ID` and `TENCENTCLOUD_SECRET_KEY` env vars on the `cloud-proxy` Deployment (e.g., via a Secret)

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.clientRef.name` | `string` | Yes | — | Name of the NexusClient (spoke) that owns the tunnel |
| `spec.serviceAccountName` | `string` | Yes | — | Kubernetes SA annotated with IRSA / Workload Identity for cloud auth |
| `spec.proxyPort` | `integer` | No | `8080` | Port the `cloud-proxy` pod listens on inside the spoke |
| `spec.allowedDomains` | `[]string` | No | all allowed | Allowlist of upstream cloud FQDNs; supports `*.` wildcards. Empty = permit all |
| `spec.image.repository` | `string` | No | inherited from NexusClient | Container image repository override |
| `spec.image.tag` | `string` | No | inherited from NexusClient | Container image tag override |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_cloudrestapiproxy.yaml
```

## Verify

```bash
kubectl get cloudrestapiproxy spoke1-cloud
kubectl describe cloudrestapiproxy spoke1-cloud
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Operator is creating the `cloud-proxy` Deployment and HTTPProxy tunnel |
| `Running` | Both the `cloud-proxy` Deployment and FRP tunnel are healthy |
| `Failed` | One or both components failed; check `status.conditions` |

`status.gatewayURL` contains the base URL on the hub gateway once the tunnel is up, e.g.:
```
http://main-hub-gateway.default.svc.cluster.local:8080/spoke1-client/cloud
```

Hub workloads call cloud APIs by routing through this URL:

```
GET {gatewayURL}/{cloudFQDN}{apiPath}
```

For example, to call AWS Secrets Manager from the hub:
```bash
curl http://main-hub-gateway.default.svc.cluster.local:8080/spoke1-client/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp
```

Check individual condition types:
```bash
kubectl get cloudrestapiproxy spoke1-cloud -o jsonpath='{.status.conditions}' | jq .
```

| Condition | Meaning |
|-----------|---------|
| `ProxyReady` | The `cloud-proxy` Deployment has at least one ready replica |
| `TunnelConnected` | The owned HTTPProxy is in `Running` phase |

## Request Routing

The `cloud-proxy` sidecar parses the incoming request path to extract the target cloud FQDN and API path:

```
/spoke1-client/cloud/{cloudFQDN}{apiPath}
                     ↑
                 e.g. secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp
```

It then detects the cloud provider from the FQDN suffix:

| FQDN suffix | Provider | Auth method |
|-------------|----------|-------------|
| `*.amazonaws.com` | AWS | SigV4 (IRSA) |
| `*.googleapis.com` | GCP | Bearer token (Workload Identity) |
| `*.azure.net`, `*.azure.com`, `*.azconfig.io` | Azure | Bearer token (Workload Identity) |
| `*.tencentcloudapi.com`, `*.myqcloud.com` | Tencent Cloud | TC3-HMAC-SHA256 (env vars) |

## Real-World Example

Access AWS Secrets Manager and GCP Secret Manager from hub workloads via a spoke in a different cloud account:

```yaml
apiVersion: tunnel.io/v1alpha1
kind: CloudRestApiProxy
metadata:
  name: prod-spoke-cloud
  namespace: tunnels
spec:
  clientRef:
    name: prod-spoke-client
  serviceAccountName: cloud-proxy-sa   # annotated: eks.amazonaws.com/role-arn=arn:aws:iam::...
  proxyPort: 8080
  allowedDomains:
    - "*.amazonaws.com"
    - "*.googleapis.com"
    - "*.tencentcloudapi.com"
```

After `status.phase` reaches `Running`, hub workloads call:
```bash
# AWS Secrets Manager
curl http://prod-spoke-cloud-gateway.tunnels.svc.cluster.local:8080/prod-spoke-client/cloud/secretsmanager.us-east-1.amazonaws.com/v1/secret/myapp

# GCP Secret Manager
curl http://prod-spoke-cloud-gateway.tunnels.svc.cluster.local:8080/prod-spoke-client/cloud/secretmanager.googleapis.com/v1/projects/my-project/secrets/my-secret/versions/latest:access

# Tencent Cloud CVM
curl -H "X-TC-Action: DescribeInstances" -H "X-TC-Version: 2017-03-12" \
  http://prod-spoke-cloud-gateway.tunnels.svc.cluster.local:8080/prod-spoke-client/cloud/cvm.tencentcloudapi.com/
```

## Troubleshooting

- **Phase stays `Pending`**: Ensure the NexusServer has `spec.vhostHTTPPort` set. Check `kubectl describe cloudrestapiproxy <name>` for condition messages.
- **`ProxyReady` is False**: The `cloud-proxy` Deployment failed to start. Check `kubectl logs -n <namespace> deploy/<name>-cloud-proxy`.
- **`TunnelConnected` is False**: The HTTPProxy tunnel is not running. Check `kubectl get httpproxy -n <namespace> <name>-tunnel` for its phase.
- **`403 domain not in allowedDomains`**: The target FQDN is blocked by `spec.allowedDomains`. Add the domain or its wildcard pattern.
- **`400 unsupported cloud domain`**: The FQDN doesn't match any known provider suffix. Only `*.amazonaws.com`, `*.googleapis.com`, `*.azure.net`, `*.azure.com`, `*.azconfig.io`, `*.tencentcloudapi.com`, and `*.myqcloud.com` are supported.
- **AWS auth failing**: Ensure the ServiceAccount has `eks.amazonaws.com/role-arn` set and the IAM role has the required permissions.
- **GCP auth failing**: Ensure the ServiceAccount has `iam.gke.io/gcp-service-account` set and the GSA has `roles/iam.workloadIdentityUser` on the KSA.
- **Azure auth failing**: Ensure the Pod has `azure.workload.identity/use: "true"` and the SA has `azure.workload.identity/client-id` set.
- **Tencent auth failing**: Ensure `TENCENTCLOUD_SECRET_ID` and `TENCENTCLOUD_SECRET_KEY` are set in the `cloud-proxy` pod environment.

## Related Resources

- [NexusServer](tunnel_v1alpha1_nexusserver.md) — must have `vhostHTTPPort` set to create the gateway Service
- [NexusClient](tunnel_v1alpha1_nexusclient.md) — referenced by `spec.clientRef`
- [HTTPProxy](tunnel_v1alpha1_httpproxy.md) — CloudRestApiProxy creates and owns an HTTPProxy internally
- [RegistryProxy](tunnel_v1alpha1_registryproxy.md) — similar pattern for private container registry access
