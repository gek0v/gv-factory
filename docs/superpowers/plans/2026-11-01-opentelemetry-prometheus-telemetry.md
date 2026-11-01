<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# OpenTelemetry and Prometheus Telemetry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the complete production observability subsystem in `internal/telemetry` for `gv-factory`, including OpenTelemetry OTLP distributed tracing with W3C TraceContext propagation and Prometheus metrics collectors covering RED, DAG, worker queues, and LLM token costs.

**Architecture:** Create an isolated, cohesive `internal/telemetry` package providing a resilient OTel `TracerProvider` with OTLP exporter and local No-Op fallback, a dedicated `prometheus.Registry` with 9 canonical collectors, W3C HTTP/NATS propagators, HTTP middleware, and structured DAG step spans (`dag.step.<node_type>`), wired seamlessly into `ov-server`, `internal/dag`, and webhook connectors.

**Tech Stack:** Go 1.22+, `go.opentelemetry.io/otel` (v1.34.0), `go.opentelemetry.io/otel/trace`, `go.opentelemetry.io/otel/sdk`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`, `github.com/prometheus/client_golang` (v1.21.0), NATS JetStream, standard `net/http`.

**Spec:** `docs/superpowers/specs/2026-11-01-opentelemetry-prometheus-telemetry-design.md`

## Global Constraints

- Every new Go file must contain the SPDX header:
  ```text
  // SPDX-License-Identifier: Apache-2.0
  // SPDX-FileCopyrightText: 2026 gv-factory contributors
  ```
- Code formatting: Tabulation (`\t`) for Go code, Unix line endings (`LF`).
- Conventional commits: `feat(telemetry): ...` or `test(telemetry): ...` referencing Issue #43.
- Isolated registries: Prometheus metrics must use dedicated `*prometheus.Registry` per instance, never mutating the global default registry.
- Zero network reliance in tests: All tests must run offline with No-Op / In-Memory providers.
- Thread-safety: 100% data-race free under `go test -race ./...`.

## Review Focus

1. **Malformed W3C `traceparent` headers:** When incoming HTTP or NATS events carry invalid, truncated, or malicious trace headers, the system must not panic; it must gracefully log or ignore and generate a clean root span. (Tested in Task 5).
2. **Unreachable OTLP Collector:** When `OTEL_EXPORTER_OTLP_ENDPOINT` points to a dead or missing host, the server must not hang or fail to start; it must fallback to a local no-op tracer with a logged warning. (Tested in Task 4).
3. **High-concurrency DAG step execution:** When dozens of DAG nodes execute concurrently across goroutines, recording span attributes, status codes, and Prometheus histogram observations must be lock-free or cleanly serialized without race conditions. (Tested in Task 6).
4. **Empty or missing NATS `_trace` field:** When extracting trace context from legacy or external NATS events without `_trace` payload metadata, the system must return a valid non-nil `context.Context` without dereferencing nil. (Tested in Task 5).
5. **Path cardinality protection in RED metrics:** When arbitrary HTTP paths are requested, the middleware must sanitize/normalize paths to prevent Prometheus metric explosion (e.g. normalizing IDs in webhook paths). (Tested in Task 7).

---

### Task 1: Go Dependencies for OpenTelemetry & Prometheus

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `go.opentelemetry.io/otel` v1.34.0, `github.com/prometheus/client_golang` v1.21.0 dependencies ready for compilation.

- [ ] **Step 1: Write the failing check**
Run `go list -m github.com/prometheus/client_golang` and `go list -m go.opentelemetry.io/otel`
- [ ] **Step 2: Verify dependencies resolution**
Run: `go mod tidy`
Expected: PASS with dependencies recorded in `go.mod`
- [ ] **Step 3: Run all existing tests to ensure no regressions**
Run: `go test ./...`
Expected: PASS
- [ ] **Step 4: Commit**
```bash
git add go.mod go.sum
git commit -m "build(telemetry): add OpenTelemetry and Prometheus Go dependencies (#43)"
```

---

### Task 2: Telemetry Configuration (`internal/telemetry/config.go`)

**Files:**
- Create: `internal/telemetry/config.go`
- Test: `internal/telemetry/config_test.go`

**Interfaces:**
- Produces: `type Config struct`, `func DefaultConfig() Config`, `func ConfigFromEnv() Config`, `func (c Config) Validate() error`

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/config_test.go` testing default values, environment variable overrides (`OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_ENVIRONMENT`, `OTEL_TRACES_SAMPLER_ARG`), and validation logic.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test ./internal/telemetry`
Expected: FAIL (package does not exist)
- [ ] **Step 3: Implement `internal/telemetry/config.go`**
Implement `Config` struct with SPDX headers, default values (`ServiceName: "ov-server"`, `ServiceVersion: "1.0.0"`, `Environment: "development"`), and `ConfigFromEnv()`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/telemetry -run TestConfig`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/config.go internal/telemetry/config_test.go
git commit -m "feat(telemetry): add configuration options and environment parser (#43)"
```

---

### Task 3: Prometheus Metrics Registry & Collectors (`internal/telemetry/metrics.go`)

**Files:**
- Create: `internal/telemetry/metrics.go`
- Test: `internal/telemetry/metrics_test.go`

**Interfaces:**
- Consumes: `Config`
- Produces: `type Metrics struct`, `func NewMetrics() *Metrics`, `func (m *Metrics) Handler() http.Handler`, plus collector fields:
  - `HTTPRequestsTotal` (`*prometheus.CounterVec`)
  - `HTTPRequestDuration` (`*prometheus.HistogramVec`)
  - `FactoryRunsTotal` (`*prometheus.CounterVec`)
  - `DAGStepDuration` (`*prometheus.HistogramVec`)
  - `WorkersActiveCount` (`prometheus.Gauge`)
  - `QueueDepth` (`*prometheus.GaugeVec`)
  - `WorkerSandboxesActive` (`prometheus.Gauge`)
  - `LLMTokensConsumedTotal` (`*prometheus.CounterVec`)
  - `LLMEstimatedCostUSD` (`*prometheus.CounterVec`)

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/metrics_test.go` testing metric registration, incrementing counters/histograms/gauges, recording LLM tokens and costs, and rendering output through `Handler()` verifying OpenMetrics formatted text.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/telemetry -run TestMetrics`
Expected: FAIL
- [ ] **Step 3: Implement `internal/telemetry/metrics.go`**
Create isolated `prometheus.NewRegistry()`, define the 9 metric collectors with exact labels specified in Issue #43, and expose `m.Handler()`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/telemetry -run TestMetrics`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/metrics.go internal/telemetry/metrics_test.go
git commit -m "feat(telemetry): add Prometheus metrics registry and collectors (#43)"
```

---

### Task 4: OpenTelemetry Tracer Provider & OTLP Exporter (`internal/telemetry/tracer.go`)

**Files:**
- Create: `internal/telemetry/tracer.go`
- Test: `internal/telemetry/tracer_test.go`

**Interfaces:**
- Consumes: `Config`
- Produces: `type TracerProvider struct`, `func NewTracerProvider(ctx context.Context, cfg Config) (*TracerProvider, error)`, `func (tp *TracerProvider) Tracer(name string) trace.Tracer`, `func (tp *TracerProvider) Shutdown(ctx context.Context) error`, `func (tp *TracerProvider) IsNoop() bool`

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/tracer_test.go` testing:
  - Fallback to No-Op provider when endpoint is empty or unreachable.
  - Resource attributes (`service.name`, `service.version`, `deployment.environment`).
  - Graceful `Shutdown` without errors.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/telemetry -run TestTracer`
Expected: FAIL
- [ ] **Step 3: Implement `internal/telemetry/tracer.go`**
Implement `NewTracerProvider` with OTLP gRPC/HTTP exporter, resilience against connection failure, global text map propagator configuration, and clean shutdown.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/telemetry -run TestTracer`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/tracer.go internal/telemetry/tracer_test.go
git commit -m "feat(telemetry): add OpenTelemetry TracerProvider with OTLP exporter and fallback (#43)"
```

---

### Task 5: W3C TraceContext Propagation for HTTP and NATS (`internal/telemetry/propagation.go`)

**Files:**
- Create: `internal/telemetry/propagation.go`
- Test: `internal/telemetry/propagation_test.go`

**Interfaces:**
- Consumes: `go.opentelemetry.io/otel/propagation`, `internal/gateway.Event`
- Produces:
  - `func InjectHTTP(ctx context.Context, header http.Header)`
  - `func ExtractHTTP(ctx context.Context, header http.Header) context.Context`
  - `func InjectEvent(ctx context.Context, ev *gateway.Event)`
  - `func ExtractEvent(ctx context.Context, ev gateway.Event) context.Context`
  - `type EventCarrier struct`

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/propagation_test.go` testing:
  - Roundtrip injection and extraction across HTTP headers (`traceparent`, `tracestate`).
  - Roundtrip injection and extraction across `gateway.Event.Payload["_trace"]`.
  - Resilience against malformed or missing headers (no panics, returns valid child or root context).
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/telemetry -run TestPropagation`
Expected: FAIL
- [ ] **Step 3: Implement `internal/telemetry/propagation.go`**
Implement standard W3C TraceContext injectors and extractors using `propagation.TraceContext`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/telemetry -run TestPropagation`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/propagation.go internal/telemetry/propagation_test.go
git commit -m "feat(telemetry): add W3C TraceContext propagation for HTTP and NATS events (#43)"
```

---

### Task 6: Structured DAG Step Spans & Metrics (`internal/telemetry/dag_span.go`)

**Files:**
- Create: `internal/telemetry/dag_span.go`
- Test: `internal/telemetry/dag_span_test.go`

**Interfaces:**
- Consumes: `Metrics`, `trace.Tracer`
- Produces:
  - `type StepSpanOpts struct { FactoryID, RunID, NodeID, NodeType, GitCommit, AgentModel string }`
  - `func StartStepSpan(ctx context.Context, tracer trace.Tracer, metrics *Metrics, opts StepSpanOpts) (context.Context, trace.Span, func(status string, err error))`

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/dag_span_test.go` verifying:
  - Span name equals `dag.step.<node_type>` (e.g. `dag.step.tdd`, `dag.step.review`).
  - All 7 attributes are recorded: `factory.id`, `run.id`, `node.id`, `node.type`, `git.commit`, `agent.model`, `step.status`.
  - Calling `done(status, err)` sets span error status, updates `step.status`, and records `ov_dag_step_duration_seconds`.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/telemetry -run TestStepSpan`
Expected: FAIL
- [ ] **Step 3: Implement `internal/telemetry/dag_span.go`**
Implement `StartStepSpan` using `trace.SpanStart` and `metrics.DAGStepDuration`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/telemetry -run TestStepSpan`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/dag_span.go internal/telemetry/dag_span_test.go
git commit -m "feat(telemetry): add structured DAG step spans and metrics (#43)"
```

---

### Task 7: HTTP Middleware for Telemetry and RED Metrics (`internal/telemetry/middleware.go`)

**Files:**
- Create: `internal/telemetry/middleware.go`
- Test: `internal/telemetry/middleware_test.go`

**Interfaces:**
- Consumes: `Metrics`, `trace.Tracer`, `ExtractHTTP`, `InjectHTTP`
- Produces: `func NewHTTPMiddleware(metrics *Metrics, tracer trace.Tracer) func(http.Handler) http.Handler`

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/middleware_test.go` testing:
  - Server spans creation and context propagation into handler request context.
  - Injected `traceparent` header in HTTP response.
  - Proper increment of `HTTPRequestsTotal` and `HTTPRequestDuration` on 200, 400, and 500 status codes.
  - Skipping span creation on `/healthz`, `/readyz`, and `/metrics`.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/telemetry -run TestMiddleware`
Expected: FAIL
- [ ] **Step 3: Implement `internal/telemetry/middleware.go`**
Implement response status recorder and telemetry wrapper around `http.Handler`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/telemetry -run TestMiddleware`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/middleware.go internal/telemetry/middleware_test.go
git commit -m "feat(telemetry): add HTTP middleware for tracing and RED metrics (#43)"
```

---

### Task 8: Unified Telemetry Lifecycle Facade (`internal/telemetry/telemetry.go`)

**Files:**
- Create: `internal/telemetry/telemetry.go`
- Test: `internal/telemetry/telemetry_test.go`

**Interfaces:**
- Consumes: `Config`, `TracerProvider`, `Metrics`
- Produces:
  - `type Telemetry struct { TracerProvider *TracerProvider; Tracer trace.Tracer; Metrics *Metrics; Config Config }`
  - `func New(ctx context.Context, cfg Config) (*Telemetry, error)`
  - `func (t *Telemetry) Shutdown(ctx context.Context) error`
  - `func (t *Telemetry) Status() string`

- [ ] **Step 1: Write the failing test**
Create `internal/telemetry/telemetry_test.go` verifying unified initialization, status string ("healthy", "degraded", "noop"), and shutdown.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/telemetry -run TestTelemetry`
Expected: FAIL
- [ ] **Step 3: Implement `internal/telemetry/telemetry.go`**
Assemble tracer, metrics, and lifecycle manager into a single cohesive entrypoint.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/telemetry`
Expected: PASS (all tests in package pass cleanly)
- [ ] **Step 5: Commit**
```bash
git add internal/telemetry/telemetry.go internal/telemetry/telemetry_test.go
git commit -m "feat(telemetry): add unified Telemetry facade and lifecycle manager (#43)"
```

---

### Task 9: Integration into `ov-server` (`cmd/ov-server/server.go` & `main.go`)

**Files:**
- Modify: `cmd/ov-server/server.go`
- Modify: `cmd/ov-server/main.go`
- Modify: `cmd/ov-server/main_test.go`

**Interfaces:**
- Consumes: `internal/telemetry.Telemetry`, `NewServerMux`
- Produces: `/metrics` endpoint, enriched `/healthz`, new `/readyz` endpoint, and global HTTP middleware wrapping.

- [ ] **Step 1: Write the failing test in `cmd/ov-server/main_test.go`**
Test:
  - GET `/metrics` returns 200 OK with Prometheus format.
  - GET `/healthz` returns 200 OK with `"telemetry"` field.
  - GET `/readyz` returns 200 OK with readiness status.
  - API endpoints return `traceparent` response header.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./cmd/ov-server`
