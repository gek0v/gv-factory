# Candidate Validation, Structured Output & Reporting

## Phase 3: Adversarial Candidate Validation

Every candidate finding returned by hunters in Phase 2 must be independently validated before it can be considered a confirmed defect.

### Independence & Clean-Context Rule
**The agent that validates a candidate defect is NEVER the agent that discovered it.** The parent spawns a fresh `general` or `research` subagent acting as an adversarial verifier **with a completely clean context window**.

The verifier receives ONLY:
1. The claimed invariant failure, root cause, and source trace from the candidate finding.
2. The target repository files.
3. The verification objectives below.

The verifier MUST NOT receive the hunter's scratchpad notes, subjective musings, or chat history.

### Adversarial Verifier Objectives
The verifier's role is to challenge and attempt to disprove the candidate:

1. **Check for Mitigating Context**:
   - Is the alleged race condition already prevented by a higher-level mutex, queue serializer, or framework transaction?
   - Is the alleged N+1 query actually intercepted and batched by a DataLoader or local memory cache?
   - Is the alleged memory leak actually bounded by an LRU eviction policy or garbage-collected upon request completion?
2. **Apply the Anti-Nitpicking Filter**:
   - Is this finding merely a coding style preference, cosmetic naming dispute, or pedantic linter opinion?
   - If there is no provable defect, race, leak, performance cliff, or unmaintainable architectural coupling, **the finding MUST be rejected**.
3. **Determine the Verdict**:
   - **`confirmed`**: The defect is conclusively demonstrated in source. The verifier refines the trace, confirms the failure mechanism, and verifies the minimal refactoring diff and regression test plan.
   - **`needs_validation`**: The defect is plausible and grounded in code, but whether it manifests depends on runtime load, production data volume, or missing infrastructure configuration. The verifier defines a clear, owner-executable `validation_plan`.
   - **`rejected`**: The candidate is disproved, mitigated, or cosmetic. The verifier records the `rejection_reason` and an explanation in `refutation`.

---

## Phase 4: Structured Output & Schema Validation

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

Both commands must exit with code `0`. If any schema error, duplicate fingerprint, absolute path, or missing field is found, it must be corrected immediately.

---

## Phase 5: Independent Record Verification

For large or high-stakes codebases (`standard` and `deep` profiles), a fresh independent verifier performs a final sanity pass on all `confirmed` records:
1. Verify that every referenced line number matches current source files.
2. Verify that the proposed refactoring diff does not introduce syntax errors or broken imports.
3. Verify that severity calibrations match demonstrated impact.
4. Verify that the refactoring diff is strictly surgical and bounded ($\le 25$ lines recommended, maximum 60 lines) rather than an unnecessary whole-file rewrite.

---

## Phase 6: Target-Neutral Reporting

The parent generates three primary Markdown reports in `<output-dir>/`:

### 1. `REPORT.md` (Executive Summary)
- **Executive Summary**: Overview of codebase quality, architectural health, and key hotspots.
- **Coverage Summary**: Table of reviewed subsystems, boundaries, and quality classes derived from `coverage-ledger.json`.
- **Findings by Severity & Quality Class**: Summary table counting `critical`, `high`, `medium`, `low`, and `informational` defects.
- **Strategic Recommendations**: Top 3 architectural investments with the highest return on engineering velocity and system reliability.

### 2. `FINDINGS-DETAIL.md` (Technical Deep Dive)
For every `confirmed` finding:
- **Title, Severity, Quality Class, Subsystem, Fingerprint**
- **Description & Broken Invariant**
- **Root Cause & Intended Design**
- **Source Trace Table**: `Kind`, `File`, `Line`, `Symbol`, `Explanation`
- **Degradation Demonstration**: Failure mode, mechanism, impact summary
- **Remediation & Minimal Diff**: Code diff and regression test plan

### 3. `NEEDS-REFACTOR.md` (Prioritized Roadmap)
- Actionable, phased refactoring guide grouped into:
  - **Immediate Actions (P0 / Critical)**: Fixes for data corruption, race conditions, and catastrophic bottlenecks.
  - **Planned Improvements (P1 / High)**: Modularization, decoupling, query batching.
  - **Iterative Hygiene (P2 / Medium-Low)**: Complexity reduction and error handling consistency.
- Step-by-step instructions for engineers implementing the fixes safely.

### 4. `NEEDS-VALIDATION.md` (If applicable)
Lists all `needs_validation` candidates with their exact blockers and executable profiling/benchmark plans (e.g. load testing scripts, profiling commands).

---

## Token-Efficient Synthesis Invariant

To prevent catastrophic token inflation and context exhaustion during reporting:
1. **`REPORT.md` is an Executive Brief**: Keep `REPORT.md` bounded to concise summary tables and top strategic recommendations ($\le 80$ lines).
2. **No Redundant Chat Echoing**: The parent agent MUST NOT copy-paste the entire contents of `REPORT.md`, `FINDINGS-DETAIL.md`, or `findings.json` into its final conversational output. The conversational reply must be an executive summary with finding counts, key risks, and file paths to the generated reports.
3. **Bounded Diffs in Detail Reports**: Every diff in `FINDINGS-DETAIL.md` must be a surgical patch ($\le 25$ lines), never entire rewritten files.
