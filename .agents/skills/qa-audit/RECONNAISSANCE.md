# Test Topology Reconnaissance & Ledger Seeding

## Phase 1: Reconnaissance

Before hunting for QA defects, the parent agent or delegated research subagents must systematically explore the target repository to map its test suite topology, test frameworks, execution surfaces, mock strategies, and CI gating configurations.

### Reconnaissance Objectives

1. **Test Suite Topology & Surfaces**:
   - `unit_suite`: Fast in-memory tests isolated to individual functions, classes, or modules.
   - `integration_suite`: Multi-component tests exercising service boundaries, databases, queues, or local APIs.
   - `e2e_suite`: Browser/UI automation (Playwright, Cypress, Selenium) or full system black-box tests.
   - `api_contract`: Schema validation (OpenAPI, Pact, GraphQL), HTTP status assertions, payload contracts.
   - `performance_suite`: Benchmark harnesses, k6/Locust load scripts, latency thresholds.
   - `ci_pipeline`: GitHub Actions, GitLab CI, or Jenkins pipelines executing test stages, parallel matrices, and retry policies.

2. **Test Frameworks & Tooling**:
   - Test runners: Jest, Vitest, Pytest, Go `testing`, Cargo test, JUnit 5, Playwright, Mocha.
   - Assertion libraries: `expect`, Chai, testify, hamcrest.
   - Mocking & stubbing: `jest.mock()`, `vi.fn()`, `unittest.mock`, `gomock`, WireMock, MSW (Mock Service Worker), nock.
   - Test fixture factories: FactoryBot, Fishery, Faker, DB seed scripts.

3. **Concurrency & Execution Model**:
   - Worker concurrency: `--maxWorkers`, `pytest -n auto` (pytest-xdist), `go test -parallel`.
   - Shared resource isolation: Are databases isolated per worker (e.g. `test_db_1`, `test_db_2`), reset per test via transaction rollback, or unsafely shared?
   - Test isolation: Does each test clean up its state, or do tests rely on sequential execution order?

4. **CI Retries & Flakiness Indicators**:
   - Inspect CI configs for `retries: 2`, `flaky-test-reporter`, or `jest-circus` retry hooks. Retries in CI are the #1 smoking gun for hidden flakiness and race conditions.
   - Look for `.skip`, `.only`, `xit`, `pytest.mark.skip`, `@Disabled` annotations masking broken tests.

---

## Deliverable 1: `test-topology.md`

The parent creates `test-topology.md` summarizing the discovered test architecture:

```markdown
# Target Test Topology & QA Baseline

## 1. Test Suite Overview
- **Repository**: [Name / Git Remote]
- **Primary Frameworks**: [e.g. Vitest 3.0, Playwright 1.48, Pytest 8.2]
- **Mocking & Stubbing Strategy**: [e.g. MSW for network, jest.mock for internal modules, real Postgres in testcontainers]
- **Test Runner Concurrency**: [e.g. 4 workers in CI, sequential in local development]

## 2. Identified Test Surfaces & Directories
- `unit_suite`: `src/**/*.spec.ts` (Fast unit tests with isolated mocks)
- `integration_suite`: `tests/integration/` (Spins up Dockerized Redis and Postgres)
- `e2e_suite`: `tests/e2e/` (Playwright browser specs)
- `api_contract`: `tests/contract/` (Pact consumer/provider specs)
- `ci_pipeline`: `.github/workflows/test.yml` (Matrix across Node 20 and Node 22)

## 3. Test Data & State Management
- **Database Strategy**: [e.g. Global truncate after each suite, transaction rollback per test, or shared dirty DB]
- **Fixture Generation**: [e.g. Factory functions in `tests/factories/`]
- **External Network Access**: [e.g. Blocked via MSW, or live sandbox endpoints]

## 4. Suspected QA Hotspots & Fragility Areas
- High-risk areas: un-awaited promises in async tests, hardcoded `sleep(1000)` timeouts in E2E specs, over-mocking in payment flows.
- CI retry patterns or disabled test files.
```

---

## Deliverable 2: `coverage-ledger.json`

The parent initializes `coverage-ledger.json`. Every unit is identified by a canonical ID:
`qa/<test_surface>/<test_level>/<subsystem>/<qa_class>`

### Canonical Ledger Unit Structure

```json
{
  "coverage_id": "qa/unit_suite/unit/billing_service/mock_fidelity_and_isolation",
  "canonical_refs": {
    "surface": "unit_suite",
    "level": "unit",
    "subsystem": "billing_service",
    "qa_class": "mock_fidelity_and_isolation"
  },
  "surface": "unit_suite",
  "level": "unit",
  "subsystem": "billing_service",
  "qa_class": "mock_fidelity_and_isolation",
  "starting_paths": ["tests/unit/billing/webhook.test.ts"],
  "selected_companion_blocks": ["unit_and_integration"],
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

### Ledger Validation

Before launching Phase 2 hunters, validate the seeded ledger:
```bash
node <skill-dir>/validate-coverage-ledger.cjs <output-dir>/coverage-ledger.json
```

The command must exit with code `0`. Fix any validation errors before proceeding.
