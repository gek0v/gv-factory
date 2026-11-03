# Domain Companion: Unit & Integration Testing

This companion provides specialized inspection patterns for unit tests, service integration tests, module contracts, mocking fidelity, and component lifecycle hooks.

---

## 1. Mock Fidelity & Drift Traps

### Invariant
Stubs, spies, and mocks must reflect the real production contract of the mocked dependency. A mock that returns outdated or invalid data while passing tests creates false confidence.

### Critical Failure Modes
1. **Outdated Return Schema**:
   The real API or module returns `{ userId: string, active: boolean }`, but the unit test mock still returns `{ user_id: number }`.
2. **Missing Async Promise Wrapping**:
   Production method is `async` (returns a Promise), but mock returns a raw value synchronously (`mockReturnValue` instead of `mockResolvedValue`), masking unhandled promise rejections in production callers.
3. **Over-Mocking (The "Testing the Mock" Anti-Pattern)**:
   A service with 3 collaborator classes has all 3 classes completely mocked with static return values. The test only verifies that mocked methods were called with mocked arguments, testing zero real business logic.
4. **Leaky Spies Between Test Cases**:
   Using `jest.spyOn()` without calling `jest.restoreAllMocks()` in `afterEach()`. Mutations to global objects (e.g. `Date.now()`, `Math.random()`, `process.env`) bleed into other test suites.

### Anti-Nitpicking Filter
- Reject findings that criticize using mocks in unit tests when the test is explicitly designed as an isolated unit test.
- Reject complaints about mocking external third-party SDKs (Stripe, AWS S3, SendGrid) as long as the mock schema matches the SDK contract.

---

## 2. Assertion Rigor & Soundness

### Invariant
Every test must assert specific, non-vacuous outcomes that would fail if the unit under test produced incorrect results.

### Critical Failure Modes
1. **Vacuous Truth Assertions**:
   - `expect(result).toBeDefined()` when `result` is `{ error: "Fatal crash" }`.
   - `expect(typeof result).toBe("object")` when `result` is `null`.
2. **Unasserted Async Errors**:
   ```typescript
   // Defect: Test does not await rejects, so test completes before expectation fires
   it("throws on negative input", () => {
     expect(calc.sqrt(-1)).rejects.toThrow(); // Missing await!
   });
   ```
3. **Snapshot Testing Traps**:
   - Giant snapshots (> 100 lines) of serialized HTML or state trees that developers indiscriminately update with `-u` without reviewing changes.
   - Snapshots capturing non-deterministic fields like timestamps (`createdAt: new Date().toISOString()`) or auto-increment IDs.

### Anti-Nitpicking Filter
- Reject complaints that prefer `toBe(true)` over `toBeTruthy()` unless `toBeTruthy()` allows an unexpected truthy value (like `{}` or `"false"`).
- Reject style preferences regarding assertion chaining (`expect().toBe().and.have()`).

---

## 3. Integration Lifecycle & Database Rollback

### Invariant
Integration tests exercising database persistence or messaging queues must maintain strict state isolation.

### Critical Failure Modes
1. **Truncation Cascades**:
   Executing `TRUNCATE TABLE` across 50 tables before every individual test, turning a 5-second test suite into a 3-minute bottleneck.
2. **Missing Transaction Rollback**:
   Tests mutating database rows directly without wrapping each test case in a database transaction (`BEGIN ... ROLLBACK`). A failing test leaves dirty rows that break subsequent tests.
3. **Hardcoded Unique Keys**:
   Inserting static emails (`user@test.com`) or slug IDs in fixtures without dynamic generators (e.g. `user_${Date.now()}_${Math.random()}@test.com`), causing unique constraint collisions under concurrent test workers.

### Anti-Nitpicking Filter
- Reject complaints about using an in-memory database (SQLite) instead of a containerized database if the SQL dialect used does not rely on engine-specific features.
