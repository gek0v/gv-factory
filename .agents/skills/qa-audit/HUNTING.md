# QA & Test Invariant Hunting

## Phase 2: Run coverage-led hunting waves

The parent assigns `planned` ledger units to focused `general` hunter subagents. Each hunter **MUST be spawned in a fresh subagent with a clean context**—devoid of prior chat history, parent deliberations, or speculative bias.

One hunter may own closely related units within the same test surface or subsystem (e.g. all unit tests for `billing_service`), but disparate test surfaces (e.g. E2E browser tests vs backend unit tests) should be assigned to independent hunters.

When profile constraints limit hunter invocations, prioritize units in this order:
1. **Assertion Rigor & Soundness** (Tautological assertions, unasserted promise rejections, swallowed errors, false-positive passing tests).
2. **Mock Fidelity & Isolation** (Mock drift masking breaking changes, over-mocking, leaky mock state across tests).
3. **Flakiness & Concurrency Safety** (Async race conditions, missing awaits, arbitrary sleep calls, shared DB state under parallel workers).
4. **Boundary & Error Handling Gaps** (Missing negative tests, unasserted error branches, payload boundary fuzzing).
5. **Test Data & Lifecycle** (Dirty DB state, missing rollback, unisolated temp files, port collisions).
6. **Test Architecture & Performance** (Test pyramid inversion, slow redundant suites, CI execution bottlenecks).

---

### Token Efficiency & Anti-Hallucination Rules

1. **Targeted Reads Only**: NEVER read entire test or source files. Always use line-range reads (e.g. `path:15-55`). Maximum 80 lines per read.
2. **No Hallucinated Line Numbers**: Every line number cited in a `trace` or `evidence` item must be visually confirmed in a recent line-range read before recording.
3. **Raw JSON Return**: Subagents must output ONLY the JSON payload conforming to `report-schema.json`. Never output chat pleasantries, markdown intros ("Sure! Here are my findings:"), or conversational summaries.
4. **Bounded Outputs**:
   - `trace`: Maximum 5 steps (`test_case`, `fixture_or_mock`, `defect_site`, `impact_site`).
   - `evidence`: Maximum 3 decisive code snippets.
   - `recommended_diff`: Maximum 40 lines (recommended $\le 20$ lines). Surgical fixes only.
   - Text fields bounded: titles $\le 120$ chars, descriptions $\le 350$ chars, diffs $\le 40$ lines.

---

### Hunter Prompt Structure

Every hunter subagent prompt must contain:
1. **Role preamble**: Search for source-grounded test engineering defects and invariant failures in assigned units and return structured JSON.
2. **`test-topology.md`** summary for architectural context.
3. **Assigned coverage IDs, subsystem, test surface, and starting paths** from `coverage-ledger.json`.
4. **Progressive Disclosure**: The specific assigned QA class from `QA-CLASSES.md` and AT MOST ONE relevant domain companion file (`UNIT-AND-INTEGRATION-TESTING.md`, `E2E-AND-UI-TESTING.md`, `API-AND-CONTRACT-TESTING.md`, `PERFORMANCE-AND-RESILIENCE-TESTING.md`, or `TEST-DATA-AND-ENVIRONMENT.md`). Never inject multiple companion files into a single hunter.
5. **The Core Defensive QA Hunting Method** (below).
6. **The Anti-Nitpicking Rule**: Strictly ignore formatting, styling, cosmetic test naming (`should` vs `test`), and trivial syntax preferences.
7. **The Structured Hunter Result schema** with bounded diff ($\le 20$ lines recommended, max 40 lines) and trace ($\le 5$ steps) constraints.

---

### Core Defensive QA Hunting Method

```text
## Defensive QA & Test Engineering Review Method

Your goal is to identify concrete, source-grounded test engineering invariant failures that degrade
test suite reliability, mask production defects, introduce non-deterministic flakiness, or corrupt test state.

DO NOT REPORT COSMETIC ISSUES. Test method naming tastes, BDD vs TDD style arguments, and pedantic
syntax opinions are strictly prohibited. A valid finding must demonstrate a measurable test failure mode.

WORK FROM A CONCRETE TEST INVARIANT:
1. Identify the test contract or expected guarantee:
   - What real production behavior is this test supposed to guard?
   - What failure condition should cause this test to fail?
2. Trace the exact source path where the invariant breaks:
   - Read test code and tested source code using targeted line-range reads (e.g. `path:20-60`).
   - Directly verify line numbers and symbols. Never guess line numbers.
   - Trace from test_case -> fixture_or_mock -> defect_site -> impact_site.
3. Prove the concrete failure mechanism:
   - False positive pass: Why does this test pass even when the underlying production code is broken?
     (e.g., tautological assertion expect(true).toBe(true), swallowed async rejection).
   - Mock drift: How does the mock diverge from real API behavior? What breaking change does it hide?
   - Flakiness: Under what parallel execution order or timing delay will this test fail?
   - State leakage: What database record or global variable remains dirty after test teardown?
   - If the failure depends on runtime timing or concurrency that cannot be proven statically,
     mark as needs_validation, NOT confirmed.
4. Classify using the canonical flaw_mode taxonomy:
   - tautological_assertion, unhandled_async_rejection, mock_drift, over_mocking,
     timing_flakiness, shared_state_collision, dirty_state_leakage, unasserted_error_path,
     test_pyramid_inversion, unbounded_execution_timeout, domain_specific.
5. Formulate the smallest effective refactoring:
   - Provide a clean, minimal code diff (maximum 20 lines recommended, maximum 40 lines) targeting the exact test site.
   - Never rewrite entire test files.
   - Describe a concise regression test verifying the test fix itself.
```

---

### Coverage Critic Wave

Between hunter waves (in `standard` and `deep` profiles), a dedicated **Coverage Critic** subagent reviews `coverage-ledger.json` against `test-topology.md`:
- Identifies uncovered test surfaces or untested subsystems.
- Identifies test suites with 0 test assertions (smoke tests that only check HTTP 200 without payload validation).
- Adds new `planned` units to `coverage-ledger.json` for subsequent hunter waves.
