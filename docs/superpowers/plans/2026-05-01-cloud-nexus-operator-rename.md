# cloud-nexus-operator Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the project from `crd-tunnel-service` / `frp-operator` to `cloud-nexus-operator`, and rename CRD kinds `FrpClient` → `NexusClient` and `FrpServer` → `NexusServer` throughout the entire codebase.

**Architecture:** This is a mechanical rename across Go source files, YAML manifests, and config files. Go types are renamed first so `make generate manifests` can regenerate auto-generated files (deepcopy, CRD YAMLs, role.yaml). Manually-maintained YAML files are updated by hand. Files are git-renamed to keep history.

**Tech Stack:** Go 1.25, Kubebuilder v4, controller-gen v0.20.1, kustomize v5, Kubernetes CRD operator pattern.

---

## Rename Map

| Old | New |
|-----|-----|
| `tunnel.io/frp-operator` (Go module) | `tunnel.io/cloud-nexus-operator` |
| `crd-tunnel-service` (project name) | `cloud-nexus-operator` |
| `crd-tunnel-service-system` (namespace) | `cloud-nexus-operator-system` |
| `crd-tunnel-service-` (namePrefix) | `cloud-nexus-operator-` |
| `FrpClient` (Go kind) | `NexusClient` |
| `FrpServer` (Go kind) | `NexusServer` |
| `frpclients` (CRD plural) | `nexusclients` |
| `frpservers` (CRD plural) | `nexusservers` |

> `FrpProxy` is **not** renamed.

---

### Task 1: Update Go module path

**Files:**
- Modify: `go.mod:1`

- [ ] **Step 1: Update module declaration**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
go mod edit -module tunnel.io/cloud-nexus-operator
```

Expected: `go.mod` line 1 becomes `module tunnel.io/cloud-nexus-operator`

- [ ] **Step 2: Verify**

```bash
head -1 go.mod
```

Expected output: `module tunnel.io/cloud-nexus-operator`

- [ ] **Step 3: Commit**

```bash
git add go.mod
git commit -m "chore: update go module to tunnel.io/cloud-nexus-operator"
```

---

### Task 2: Update all Go import paths

**Files:**
- Modify: all `*.go` files under `cmd/`, `internal/`, `api/`, `test/`

- [ ] **Step 1: Bulk replace import path in all Go files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
find . -name "*.go" -not -path "./.git/*" | xargs LC_ALL=C sed -i '' 's|tunnel\.io/frp-operator|tunnel.io/cloud-nexus-operator|g'
```

- [ ] **Step 2: Verify no old import paths remain**

```bash
grep -r "tunnel\.io/frp-operator" --include="*.go" .
```

Expected: no output (zero matches)

- [ ] **Step 3: Verify Go builds**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: update all Go import paths to tunnel.io/cloud-nexus-operator"
```

---

### Task 3: Rename Go types FrpClient → NexusClient

**Files:**
- Modify: `api/v1alpha1/frpclient_types.go`
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (will be regenerated in Task 8)
- Modify: `internal/controller/frpclient_controller.go`
- Modify: `internal/controller/frpclient_controller_test.go`
- Modify: `cmd/main.go`
- Modify: `internal/config/client.go`
- Modify: `internal/config/client_test.go`

- [ ] **Step 1: Rename all FrpClient type variants in all Go files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
find . -name "*.go" -not -path "./.git/*" | xargs LC_ALL=C sed -i '' \
  -e 's/FrpClientSpec/NexusClientSpec/g' \
  -e 's/FrpClientStatus/NexusClientStatus/g' \
  -e 's/FrpClientList/NexusClientList/g' \
  -e 's/FrpClient/NexusClient/g'
```

- [ ] **Step 2: Verify FrpClient no longer appears in Go files (except comments about frp internals)**

```bash
grep -rn "FrpClient" --include="*.go" .
```

Expected: no output

- [ ] **Step 3: Verify Go builds**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: rename FrpClient → NexusClient Go types"
```

---

### Task 4: Rename Go types FrpServer → NexusServer

**Files:**
- Modify: `api/v1alpha1/frpserver_types.go`
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (will be regenerated in Task 8)
- Modify: `internal/controller/frpserver_controller.go`
- Modify: `internal/controller/frpserver_controller_test.go`
- Modify: `cmd/main.go`
- Modify: `internal/config/server.go`
- Modify: `internal/config/server_test.go`
- Modify: `internal/webhook/frps_events.go`
- Modify: `internal/webhook/frps_events_test.go`

- [ ] **Step 1: Rename all FrpServer type variants in all Go files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
find . -name "*.go" -not -path "./.git/*" | xargs LC_ALL=C sed -i '' \
  -e 's/FrpServerSpec/NexusServerSpec/g' \
  -e 's/FrpServerStatus/NexusServerStatus/g' \
  -e 's/FrpServerList/NexusServerList/g' \
  -e 's/FrpServer/NexusServer/g'
```

