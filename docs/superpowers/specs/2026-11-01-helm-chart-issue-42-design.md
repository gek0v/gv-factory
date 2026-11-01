<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Production-Ready Helm Chart & Kubernetes Manifests for OV-Factory (Issue #42)

## 1. Executive Summary & Goals

This specification defines the production-ready Helm chart and Kubernetes manifest architecture for deploying the complete **OV-Factory** autonomous multi-agent platform into enterprise Kubernetes environments (EKS, GKE, AKS, OpenShift, k3s, Minikube).

The chart resides in `deployments/helm/ov-factory` and delivers:
- Zero-root security compliance according to Kubernetes Pod Security Standards (Restricted profile).
- High availability with PodDisruptionBudgets, probes, and Horizontal Pod Autoscalers (HPA).
- Dual-mode deployment: self-contained native storage and message bus (PostgreSQL 16 with `pgvector`, NATS JetStream, Hatchet) vs. external managed cloud infrastructure.
- Unified Layer 7 Ingress routing with WebSocket/SSE support for real-time factory event streaming.
- Rigorous automated verification without mandatory dependency on local Helm binary through Go test harness and standard Makefile targets.

---

## 2. Directory Structure & Layout

The chart is located at `deployments/helm/ov-factory`:

```text
deployments/helm/ov-factory/
├── .helmignore
├── Chart.yaml
├── values.yaml
├── chart_test.go
└── templates/
    ├── _helpers.tpl
    ├── configmap.yaml
    ├── secret.yaml
    ├── pvc.yaml
    ├── server-deployment.yaml
    ├── server-service.yaml
    ├── server-hpa.yaml
    ├── worker-deployment.yaml
    ├── worker-service.yaml
    ├── worker-hpa.yaml
    ├── web-deployment.yaml
    ├── web-service.yaml
    ├── postgres-statefulset.yaml
    ├── postgres-service.yaml
    ├── nats-statefulset.yaml
    ├── nats-service.yaml
    ├── hatchet-deployment.yaml
    ├── hatchet-service.yaml
    ├── ingress.yaml
    ├── networkpolicy.yaml
    └── pdb.yaml
```

---

## 3. Workload Topology & Service Architecture

```text
                                       +-------------------------------+
                                       |        Ingress Controller     |
                                       +---------------+---------------+
                                                       |
                             +-------------------------+-------------------------+
                             | (path: /api/*, /events)                           | (path: /)
                             v                                                   v
                  +---------------------+                             +---------------------+
                  |   ov-server (API)   |<----------------------------|     ov-web (UI)     |
                  |  Port: 8080 (HTTP)  |  (direct internal API proxy)|  Port: 8080 (HTTP)  |
                  +---+-------------+---+                             +---------------------+
                      |             |
        SQL / pgvector|             | NATS JetStream
                      v             v
       +------------------+     +------------------+
       |     postgres     |     |       nats       |
       | Port: 5432 (TCP) |     | Port: 4222 (TCP) |
       +------------------+     +------------------+
                      ^             ^
        SQL / pgvector|             | NATS JetStream
                      |             |
                  +---+-------------+---+
                  |  ov-worker (Daemon) |
                  |  Port: 8081 (Health)|
                  +----------+----------+
                             |
                      +------+------+
                      |  PVC Volume | (/tmp/ov-sandboxes)
                      +-------------+
```

### 3.1. `ov-server`
- **Role:** Core REST API, SSE streaming (`/events`), webhook intake (GitHub, GitLab, Sentry), and chat adapters (Slack, Discord).
- **Probes:**
  - `livenessProbe`: HTTP GET `/healthz` on port 8080 (initialDelay: 15s, period: 10s).
  - `readinessProbe`: HTTP GET `/healthz` on port 8080 (initialDelay: 5s, period: 5s).
  - `startupProbe`: HTTP GET `/healthz` on port 8080 (failureThreshold: 30, period: 5s).
- **Security:** Non-root user UID 10001, read-only root filesystem, `tmpfs` volume mounted at `/tmp`.

### 3.2. `ov-worker`
- **Role:** Ephemeral Git worktree management, Tree-sitter code analysis, Extism WASM plugin execution.
- **Persistence:** Mounts dedicated PVC (`worker.persistence`) at `/tmp/ov-sandboxes`.
- **Health Check:** HTTP GET `/healthz` on port 8081.
- **Autoscaling:** Horizontal Pod Autoscaler (HPA) targeting 75% CPU and 80% Memory utilization.

### 3.3. `ov-web`
- **Role:** SPA frontend canvas built on Vite, React 18, and `@xyflow/react` served via non-root Nginx.
- **Reverse Proxy:** Proxies `/api` and `/events` to `ov-server` service internally.
- **Security:** Writable cache directories (`/tmp`, `/var/cache/nginx`, `/var/run`) mounted as `emptyDir` to support read-only root filesystem.

### 3.4. Native Persistence & Messaging (Self-Contained Mode)
- **`postgres`:** StatefulSet running `pgvector/pgvector:pg16` with persistent storage volume (`10Gi` default).
- **`nats`:** StatefulSet running `nats:2.10-alpine` with JetStream enabled (`-js`, `-m 8222`, `-sd /data`) and volume (`5Gi` default).
- **External Integration Mode:** Configurable flags `postgres.external.enabled: true` and `nats.external.enabled: true` allow direct connection to managed cloud services without deploying internal StatefulSets.

---

## 4. Kubernetes Enterprise Governance & Security

### 4.1. SecurityContext & Hardening
- **Pod Level:**
  ```yaml
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    runAsGroup: 10001
    fsGroup: 10001
    seccompProfile:
      type: RuntimeDefault
  ```
- **Container Level:**
  ```yaml
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop:
        - ALL
  ```

### 4.2. Network Policies (`networkpolicy.yaml`)
- Enforces strict egress/ingress microsegmentation:
  - `ov-web` receives external ingress only.
  - `ov-server` receives ingress and `ov-web` traffic only; egresses to `postgres` and `nats`.
  - `ov-worker` egresses to `postgres` and `nats`; receives only health probes.
  - `postgres` accepts ingress strictly on port 5432 from `ov-server`, `ov-worker`, and `hatchet`.
  - `nats` accepts ingress strictly on port 4222 from `ov-server` and `ov-worker`.

### 4.3. High Availability & Resilience
- **PodDisruptionBudget (PDB):** Enforces `minAvailable: 1` on `ov-server` and `ov-web`.
- **Horizontal Pod Autoscaling (HPA):** Dynamically scales `ov-server` and `ov-worker` between `minReplicas` (2) and `maxReplicas` (10).

---

## 5. Configuration Schema (`values.yaml`)

```yaml
global:
  imageRegistry: ""
  imagePullSecrets: []
  storageClass: ""

server:
  enabled: true
  replicaCount: 2
  image:
    repository: gv-factory/ov-server
    tag: latest
    pullPolicy: IfNotPresent
  service:
    type: ClusterIP
    port: 8080
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 75
    targetMemoryUtilizationPercentage: 80

worker:
  enabled: true
  replicaCount: 2
  image:
    repository: gv-factory/ov-worker
    tag: latest
    pullPolicy: IfNotPresent
  service:
    type: ClusterIP
    port: 8081
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 8
    targetCPUUtilizationPercentage: 75
  persistence:
    enabled: true
    size: 20Gi
    accessModes:
      - ReadWriteOnce

web:
  enabled: true
  replicaCount: 2
  image:
    repository: gv-factory/ov-web
    tag: latest
    pullPolicy: IfNotPresent
  service:
    type: ClusterIP
    port: 8080
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      cpu: 200m
      memory: 128Mi

postgres:
  enabled: true
  image:
    repository: pgvector/pgvector
    tag: pg16
  persistence:
    size: 10Gi
  auth:
    database: ov_factory
    username: ov_user
    password: ov_password
  external:
    enabled: false
    url: ""

nats:
  enabled: true
  image:
    repository: nats
    tag: 2.10-alpine
  persistence:
    size: 5Gi
  external:
    enabled: false
    url: ""

hatchet:
  enabled: false
  image:
    repository: ghcr.io/hatchet-dev/hatchet/hatchet-engine
    tag: latest

ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/proxy-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
  hosts:
    - host: factory.local
      paths:
        - path: /api
          pathType: Prefix
          service: server
        - path: /events
          pathType: Prefix
          service: server
        - path: /
          pathType: Prefix
          service: web
  tls: []

networkPolicy:
  enabled: true

podDisruptionBudget:
  enabled: true
```

---

## 6. Testing & Validation Strategy

1. **Go Automated Test Suite (`deployments/helm/ov-factory/chart_test.go`):**
   - Validates that `Chart.yaml` contains correct metadata (`name: ov-factory`, `version: 0.1.0`, `appVersion: 1.0.0`).
   - Validates `values.yaml` structure and default security properties (`runAsNonRoot`, `readOnlyRootFilesystem`, dropped capabilities).
   - Validates all 15+ template files for well-formed YAML structures and required template blocks.
   - Runs directly with `go test -v ./deployments/helm/ov-factory/...`.
2. **Makefile Integration:**
   - `make helm-lint`
   - `make helm-template`
3. **REUSE & License Compliance:**
   - Every file begins with standard SPDX header:
     ```yaml
     # SPDX-License-Identifier: Apache-2.0
     # SPDX-FileCopyrightText: 2026 gv-factory contributors
     ```
