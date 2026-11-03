<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# gv-factory Production Helm Chart

[![Helm Version](https://img.shields.io/badge/Helm-v3.10+-0F1689.svg?style=flat&logo=helm)](https://helm.sh)
[![Kubernetes Version](https://img.shields.io/badge/Kubernetes-v1.26+-326CE5.svg?style=flat&logo=kubernetes)](https://kubernetes.io)
[![License](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

Production-ready Kubernetes Helm Chart for **gv-factory**, the autonomous multi-agent software factory platform.

---

## 1. Architecture Overview

This chart deploys all components required for enterprise, highly-available autonomous software delivery:

```text
                           [ Ingress Controller (NGINX / Traefik) ]
                                      |                     |
                        +-------------+                     +-------------+
                        | /api, /events, /healthz, /webhooks              | / (Frontend SPA)
                        v                                                 v
             [ ov-server (Deployment + HPA) ]                 [ ov-web (Deployment + Service) ]
                   |                 |
         +---------+                 +---------+
         v                                     v
[ PostgreSQL 16 + pgvector ]          [ NATS JetStream 2.10 ]
(StatefulSet + 10Gi PVC)              (Deployment + 5Gi PVC)
         ^                                     ^
         |                                     |
         +---------+                 +---------+
                   |                 |
             [ ov-worker (Daemon / Sandboxes) ]
                   |
     +-------------+-------------+
     v                           v
[ /tmp/ov-sandboxes (PVC) ]  [ Docker Socket (Optional) ]
```

### Components Deployed
* **ov-server**: REST & WebSocket API, DAG workflow engine, human-in-the-loop gatekeeper and LLM connector. Scales horizontally with HPA.
* **ov-worker**: Autonomous agent execution runner, ephemeral Git worktrees manager, code indexer and test runner.
* **ov-web**: Vite + React Flow interactive visual DAG canvas and real-time execution dashboard served via Nginx.
* **PostgreSQL 16 with pgvector**: StatefulSet with persistent storage for knowledge embeddings, memory graph, and transactional state.
* **NATS JetStream 2.10**: Low-latency, distributed message bus and event log for tasks and streaming logs.
* **Ingress**: Unified routing for frontend and backend with SSL/TLS termination and WebSocket/SSE support.

---

## 2. Prerequisites

* **Kubernetes:** v1.26+
* **Helm:** v3.10+
* **Storage:** Default `StorageClass` configured in the cluster for dynamic PVC provisioning.
* **Ingress Controller:** NGINX Ingress Controller (`ingress-nginx`) or Traefik installed in the cluster.

---

## 3. Quick Start

### 3.1. Basic Installation (Local Development / Testing)

```bash
# Clone the repository
git clone https://github.com/gv-factory/gv-factory.git
cd gv-factory/deployments/helm/gv-factory

# Install the chart with default release name 'gv-factory'
helm install gv-factory . \
  --namespace gv-factory \
  --create-namespace
```

### 3.2. Verification

```bash
# Verify all pods are running and ready
kubectl get pods -n gv-factory -o wide

# Check services and ingress
kubectl get svc,ingress -n gv-factory
```

---

## 4. Configuration Reference

The following table lists the configurable parameters of the `gv-factory` chart and their default values:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imageRegistry` | Global container registry prefix | `""` |
| `global.imagePullSecrets` | Global image pull secrets for private registries | `[]` |
| `global.storageClass` | Global StorageClass for PVCs | `""` |
| `server.enabled` | Deploy ov-server API & orchestrator | `true` |
| `server.replicaCount` | Replicas when autoscaling is disabled | `2` |
| `server.image.repository` | ov-server container image repository | `gv-factory/ov-server` |
| `server.image.tag` | ov-server image tag | `latest` |
| `server.service.port` | ov-server listening port | `8080` |
| `server.resources.requests.cpu` | CPU request for ov-server | `100m` |
| `server.resources.requests.memory` | Memory request for ov-server | `128Mi` |
| `server.resources.limits.cpu` | CPU limit for ov-server | `500m` |
| `server.resources.limits.memory` | Memory limit for ov-server | `512Mi` |
| `server.autoscaling.enabled` | Enable Horizontal Pod Autoscaler for ov-server | `true` |
| `server.autoscaling.minReplicas` | Minimum HPA replicas | `2` |
| `server.autoscaling.maxReplicas` | Maximum HPA replicas | `10` |
| `worker.enabled` | Deploy ov-worker execution daemon | `true` |
| `worker.replicaCount` | Replicas for ov-worker | `2` |
| `worker.image.repository` | ov-worker container image repository | `gv-factory/ov-worker` |
| `worker.image.tag` | ov-worker image tag | `latest` |
| `worker.persistence.enabled` | Enable sandbox PVC storage for Git worktrees | `true` |
| `worker.persistence.size` | Sandbox storage volume size | `20Gi` |
| `worker.persistence.mountPath` | Path to mount sandboxes volume | `/tmp/ov-sandboxes` |
| `worker.dockerSocket.enabled` | Mount host Docker socket into worker pods | `false` |
| `worker.rootless.enabled` | Run sandboxes in rootless mode | `true` |
| `web.enabled` | Deploy ov-web frontend UI | `true` |
| `web.replicaCount` | Replicas for ov-web | `2` |
| `web.image.repository` | ov-web container image repository | `gv-factory/ov-web` |
| `web.image.tag` | ov-web image tag | `latest` |
| `web.service.port` | Web service internal port | `8080` |
| `postgres.enabled` | Deploy in-cluster PostgreSQL 16 with pgvector | `true` |
| `postgres.image.repository` | PostgreSQL container repository | `pgvector/pgvector` |
| `postgres.image.tag` | PostgreSQL image tag | `pg16` |
| `postgres.auth.database` | Database name | `ov_factory` |
| `postgres.auth.username` | Database user | `ov_user` |
| `postgres.auth.password` | Database password | `ov_password` |
| `postgres.persistence.size` | Database volume storage size | `10Gi` |
| `postgres.external.enabled` | Use external PostgreSQL database | `false` |
| `postgres.external.url` | External PostgreSQL connection URL | `""` |
| `nats.enabled` | Deploy in-cluster NATS JetStream 2.10 | `true` |
| `nats.image.repository` | NATS container repository | `nats` |
| `nats.image.tag` | NATS image tag | `2.10-alpine` |
| `nats.persistence.size` | NATS JetStream storage volume size | `5Gi` |
| `nats.external.enabled` | Use external NATS JetStream cluster | `false` |
| `nats.external.url` | External NATS connection URL | `""` |
| `ingress.enabled` | Deploy Ingress resource | `true` |
| `ingress.className` | Ingress class (`nginx` or `traefik`) | `nginx` |
| `ingress.hosts[0].host` | Ingress virtual hostname | `factory.local` |
| `ingress.tls` | TLS certificate secrets configuration | `[]` |
| `llm.provider` | Active LLM provider (`auto`, `openai`, `ollama`, `vllm`, `anthropic`) | `auto` |
| `llm.model` | Default LLM model name | `gpt-4o` |
| `secret.existingSecret` | Reference existing Kubernetes Secret | `""` |
| `secret.authSecretKey` | JWT & HMAC authentication secret key | `factory-default-...` |
| `secret.githubWebhookSecret` | GitHub webhook validation secret | `dev_github_secret` |

---

## 5. LLM Provider Setup

gv-factory features cognitive agent execution powered by pluggable LLM backends. Configure your preferred model provider in `values.yaml` or via Helm flags:

### 5.1. OpenAI (Official)

```yaml
llm:
  provider: "openai"
  model: "gpt-4o"
  openai:
    apiKey: "sk-proj-YOUR_OPENAI_API_KEY"
    model: "gpt-4o"
    baseUrl: "https://api.openai.com/v1"
```

Deploy with:
```bash
helm upgrade --install gv-factory . \
  --namespace gv-factory \
  --set llm.provider=openai \
  --set llm.openai.apiKey="sk-proj-YOUR_API_KEY"
```

### 5.2. Ollama (In-Cluster or Local Edge)

Run private, fully offline agents using Ollama on GPUs or local nodes:

```yaml
llm:
  provider: "ollama"
  model: "llama3.2"
  ollama:
    baseUrl: "http://ollama.ollama.svc.cluster.local:11434"
    model: "llama3.2"
```

Deploy with:
```bash
helm upgrade --install gv-factory . \
  --namespace gv-factory \
  --set llm.provider=ollama \
  --set llm.ollama.baseUrl="http://ollama-service:11434" \
  --set llm.ollama.model="llama3.2"
```

### 5.3. vLLM / High-Throughput Self-Hosted Cluster

For enterprise high-throughput local inference running on Triton or Kubernetes vLLM deployments:

```yaml
llm:
  provider: "vllm"
  model: "meta-llama/Llama-3-8B-Instruct"
  vllm:
    baseUrl: "http://vllm-service.inference.svc.cluster.local:8000/v1"
    apiKey: "token-vllm-secret"
    model: "meta-llama/Llama-3-8B-Instruct"
```

### 5.4. Anthropic Claude

```yaml
llm:
  provider: "anthropic"
  model: "claude-3-5-sonnet-20241022"
  anthropic:
    apiKey: "sk-ant-YOUR_API_KEY"
    model: "claude-3-5-sonnet-20241022"
```

---

## 6. Sandboxes & Worker Execution Modes

`ov-worker` executes tests, builds, and code generation in isolated environments. Two modes are supported:

### 6.1. Rootless Mode (Recommended for Multi-Tenant Clusters)
By default, `worker.rootless.enabled=true`. Sandboxes run unprivileged without mounting the host Docker socket, keeping your Kubernetes worker nodes completely secure.

### 6.2. Docker Socket Mount (For Docker-in-Docker / Container Builds)
If your agent tasks build and run OCI container images directly:

```yaml
worker:
  dockerSocket:
    enabled: true
    hostPath: /var/run/docker.sock
    mountPath: /var/run/docker.sock
```

---

## 7. Ingress & TLS Configuration

### 7.1. NGINX Ingress Controller with cert-manager (Let's Encrypt)

```yaml
ingress:
  enabled: true
  className: "nginx"
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/proxy-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
  hosts:
    - host: factory.example.com
      paths:
        - path: /api
          pathType: Prefix
          service: server
        - path: /events
          pathType: Prefix
          service: server
        - path: /healthz
          pathType: Exact
          service: server
        - path: /webhooks
          pathType: Prefix
          service: server
        - path: /
          pathType: Prefix
          service: web
  tls:
    - secretName: factory-tls-cert
      hosts:
        - factory.example.com
```

### 7.2. Traefik Ingress Controller

```yaml
ingress:
  enabled: true
  className: "traefik"
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: "web,websecure"
    traefik.ingress.kubernetes.io/router.tls: "true"
```

---

## 8. Production Secrets Management

In production, never store plaintext secrets in `values.yaml`. Instead, create a Kubernetes Secret beforehand using HashiCorp Vault, AWS Secrets Manager, or SealedSecrets:

```bash
kubectl create secret generic gv-factory-production-secrets \
  --namespace gv-factory \
  --from-literal=POSTGRES_USER="ov_prod_user" \
  --from-literal=POSTGRES_PASSWORD="SuperSecureDBPassword123!" \
  --from-literal=POSTGRES_DB="ov_factory" \
  --from-literal=AUTH_SECRET_KEY="e4d6a8f1b3c94d2e8f0a1b3c5d7e9f1a" \
  --from-literal=OPENAI_API_KEY="sk-proj-..." \
  --from-literal=GITHUB_WEBHOOK_SECRET="gh-secret-token"
```

Then configure the chart to use your existing secret:

```yaml
secret:
  existingSecret: "gv-factory-production-secrets"
```

---

## 9. Operations, Upgrades & Maintenance

### 9.1. Upgrading the Deployment

To perform rolling updates with zero downtime:

```bash
helm upgrade gv-factory deployments/helm/gv-factory \
  --namespace gv-factory \
  --reuse-values \
  --set server.image.tag="1.1.0" \
  --set worker.image.tag="1.1.0" \
  --set web.image.tag="1.1.0"
```

### 9.2. Rollback

If an upgrade needs to be reverted:

```bash
# Check revision history
helm history gv-factory -n gv-factory

# Rollback to revision 1
helm rollback gv-factory 1 -n gv-factory
```

### 9.3. Backing Up pgvector Brain Memory

```bash
kubectl exec -it statefulset/gv-factory-postgres -n gv-factory -- \
  pg_dump -U ov_user -d ov_factory -F c -b -v -f /var/lib/postgresql/data/backup_brain.dump
```

### 9.4. Uninstalling

```bash
helm uninstall gv-factory --namespace gv-factory
```

*Note: PersistentVolumeClaims (PVCs) for Postgres, NATS, and Worker Sandboxes are retained by default to prevent accidental data loss. To delete them manually: `kubectl delete pvc -l app.kubernetes.io/instance=gv-factory -n gv-factory`.*

---

## 10. License

Licensed under the Apache License, Version 2.0 (the "License").
See the [LICENSE](../../../LICENSES/Apache-2.0.txt) file for details.
