<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# gv-factory (Fábrica de Software Autónoma & Modular)

<p align="center">
  <a href="https://github.com/gek0v/gv-factory/actions/workflows/ci.yml">
    <img src="https://github.com/gek0v/gv-factory/actions/workflows/ci.yml/badge.svg" alt="CI Verification" />
  </a>
  <a href="https://opensource.org/licenses/Apache-2.0">
    <img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache-2.0" />
  </a>
  <a href="https://api.reuse.software/info/github.com/gek0v/gv-factory">
    <img src="https://img.shields.io/badge/REUSE-3.3_Compliant-blueviolet.svg" alt="REUSE 3.3 Compliant" />
  </a>
  <a href="https://golang.org">
    <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white" alt="Go Version" />
  </a>
  <a href="https://react.dev">
    <img src="https://img.shields.io/badge/Frontend-Vite%20%7C%20React%2018-646CFF?logo=vite&logoColor=white" alt="Frontend: Vite & React 18" />
  </a>
  <a href="https://www.docker.com">
    <img src="https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white" alt="Docker Ready" />
  </a>
  <a href="deployments/helm/gv-factory">
    <img src="https://img.shields.io/badge/Helm%20Chart-v1.0.0-0F1689?logo=helm&logoColor=white" alt="Helm Chart v1.0.0" />
  </a>
  <a href="docs/adr/0009-release-packaging-cosign-supply-chain.md">
    <img src="https://img.shields.io/badge/Supply%20Chain-Cosign%20%7C%20SLSA%20L3-4B32C3?logo=sigstore&logoColor=white" alt="Cosign & SLSA L3 Supply Chain" />
  </a>
</p>

---

**gv-factory** es una plataforma de ingeniería de software autónoma, modular y de nivel empresarial diseñada para orquestar agentes inteligentes en bucles cerrados de desarrollo. Transforma especificaciones técnicas, incidencias o tareas de desarrollo en código probado, auditado y verificado en producción, garantizando cero mutaciones directas a ramas protegidas, aislamiento estricto en sandboxes efímeros y supervisión humana criptográfica (*Human-in-the-Loop*).

---

## Tabla de Contenidos

