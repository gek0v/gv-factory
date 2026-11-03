# Reconnaissance & Architecture Mapping

## Phase 1: Reconnaissance

Before hunting for quality defects, the parent agent or delegated research subagents must systematically explore the target repository to map its architecture, component boundaries, entry points, and domain dependencies.

### Reconnaissance Objectives

1. **System Topology & Entry Surfaces**:
   - HTTP/REST/GraphQL/gRPC APIs, CLI entry points, background consumers, websocket handlers, GUI/frontend controllers.
2. **Architectural Boundaries**:
   - Interface contracts between transport layers, application domain logic, persistence/storage layers, and external service clients.
3. **Subsystems & Modules**:
   - Discrete functional components (e.g. `auth`, `billing`, `notification`, `inventory`, `data_pipeline`, `cache`).
4. **Concurrency & Execution Model**:
   - Thread pools, async event loops (Node.js/Tokio/asyncio), goroutines/channels, lock primitives, actor models, process boundaries.
5. **Data & State Management**:
   - In-memory shared state, database connections, ORM models, transaction boundaries, caching layers, serialization formats.
6. **Existing Quality Guardrails**:
   - Unit/integration test coverage, strict type checks (`strict: true` in tsconfig, Rust clippy, Go vet, Python mypy/ruff), CI linters.

---

## Deliverable 1: `architecture.md`

The parent creates `architecture.md` summarizing the discovered topology. It must contain:

```markdown
# Target Architecture & Quality Baseline

## 1. System Overview
- **Repository**: [Name / Git Remote]
- **Primary Languages & Runtimes**: [e.g. TypeScript (Node.js 22), Go 1.24, Rust 2024]
- **Frameworks & Core Libraries**: [e.g. Express / Fastify, Tokio, Spring Boot, React]
- **Persistence & Storage**: [e.g. PostgreSQL via Prisma, Redis, SQLite]

## 2. Identified Subsystems
- `[subsystem_name]`: [Responsibility, key entry file, core interfaces]

## 3. Boundary & Interaction Map
- **Transport -> Application**: [How requests are routed, validated, and dispatched]
- **Domain -> Persistence**: [How business logic communicates with storage]
- **Async & Background**: [Worker pools, job queues, event busses]

## 4. Invariant & Quality Hotspots
- High-concurrency or high-throughput paths identified for deep inspection.
- Complex state transitions or unbuffered I/O pipelines.
- Suspected coupling or layering debt visible from module directory structure.
```

---

## Deliverable 2: `coverage-ledger.json`

The parent initializes the coverage ledger. Every unit is identified by a canonical ID:
`quality/<surface>/<boundary>/<subsystem>/<quality_class>`

### Canonical Ledger Unit Structure

```json
{
  "coverage_id": "quality/api_entrypoint/client_to_controller/orders/performance_and_algorithmic",
  "canonical_refs": {
    "surface": "api_entrypoint",
    "boundary": "client_to_controller",
    "subsystem": "orders",
    "quality_class": "performance_and_algorithmic"
  },
  "surface": "api_entrypoint",
  "boundary": "client_to_controller",
  "subsystem": "orders",
  "quality_class": "performance_and_algorithmic",
  "starting_paths": [
    "src/controllers/orderController.ts",
    "src/services/orderService.ts"
  ],
  "selected_companion_blocks": ["web_and_apis"],
  "excluded_blocks": [],
  "prior_status": null,
  "attempts": [],
  "wave": 1,
  "status": "planned",
  "agent_id": null,
  "reviewed_paths": [],
  "inspected_invariants": [],
  "result_fingerprints": [],
  "unresolved": null
}
```

### Seeding Rules for Ledger Units

1. **Surfaces**: `api_entrypoint`, `background_worker`, `data_access`, `domain_core`, `client_ui`, `cli_tool`.
2. **Boundaries**: `client_to_controller`, `controller_to_service`, `service_to_repository`, `domain_to_storage`, `thread_boundary`, `event_boundary`.
3. **Subsystems**: Extracted directly from repository packages/directories.
4. **Quality Classes**: Assign relevant classes from `QUALITY-CLASSES.md` (`architecture_and_coupling`, `concurrency_and_resources`, `performance_and_algorithmic`, `error_handling_and_resilience`, `maintainability_and_testability`, `type_safety_and_contracts`).
5. **Progressive Disclosure (`selected_companion_blocks`)**: Assign AT MOST ONE domain companion file (`web_and_apis`, `data_and_storage`, `systems_and_concurrency`, `frontend_and_client`, `cloud_and_serverless`) matching the unit's subsystem to avoid token inflation.

After seeding, the parent MUST validate the ledger:
```bash
node validate-coverage-ledger.cjs <output-dir>/coverage-ledger.json
```
If errors occur, fix the canonical IDs or unit references before proceeding to Phase 2.
