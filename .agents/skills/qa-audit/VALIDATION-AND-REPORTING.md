# Candidate Validation, Structured Output & Reporting

## Phase 3: Adversarial Candidate Validation

Every candidate finding returned by hunters in Phase 2 must be independently validated before it can be considered a confirmed QA defect.

### Independence & Clean-Context Rule
**The agent that validates a candidate defect is NEVER the agent that discovered it.** The parent spawns a fresh `general` or `research` subagent acting as an adversarial verifier **with a completely clean context window**.

The verifier receives ONLY:
1. The claimed defect, failure mode, root cause, and source trace from the candidate finding.
2. The target repository test and production files.
3. The 4-Point Adversarial Refutation Protocol below.

The verifier MUST NOT receive the hunter's scratchpad notes, subjective musings, or chat history.

---

### The 4-Point Adversarial Refutation Protocol

The verifier executes 4 systematic checks to aggressively eliminate false positives:

1. **Mitigation Check**:
   Is the alleged unasserted error or missing test case already covered elsewhere in the test suite (e.g. an upstream helper, a global error filter test, or a companion integration suite)?
2. **Framework Auto-Waiting & Contract Check**:
   Does the test runner or framework already guarantee the invariant automatically (e.g. Playwright auto-waiting on element locators, Jest/Vitest unhandled rejection fail-fast)?
3. **Intentional Isolation Check**:
   Is the mock deliberately isolated because this unit test is scoped strictly to domain logic, while the integrated behavior is validated in an integration or contract test?
4. **Deterministic Reproduction Check**:
   Does the alleged race condition or flakiness have a provable, deterministic interleaving, or is it merely speculative? If speculative, downgrade to `needs_validation` or reject.

### Determining the Verdict:
- **`confirmed`**: The defect passes all 4 checks and is conclusively proven in source. The verifier refines the trace ($\le 5$ steps), confirms the flaw mode, and verifies the minimal refactoring diff ($\le 20$ lines recommended, max 40 lines).
- **`needs_validation`**: The defect is plausible and grounded in code, but manifestation requires execution under load, CI runner timing, or external sandbox credentials. Requires non-empty `blockers` and a clear `validation_plan`.
- **`rejected`**: The candidate fails any of the 4 checks, is disproved, or is cosmetic. Requires `rejection_reason` and an explanation in `refutation`.

---

## Phase 4: Structured Output & Schema Validation Gate

The parent synthesizes all verified results into `findings.json` in `<output-dir>/`.

### Validation Gate
The parent executes:
```bash
node <skill-dir>/validate-findings.cjs <output-dir>/findings.json <skill-dir>/report-schema.json
```
and re-checks the ledger:
```bash
node <skill-dir>/validate-coverage-ledger.cjs <output-dir>/coverage-ledger.json
```

Both commands must exit with code `0`. If any schema error, duplicate fingerprint, absolute path, oversized diff ($> 40$ lines), oversized trace ($> 5$ steps), or string length violation is found, it must be corrected immediately.

---

## Phase 5: Independent Record & Diff Verification

For standard and deep audit profiles, a fresh independent verifier performs a final sanity pass on all `confirmed` records:
1. **Line Number Verification**: Run targeted `read` on every referenced line to guarantee line numbers match actual source code.
2. **Surgical Patch Verification**: Verify that the proposed test refactoring diff does not exceed 40 lines, is syntactically valid, and does not delete unrelated test coverage.
3. **Severity Calibration**: Ensure critical ratings are reserved for false-positive passes and payment/auth mock drift.

---

## Phase 6: Reporting Deliverables

The parent generates three primary Markdown reports in `<output-dir>/` designed for maximum signal-to-noise ratio:

### 1. `REPORT.md` (Executive Summary)
- **Executive Summary**: Overview of test suite health and key risk areas.
- **Test Suite Health Score**: Composite score (0–100) assessing assertion soundness, mock fidelity, and flakiness rate.
- **Test Pyramid Balance**: Assessment of unit vs integration vs E2E distribution.
- **Defect Matrix Table**: Counts of confirmed defects grouped by Severity and QA Class.
- **Top 3 Strategic Investments**: Highest-leverage improvements to test suite reliability and CI velocity.

### 2. `FINDINGS-DETAIL.md` (Technical Deep Dive)
For every `confirmed` finding:
- **Title, Severity, QA Class, Subsystem, Fingerprint**
- **Description & Broken Test Invariant**
- **Root Cause & Intended Behavior**
- **Source Trace Table**: `Kind`, `File`, `Line`, `Symbol`, `Explanation`
- **Test Failure Demonstration**: Flaw mode, mechanism, impact summary
- **Surgical Remediation Patch**: Bounded code diff ($\le 20$ lines recommended, max 40 lines) and regression test plan

### 3. `QA-REMEDIATION-ROADMAP.md` (Prioritized Roadmap)
Actionable, phased roadmap:
- **Immediate Actions (P0 / Critical)**: Fix false-positive passes, unasserted error paths in payment/auth, and CI-breaking flakiness.
- **Planned Improvements (P1 / High)**: Replace arbitrary sleeps with event-driven synchronization, eliminate mock drift, isolate database state.
- **Iterative Hygiene (P2 / Medium-Low)**: Rebalance test pyramid, eliminate redundant tests, and optimize unit execution speeds.