- [gv-factory (Fábrica de Software Autónoma \& Modular)](#gv-factory-fábrica-de-software-autónoma--modular)
  - [Tabla de Contenidos](#tabla-de-contenidos)
  - [Arquitectura del Pipeline Autónomo Multi-Agente](#arquitectura-del-pipeline-autónomo-multi-agente)
  - [Pilares Arquitectónicos](#pilares-arquitectónicos)
    - [1. El Cerebro (`pgvector` + Tree-sitter)](#1-el-cerebro-pgvector--tree-sitter)
    - [2. Sandboxes Git Efímeros \& Aislamiento](#2-sandboxes-git-efímeros--aislamiento)
    - [3. Revisión Semántica Concurrente (*Review Council*)](#3-revisión-semántica-concurrente-review-council)
    - [4. Gobernanza Human-in-the-Loop (HITL)](#4-gobernanza-human-in-the-loop-hitl)
    - [5. Ecosistema de Conectores (Git, Chat, Webhooks)](#5-ecosistema-de-conectores-git-chat-webhooks)
    - [6. Telemetría de Costes \& Guardarraíles Financieros](#6-telemetría-de-costes--guardarraíles-financieros)
  - [Guía de Inicio Rápido](#guía-de-inicio-rápido)
    - [Requisitos Previos](#requisitos-previos)
    - [Opción A: Docker Compose (Despliegue Local Rápido)](#opción-a-docker-compose-despliegue-local-rápido)
    - [Opción B: Helm Chart para Kubernetes (Producción)](#opción-b-helm-chart-para-kubernetes-producción)
    - [Opción C: Entorno de Desarrollo Local](#opción-c-entorno-de-desarrollo-local)
  - [Configuración Multi-Proveedor de LLMs](#configuración-multi-proveedor-de-llms)
    - [Estrategia de Enrutamiento y Resiliencia](#estrategia-de-enrutamiento-y-resiliencia)
    - [Modelos Comerciales en la Nube](#modelos-comerciales-en-la-nube)
    - [Modelos Locales y Soberanía de Datos (Air-Gapped)](#modelos-locales-y-soberanía-de-datos-air-gapped)
  - [Cheat Sheet Interactivo de `ov-cli`](#cheat-sheet-interactivo-de-ov-cli)
    - [Compilación y Configuración](#compilación-y-configuración)
    - [Comandos Esenciales](#comandos-esenciales)
      - [1. `ov-cli factory run` — Despacho de Pipelines Autónomos](#1-ov-cli-factory-run--despacho-de-pipelines-autónomos)
      - [2. `ov-cli factory gates` — Inspección de Compuertas HITL](#2-ov-cli-factory-gates--inspección-de-compuertas-hitl)
      - [3. `ov-cli factory approve` — Aprobación Criptográfica](#3-ov-cli-factory-approve--aprobación-criptográfica)
      - [4. `ov-cli memory query` — Búsqueda Semántica e Híbrida en el Cerebro](#4-ov-cli-memory-query--búsqueda-semántica-e-híbrida-en-el-cerebro)
      - [5. `ov-cli memory sync-adrs` — Sincronización de Decisiones Arquitectónicas](#5-ov-cli-memory-sync-adrs--sincronización-de-decisiones-arquitectónicas)
      - [Tabla Rápida de Comandos CLI](#tabla-rápida-de-comandos-cli)
  - [Documentación Profunda \& Enlaces de Referencia](#documentación-profunda--enlaces-de-referencia)
  - [Cumplimiento Legal y Licencia](#cumplimiento-legal-y-licencia)

---

## Arquitectura del Pipeline Autónomo Multi-Agente

El flujo operativo central de **gv-factory** sigue un patrón riguroso de bucle cerrado (*closed-loop software delivery*) orquestado por `FactoryCoordinator`:

```mermaid
flowchart TD
    subgraph Trigger ["1. Ingestión & Desencadenante"]
        Event["Inbound Webhook / CLI Task<br/><i>(GitHub, GitLab, Slack, Discord, ov-cli)</i>"]
    end

    subgraph Memory ["El Cerebro (Long-Term Memory)"]
        Brain[("PostgreSQL 16 + pgvector<br/>AST Tree-sitter & Memoria Episódica")]
    end

    subgraph Pipeline ["2. Pipeline Autónomo Multi-Agente"]
        direction TB
        SpecWriter["<b>SpecWriter</b><br/>Generación Formal de SDD y Plan TDD"]
        Builder["<b>Builder TDD (Red-Green-Refactor)</b><br/>Worktree efímero (/tmp/ov-sandboxes/<run_id>)<br/>Ejecución de Tests y Cobertura (≥90%)"]
        
        subgraph Council ["Review Council (Auditoría Concurrente)"]
            direction LR
            SecRev["<b>SecurityReviewer</b><br/>SAST, Secretos, Zero-Trust"]
            ArchRev["<b>ArchitectureReviewer</b><br/>Fronteras de Paquetes, ADRs"]
            SemRev["<b>SemanticReviewer</b><br/>Spec Compliance, Edge Cases"]
        end

        Approver{"<b>GateApprover</b><br/>Evaluación Multi-Criterio"}

        SpecWriter -->|Especificación Formal| Builder
        Builder -->|Diff de Código & Resultados| Council
        Council -->|Veredictos Consolidados| Approver
    end

    subgraph Outcomes ["3. Veredictos & Resolución"]
        RetryLoop["<b>RETRY</b><br/>Bucle de Refactor con Feedback de Tests"]
        HumanGate["<b>HUMAN_GATE</b><br/>Token Criptográfico HMAC-SHA256<br/>Notificación Slack/Discord/Web/CLI"]
        Approved["<b>APPROVED</b><br/>Fast-Track / Aprobación Verificada"]
    end

    subgraph Integration ["4. Integración Continua"]
        MergeQueue["<b>Sequential Merge Queue</b><br/>Validación con git merge-tree<br/>Rebase & Fast-Forward a <code>main</code>"]
    end

    Event --> SpecWriter
    Brain -.->|Contexto Histórico & Símbolos| SpecWriter
    Approver -->|Errores en Tests / Cobertura Baja| RetryLoop
    RetryLoop -->|Reintento con Contexto| Builder
    Approver -->|Violaciones Graves / Impacto Crítico| HumanGate
    HumanGate -->|Aprobación Criptográfica| Approved
    Approver -->|Criterios Satisfechos (≥90% Cobertura)| Approved
    Approved --> MergeQueue
    MergeQueue -.->|Persistir Episodio de Éxito| Brain
```

---

## Pilares Arquitectónicos

### 1. El Cerebro (`pgvector` + Tree-sitter)
- **Memoria Semántica y Episódica:** Persistencia de decisiones pasadas, soluciones técnicas y contextos de fallo en PostgreSQL 16 con extensión `pgvector`.
- **Indexación Estructural AST:** Parsing granular de archivos fuente mediante Tree-sitter para almacenar símbolos (funciones, estructuras, interfaces) y hashes diferenciales XXH64.
- **Búsqueda Híbrida Ponderada:** Fusión de similitud vectorial y búsqueda léxica de texto completo (*Full-Text Search*) controlada por el coeficiente $\alpha$ (`0.0` léxico puro, `1.0` vectorial puro, default `0.7`).
- **Sincronización Automática de ADRs:** Ingesta bidireccional y actualización incremental de Architecture Decision Records desde `docs/adr/`.

### 2. Sandboxes Git Efímeros & Aislamiento
- **Aislamiento en Filesystem:** Cada ejecución se aprovisiona en `/tmp/ov-sandboxes/<run_id>` mediante Git Worktrees dedicados.
- **Invariante de Rama Principal:** Queda terminantemente prohibido ejecutar `git checkout` o modificar `main` directamente. Los sandboxes operan en ramas efímeras `ov-run/<run_id>`.
- **Contenedores Rootless:** Ejecución de suites de prueba y linters en entornos Docker o Podman sin privilegios de root con asignación determinista de puertos:  
  $$\text{Puerto} = \text{BASE\_PORT} + (\text{XXH64}(\text{run\_id}) \pmod{\text{PORT\_RANGE}})$$
- **Limpieza de Bloqueos:** Detección y purga preventiva de archivos huérfanos `index.lock` antes de iniciar cada ciclo de trabajo.

### 3. Revisión Semántica Concurrente (*Review Council*)
Antes de emitir cualquier veredicto de fusión, un panel de agentes especializados audita concurrentemente el código generado:
- **`SecurityReviewer`:** Detección de fugas de secretos, vulnerabilidades SAST (inyecciones SQL, RCE, traversal), evaluación de dependencias y políticas de menor privilegio.
- **`ArchitectureReviewer`:** Verificación de acoplamiento de paquetes, coherencia con las directrices de `AGENTS.md` y respeto estricto a las decisiones registradas en `docs/adr/`.
- **`SemanticReviewer`:** Validación de cobertura funcional frente al documento de requerimientos generado por `SpecWriter`, análisis de regresiones y condiciones de borde.

### 4. Gobernanza Human-in-the-Loop (HITL)
- **Tokens Criptográficos HMAC-SHA256:** Las compuertas de decisión generan un token opaco firmado con clave secreta y caducidad temporal (TTL).
- **Escalado Automático:** Si la cobertura de pruebas unitarias cae por debajo del umbral mínimo ($\ge 90\%$), si se detectan violaciones de severidad `CRITICAL`/`ERROR`, o si se modifican rutas críticas de infraestructura, el sistema conmuta a estado `gate_waiting`.
- **Resolución Multi-Canal:** Los ingenieros pueden aprobar o rechazar ejecuciones desde la interfaz web interactiva, mediante comandos de chat en Slack/Discord, o directamente desde la consola con `ov-cli factory approve <run-id>`.

### 5. Ecosistema de Conectores (Git, Chat, Webhooks)
- **Plataformas Git:** Soporte nativo para webhooks entrantes de GitHub y GitLab (apertura de issues, menciones de bot, peticiones de revisión de PR).
- **Adaptadores de Chat Bidireccionales:** Bots interactivos para Slack y Discord que emiten tarjetas con métricas de ejecución, diffs formateados y botones interactivos de decisión.
- **Webhooks Salientes Resilientes:** Despacho de eventos de ciclo de vida mediante Svix con reintentos exponenciales, firmas HMAC y gestión de Dead Letter Queues (DLQ).

### 6. Telemetría de Costes & Guardarraíles Financieros
- **Contabilidad Granular de Tokens:** Medición en tiempo real de tokens de entrada (*prompt*), tokens de salida (*completion*) y aciertos de caché por modelo.
- **Topes Presupuestarios:** Cancelación preventiva o escalado HITL si una fábrica supera el presupuesto financiero o límite de tokens asignado.
- **Métricas Prometheus & Tracing OTel:** Exportación nativa de trazas distribuidas y métricas de rendimiento en `/metrics`.

---

## Guía de Inicio Rápido

### Requisitos Previos

| Herramienta | Versión Mínima | Uso |
| :--- | :--- | :--- |
| **Go** | 1.22+ (recomendado 1.24+) | Compilación y ejecución del backend (`ov-server`, `ov-worker`, `ov-cli`) |
| **Node.js** & **pnpm** | Node 22+, pnpm 9+ | Interfaz visual interactiva en `/web` |
| **Docker** & **Compose** | Docker 24+, Compose v2.20+ | Infraestructura de servicios auxiliares y sandboxes rootless |
| **Helm** & **Kubernetes** | Helm v3.10+, K8s v1.26+ | Despliegue en clusters de producción |
| **Git** | 2.40+ | Soporte nativo para Git Worktrees efímeros |

---

### Opción A: Docker Compose (Despliegue Local Rápido)

La vía más rápida para levantar la infraestructura base de persistencia y mensajería:

```bash
# 1. Clonar el repositorio
git clone https://github.com/gek0v/gv-factory.git
cd gv-factory

# 2. Configurar variables de entorno iniciales
cp deployments/.env.example .env

# 3. Levantar PostgreSQL con pgvector, NATS JetStream, Hatchet y Svix
docker compose -f deployments/docker-compose.yml up -d

# 4. Verificar estado de los contenedores
docker compose -f deployments/docker-compose.yml ps
```

Una vez levantada la base, arranca el servidor central en una terminal:
```bash
go run ./cmd/ov-server
```

---

### Opción B: Helm Chart para Kubernetes (Producción)

El repositorio incluye un chart de Helm completamente probado y empaquetado para despliegues empresariales de alta disponibilidad en Kubernetes:

```bash
# 1. Navegar al directorio del chart
cd deployments/helm/gv-factory

# 2. Inspeccionar y personalizar los valores de configuración
# Edita values.yaml según tu ingress, certificados TLS y credenciales de BD
vi values.yaml

# 3. Instalar la release 'gv-factory' en el namespace de producción
helm upgrade --install gv-factory . \
  --namespace gv-factory \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=factory.example.com \
  --set ingress.hosts[0].paths[0].path=/ \
  --set ingress.hosts[0].paths[0].pathType=Prefix

# 4. Monitorear el despliegue
kubectl get pods -n gv-factory -w
```

Para documentación detallada sobre HPA, persistencia en PVCs y configuraciones de TLS, consulta el manual dedicado en [deployments/helm/gv-factory/README.md](deployments/helm/gv-factory/README.md).

---

### Opción C: Entorno de Desarrollo Local

Si deseas contribuir activamente al código o depurar el pipeline:

```bash
# 1. Levantar servicios auxiliares en segundo plano
docker compose -f deployments/docker-compose.yml up -d postgres nats

# 2. Aplicar migraciones del Cerebro a la base de datos
go run ./cmd/ov-cli memory sync-adrs

# 3. Ejecutar el orquestador principal
go run ./cmd/ov-server

# 4. En otra terminal, compilar y arrancar la interfaz web SPA
cd web
pnpm install
pnpm dev
```

La consola web interactiva con el canvas de React Flow estará disponible en `http://localhost:5173`.

---

## Configuración Multi-Proveedor de LLMs

**gv-factory** incorpora una capa de abstracción desacoplada de LLMs con enrutamiento inteligente, balanceo y conmutación automática por error (*fallback chain*), permitiendo operar tanto con proveedores líderes en la nube como con modelos auto-hospedados sin conexión a internet.

### Estrategia de Enrutamiento y Resiliencia

Configura el proveedor activo en tu archivo `.env` o en las variables de entorno del sistema:

```bash
# Modos de selección de proveedor:
# "auto"                -> Detección automática según variables de entorno disponibles
# "openai"              -> OpenAI oficial (GPT-4o, o1, o3-mini)
# "anthropic"           -> Anthropic Messages API (Claude 3.5 Sonnet, Claude 3.7)
# "ollama"              -> Instancia local de Ollama (modelos abiertos)
# "vllm"                -> Servidor vLLM de alta velocidad con Continuous Batching
# "openai_compatible"   -> Endpoint genérico compatible con OpenAI (Gemini, OpenRouter, Groq, DeepSeek)
PROVIDER=auto
MODEL=gpt-4o
```

---

### Modelos Comerciales en la Nube

#### 1. OpenAI
```dotenv
PROVIDER=openai
OPENAI_API_KEY=sk-proj-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
OPENAI_MODEL=gpt-4o
# Opcional para endpoints privados de Azure OpenAI:
# OPENAI_BASE_URL=https://<resource>.openai.azure.com/openai/deployments/<model>
```

#### 2. Anthropic (Claude)
```dotenv
PROVIDER=anthropic
ANTHROPIC_API_KEY=sk-ant-api03-xxxxxxxxxxxxxxxxxxxxxxxxxxxx
MODEL=claude-3-5-sonnet-20241022
```

#### 3. Google Gemini (Vía Endpoint OpenAI-Compatible)
Los modelos Gemini 1.5 Pro, Flash y 2.0 pueden conectarse de manera inmediata utilizando el conector compatible:
```dotenv
PROVIDER=openai_compatible
OPENAI_COMPATIBLE_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai/
OPENAI_COMPATIBLE_API_KEY=AIzaSyxxxxxxxxxxxxxxxxxxxxxxxxxxxx
OPENAI_COMPATIBLE_MODEL=gemini-1.5-pro
```

---

### Modelos Locales y Soberanía de Datos (Air-Gapped)

Para organizaciones con políticas estrictas de privacidad o entornos sin acceso a internet:

#### 4. Ollama (Modelos Open-Weights)
```dotenv
PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
MODEL=qwen2.5-coder:32b
# Modelos recomendados: qwen2.5-coder:32b, deepseek-r1:14b, llama3.1:70b
```

#### 5. vLLM (Inferencia Acelerada Local con PagedAttention)
```dotenv
PROVIDER=vllm
VLLM_BASE_URL=http://localhost:8000/v1
MODEL=Qwen/Qwen2.5-Coder-32B-Instruct
```

#### Cadena de Fallback Automática
Puedes configurar un proveedor de respaldo que tomará el relevo instantáneamente si el conector primario sufre agotamiento de cuota (HTTP 429) o fallos de red (HTTP 5xx):
```dotenv
PROVIDER=openai
OPENAI_MODEL=gpt-4o
LLM_FALLBACK_PROVIDER=ollama
LLM_FALLBACK_BASE_URL=http://localhost:11434
LLM_FALLBACK_MODEL=qwen2.5-coder:32b
```

---

## Cheat Sheet Interactivo de `ov-cli`

`ov-cli` es la navaja suiza de línea de comandos para desarrolladores y operadores de **gv-factory**. Permite despachar tareas, monitorear ejecuciones, resolver compuertas de seguridad y realizar consultas cognitivas sobre el repositorio.

### Compilación y Configuración

```bash
# Compilar binario en la raíz del proyecto
go build -o ov-cli ./cmd/ov-cli

# Opcional: Configurar URL del servidor central (por defecto: http://localhost:8080)
export OV_SERVER_URL="http://localhost:8080"
```

---

### Comandos Esenciales

#### 1. `ov-cli factory run` — Despacho de Pipelines Autónomos
Inicia una nueva fábrica autónoma a partir de una descripción o título de tarea:

```bash
# Despacho en segundo plano (asíncrono, devuelve Run ID)
./ov-cli factory run "Implementar autenticación mTLS en internal/gateway" \
  --description "Añadir soporte de certificados de cliente y rotación de CAs" \
  --labels "security,networking" \
  --constraints "no external deps,coverage >= 90"

# Despacho síncrono con streaming y espera de resultados en consola
./ov-cli factory run "Refactorizar parsing AST con tree-sitter" --sync

# Especificar un identificador de tarea explícito y salida JSON
./ov-cli factory run "Fix memory leak in buffer pool" \
  --task-id "task-core-892" \
  --json
```

#### 2. `ov-cli factory gates` — Inspección de Compuertas HITL
Consulta las fábricas en ejecución que requieren intervención o aprobación humana:

```bash
# Listar compuertas pendientes con detalles de expiración y motivo
./ov-cli factory gates

# Salida estructurada JSON para integraciones con scripts de CI/CD
./ov-cli factory gates --json
```

#### 3. `ov-cli factory approve` — Aprobación Criptográfica
Autoriza el paso de una compuerta HITL hacia la cola de integración secuencial:

```bash
# Aprobación automática (resuelve el token criptográfico pendiente de la tarea)
./ov-cli factory approve run-b649ef28-403b

# Aprobación explícita mediante token HMAC recibido por canal seguro
./ov-cli factory approve run-b649ef28-403b --token "c4f981a8b27341e9..."

# Rechazar formalmente una ejecución y abortar el pipeline
./ov-cli factory reject run-b649ef28-403b --reason "Cobertura insuficiente en pruebas de integración"
```

#### 4. `ov-cli memory query` — Búsqueda Semántica e Híbrida en el Cerebro
Consulta la memoria cognitiva persistente indexada en PostgreSQL con `pgvector`:

```bash
# Búsqueda semántica densa por concepto o patrón arquitectónico
./ov-cli memory query "aislamiento de worktree y manejo de index.lock"

# Búsqueda híbrida (vectorial + Full-Text Search) con umbral de similitud
./ov-cli memory query "algoritmo de merge-tree" \
  --hybrid \
  --alpha 0.75 \
  --threshold 0.6 \
  --limit 3

# Filtrar por categoría (knowledge, rules, episodes)
./ov-cli memory query "fallos recurrentes de concurrencia" --category episodes --json
```

#### 5. `ov-cli memory sync-adrs` — Sincronización de Decisiones Arquitectónicas
Parsea los documentos Markdown de decisiones técnicas y los sincroniza en la base vectorial:

```bash
# Sincronización estándar de la carpeta docs/adr
./ov-cli memory sync-adrs

# Modo simulación (Dry Run) sin modificar la base de datos
./ov-cli memory sync-adrs docs/adr --dry-run

# Sincronización dirigida a un servidor remoto de gv-factory
./ov-cli memory sync-adrs docs/adr --server https://factory.corp.internal
```

---

#### Tabla Rápida de Comandos CLI

| Comando | Sintaxis | Propósito |
| :--- | :--- | :--- |
| **`factory run`** | `ov-cli factory run <título> [flags]` | Despacha un pipeline autónomo en un sandbox aislado |
| **`factory status`** | `ov-cli factory status <run-id>` | Consulta el estado, diff generado y veredictos de una ejecución |
| **`factory list`** | `ov-cli factory list` | Lista las ejecuciones activas y recientes |
| **`factory gates`** | `ov-cli factory gates` | Muestra compuertas que esperan decisión humana |
| **`factory approve`** | `ov-cli factory approve <run-id> [flags]` | Aprueba y envía a la cola de merge una compuerta pausada |
| **`factory reject`** | `ov-cli factory reject <run-id> [flags]` | Rechaza una ejecución indicando el motivo |
| **`memory query`** | `ov-cli memory query <texto> [flags]` | Búsqueda semántica/híbrida sobre símbolos y memoria episódica |
| **`memory sync-adrs`** | `ov-cli memory sync-adrs [ruta] [flags]` | Parsea y vectoriza Architecture Decision Records |
| **`memory index`** | `ov-cli memory index [ruta]` | Analiza archivos fuente con Tree-sitter e indexa símbolos AST |
| **`memory stats`** | `ov-cli memory stats` | Estadísticas del Cerebro (embeddings, chunks, episodios) |

---

## Documentación Profunda & Enlaces de Referencia

Para comprender en detalle el diseño técnico, las garantías de seguridad y la topología del proyecto:

- 🏛️ **Decisiones Arquitectónicas (ADRs):** [docs/adr/](docs/adr/) — Registro histórico de decisiones:
  - [ADR 0001: Microkernel de Plugins WebAssembly (WASM) con Extism](docs/adr/0001-wasm-extism-plugins.md)
  - [ADR 0002: Cerebro con PostgreSQL y pgvector](docs/adr/0002-pgvector-brain-memory.md)
  - [ADR 0003: Sequential Merge Queue con git merge-tree](docs/adr/0003-sequential-merge-queue-merge-tree.md)
  - [ADR 0004: Gobernanza Human-in-the-Loop con Tokens Criptográficos HMAC](docs/adr/0004-hitl-hmac-governance.md)
  - [ADR 0005: Conectores LLM Dinámicos y Cadenas de Fallback](docs/adr/0005-multi-provider-dynamic-llm.md)
  - [ADR 0006: Webhooks Salientes Seguros con Svix y DLQ](docs/adr/0006-despachador-webhooks-salientes-hmac-dlq.md)
  - [ADR 0007: Adaptadores Interactivos para Slack y Discord](docs/adr/0007-adaptadores-chat-interactivos-slack-discord.md)
  - [ADR 0008: CLI de Desarrollador y Memoria](docs/adr/0008-cli-desarrollador-runner-memoria-worker.md)
  - [ADR 0009: Distribución Segura, Cosign, SBOM y SLSA L3](docs/adr/0009-release-packaging-cosign-supply-chain.md)
  - [ADR 0010: Capa de Persistencia y Repositorios en PostgreSQL](docs/adr/0010-repositorio-persistencia-postgresql.md)
  - [ADR 0011: Inspección de Pasos y Dashboard del Cluster](docs/adr/0011-step-inspection-worker-cluster-dashboard.md)
- 📂 **Topología y Módulos:** [ESTRUCTURA.md](ESTRUCTURA.md) — Disposición de directorios y límites de paquetes Go.
- 🛡️ **Aislamiento y Sandboxing:** [INFRAESTRUCTURA.md](INFRAESTRUCTURA.md) — Detalles de Git Worktrees, contenedores y `git merge-tree`.
- 🗺️ **Hoja de Ruta:** [ROADMAP.MD](ROADMAP.MD) — Hitos y evolución planificada de la plataforma.
- ☸️ **Despliegue Kubernetes:** [deployments/helm/gv-factory/README.md](deployments/helm/gv-factory/README.md) — Manual exhaustivo de configuración Helm.
- 🔐 **Seguridad:** [SECURITY.md](SECURITY.md) — Política de divulgación responsable de vulnerabilidades.
- 🤝 **Contribuciones:** [CONTRIBUTING.md](CONTRIBUTING.md) — Guía para colaboradores y directrices de código.

---

## Cumplimiento Legal y Licencia

Este proyecto está licenciado bajo la **Licencia Apache 2.0** ([Apache-2.0](LICENSE)).

El repositorio cumple rigurosamente con el estándar **REUSE 3.3 de la Free Software Foundation Europe (FSFE)**:
- Cada fichero fuente incluye su cabecera SPDX formal (`SPDX-License-Identifier` y `SPDX-FileCopyrightText`).
- Los ficheros auxiliares y multimedia cuentan con atribución estandarizada en `.reuse/dep5`.
- Todos los textos completos de licencias se encuentran archivados en el directorio [LICENSES/](LICENSES/).
