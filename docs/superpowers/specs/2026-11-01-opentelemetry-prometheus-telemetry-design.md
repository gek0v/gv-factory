# Spec: OpenTelemetry Distributed Tracing y Métricas Prometheus en internal/telemetry

- **Fecha:** 2026-11-01
- **Estado:** Aprobado
- **Issue Asociado:** Issue #43 (Observabilidad nativa, OpenTelemetry OTLP tracing y métricas Prometheus)
- **Autor / Implementador:** Subagente issue-43 (gv-factory team)

---

## 1. Resumen Ejecutivo
Implementar el subsistema integral de observabilidad de producción `internal/telemetry` en la plataforma `gv-factory`. Este módulo provee:
1. **Trazabilidad distribuida con OpenTelemetry** mediante exportador OTLP (gRPC / HTTP) configurable y propagación de contexto estándar W3C (`traceparent`, `tracestate`) en endpoints HTTP, webhooks y el bus de eventos NATS JetStream.
2. **Métricas de producción con Prometheus** expuestas en el endpoint `/metrics` de `ov-server`, cubriendo las 4 dimensiones clave:
   - Métricas RED HTTP / Webhooks (`ov_http_requests_total`, `ov_http_request_duration_seconds`).
   - Ejecución del motor DAG (`ov_factory_runs_total`, `ov_dag_step_duration_seconds`).
   - Salud de Workers y Colas (`ov_workers_active_count`, `ov_queue_depth`, `ov_worker_sandboxes_active`).
   - Contabilidad de tokens y costes de LLMs (`ov_llm_tokens_consumed_total`, `ov_llm_estimated_cost_usd`).
3. **Middleware HTTP y adaptadores de transporte** para interceptar peticiones de manera no invasiva e inyectar cabeceras de correlación.
4. **Comprobación de preparación y salud (`/healthz` y `/readyz`)** con reporte del estado del subsistema de telemetría.

---

## 2. Arquitectura de Módulos y Paquetes

```
internal/telemetry/
├── config.go            # Opciones de configuración a partir de variables de entorno (OTEL_*, etc.)
├── config_test.go       # Tests de parsing y validación de variables de entorno
├── tracer.go            # TracerProvider, exportador OTLP gRPC/HTTP y fallback no-op/local
├── tracer_test.go       # Tests unitarios del proveedor de trazas y ciclo de vida
├── metrics.go           # Registry Prometheus aislado y declaración de colectores de métricas
├── metrics_test.go      # Tests unitarios de colectores, incrementos y renderizado OpenMetrics
├── propagation.go       # Utilidades W3C TraceContext para HTTP (headers) y NATS (Event.Payload)
├── propagation_test.go  # Tests de inyección y extracción de contexto en transportes
├── middleware.go        # Middleware HTTP para captura de códigos de estado, latencia y traceparent
├── middleware_test.go   # Tests del middleware HTTP sobre endpoints de servidor
├── dag_span.go          # Constructores de spans estructurados (dag.step.<node_type>) y métricas
├── dag_span_test.go     # Tests de creación de spans semánticos y reporte de atributos
└── telemetry.go         # Fachada unificada Telemetry (Lifecycle manager, Shutdown, Status)
```

---

## 3. Especificación Detallada de Componentes

### 3.1. Configuración (`internal/telemetry/config.go`)
Variables de entorno soportadas con valores por defecto seguros:
- `OTEL_SERVICE_NAME`: Nombre del servicio reportado a OTel (default: `ov-server`).
- `OTEL_SERVICE_VERSION`: Versión del binario/plataforma (default: `1.0.0`).
- `OTEL_ENVIRONMENT`: Entorno de despliegue (default: `development`).
- `OTEL_EXPORTER_OTLP_ENDPOINT`: Endpoint del collector OTLP (ej. `localhost:4317` o `otel-collector:4317`). Si está vacío, opera en modo local No-Op.
- `OTEL_EXPORTER_OTLP_PROTOCOL`: Protocolo (`grpc` o `http/protobuf`, default: `grpc`).
- `OTEL_EXPORTER_OTLP_INSECURE`: Booleano para deshabilitar TLS en desarrollo local (default: `true`).
- `OTEL_TRACES_SAMPLER_ARG`: Ratio de muestreo probabilístico entre 0.0 y 1.0 (default: `1.0`).

### 3.2. Proveedor de Trazas (`internal/telemetry/tracer.go`)
- Estructura `TracerProvider` que inicializa el SDK de OpenTelemetry Go (`go.opentelemetry.io/otel/sdk/trace`).
- Si `OTLPEndpoint` no está configurado, utiliza un `trace.NewNoopTracerProvider()` garantizando cero latencia y cero llamadas de red en pruebas o despliegues locales offline.
- Asigna el propagador global `otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))`.
- Cierre ordenado mediante `Shutdown(ctx context.Context) error` para asegurar el vaciado (flush) de buffers antes del apagado del proceso.