- [ ] **Step 2: Verify FrpServer no longer appears in Go files**

```bash
grep -rn "FrpServer" --include="*.go" .
```

Expected: no output

- [ ] **Step 3: Verify Go builds**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: rename FrpServer → NexusServer Go types"
```

---

### Task 5: Rename Go source files

**Files:**
- Rename: `api/v1alpha1/frpclient_types.go` → `api/v1alpha1/nexusclient_types.go`
- Rename: `api/v1alpha1/frpserver_types.go` → `api/v1alpha1/nexusserver_types.go`
- Rename: `internal/controller/frpclient_controller.go` → `internal/controller/nexusclient_controller.go`
- Rename: `internal/controller/frpclient_controller_test.go` → `internal/controller/nexusclient_controller_test.go`
- Rename: `internal/controller/frpserver_controller.go` → `internal/controller/nexusserver_controller.go`
- Rename: `internal/controller/frpserver_controller_test.go` → `internal/controller/nexusserver_controller_test.go`

- [ ] **Step 1: Git rename all frpclient and frpserver Go files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
git mv api/v1alpha1/frpclient_types.go api/v1alpha1/nexusclient_types.go
git mv api/v1alpha1/frpserver_types.go api/v1alpha1/nexusserver_types.go
git mv internal/controller/frpclient_controller.go internal/controller/nexusclient_controller.go
git mv internal/controller/frpclient_controller_test.go internal/controller/nexusclient_controller_test.go
git mv internal/controller/frpserver_controller.go internal/controller/nexusserver_controller.go
git mv internal/controller/frpserver_controller_test.go internal/controller/nexusserver_controller_test.go
```

- [ ] **Step 2: Verify Go builds after rename**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "chore: rename frpclient/frpserver Go source files to nexusclient/nexusserver"
```

---

### Task 6: Update PROJECT file

**Files:**
- Modify: `PROJECT`

- [ ] **Step 1: Update PROJECT file**

Replace the full contents of `PROJECT` with:

```yaml
# Code generated by tool. DO NOT EDIT.
# This file is used to track the info used to scaffold your project
# and allow the plugins properly work.
# More info: https://book.kubebuilder.io/reference/project-config.html
cliVersion: 4.13.1
domain: tunnel.io
layout:
- go.kubebuilder.io/v4
projectName: cloud-nexus-operator
repo: tunnel.io/cloud-nexus-operator
resources:
- api:
    crdVersion: v1
    namespaced: true
  controller: true
  domain: tunnel.io
  group: tunnel
  kind: NexusServer
  path: tunnel.io/cloud-nexus-operator/api/v1alpha1
  version: v1alpha1
- api:
    crdVersion: v1
    namespaced: true
  controller: true
  domain: tunnel.io
  group: tunnel
  kind: NexusClient
  path: tunnel.io/cloud-nexus-operator/api/v1alpha1
  version: v1alpha1
- api:
    crdVersion: v1
    namespaced: true
  controller: true
  domain: tunnel.io
  group: tunnel
  kind: FrpProxy
  path: tunnel.io/cloud-nexus-operator/api/v1alpha1
  version: v1alpha1
version: "3"
```

- [ ] **Step 2: Commit**

```bash
git add PROJECT
git commit -m "chore: update PROJECT file for cloud-nexus-operator rename"
```

---

### Task 7: Update static config YAML files

**Files:**
- Modify: `config/default/kustomization.yaml`
- Modify: `config/manager/manager.yaml`
- Modify: `config/rbac/kustomization.yaml`

- [ ] **Step 1: Update namespace and namePrefix in config/default/kustomization.yaml**

In `config/default/kustomization.yaml`, change:
```yaml
namespace: crd-tunnel-service-system
namePrefix: crd-tunnel-service-
```
to:
```yaml
namespace: cloud-nexus-operator-system
namePrefix: cloud-nexus-operator-
```

- [ ] **Step 2: Update app labels and WEBHOOK_SERVICE_ADDR in config/manager/manager.yaml**

In `config/manager/manager.yaml`, replace all `crd-tunnel-service` with `cloud-nexus-operator`:
- Line 7: `app.kubernetes.io/name: cloud-nexus-operator`
- Line 17: `app.kubernetes.io/name: cloud-nexus-operator`
- Line 23: `app.kubernetes.io/name: cloud-nexus-operator`
- Line 30: `app.kubernetes.io/name: cloud-nexus-operator`
- Line 70 (WEBHOOK_SERVICE_ADDR value): `cloud-nexus-operator-controller-manager-frps-plugin.cloud-nexus-operator-system.svc.cluster.local:9090`

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
LC_ALL=C sed -i '' 's/crd-tunnel-service/cloud-nexus-operator/g' config/manager/manager.yaml
LC_ALL=C sed -i '' 's/crd-tunnel-service/cloud-nexus-operator/g' config/default/kustomization.yaml
```

