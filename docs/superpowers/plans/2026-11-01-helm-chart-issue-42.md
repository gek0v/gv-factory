<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Production-Ready Helm Chart & Kubernetes Manifests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a production-ready Helm chart in `deployments/helm/ov-factory` that packages the entire OV-Factory multi-agent software platform for secure, highly-available deployment across any standard Kubernetes cluster.

**Architecture:** The chart defines workloads for `ov-server` (API, SSE, Webhooks), `ov-worker` (Ephemeral Sandboxes), and `ov-web` (SPA Frontend), combined with dual-mode storage/broker manifests (native PostgreSQL with `pgvector` & NATS JetStream vs. external managed endpoints). Security conforms to Kubernetes Pod Security Standards (Restricted profile) with non-root execution (UID 10001), read-only root filesystems, zero-privilege escalation, NetworkPolicies, and PodDisruptionBudgets. An automated Go test harness in `chart_test.go` validates chart metadata, value schemas, template generation, and compliance without requiring a local Helm binary.

**Tech Stack:** Kubernetes (v1.23+), Helm (v3/v4 format), YAML, Go (v1.22+ test harness with `gopkg.in/yaml.v3`).

**Spec:** `docs/superpowers/specs/2026-11-01-helm-chart-issue-42-design.md`

## Global Constraints

- Every new file (`.yaml`, `.tpl`, `.go`) must start with the REUSE SPDX Apache-2.0 header:
  `# SPDX-License-Identifier: Apache-2.0`
  `# SPDX-FileCopyrightText: 2026 gv-factory contributors`
- Line endings must be LF across all created files.
- YAML files must use 2-space indentation.
- Standard Kubernetes UID `10001` and GID `10001` for all non-root container workloads.
- Every commit must follow Conventional Commits (`feat(infra): ...`).

## Review Focus

- **Empty or missing external database/broker URLs:** When `postgres.external.enabled: true` or `nats.external.enabled: true`, the templates must fail or correctly fallback without crashing.
- **SSE Stream Buffering:** Ingress annotations must explicitly disable response buffering (`nginx.ingress.kubernetes.io/proxy-buffering: "off"`) and extend read timeouts (`3600`) so `/events` real-time streams do not disconnect.
- **Read-Only Root Filesystem failures:** All containers with `readOnlyRootFilesystem: true` must mount writable `emptyDir` volumes on `/tmp` and runtime cache paths (`/var/cache/nginx`, `/var/run`).
- **NetworkPolicy isolation leak:** Network policies must isolate database and message broker ports so only authorized components (`server`, `worker`, `hatchet`) can connect.
- **HPA target definitions:** HPA manifests must target the exact API version supported (`autoscaling/v2`) and bind to the correct workload deployments.

---

### Task 1: Chart Metadata, Helpers, and Go Test Harness Setup

**Files:**
- Create: `deployments/helm/ov-factory/Chart.yaml`
- Create: `deployments/helm/ov-factory/.helmignore`
- Create: `deployments/helm/ov-factory/templates/_helpers.tpl`
- Test: `deployments/helm/ov-factory/chart_test.go`

**Interfaces:**
- Produces: Chart metadata `ov-factory` (version 0.1.0, appVersion 1.0.0), template helper macros (`ov-factory.name`, `ov-factory.fullname`, `ov-factory.labels`, `ov-factory.selectorLabels`).

- [ ] **Step 1: Write the failing test for Chart metadata and helpers**

Create `deployments/helm/ov-factory/chart_test.go` containing:
```go
package helm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"gopkg.in/yaml.v3"
)

type ChartMetadata struct {
	APIVersion  string `yaml:"apiVersion"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
	Version     string `yaml:"version"`
	AppVersion  string `yaml:"appVersion"`
}