### 3.3. Proveedor de Métricas (`internal/telemetry/metrics.go`)
- Encapsula una instancia aislada `*prometheus.Registry` para evitar cualquier colisión en ejecuciones de pruebas paralelas (`go test -race ./...`).
- Métricas declaradas:
  1. `ov_http_requests_total`: CounterVec con labels `[method, path, status]`.
  2. `ov_http_request_duration_seconds`: HistogramVec con labels `[method, path, status]` y buckets: `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30]`.
  3. `ov_factory_runs_total`: CounterVec con labels `[status]`.
  4. `ov_dag_step_duration_seconds`: HistogramVec con labels `[node_type, status]` y buckets: `[0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300]`.
  5. `ov_workers_active_count`: Gauge.
  6. `ov_queue_depth`: GaugeVec con labels `[stream, topic]`.
  7. `ov_worker_sandboxes_active`: Gauge.
  8. `ov_llm_tokens_consumed_total`: CounterVec con labels `[provider, model, token_type]`.
  9. `ov_llm_estimated_cost_usd`: CounterVec con labels `[provider, model]`.
- Método `Handler() http.Handler` que devuelve un handler `promhttp.HandlerFor(registry, ...)`.

### 3.4. Propagación de Contexto W3C (`internal/telemetry/propagation.go`)
- `InjectHTTP(ctx context.Context, header http.Header)`: Inyecta cabeceras `traceparent` y `tracestate`.
- `ExtractHTTP(ctx context.Context, header http.Header) context.Context`: Extrae el span padre y retorna el nuevo contexto derivado.
- `EventCarrier`: Adaptador para `gateway.Event` serializando el mapa de correlación en `event.Payload["_trace"]`.
- `InjectEvent(ctx context.Context, ev *gateway.Event)` y `ExtractEvent(ctx context.Context, ev gateway.Event) context.Context`.

### 3.5. Middleware HTTP (`internal/telemetry/middleware.go`)
- Intercepta cada solicitud entrante en `ov-server`.
- Extrae el contexto padre con `ExtractHTTP`.
- Inicia el span de servidor: `HTTP <METHOD> <PATH>`.
- Registra el código de estado retornado mediante un `responseRecorder`.
- Al finalizar, inyecta `traceparent` en los headers de respuesta para diagnóstico externo.
- Incrementa `ov_http_requests_total` y mide la duración en `ov_http_request_duration_seconds`.
- Omite la creación de spans sobre `/healthz`, `/readyz` y `/metrics` para evitar saturación de trazas de health check.

### 3.6. Spans Estructurados del DAG (`internal/telemetry/dag_span.go`)
- Función de utilidad `StartStepSpan(ctx context.Context, tracer trace.Tracer, metrics *Metrics, opts StepSpanOpts) (context.Context, trace.Span, func(status string, err error))`.
- Nombre de span: `dag.step.<node_type>`.
- Atributos semánticos:
  - `factory.id`: ID o nombre de la plantilla de fábrica.
  - `run.id`: Identificador único de la corrida.
  - `node.id`: Identificador del nodo en el grafo.
  - `node.type`: Rol o tipo inferido (`tdd`, `reviewer`, `gate`, `spec_writer`, `trigger`).
  - `git.commit`: Commit SHA bajo análisis en el sandbox.
  - `agent.model`: Modelo LLM utilizado (ej. `claude-3-5-sonnet`).
  - `step.status`: Estado del paso (`pending`, `running`, `completed`, `failed`).
- Al invocar la función `done(status, err)`:
  - Se registra el evento de error en el span si corresponde.
  - Se fija el atributo final `step.status`.
  - Se registra la duración en `ov_dag_step_duration_seconds`.
  - Se cierra el span con `span.End()`.

---

## 4. Puntos de Integración en la Plataforma

1. **`cmd/ov-server/server.go` y `main.go`:**
   - Inclusión de `/metrics` con `tel.Metrics.Handler()`.
   - Modificación de `/healthz` y creación de `/readyz` reportando el estado del subsistema (`"telemetry": "healthy"` o `"degraded"` o `"noop"`).
   - Envoltura del enrutador HTTP con `telemetry.NewHTTPMiddleware`.
   - Cierre ordenado (`tel.Shutdown`) en el shutdown graceful del servidor.
2. **`internal/dag/dispatcher.go`:**
   - Instrumentación de `executeNodeWithPolicy` con `StartStepSpan`.
   - Registro de `ov_factory_runs_total` al finalizar `ExecuteWorkflow`.
3. **`internal/connectors/inbound` y `internal/connectors/outbound`:**
   - Extracción de traceparent en la ingestión de webhooks e inyección en el evento NATS.
   - Inyección de traceparent en la cabecera HTTP de webhooks salientes.

---

## 5. Estrategia de Pruebas y Criterios de Aceptación
- **Pruebas unitarias sin dependencias externas:**
  - Todas las pruebas de `internal/telemetry` corren con tracers No-Op o in-memory exporter y registros Prometheus aislados.
  - Validación de parsing de `traceparent` malformados (no deben causar pánicos).
  - Validación de concurrencia con `go test -race ./...`.
- **Verificación de linters y formato:**
  - `go vet ./...` limpio con código de salida 0.
  - Cabeceras SPDX Apache-2.0 en cada archivo nuevo.
  - Tabulaciones estándar para código Go y saltos de línea LF.
