# QA Defect Classes & Test Invariant Patterns

This document defines the 6 canonical QA defect classes used across reconnaissance, hunting, validation, and reporting in `qa-audit`.

---

## 1. Assertion Rigor & Soundness (`assertion_and_soundness`)

### Invariants & Contracts
- **Definitive Failure on Regression**: If the production code breaks or returns invalid data, the test MUST fail. A test that passes regardless of production bugs provides dangerous false confidence.
- **Explicit Negative Contract**: Negative test cases must assert specific error types, HTTP status codes, and error messages, not merely check that an error occurred.
- **Asynchronous Assertion Execution**: Async tests must properly await all assertions. Un-awaited assertions in promises or callbacks can fail silently without failing the test runner.
- **Assertion Non-Vacuity**: Assertions must not evaluate tautologically true statements (e.g. `expect(response).toBeDefined()` when `response` is an empty object, or `expect(array.length).toBeGreaterThanOrEqual(0)`).

### Critical Defect Patterns
- **Tautological Assertions**:
  ```typescript
  // Defect: Passes for any object, even { error: "Crash" }
  const result = await userService.getUser(1);
  expect(result).toBeDefined(); // VACUOUS: does not check result.id === 1 or user properties
  ```
- **Un-awaited Async Promises**:
  ```typescript
  // Defect: Missing await causes assertion to run after test completes; runner exits with PASS
  it("rejects unauthorized token", () => {
    expect(authService.verify("bad-token")).rejects.toThrow("Unauthorized"); // Missing await!
  });
  ```
- **Swallowed Exceptions in Test Logic**:
  ```typescript
  // Defect: Catch block catches error and suppresses test failure
  try {
    await processOrder();
  } catch (err) {
    // Defect: Missing expect(err).toBeInstanceOf(...) or fail() if processOrder should not throw
  }
  ```
- **Empty or Incomplete Loop Assertions**:
  ```typescript
  // Defect: If users array is empty, loop never executes and test passes vacuously
  const users = await fetchUsers();
  users.forEach(u => expect(u.active).toBe(true)); // Needs expect(users.length).toBeGreaterThan(0)
  ```

### Excluded as Cosmetic
- Preference for `expect(a).toBe(b)` vs `expect(a).toEqual(b)` on primitives.
- Debates over single assertion per test vs multiple assertions per test when all assertions are sound.
- Test description wording conventions (`it should ...` vs `test(...)`).

---

## 2. Mock Fidelity & Isolation (`mock_fidelity_and_isolation`)

### Invariants & Contracts
- **Contract Conformance**: Stubs and mocks must match the current contract (parameter types, response schemas, error responses) of the real implementation or external service.
- **Mock State Isolation**: Mocks, spies, and stubs created in a test must be strictly reset (`restoreAllMocks`, `resetMocks`) between test runs to prevent state leaking across test files.
- **No Over-Mocking Domain Logic**: Test the system under test, not the mock. If 90% of the tested function's internal collaborators are mocked, the test is testing mock configuration rather than real behavior.

### Critical Defect Patterns
- **Mock Drift (Schema Desynchronization)**:
  Mock returns an outdated API payload that production no longer accepts or produces:
  ```typescript
  // Real Stripe API deprecated `source` in favor of `payment_method`
  // Mock still returns `{ source: "tok_123" }`, masking production crash
  mockStripe.charges.create.mockResolvedValue({ id: "ch_123", source: "tok_123" });
  ```
- **Tautological Mocks**:
  ```typescript
  // Defect: Mocking the exact function under test
  const calculateTotal = vi.fn().mockReturnValue(100);
  expect(calculateTotal([item1, item2])).toBe(100); // Tests the mock, not the calculation!
  ```
- **Leaked Mock State Across Tests**:
  ```typescript
  // Defect: spyOn without afterEach cleanup leaks into next test file
  jest.spyOn(Date, 'now').mockReturnValue(1600000000000);
  // Missing afterEach(() => jest.restoreAllMocks())
  ```
- **Incomplete Mocking of Error Paths**:
  Mocking only happy path responses while the system's real error handling (timeouts, 429 rate limits, 500 errors) remains completely un-mocked and un-tested.

### Excluded as Cosmetic
- Disagreements on using `vi.spyOn` vs `vi.fn()` when cleanup is handled cleanly.
- Choosing MSW (Mock Service Worker) vs Nock for HTTP stubbing when contract fidelity is preserved.

---

## 3. Flakiness & Concurrency Safety (`flakiness_and_concurrency`)

### Invariants & Contracts
- **Deterministic Execution**: Given unchanged code, a test suite must yield identical results across 1,000 runs, regardless of CPU speed, execution order, or concurrency.
- **No Arbitrary Timing Sleeps**: Tests must synchronize on observable state (events, promises, DOM elements, DB polling with timeout), NEVER on arbitrary `sleep(1000)` or `setTimeout` delays.
- **Worker Isolation**: Tests executing concurrently across multiple worker processes or threads must not collide on shared resources (ports, file paths, database tables).

### Critical Defect Patterns
- **Arbitrary Sleep Polling**:
  ```typescript
  // Defect: 500ms is enough on developer laptop, fails on loaded CI runner
  await submitForm();
  await new Promise(r => setTimeout(r, 500));
  expect(screen.getByText("Success")).toBeVisible();
  ```