Expected: FAIL
- [ ] **Step 3: Update `cmd/ov-server/server.go` and `cmd/ov-server/main.go`**
Update `NewServerMux` to accept `*telemetry.Telemetry`, register `/metrics`, `/readyz`, update `/healthz`, and wrap mux with `telemetry.NewHTTPMiddleware`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./cmd/ov-server`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add cmd/ov-server/server.go cmd/ov-server/main.go cmd/ov-server/main_test.go
git commit -m "feat(server): expose /metrics, /readyz and telemetry middleware in ov-server (#43)"
```

---

### Task 10: Integration into DAG Task Execution (`internal/dag/dispatcher.go`)

**Files:**
- Modify: `internal/dag/dispatcher.go`
- Modify: `internal/dag/template.go`
- Modify: `internal/dag/dispatcher_test.go`

**Interfaces:**
- Consumes: `internal/telemetry.StartStepSpan`, `internal/telemetry.StepSpanOpts`, `internal/telemetry.Metrics`
- Produces: Traced step executions with span name `dag.step.<node_type>` and duration metrics recorded in `ov_dag_step_duration_seconds`.

- [ ] **Step 1: Write the failing test in `internal/dag/dispatcher_test.go`**
Test that `ExecuteWorkflow` records spans for every step executed and registers `ov_factory_runs_total` and `ov_dag_step_duration_seconds`.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/dag -run TestDispatcher`
- [ ] **Step 3: Instrument `internal/dag/dispatcher.go`**
Wire `tracer` and `metrics` in `DispatcherConfig`/`Dispatcher`, wrap `executeNodeWithPolicy` with `StartStepSpan`, and record completion metrics in `ExecuteWorkflow`.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/dag`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/dag/dispatcher.go internal/dag/template.go internal/dag/dispatcher_test.go
git commit -m "feat(dag): instrument node executions with OpenTelemetry spans and metrics (#43)"
```

---

### Task 11: Integration into Inbound & Outbound Webhooks (`internal/connectors`)

**Files:**
- Modify: `internal/connectors/inbound/ingestor.go`
- Modify: `internal/connectors/inbound/inbound_test.go`
- Modify: `internal/connectors/outbound/dispatcher.go`
- Modify: `internal/connectors/outbound/dispatcher_test.go`

**Interfaces:**
- Consumes: `telemetry.ExtractHTTP`, `telemetry.InjectHTTP`, `telemetry.InjectEvent`
- Produces: End-to-end W3C trace propagation from webhook ingestion to JetStream event, and outward to destination subscribers.

- [ ] **Step 1: Write failing test in `inbound_test.go` and `dispatcher_test.go`**
Test that incoming `traceparent` is preserved and propagated to published event, and outbound dispatcher attaches `traceparent` to outgoing requests.
- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/connectors/inbound ./internal/connectors/outbound`
- [ ] **Step 3: Update `internal/connectors/inbound` and `internal/connectors/outbound`**
Add trace context extraction and injection.
- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v -race ./internal/connectors/inbound ./internal/connectors/outbound`
Expected: PASS
- [ ] **Step 5: Commit**
```bash
git add internal/connectors/inbound/ internal/connectors/outbound/
git commit -m "feat(connectors): propagate W3C TraceContext across inbound and outbound webhooks (#43)"
```

---

### Task 12: Full Verification, Race Detector & Linting

**Files:**
- Repo-wide verification

- [ ] **Step 1: Run full unit test suite with race detector**
Run: `go test -race ./...`
Expected: ALL PASS
- [ ] **Step 2: Run `go vet ./...`**
Run: `go vet ./...`
Expected: CLEAN (no warnings)
- [ ] **Step 3: Check git status and branch**
Run: `git status`
Expected: clean working directory on `feat/issue-43-telemetry`
- [ ] **Step 4: Final commit / push if required**
Final check of commit log and compliance with REUSE.