- [ ] **Step 3: Update comment reference in config/rbac/kustomization.yaml**

```bash
LC_ALL=C sed -i '' 's/crd-tunnel-service/cloud-nexus-operator/g' config/rbac/kustomization.yaml
```

- [ ] **Step 4: Verify**

```bash
grep -r "crd-tunnel-service" config/default/ config/manager/ config/rbac/kustomization.yaml
```

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add config/default/kustomization.yaml config/manager/manager.yaml config/rbac/kustomization.yaml
git commit -m "chore: update config YAML namespace/namePrefix/labels to cloud-nexus-operator"
```

---

### Task 8: Regenerate deepcopy and CRD manifests

**Files:**
- Regenerate: `api/v1alpha1/zz_generated.deepcopy.go`
- Regenerate: `config/crd/bases/tunnel.tunnel.io_nexusclients.yaml`
- Regenerate: `config/crd/bases/tunnel.tunnel.io_nexusservers.yaml`
- Delete: `config/crd/bases/tunnel.tunnel.io_frpclients.yaml`
- Delete: `config/crd/bases/tunnel.tunnel.io_frpservers.yaml`
- Regenerate: `config/rbac/role.yaml`

- [ ] **Step 1: Run code generation**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
make generate manifests
```

Expected: exits 0, no errors. New files `tunnel.tunnel.io_nexusclients.yaml` and `tunnel.tunnel.io_nexusservers.yaml` appear in `config/crd/bases/`.

- [ ] **Step 2: Delete old CRD YAML files that are no longer generated**

```bash
rm -f config/crd/bases/tunnel.tunnel.io_frpclients.yaml
rm -f config/crd/bases/tunnel.tunnel.io_frpservers.yaml
```

- [ ] **Step 3: Verify new CRD files exist with correct kinds**

```bash
grep "kind:" config/crd/bases/tunnel.tunnel.io_nexusclients.yaml | head -3
grep "kind:" config/crd/bases/tunnel.tunnel.io_nexusservers.yaml | head -3
```

Expected output includes `kind: NexusClient` and `kind: NexusServer`.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: regenerate deepcopy and CRD manifests for NexusClient/NexusServer"
```

---

### Task 9: Rename RBAC role files and update their content

**Files:**
- Rename: `config/rbac/frpclient_admin_role.yaml` → `config/rbac/nexusclient_admin_role.yaml`
- Rename: `config/rbac/frpclient_editor_role.yaml` → `config/rbac/nexusclient_editor_role.yaml`
- Rename: `config/rbac/frpclient_viewer_role.yaml` → `config/rbac/nexusclient_viewer_role.yaml`
- Rename: `config/rbac/frpserver_admin_role.yaml` → `config/rbac/nexusserver_admin_role.yaml`
- Rename: `config/rbac/frpserver_editor_role.yaml` → `config/rbac/nexusserver_editor_role.yaml`
- Rename: `config/rbac/frpserver_viewer_role.yaml` → `config/rbac/nexusserver_viewer_role.yaml`

- [ ] **Step 1: Git rename RBAC files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
git mv config/rbac/frpclient_admin_role.yaml   config/rbac/nexusclient_admin_role.yaml
git mv config/rbac/frpclient_editor_role.yaml  config/rbac/nexusclient_editor_role.yaml
git mv config/rbac/frpclient_viewer_role.yaml  config/rbac/nexusclient_viewer_role.yaml
git mv config/rbac/frpserver_admin_role.yaml   config/rbac/nexusserver_admin_role.yaml
git mv config/rbac/frpserver_editor_role.yaml  config/rbac/nexusserver_editor_role.yaml
git mv config/rbac/frpserver_viewer_role.yaml  config/rbac/nexusserver_viewer_role.yaml
```