- **Shared Mutable Database State**:
  Tests reading/modifying a single static test user `id: "test-user"` simultaneously in parallel workers, causing random unique constraint violations or conflicting updates.
- **Race Condition in Async Event Listeners**:
  Attaching event listeners AFTER dispatching the triggering action:
  ```typescript
  // Defect: Event may fire before listener is attached if execution is fast
  emitter.emit("ready");
  await new Promise(resolve => emitter.once("ready", resolve));
  ```
- **Order-Dependent Test Execution**:
  Test B assumes Test A ran first and created an entity in the database. When tests run in randomized order or parallel workers, Test B fails.

### Excluded as Cosmetic
- Flakiness suspected without a clear reproduction mechanism or identifiable race condition (must be categorized as `needs_validation`).
- Adding safety timeouts to already deterministic wait conditions.

---

## 4. Boundary & Error Handling Gaps (`boundary_and_coverage_gaps`)

### Invariants & Contracts
- **Boundary Value Verification**: Tests must verify behavior at boundary extremes: $0, \pm 1, \infty$, empty collections, maximum payload sizes, Unicode/null bytes.
- **Negative & Failure Path Coverage**: For every valid happy path, tests must verify that invalid inputs, unauthenticated requests, and corrupted payloads trigger controlled error responses, not unhandled 500 crashes or silent corruptions.
- **Idempotency Verification**: Mutating operations (payments, webhook handlers, deletions) must be tested for multiple consecutive invocations with the same payload.

### Critical Defect Patterns
- **Unasserted Fallback Branches**:
  Service has retry logic or circuit breaker fallback, but no test exercises what happens when all retries are exhausted.
- **Missing Authorization Boundary Tests**:
  Test suite verifies admin access, but never tests that non-admin or unauthenticated requests are rejected with 401/403.
- **Empty Array / Zero Quantity Blindspot**:
  Billing engine tested with 1, 5, and 10 items, but never with 0 items, leading to division-by-zero crashes in production discount calculation.
- **Truncated Error Assertions**:
  Checking only `expect(res.status).toBe(400)` without verifying that the response contains the specific error message explaining *why* the input was invalid.

### Excluded as Cosmetic
- Demanding 100% line coverage for trivial logging statements or boilerplate getters.
- Requesting combinatorial tests for mutually exclusive enum flags where the underlying code has no branching.

---

## 5. Test Architecture & Performance (`test_architecture_and_performance`)

### Invariants & Contracts
- **Test Pyramid Proportions**: The foundation of the test suite should consist of fast, reliable unit and integration tests. Slow, heavy E2E tests should be reserved for critical user journeys.
- **Sub-Second Unit Execution**: Unit test suites should execute in seconds, not minutes. Heavy I/O or database operations belong in integration suites.
- **No Cascading Fixture Inflation**: Avoid spinning up a full browser, running DB migrations, or seeding 10,000 records for a test that only verifies a date formatting utility.

### Critical Defect Patterns
- **Test Pyramid Inversion (Ice Cream Cone Anti-Pattern)**:
  Zero unit tests for complex business calculations, with the team relying exclusively on 30-minute Cypress/Playwright browser suites to test every permutation.
- **Cascading Setup Overhead**:
  `beforeEach` hook truncating and re-migrating the entire database schema for 500 individual tests instead of wrapping tests in rolling-back transactions.
- **Redundant Suite Duplication**:
  Testing the identical API serialization logic in 4 different places (unit test, controller integration test, contract test, and E2E test) without adding distinct invariant coverage.
- **CI Pipeline Bottlenecks**:
  Test stages running sequentially for 45 minutes without parallelization, fixture caching, or test sharding.

### Excluded as Cosmetic
- Choosing between Jest and Vitest when test execution time is acceptable.
- Organization of test directory structure (`__tests__` next to code vs top-level `tests/`) when discovery is clean.

---

## 6. Test Data & Environment Lifecycle (`test_data_and_lifecycle`)

### Invariants & Contracts
- **Strict Teardown & Clean State**: Every test must return the environment to its initial state. No leftover files, dirty DB rows, or open network sockets.
- **Dynamic Fixture Factories**: Tests should generate unique, isolated test entities via factories (e.g. `createTestUser()`) with random UUIDs rather than static hardcoded IDs.
- **Deterministic Port & Socket Allocation**: Ephemeral services (test HTTP servers, mock listeners) must bind to port `0` (dynamic port allocation) to prevent `EADDRINUSE` collisions under concurrency.

### Critical Defect Patterns
- **Dirty State Cascading**:
  A test creates an admin user `admin@example.com` and fails to delete it in `afterEach`. Subsequent tests fail because the email already exists in the database.
- **Temporary File Pollution**:
  Tests writing output to `/tmp/test-output.json` without cleanup, causing collisions on CI build agents shared across branches.
- **Dangling Process / Socket Leaks**:
  Spawning child processes or background HTTP servers without `server.close()` in teardown hooks, preventing test runners from cleanly exiting (`Jest did not exit one second after the test run has completed`).
- **Static Seed Collisions**:
  Using hardcoded auto-increment IDs (`id: 1`) in fixture scripts, breaking foreign key relationships when tests run concurrently.

### Excluded as Cosmetic
- Style of fixture definition (FactoryBot style vs plain TypeScript helper functions).
- Preference for SQLite in-memory vs Dockerized Postgres for integration tests if fidelity is maintained.
