# ADR 0009: Observabilidad Distribuida con OpenTelemetry OTLP y Métricas Prometheus

* **Estado:** Aceptado
* **Fecha:** 2026-11-01
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
En una plataforma de fábricas de software autónomas multi-agente (`gv-factory`), las ejecuciones de tareas involucran múltiples subsistemas heterogéneos: ingestores de webhooks externos (GitHub, GitLab, Sentry), enrutamiento por NATS JetStream, ejecución de nodos en WebAssembly (Extism), aislamiento en Git Worktrees y llamadas a modelos de lenguaje (LLM).

Para operar en producción con alta confiabilidad y depurar latencias o fallos entre pasos, es imperativo contar con:
1. **Trazabilidad distribuida extremo a extremo:** Correlación entre la petición HTTP entrante, la publicación del evento en NATS y la ejecución de cada paso en el grafo DAG mediante el estándar W3C TraceContext (`traceparent`).
2. **Monitoreo cuantitativo (Métricas RED):** Exposición de métricas de tráfico, latencia y errores en endpoints HTTP y webhooks.
3. **Métricas de negocio y ejecución:** Duración y estado de cada paso del DAG por tipo de nodo (`tdd`, `reviewer`, `gate`, etc.).
4. **Salud de infraestructura y colas:** Número de workers activos, sandboxes aprovisionados y profundidad de colas de tareas.
5. **Contabilidad de costes y tokens LLM:** Seguimiento en tiempo real de tokens consumidos (input, output, cache) y estimación de coste en dólares por proveedor y modelo.

## Decisión
Implementar el subsistema nativo `internal/telemetry`:

1. **OpenTelemetry TracerProvider con Exportador OTLP y Modo Resiliente (`tracer.go`):**
   - Configuración mediante variables estándar (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_ENVIRONMENT`).
   - Modo de respaldo local (*No-Op / In-Memory*) cuando no se especifica un collector OTLP, garantizando que el entorno de desarrollo local y la suite de pruebas unitarias funcionen sin dependencias de red externas.
   - Propagador global W3C TraceContext (`propagation.TraceContext`) y Baggage.

2. **Registro Prometheus Aislado con Colectores Canónicos (`metrics.go`):**
   - Creación de un `prometheus.Registry` dedicado por instancia para evitar colisiones de registro durante pruebas concurrentes.
   - Colectores RED: `ov_http_requests_total` y `ov_http_request_duration_seconds`.
   - Colectores de DAG: `ov_factory_runs_total` y `ov_dag_step_duration_seconds`.
   - Colectores de Capacidad: `ov_workers_active_count`, `ov_queue_depth` y `ov_worker_sandboxes_active`.
   - Colectores de LLM: `ov_llm_tokens_consumed_total` y `ov_llm_estimated_cost_usd`.
   - Exposición estándar vía `promhttp.HandlerFor` en el endpoint `/metrics` de `ov-server`.

3. **Propagación Uniforme entre Transportes (`propagation.go`):**
   - Inyección y extracción transparente de cabeceras W3C en peticiones HTTP y en el envelope de eventos de NATS (`gateway.Event.Payload["_trace"]`).

4. **Middleware HTTP de Observabilidad (`middleware.go`):**
   - Interceptación automática de peticiones en `ov-server` para registrar latencias, códigos HTTP y añadir cabeceras `traceparent` a las respuestas para facilitar la correlación de logs.

5. **Spans Semánticos del Grafo DAG (`dag_span.go`):**
   - Creación de spans estructurados bajo la convención de nombres `dag.step.<node_type>` con atributos estándar de observabilidad: `factory.id`, `run.id`, `node.id`, `node.type`, `git.commit`, `agent.model` y `step.status`.

6. **Endpoints de Salud Enriquecidos (`/healthz` y `/readyz`):**
   - Inclusión del estado del proveedor de telemetría en `/healthz`.
   - Nuevo endpoint `/readyz` que evalúa la preparación integral de `ov-server` antes de enrutar tráfico de producción.

## Consecuencias
* **Positivas:**
  - Observabilidad de grado de producción lista para integrarse con Datadog, Jaeger, Grafana, OpenTelemetry Collector y Prometheus.
  - Cero dependencias duras externas en modo local/test gracias al fallback a No-Op.
  - Trazabilidad continua a través de límites de proceso y red (HTTP -> NATS -> Worker -> WASM).
* **Negativas:**
  - Incremento ligero en la huella de dependencias en `go.mod` al incluir los paquetes oficiales de `go.opentelemetry.io/otel` y `github.com/prometheus/client_golang`.