func TestChartMetadataAndHelpers(t *testing.T) {
	chartPath := filepath.Join(".", "Chart.yaml")
	data, err := os.ReadFile(chartPath)
	if err != nil {
		t.Fatalf("Failed to read Chart.yaml: %v", err)
	}

	var meta ChartMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		t.Fatalf("Failed to unmarshal Chart.yaml: %v", err)
	}

	if meta.APIVersion != "v2" {
		t.Errorf("Expected apiVersion 'v2', got %q", meta.APIVersion)
	}
	if meta.Name != "ov-factory" {
		t.Errorf("Expected name 'ov-factory', got %q", meta.Name)
	}
	if meta.Version != "0.1.0" {
		t.Errorf("Expected version '0.1.0', got %q", meta.Version)
	}
	if meta.AppVersion != "1.0.0" {
		t.Errorf("Expected appVersion '1.0.0', got %q", meta.AppVersion)
	}

	helpersPath := filepath.Join(".", "templates", "_helpers.tpl")
	helperData, err := os.ReadFile(helpersPath)
	if err != nil {
		t.Fatalf("Failed to read _helpers.tpl: %v", err)
	}
	helpersStr := string(helperData)
	for _, expectedMacro := range []string{"ov-factory.name", "ov-factory.fullname", "ov-factory.labels", "ov-factory.selectorLabels"} {
		if !strings.Contains(helpersStr, expectedMacro) {
			t.Errorf("Expected _helpers.tpl to contain definition of %q", expectedMacro)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./deployments/helm/ov-factory/...`
Expected: FAIL (Chart.yaml not found)

- [ ] **Step 3: Implement `Chart.yaml`, `.helmignore`, and `templates/_helpers.tpl`**

Create `Chart.yaml` with `apiVersion: v2`, `name: ov-factory`, `version: 0.1.0`, `appVersion: 1.0.0`.
Create `.helmignore` excluding backup files, hidden files, and tests.
Create `templates/_helpers.tpl` with standard labels and selector labels.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./deployments/helm/ov-factory/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/ov-factory/
git commit -m "feat(infra): initialize Helm chart metadata and standard helper templates (#42)"
```

---

### Task 2: Comprehensive Configuration Values, ConfigMap, and Secret Templates

**Files:**
- Create: `deployments/helm/ov-factory/values.yaml`
- Create: `deployments/helm/ov-factory/templates/configmap.yaml`
- Create: `deployments/helm/ov-factory/templates/secret.yaml`
- Test: `deployments/helm/ov-factory/chart_test.go` (Add `TestValuesSchemaAndConfiguration`)

**Interfaces:**
- Consumes: `_helpers.tpl` macros.
- Produces: Production defaults in `values.yaml` and parameterized K8s `ConfigMap` and `Secret` resources.

- [ ] **Step 1: Write the failing test for Values schema and ConfigMap/Secret templates**

Add test function `TestValuesSchemaAndConfiguration` to `chart_test.go` verifying:
- `values.yaml` loads and contains `server`, `worker`, `web`, `postgres`, `nats`, `hatchet`, `ingress`.
- Security defaults: non-root execution (`runAsUser: 10001`), `readOnlyRootFilesystem: true`, capabilities drop `ALL`.
- `templates/configmap.yaml` and `templates/secret.yaml` exist and contain correct Kubernetes metadata and labels.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestValuesSchemaAndConfiguration ./deployments/helm/ov-factory/...`
Expected: FAIL (values.yaml and templates not found)

- [ ] **Step 3: Implement `values.yaml`, `configmap.yaml`, and `secret.yaml`**

Create `values.yaml` with production-ready defaults, resource limits, security contexts, probes, and component toggles.
Create `templates/configmap.yaml` rendering database host, ports, worker directories, and feature flags.
Create `templates/secret.yaml` supporting both inline credentials and `existingSecret` references.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestValuesSchemaAndConfiguration ./deployments/helm/ov-factory/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/ov-factory/
git commit -m "feat(infra): add Helm values schema, ConfigMap and Secret templates (#42)"
```

---

### Task 3: Core Application Workloads (`server`, `worker`, `web`)

**Files:**
- Create: `deployments/helm/ov-factory/templates/server-deployment.yaml`
- Create: `deployments/helm/ov-factory/templates/server-service.yaml`
- Create: `deployments/helm/ov-factory/templates/worker-deployment.yaml`
- Create: `deployments/helm/ov-factory/templates/worker-service.yaml`
- Create: `deployments/helm/ov-factory/templates/web-deployment.yaml`
- Create: `deployments/helm/ov-factory/templates/web-service.yaml`
- Test: `deployments/helm/ov-factory/chart_test.go` (Add `TestCoreWorkloadDeployments`)

**Interfaces:**
- Consumes: ConfigMap, Secret, and helpers.
- Produces: Deployments and ClusterIP Services for `ov-server` (8080), `ov-worker` (8081), and `ov-web` (8080).

- [ ] **Step 1: Write the failing test for Core Workloads**

Add test function `TestCoreWorkloadDeployments` verifying:
- Presence of deployment and service templates for server, worker, and web.
- Server template includes probes on `/healthz` port 8080 and `tmpfs` volume for `/tmp`.
- Worker template mounts sandbox directory at `/tmp/ov-sandboxes`.
- Web template mounts writable cache directories (`/tmp`, `/var/cache/nginx`, `/var/run`).
- All services specify ClusterIP and target correct ports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestCoreWorkloadDeployments ./deployments/helm/ov-factory/...`
Expected: FAIL

- [ ] **Step 3: Implement Deployment and Service templates for `server`, `worker`, and `web`**

Write standard Kubernetes Deployment specs with hardened security contexts, environment mappings from ConfigMap/Secret, probes, and corresponding ClusterIP Service templates.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestCoreWorkloadDeployments ./deployments/helm/ov-factory/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/ov-factory/
git commit -m "feat(infra): add server, worker and web deployment and service templates (#42)"
```

---

### Task 4: High Availability, Autoscaling (HPA), PDB, and PVC Manifests

**Files:**
- Create: `deployments/helm/ov-factory/templates/pvc.yaml`
- Create: `deployments/helm/ov-factory/templates/server-hpa.yaml`
- Create: `deployments/helm/ov-factory/templates/worker-hpa.yaml`
- Create: `deployments/helm/ov-factory/templates/pdb.yaml`
- Test: `deployments/helm/ov-factory/chart_test.go` (Add `TestScalingAndResilienceManifests`)

**Interfaces:**
- Consumes: Deployment selectors and values.yaml autoscaling/resilience sections.
- Produces: PVCs for persistent sandboxes, HPAs targeting `ov-server` and `ov-worker`, PDBs for zero-downtime draining.

- [ ] **Step 1: Write the failing test for Autoscaling, PDB, and PVC**

Add test function `TestScalingAndResilienceManifests` verifying:
- `pvc.yaml` defines PersistentVolumeClaim for worker sandboxes with configurable storage size.
- `server-hpa.yaml` and `worker-hpa.yaml` target `autoscaling/v2` with CPU and Memory metric specifications.
- `pdb.yaml` enforces `minAvailable: 1` when enabled.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestScalingAndResilienceManifests ./deployments/helm/ov-factory/...`
Expected: FAIL

- [ ] **Step 3: Implement PVC, HPA, and PDB templates**

Write templates with conditional rendering (`{{- if .Values.<component>.autoscaling.enabled }}` and `{{- if .Values.podDisruptionBudget.enabled }}`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestScalingAndResilienceManifests ./deployments/helm/ov-factory/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/ov-factory/
git commit -m "feat(infra): add autoscaling HPA, PDB and storage PVC templates (#42)"
```

---

### Task 5: Self-Contained Native Persistence & Messaging (Postgres, NATS, Hatchet)

**Files:**
- Create: `deployments/helm/ov-factory/templates/postgres-statefulset.yaml`
- Create: `deployments/helm/ov-factory/templates/postgres-service.yaml`
- Create: `deployments/helm/ov-factory/templates/nats-statefulset.yaml`
- Create: `deployments/helm/ov-factory/templates/nats-service.yaml`
- Create: `deployments/helm/ov-factory/templates/hatchet-deployment.yaml`
- Create: `deployments/helm/ov-factory/templates/hatchet-service.yaml`
- Test: `deployments/helm/ov-factory/chart_test.go` (Add `TestPersistenceAndMessagingManifests`)

**Interfaces:**
- Consumes: `postgres.enabled`, `nats.enabled`, `hatchet.enabled` values.
- Produces: StatefulSets with volume claims for PostgreSQL (`pgvector/pgvector:pg16`) and NATS JetStream, and Hatchet engine deployment.

- [ ] **Step 1: Write the failing test for Persistence and Messaging templates**

Add test function `TestPersistenceAndMessagingManifests` in `chart_test.go` verifying:
- StatefulSet definitions for Postgres and NATS include volume claim templates and health checks.
- Postgres and NATS services expose ports 5432 and 4222/8222.
- Hatchet deployment conditionally renders and connects to Postgres database.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestPersistenceAndMessagingManifests ./deployments/helm/ov-factory/...`
Expected: FAIL

- [ ] **Step 3: Implement StatefulSet and Service templates for Postgres, NATS, and Hatchet**

Write native Kubernetes StatefulSets and Services with proper volume claims and readiness probes.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestPersistenceAndMessagingManifests ./deployments/helm/ov-factory/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/ov-factory/
git commit -m "feat(infra): add native Postgres pgvector, NATS and Hatchet templates (#42)"
```

---

### Task 6: Unified L7 Ingress, Security NetworkPolicies, and Makefile Integration

**Files:**
- Create: `deployments/helm/ov-factory/templates/ingress.yaml`
- Create: `deployments/helm/ov-factory/templates/networkpolicy.yaml`
- Modify: `Makefile`
- Test: `deployments/helm/ov-factory/chart_test.go` (Add `TestIngressAndNetworkPolicyManifests` and complete suite run)

**Interfaces:**
- Consumes: Ingress routing paths and network isolation rules.
- Produces: Production Ingress manifest with TLS / SSE support, restrictive NetworkPolicies, and Makefile targets `helm-lint` & `helm-template`.

- [ ] **Step 1: Write the failing test for Ingress, NetworkPolicies, and Full Chart Validation**

Add test function `TestIngressAndNetworkPolicyManifests` verifying:
- Ingress template defines paths for `/api`, `/events`, and `/` with SSE buffering disabled annotations.
- NetworkPolicy template isolates internal pod-to-pod communications (DB and NATS restricted to authorized components).
- Full chart template parsing succeeds across all created `.yaml` files.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestIngressAndNetworkPolicyManifests ./deployments/helm/ov-factory/...`
Expected: FAIL

- [ ] **Step 3: Implement `ingress.yaml`, `networkpolicy.yaml`, and update `Makefile`**

Write `ingress.yaml` supporting single/multiple hosts and cert-manager TLS.
Write `networkpolicy.yaml` with explicit ingress/egress rules.
Add `helm-lint` and `helm-template` targets in `Makefile`.

- [ ] **Step 4: Run full test suite and Makefile verification**

Run: `go test -v ./deployments/helm/ov-factory/...`
Expected: PASS (all tests pass)

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/ov-factory/ Makefile
git commit -m "feat(infra): add Ingress, NetworkPolicies, Makefile targets and chart test suite (#42)"
```
