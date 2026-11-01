<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# OV-Factory Local Orchestration Stack

This directory contains the universal, production-ready local orchestration configuration for the complete **OV-Factory** multi-agent software factory platform.

Running a single command (`make compose-up` or `docker compose -f deployments/docker-compose.yml up -d --build`) provisions all core engines, persistence stores, message brokers, execution daemons, and visual canvases.

---

## 1. Stack Topology & Architecture

The local stack orchestrates 7 integrated services:

```text
                               +------------------------------------+
                               |              Browser               |
                               +-----------------+------------------+
                                                 |
                                     (HTTP 3000) |
                                                 v
                               +------------------------------------+
                               |     ov-web (Nginx Reverse Proxy)   |
                               |  Port: 3000 -> 8080 (non-root)     |
                               +--------+------------------+--------+
                                        |                  |
           /api/*, /api/v1/* (Reverse Proxy)               | /events (SSE stream)
                                        v                  v
                               +------------------------------------+
                               |             ov-server              |
                               |  (Gateway, REST API & Ingestor)    |
                               |  Port: 8080 (non-root)             |
                               +---+----------------+---------------+
                                   |                |
                     SQL / pgvector|                | NATS JetStream
                                   v                v
+------------------+         +-----+-----+    +-----+-----+
|     hatchet      |-------->|  postgres |    |    nats   |
| (Durable Engine) |         | (pgvector)|    | JetStream |
|    Port: 7077    |         | Port: 5432|    | Port: 4222|
+------------------+         +-----+-----+    +-----+-----+
                                   ^                ^
                     SQL / pgvector|                | NATS JetStream
                                   |                |
                               +---+----------------+---------------+
                               |             ov-worker              |
                               | (Execution Agent & Sandboxes)      |
                               | Health Probe: 8081 (non-root)      |
                               +------------------------------------+
```

### Services Summary


---

## 2. Hardened Multi-Stage Dockerfiles

All custom images follow strict security hardening guidelines:
- **Zero Root Execution:** Runtime processes execute as unprivileged user `nonroot:nonroot` (`UID 10001`).
- **Minimal Attack Surface:** Final layers use minimal `alpine:3.21` or `nginx:alpine-slim` images stripped of development tools.
- **Optimized Caching:** Go modules and Node `pnpm` lockfiles are copied and resolved before copying application source trees.
- **Embedded Health Probes:** Each container includes native `HEALTHCHECK` definitions utilizing `wget` probes.
- **Secure Reverse Proxy:** The Nginx configuration inside `ov-web` runs unprivileged on port `8080`, with temporary directories redirected to `/tmp`, enforcing security headers (`X-Frame-Options`, `Content-Security-Policy`, `X-Content-Type-Options`).

---

## 3. Quickstart Guide

### Step 1: Copy Environment Variables
Create a local `.env` configuration file from the provided `.env.example`:

```bash
cp deployments/.env.example deployments/.env
```

### Step 2: Launch Stack
Start all services in detached mode with automated health check synchronization:

```bash
# Using Makefile
make compose-up

# Or using Docker Compose directly
docker compose -f deployments/docker-compose.yml up -d --build
```

Docker Compose builds the images, starts dependencies in proper topological order (`postgres` & `nats` -> `hatchet`, `svix`, `ov-server` -> `ov-worker` -> `ov-web`), and ensures each service reaches a healthy state before dependents start.

### Step 3: Access Interfaces

- **Visual DAG Canvas & Dashboard:** Open [http://localhost:3000](http://localhost:3000)
- **Backend API & Health Status:** Open [http://localhost:8080/healthz](http://localhost:8080/healthz)
- **Worker Health Probe:** Open [http://localhost:8081/healthz](http://localhost:8081/healthz)
- **NATS Monitoring Dashboard:** Open [http://localhost:8222](http://localhost:8222)

---

## 4. Volume Persistence

| Volume Name | Target Directory | Description |
| :--- | :--- | :--- |
| **`pgdata`** | `/var/lib/postgresql/data` | Persists PostgreSQL database schemas, pgvector indexes, and episodic memory across container restarts. |
| **`natsdata`** | `/data` | Stores durable NATS JetStream message streams and consumer offsets. |
| **`sandboxes`** | `/tmp/ov-sandboxes` | Mounts the shared storage root for ephemeral Git worktrees provisioned for factory runs. |

---

## 5. Verification & Smoke Testing

### Check Stack Status
```bash
make compose-check
# Or:
docker compose -f deployments/docker-compose.yml ps
```

All 7 containers should show status `Up (healthy)`.

### Test Backend Health
```bash
curl -i http://localhost:8080/healthz
```
Expected response:
```json
{"brain":"connected","gateway":"nats","service":"ov-server","status":"ok"}
```

### Test Worker Health
```bash
curl -i http://localhost:8081/healthz
```
Expected response:
```json
{"service":"ov-worker","status":"ok"}
```

### Test Frontend Proxy & SSE Streaming
Open an SSE connection via `curl` to verify real-time events proxying through `ov-web`:
```bash
curl -N -H "Accept: text/event-stream" http://localhost:3000/events
```

---

## 6. Teardown & Cleanup

### Stop Containers
```bash
make compose-down
# Or:
docker compose -f deployments/docker-compose.yml down
```

### Reset Data Volumes
To delete all persisted database data, JetStream logs, and sandboxes:
```bash
docker compose -f deployments/docker-compose.yml down -v
```

| Service | Image / Build Source | Base / Security | Default Port | Health Check | Description |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **`postgres`** | `pgvector/pgvector:pg16` | PostgreSQL 16 Alpine | `5432` | `pg_isready` | Vector memory ("The Brain"), DAG states, and learning episodes. |
| **`nats`** | `nats:2.10-alpine` | Alpine (JetStream `-js`) | `4222`, `8222` | HTTP `/healthz` (8222) | High-throughput distributed event broker and pub/sub bus. |
| **`hatchet`** | `ghcr.io/hatchet-dev/hatchet/hatchet-engine:latest` | Distroless | `7077` | TCP / gRPC | Durable workflow orchestrator managing DAG execution. |
| **`svix`** | `svix/svix-server:latest` | Rust / Distroless | `8071` | TCP / HTTP | Reliable outbound webhook dispatcher with HMAC signatures. |
| **`ov-server`** | `build/package/Dockerfile.server` | `alpine:3.21` (UID 10001) | `8080` | HTTP `/healthz` | REST API, SSE streaming, webhook ingester, chat connector. |
| **`ov-worker`** | `build/package/Dockerfile.worker` | `alpine:3.21` (UID 10001) | `8081` | HTTP `/healthz` | Ephemeral Git worktree runner & WASM plugin executor. |
| **`ov-web`** | `build/package/Dockerfile.web` | `nginx:alpine-slim` (UID 10001) | `3000` | HTTP `/healthz` | Visual DAG canvas (@xyflow/react) and real-time dashboard. |
