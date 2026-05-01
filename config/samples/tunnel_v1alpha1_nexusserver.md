# NexusServer

## Purpose

NexusServer deploys and manages an frps (Fast Reverse Proxy server) instance as a Kubernetes Deployment and Service, acting as the **hub** in a hub-and-spoke tunnel topology. Spoke-side NexusClient instances connect outbound to this server.

## Prerequisites

- cloud-nexus-operator installed in the cluster
- (Optional) A Kubernetes Secret containing your own auth token

## Fields Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.image.repository` | `string` | No | `asia-southeast3-docker.pkg.dev/crd-operations/crd-gitops/crd-tunnel` | Container image repository for frps |
| `spec.image.tag` | `string` | No | `1.0.0` | Container image tag |
| `spec.replicas` | `integer` | No | `1` | Number of frps pod replicas |
| `spec.bindPort` | `integer` | No | `7000` | Port frps listens on for frpc client connections |
| `spec.vhostHTTPPort` | `integer` | No | — | Port for HTTP virtual host routing; required for HTTPProxy |
| `spec.vhostHTTPSPort` | `integer` | No | — | Port for HTTPS virtual host routing; required for HTTPSProxy |
| `spec.dashboardPort` | `integer` | No | — | Port for the frps web dashboard |
| `spec.dashboardUser` | `string` | No | — | Login username for the frps dashboard |
| `spec.dashboardPasswordSecret.name` | `string` | No | — | Name of the Secret containing the dashboard password |
| `spec.dashboardPasswordSecret.key` | `string` | No | — | Key within the Secret |
| `spec.authTokenSecret.name` | `string` | No | auto-generated | Name of the Secret containing the frps auth token |
| `spec.authTokenSecret.key` | `string` | No | auto-generated | Key within the Secret |
| `spec.extraConfig` | `string` | No | — | Raw TOML appended verbatim to frps.toml |
| `spec.service.type` | `string` | No | `LoadBalancer` | Kubernetes Service type |
| `spec.service.annotations` | `map[string]string` | No | — | Annotations passed verbatim to the Service metadata |
| `spec.service.ports` | `[]ServicePort` | No | — | Ports to expose on the Service |
| `spec.enableOperatorPlugin` | `boolean` | No | `false` | Enable frps httpPlugin for RegistryProxy lifecycle notifications |
| `spec.operatorWebhookAddr` | `string` | No | auto-derived | Address of the operator frps-plugin server; only used when `enableOperatorPlugin: true` |

## Apply

```bash
kubectl apply -f tunnel_v1alpha1_nexusserver.yaml
```

## Verify

```bash
kubectl get nexusserver nexusserver-sample
kubectl describe nexusserver nexusserver-sample
```

| Phase | Meaning |
|-------|---------|
| `Pending` | Operator is creating the Deployment and Service |
| `Running` | Deployment is ready; `status.address` and `status.port` are populated |
| `Failed` | Deployment could not reach ready state; check `status.conditions` |

Once `Running`, the resolved external address is in `status.address` and the bind port in `status.port`.

## Real-World Example

Production hub with auth token, HTTP/HTTPS virtual hosting, dashboard, and RegistryProxy plugin:

```yaml
apiVersion: tunnel.tunnel.io/v1alpha1
kind: NexusServer
metadata:
  name: hub-server
  namespace: tunnels
spec:
  replicas: 1
  bindPort: 7000
  vhostHTTPPort: 8080
  vhostHTTPSPort: 8443
  dashboardPort: 7500
  dashboardUser: admin
  dashboardPasswordSecret:
    name: hub-dashboard-secret
    key: password
  authTokenSecret:
    name: hub-auth-token
    key: token
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
    ports:
      - name: frp
        port: 7000
        protocol: TCP
      - name: http
        port: 8080
        protocol: TCP
      - name: https
        port: 8443
        protocol: TCP
  enableOperatorPlugin: true
```

## Troubleshooting

- **Phase stays `Pending`**: Run `kubectl describe nexusserver <name>` and check the `Conditions` section. Check `kubectl get events -n <namespace>` for Deployment errors.
- **No external IP in `status.address`**: The LoadBalancer Service may be pending cloud provisioning. Run `kubectl get svc` and verify your cloud LB controller is healthy.
- **frpc cannot connect**: Confirm `status.address` is reachable from the spoke cluster and that `spec.bindPort` is open in any network policy or firewall.
- **HTTPProxy / HTTPSProxy not routing**: Ensure `vhostHTTPPort` / `vhostHTTPSPort` are set on the NexusServer and those ports are included in `spec.service.ports`.
- **RegistryProxy not working**: Ensure `enableOperatorPlugin: true`. Check operator pod logs (`kubectl logs -n cloud-nexus-operator-system deploy/cloud-nexus-operator-controller-manager`) for plugin registration errors.

## Related Resources

- [NexusClient](tunnel_v1alpha1_nexusclient.md) — spoke-side frpc client that connects to this server
- [RegistryProxy](tunnel_v1alpha1_registryproxy.md) — requires `enableOperatorPlugin: true` on NexusServer
- [HTTPProxy](tunnel_v1alpha1_httpproxy.md) — requires `vhostHTTPPort` on NexusServer
- [HTTPSProxy](tunnel_v1alpha1_httpsproxy.md) — requires `vhostHTTPSPort` on NexusServer
