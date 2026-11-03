# Domain Companion: Test Data & Environment Management

This companion provides specialized inspection patterns for fixture factories, database state isolation, testcontainer lifecycles, and environment concurrency collisions.

---

## 1. Fixture Factories vs Static Database Seeding

### Invariant
Test fixtures must be generated dynamically with isolated identifiers and realistic relationships, avoiding fragile static seeds and hardcoded primary keys.

### Critical Failure Modes
1. **Hardcoded Primary Keys & Foreign Key Collisions**:
   Fixtures inserting static IDs like `id: 1` or `email: "alice@test.com"`. When tests run concurrently or in randomized order, secondary tests fail with `23505: unique_violation`.
2. **Brittle Shared Seed Scripts**:
   A single monolithic `seeds.sql` (2,000 lines) required before running any test. If one test modifies a record in the seed, subsequent unrelated tests fail mysteriously.
3. **Incomplete Cascade Teardown**:
   Creating parent records (e.g. `Company`) and child records (`Employee`), but cleanup hooks only delete the parent, leaving orphaned rows that violate foreign key constraints on the next run.

### Anti-Nitpicking Filter
- Reject complaints about using factory libraries (e.g. Fishery, Faker) vs plain TypeScript factory helper functions.
- Reject style preferences regarding builder patterns vs object spread overrides.

---

## 2. Database Isolation & Transaction Rollbacks

### Invariant
Every test requiring database interaction must execute in complete isolation without leaving dirty state for subsequent tests.

### Critical Failure Modes
1. **Truncation vs Rollback Inefficiency**:
   Truncating all database tables in `afterEach()` for hundreds of tests, wasting 80% of total test runtime on table locks and disk syncs.
   *Remediation*: Wrap each test in a transaction (`BEGIN TRANSACTION`) and issue a `ROLLBACK` at test teardown.
2. **Missing Per-Worker Database Schemas**:
   Running parallel test runners (e.g. `jest -w 4` or `pytest -n 4`) against a single shared test database without worker-specific databases (e.g. `test_db_worker_1`, `test_db_worker_2`) or isolated PostgreSQL schemas.
3. **Dirty In-Memory Caches**:
   Database rows are rolled back, but application-level in-memory caches (Redis stubs, memory Maps) retain references to deleted IDs.

### Anti-Nitpicking Filter
- Reject flagging full database truncation in `afterAll()` suite teardown; only flag truncation when executed per individual test case.

---

## 3. Ephemeral Environments, Testcontainers & Port Allocations

### Invariant
Test environments must not rely on unmanaged external services or fixed network ports that collide under parallel execution.

### Critical Failure Modes
1. **Hardcoded Network Ports (`EADDRINUSE`)**:
   Starting test HTTP servers or mock servers on fixed ports (e.g. `http.createServer().listen(3000)`). If two test files run in parallel or a prior test did not exit cleanly, the runner crashes with `listen EADDRINUSE :::3000`.
   *Remediation*: Always bind to port `0` (`listen(0)`) to let the OS allocate a free ephemeral port, then read `server.address().port`.
2. **Orphaned Testcontainer Instances**:
   Launching Docker containers (Postgres, LocalStack, Kafka) via testcontainers without registering `Ryuk` resource reaper or proper suite teardown hooks, leaving dozens of zombie Docker containers consuming host memory.
3. **Shared Temp File Collisions**:
   Tests writing to a fixed temporary file path like `/tmp/report.csv` rather than generating a unique temporary directory via `fs.mkdtempSync()`.

### Anti-Nitpicking Filter
- Reject complaints about spinning up Docker testcontainers in integration test suites if host resources and CI runners comfortably support Docker.
