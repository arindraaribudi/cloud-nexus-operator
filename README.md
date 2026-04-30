# cloud-nexus-operator

A Kubernetes operator for managing secure network tunnels and container registry proxies across clusters, built on top of [FRP (Fast Reverse Proxy)](https://github.com/fatedier/frp).

## Features

1. **Hub and Spoke Tunnel** — Deploy FRP server (`NexusServer`) on a hub cluster and connect spoke clusters via `NexusClient`, forming a secure reverse-proxy mesh across environments
2. **Docker Registry Proxy** — Expose private container registries through tunnels using `RegistryProxy`, with cloud auth (IRSA/Workload Identity) and automatic Service/Deployment lifecycle management
3. **Multi-Protocol Tunnel Proxies** — Configure individual tunnels (`FrpProxy`) for TCP, UDP, HTTP, HTTPS, STCP, XTCP, and TCPMUX traffic
4. **Hot-Reload Configuration** — Update tunnel configurations without service restarts using the FRP admin API; changes propagate live via ConfigMap volumes
5. **Event-Driven Service Management** — FRP server webhook plugin listens for proxy connect/disconnect events and automatically creates or removes Kubernetes Services
6. **Secret & Credential Management** — Auto-generate authentication tokens and dashboard passwords, or bring your own via `SecretKeyRef` references
7. **Status & Conditions Tracking** — Per-resource phase reporting (Pending/Running/Failed) with standard Kubernetes `Conditions`, address resolution, and last-reload timestamps
8. **RBAC Roles** — Pre-built Admin, Editor, and Viewer `ClusterRole` definitions for all CRDs

## Description

cloud-nexus-operator provides Kubernetes-native management of FRP-based tunnels. It introduces four CRDs — `NexusServer`, `NexusClient`, `FrpProxy`, and `RegistryProxy` — that together enable hub-and-spoke connectivity, secure port forwarding, and private registry access across Kubernetes clusters without requiring direct network peering.

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Dependencies

#### FRP (Fast Reverse Proxy)

This project uses **FRP** (Fast Reverse Proxy) as a core tunneling component. FRP provides high-performance reverse proxy and tunnel capabilities for the cloud-nexus-operator.

- **Project**: [fatedier/frp](https://github.com/fatedier/frp)
- **Version**: v0.68.1 (or as specified in `docker/Dockerfile.frp`)
- **Components Used**:
  - `frps` (FRP Server): Handles inbound tunnel connections and proxying
  - `frpc` (FRP Client): Manages outbound tunnel connections from clients
- **Integration**: FRP binaries are built as part of the multi-stage Docker build in `docker/Dockerfile.frp` and are used by the NexusClient and FrpProxy controllers for tunnel management

For more information about FRP, see the [official FRP documentation](https://github.com/fatedier/frp/blob/master/README.md).

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/cloud-nexus-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/cloud-nexus-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/cloud-nexus-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/cloud-nexus-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