- [ ] **Step 2: Update content of renamed RBAC files**

Replace `frpclients` → `nexusclients`, `frpservers` → `nexusservers`, role names, and `crd-tunnel-service` label:

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
for f in config/rbac/nexusclient_*.yaml config/rbac/nexusserver_*.yaml; do
  LC_ALL=C sed -i '' \
    -e 's/frpclients/nexusclients/g' \
    -e 's/frpservers/nexusservers/g' \
    -e 's/frpclient-admin-role/nexusclient-admin-role/g' \
    -e 's/frpclient-editor-role/nexusclient-editor-role/g' \
    -e 's/frpclient-viewer-role/nexusclient-viewer-role/g' \
    -e 's/frpserver-admin-role/nexusserver-admin-role/g' \
    -e 's/frpserver-editor-role/nexusserver-editor-role/g' \
    -e 's/frpserver-viewer-role/nexusserver-viewer-role/g' \
    -e 's/crd-tunnel-service/cloud-nexus-operator/g' \
    "$f"
done
```

- [ ] **Step 3: Update config/rbac/kustomization.yaml references**

In `config/rbac/kustomization.yaml`, replace:
```yaml
- frpclient_admin_role.yaml
- frpclient_editor_role.yaml
- frpclient_viewer_role.yaml
- frpserver_admin_role.yaml
- frpserver_editor_role.yaml
- frpserver_viewer_role.yaml
```
with:
```yaml
- nexusclient_admin_role.yaml
- nexusclient_editor_role.yaml
- nexusclient_viewer_role.yaml
- nexusserver_admin_role.yaml
- nexusserver_editor_role.yaml
- nexusserver_viewer_role.yaml
```

```bash
LC_ALL=C sed -i '' \
  -e 's/frpclient_admin_role/nexusclient_admin_role/g' \
  -e 's/frpclient_editor_role/nexusclient_editor_role/g' \
  -e 's/frpclient_viewer_role/nexusclient_viewer_role/g' \
  -e 's/frpserver_admin_role/nexusserver_admin_role/g' \
  -e 's/frpserver_editor_role/nexusserver_editor_role/g' \
  -e 's/frpserver_viewer_role/nexusserver_viewer_role/g' \
  config/rbac/kustomization.yaml
```

- [ ] **Step 4: Verify**

```bash
grep -rn "frpclient\|frpserver" config/rbac/
```

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: rename RBAC role files frpclient/frpserver → nexusclient/nexusserver"
```

---

### Task 10: Rename and update sample YAML files

**Files:**
- Rename: `config/samples/tunnel_v1alpha1_frpclient.yaml` → `config/samples/tunnel_v1alpha1_nexusclient.yaml`
- Rename: `config/samples/tunnel_v1alpha1_frpserver.yaml` → `config/samples/tunnel_v1alpha1_nexusserver.yaml`
- Modify: `config/samples/kustomization.yaml`
- Rename: `config/deploy/samples/frpclient.yaml` → `config/deploy/samples/nexusclient.yaml`
- Rename: `config/deploy/samples/frpserver.yaml` → `config/deploy/samples/nexusserver.yaml`

- [ ] **Step 1: Git rename sample files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
git mv config/samples/tunnel_v1alpha1_frpclient.yaml config/samples/tunnel_v1alpha1_nexusclient.yaml
git mv config/samples/tunnel_v1alpha1_frpserver.yaml config/samples/tunnel_v1alpha1_nexusserver.yaml
git mv config/deploy/samples/frpclient.yaml config/deploy/samples/nexusclient.yaml
git mv config/deploy/samples/frpserver.yaml config/deploy/samples/nexusserver.yaml
```

- [ ] **Step 2: Update kind and labels in renamed sample files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
for f in config/samples/tunnel_v1alpha1_nexusclient.yaml \
          config/samples/tunnel_v1alpha1_nexusserver.yaml \
          config/deploy/samples/nexusclient.yaml \
          config/deploy/samples/nexusserver.yaml; do
  LC_ALL=C sed -i '' \
    -e 's/kind: FrpClient/kind: NexusClient/g' \
    -e 's/kind: FrpServer/kind: NexusServer/g' \
    -e 's/frpclient-sample/nexusclient-sample/g' \
    -e 's/frpserver-sample/nexusserver-sample/g' \
    -e 's/crd-tunnel-service/cloud-nexus-operator/g' \
    "$f"
done
```

- [ ] **Step 3: Update config/samples/kustomization.yaml**

