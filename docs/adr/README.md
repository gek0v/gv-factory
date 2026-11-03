<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Architecture Decision Records (ADRs)

Este directorio contiene los **Architecture Decision Records (ADRs)** oficiales de **gv-factory**, redactados siguiendo la estructura estandarizada propuesta por Michael Nygard (*Context*, *Decision*, *Consequences*, *Status*, *Date*).

Los ADRs documentan de manera transparente, inmutable y auditable las decisiones arquitectónicas fundamentales tomadas durante el diseño y evolución de la plataforma. El subsistema cognitivo ("El Cerebro", `internal/brain`) analiza y sincroniza periódicamente estos registros para alimentar la tabla `architectural_rules`, permitiendo a los agentes autónomos evaluar la conformidad de los cambios de código frente a las normas de diseño vigentes.

---

## Formato Estándar de los ADRs (Michael Nygard)

Cada registro se estructura con las siguientes secciones obligatorias:

1. **Título (`# ADR XXXX: <Título>`):** Identificador ordinal y nombre descriptivo de la decisión.
2. **Metadatos:**
   - `* **Estado:**` (`Aceptado` / `Accepted`, `Propuesto` / `Proposed`, `Superado` / `Superseded`, `Obsoleto` / `Deprecated`, `Rechazado` / `Rejected`).
   - `* **Fecha:**` Fecha de aprobación formal (formato `YYYY-MM-DD`).
   - `* **Decisores:**` Autores y revisores responsables de la decisión.
3. **Contexto (`## Contexto` / `## Context`):** Problema técnico, restricciones del negocio, consideraciones de rendimiento o seguridad que motivaron la decisión.
4. **Decisión (`## Decisión` / `## Decision`):** Solución arquitectónica adoptada, componentes afectados, patrones y contratos aplicados.
5. **Consecuencias (`## Consecuencias` / `## Consequences`):** Impactos positivos obtenidos y compromisos o limitaciones aceptadas (*trade-offs*).

---

## Índice Oficial de Architecture Decision Records

| ADR | Título | Estado | Fecha | Descripción Resumida |
| :--- | :--- | :---: | :---: | :--- |
| [**ADR-0001**](0001-wasm-extism-plugins.md) | **Microkernel Extism WASM para extensiones y Host Functions de seguridad** | `Aceptado` | 2026-10-31 | Adopción de Extism/Wasmtime para aislar memoria de plugins de agentes con arranque sub-milisegundo y Host Functions seguras. |
| [**ADR-0002**](0002-pgvector-brain-memory.md) | **PostgreSQL 16 con extensión pgvector para almacenamiento relacional y memoria vectorial episódica** | `Aceptado` | 2026-10-31 | Unificación de persistencia relacional ACID y memoria vectorial RAG mediante índices HNSW y búsqueda híbrida (FTS + Coseno). |
| [**ADR-0003**](0003-sequential-merge-queue-merge-tree.md) | **Merge Queue secuencial con validación previa 'git merge-tree' antes de rebase y fast-forward** | `Aceptado` | 2026-10-31 | Pipeline de integración atómica con comprobación especulativa en memoria (`git merge-tree`) y avance rápido exclusivo (`--ff-only`) a `main`. |
| [**ADR-0004**](0004-hitl-hmac-governance.md) | **Gobernanza Human-in-the-Loop con tokens HMAC criptográficos para compuertas de decisión** | `Aceptado` | 2026-10-31 | Protocolo de autorización con tokens opacos HMAC-SHA256, expiración TTL y auditoría inmutable para compuertas de alto riesgo. |
| [**ADR-0005**](0005-multi-provider-dynamic-llm.md) | **Arquitectura de conectores LLM con detección dinámica, fallback transparente y soporte multi-proveedor** | `Aceptado` | 2026-10-31 | Abstracción de conectores para OpenAI, Anthropic, Ollama y vLLM con conmutación transparente ante fallos y guardarraíles de tokens. |
| [**ADR-0006**](0006-despachador-webhooks-salientes-hmac-dlq.md) | **Despachador de Webhooks Salientes con HMAC-SHA256, Reintentos y DLQ** | `Aceptado` | 2026-10-31 | Notificación asíncrona de eventos a sistemas externos con firmas criptográficas, reintentos exponenciales y cola de mensajes muertos. |
| [**ADR-0007**](0007-adaptadores-chat-interactivos-slack-discord.md) | **Adaptadores Interactivos de Mensajería para Slack y Discord** | `Aceptado` | 2026-10-31 | Interacción con compuertas HITL y comandos interactivos mediante Slack Block Kit y Discord Components/Embeds. |
| [**ADR-0008**](0008-cli-desarrollador-runner-memoria-worker.md) | **CLI Oficial de Desarrollador con Runner, Consultas Semánticas y Demonio Worker** | `Aceptado` | 2026-10-31 | Herramienta de consola `ov-cli` para orquestación local/remota, consultas semánticas al Cerebro e inspección de pipelines. |
| [**ADR-0009**](0009-opentelemetry-tracing-metricas-prometheus.md) | **Observabilidad Distribuida con OpenTelemetry OTLP y Métricas Prometheus** | `Aceptado` | 2026-11-01 | Trazabilidad distribuida con propagación W3C TraceContext y métricas cuantitativas RED y de contabilidad de tokens. |
| [**ADR-0009B**](0009-release-packaging-cosign-supply-chain.md) | **Empaquetado Multi-Arquitectura GoReleaser, Firma Cosign y Atestación SLSA 3** | `Aceptado` | 2026-11-01 | Distribución segura y reproducible con firma digital keyless mediante Sigstore/Fulcio/Rekor y generación de SBOM SPDX. |
| [**ADR-0010**](0010-repositorio-persistencia-postgresql.md) | **Repositorio de Persistencia PostgreSQL y Migraciones para Runs, Steps y Gates** | `Aceptado` | 2026-11-01 | Capa de repositorio unificada en `internal/store` con `pgxpool`, transacciones atómicas y migraciones versionadas embebidas. |
| [**ADR-0011**](0011-step-inspection-worker-cluster-dashboard.md) | **Inspección Granular de Steps y Dashboard de Clúster de Workers en Canvas Web** | `Aceptado` | 2026-11-01 | Interfaz de diagnóstico paso a paso en `NodeInspector` y panel en tiempo real de salud de workers en el canvas interactivo. |

---

## Sincronización Automática con el Cerebro

Para sincronizar e indexar automáticamente los ADRs en la base de datos vectorial de PostgreSQL (`architectural_rules`), ejecute el script de sincronización oficial:

```bash
./scripts/sync_adrs.sh
```

O utilice el comando correspondiente del CLI de desarrollador:

```bash
ov-cli memory sync-adrs --path docs/adr
```

Para verificar en modo simulación sin alterar la base de datos:

```bash
./scripts/sync_adrs.sh --dry-run
```