```bash
LC_ALL=C sed -i '' \
  -e 's/tunnel_v1alpha1_frpclient/tunnel_v1alpha1_nexusclient/g' \
  -e 's/tunnel_v1alpha1_frpserver/tunnel_v1alpha1_nexusserver/g' \
  config/samples/kustomization.yaml
```

- [ ] **Step 4: Verify**

```bash
grep -rn "FrpClient\|FrpServer\|frpclient\|frpserver\|crd-tunnel-service" config/samples/ config/deploy/samples/
```

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: rename sample YAML files and update kind references to NexusClient/NexusServer"
```

---

### Task 11: Update remaining config YAML files

**Files:**
- Modify: `config/deploy/bundle-install.yaml`
- Modify: `config/deploy/bundle-impl.yaml`
- Modify: all remaining YAML files still containing `crd-tunnel-service` or `frpclient`/`frpserver`

- [ ] **Step 1: Bulk replace crd-tunnel-service in all remaining YAML files**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
find config/ -name "*.yaml" -not -path "./.git/*" | xargs LC_ALL=C sed -i '' \
  -e 's/crd-tunnel-service/cloud-nexus-operator/g' \
  -e 's/frpclients/nexusclients/g' \
  -e 's/frpservers/nexusservers/g' \
  -e 's/kind: FrpClient/kind: NexusClient/g' \
  -e 's/kind: FrpServer/kind: NexusServer/g'
```

- [ ] **Step 2: Verify no old names remain in config/**

```bash
grep -rn "crd-tunnel-service\|FrpClient\|FrpServer" config/ --include="*.yaml"
```

Expected: no output (frpproxy references are fine to remain)

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "chore: bulk update remaining config YAML files with new naming"
```

---

### Task 12: Update Makefile, README, AGENTS.md

**Files:**
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Update Makefile KIND_CLUSTER variable**

In `Makefile`, change line 76:
```makefile
KIND_CLUSTER ?= crd-tunnel-service-test-e2e
```
to:
```makefile
KIND_CLUSTER ?= cloud-nexus-operator-test-e2e
```

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
LC_ALL=C sed -i '' 's/crd-tunnel-service-test-e2e/cloud-nexus-operator-test-e2e/g' Makefile
```

- [ ] **Step 2: Update README.md title and references**

```bash
LC_ALL=C sed -i '' 's/crd-tunnel-service/cloud-nexus-operator/g' README.md
```

- [ ] **Step 3: Update AGENTS.md title and references**

```bash
LC_ALL=C sed -i '' 's/crd-tunnel-service/cloud-nexus-operator/g' AGENTS.md
```

- [ ] **Step 4: Verify**

```bash
grep -n "crd-tunnel-service" Makefile README.md AGENTS.md
```

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add Makefile README.md AGENTS.md
git commit -m "chore: update Makefile, README, AGENTS.md with cloud-nexus-operator name"
```

---

### Task 13: Update e2e test constants

**Files:**
- Modify: `test/e2e/e2e_test.go`

- [ ] **Step 1: Update namespace and service account name constants**

In `test/e2e/e2e_test.go`, update the constants:

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
LC_ALL=C sed -i '' 's/crd-tunnel-service/cloud-nexus-operator/g' test/e2e/e2e_test.go
```

- [ ] **Step 2: Verify**

```bash
grep -n "crd-tunnel-service" test/e2e/e2e_test.go
```

Expected: no output

- [ ] **Step 3: Verify Go builds**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add test/e2e/e2e_test.go
git commit -m "chore: update e2e test namespace constants to cloud-nexus-operator-system"
```

---

### Task 14: Final scan and run tests

- [ ] **Step 1: Scan entire repo for any remaining old names**

```bash
cd /Users/arindra/CTO/repo/cto-sre/custom-image/crd-tunnel-service
grep -rn "crd-tunnel-service\|tunnel\.io/frp-operator" \
  --include="*.go" --include="*.yaml" --include="*.md" --include="Makefile" \
  --exclude-dir=".git" --exclude-dir="docs" \
  .
```

Expected: no output (ignoring docs/ to avoid false positives from historical plan files)

- [ ] **Step 2: Check for any FrpClient or FrpServer in Go files**

```bash
grep -rn "FrpClient\|FrpServer" --include="*.go" .
```

Expected: no output

- [ ] **Step 3: Run unit tests**

```bash
make test
```

Expected: all tests pass, `PASS` in output, no failures

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "chore: final cleanup after cloud-nexus-operator rename"
```
