---
name: qa-audit
description: Quality assurance audit, test architecture analysis, test suite health review, flakiness elimination, assertion rigor verification, and mock fidelity auditing for codebases, test suites, CI pipelines, and automated tests. Use for reviewing test suites, debugging flaky tests, evaluating test coverage and mock drift, auditing test architecture (unit, integration, E2E), identifying tautological tests or false-positive passes, and optimizing test execution performance. Run the complete workflow only for explicit test suite audit requests, full/comprehensive QA reviews, or requested QA report artifacts. Make sure to use this skill whenever the user asks for a QA audit, test suite review, flakiness check, test architecture evaluation, or assertion quality audit, even if they simply say "audit our tests" or "review our test suite".
---

# QA Audit

Audit codebases for concrete test engineering defects: assertion unsoundness, mock drift, test flakiness, test data leakage, test architecture degradation, and test pyramid inversion. Delivers verifiable source traces, reproduction mechanisms, severity ratings, and surgical, bounded test refactoring diffs.

**Anti-Nitpicking Invariant**: Purely cosmetic nits (test naming conventions, `should` vs `test`, BDD vs TDD style arguments, assertion syntax preferences like `toBe` vs `toEqual` on primitives) are strictly prohibited. A finding requires a demonstrable failure mode: false-positive pass, unasserted failure path, mock drift masking breaking changes, race condition under parallelism, or dirty state leakage.

---

## Operating Modes

- **Guidance Mode (Default)**: Rapid chat-based review for specific test files, PR diffs, or flakiness triage. Zero disk artifacts generated; findings delivered directly in conversation.
- **Full Audit Mode**: Systematic 6-phase workflow executed when the user explicitly requests a full test suite audit or report artifacts (`REPORT.md`, `findings.json`, `QA-REMEDIATION-ROADMAP.md`).

### Interactive Clarification Protocol (`ask`)

When intent or profile requires calibration, invoke platform `ask` with QA-specific options:

```json
{
  "questions": [
    {
      "id": "audit_mode",
      "header": "Audit Mode",
      "question": "Which QA audit mode would you like to execute?",
      "recommended": 0,
      "options": [
        {
          "label": "Guidance Mode (In-chat review)",
          "description": "Targeted test review in conversation without writing disk artifacts."
        },
        {
          "label": "Full Audit Mode (Complete 6-phase audit)",
          "description": "Systematic 6 phases producing REPORT.md, FINDINGS-DETAIL.md, QA-REMEDIATION-ROADMAP.md, and findings.json."
        }
      ]
    }
  ]
}
```

```json
{
  "questions": [
    {
      "id": "audit_profile",
      "header": "Coverage Profile",
      "question": "Which coverage profile should be applied to the QA audit?",
      "recommended": 0,
      "options": [
        { "label": "standard", "description": "Full 6-phase audit across unit, integration, and E2E suites with independent validation. (Recommended)" },
        { "label": "quick", "description": "Fast pass focused on high-risk flakiness, over-mocking, and tautological assertions." },
        { "label": "deep", "description": "Multi-wave hunting with concurrency stress analysis and contract validation." },
        { "label": "scoped", "description": "Targeted to specific test directories or PR diff." }
      ]
    }
  ]
}
```

```json
{
  "questions": [
    {
      "id": "qa_focus",
      "header": "Priority QA Dimensions",
      "question": "Would you like to prioritize specific QA dimensions?",
      "multi": true,
      "options": [
        { "label": "Assertion Rigor & Soundness", "description": "Tautological tests, missing negative checks, un-awaited promises, vacuous passes." },
        { "label": "Mock Fidelity & Isolation", "description": "Mock drift masking breakages, over-mocking, leaky mock state." },
        { "label": "Flakiness & Concurrency Safety", "description": "Worker collisions, arbitrary sleep delays, race conditions under parallel execution." },
        { "label": "Boundary & Coverage Gaps", "description": "Unasserted error paths, payload boundary fuzzing, missing auth checks." },
        { "label": "Test Architecture & Performance", "description": "Inverted test pyramid, redundant suites, slow execution bottlenecks." },
        { "label": "Test Data & Environment Lifecycle", "description": "Dirty database records, missing rollback, port collisions, unisolated temp files." }
      ]
    }
  ]
}
```

---

## Token Efficiency & Precision Protocols

1. **Targeted Range Reads**: NEVER read whole test or source files. Always use line-range selectors (e.g. `path:20-60`). Read only what is required to verify the invariant.
2. **Clean-Context Subagent Isolation**: Spawn hunters and verifiers in clean subagent contexts without chat history noise or speculative bias.
3. **Progressive Disclosure**: Inject AT MOST ONE domain companion file per subagent (`UNIT-AND-INTEGRATION-TESTING.md`, `E2E-AND-UI-TESTING.md`, `API-AND-CONTRACT-TESTING.md`, `PERFORMANCE-AND-RESILIENCE-TESTING.md`, or `TEST-DATA-AND-ENVIRONMENT.md`).
4. **Structured JSON Output**: Subagents must return raw JSON conforming to `report-schema.json`. No conversational filler, preambles, or markdown chattiness.
5. **Bounded Outputs**:
   - `trace`: Maximum 5 steps (`test_case`, `fixture_or_mock`, `defect_site`, `impact_site`).
   - `evidence`: Maximum 3 decisive snippets.
   - `recommended_diff`: Maximum 40 lines (recommended $\le 20$ lines). Surgical test fixes only.
   - Text fields bounded by schema `maxLength` (titles $\le 120$, descriptions $\le 350$).

---

## 6-Phase Systematic Workflow

### Phase 1: Test Topology Reconnaissance (`RECONNAISSANCE.md`)
Map test surfaces (`unit_suite`, `integration_suite`, `e2e_suite`, `api_contract`, `performance_suite`, `ci_pipeline`), test frameworks, fixtures, and CI configs. Seed `coverage-ledger.json` with canonical IDs `qa/<surface>/<level>/<subsystem>/<qa_class>`. Validate with `validate-coverage-ledger.cjs`.

### Phase 2: Coverage-Led Hunting Waves (`HUNTING.md`, `QA-CLASSES.md`)
Spawn clean-context hunters to test assigned units against concrete QA failure modes. Prioritize assertion soundness, mock drift, and concurrency flakiness.

### Phase 3: Adversarial Validation (`VALIDATION-AND-REPORTING.md`)
Independent verifiers challenge candidates in clean contexts. Apply the 4-point refutation check to eliminate false positives. Categorize into `confirmed`, `needs_validation`, or `rejected`.

### Phase 4: Schema Validation Gate (`VALIDATION-AND-REPORTING.md`)
Synthesize findings into `findings.json`. Execute:
```bash
node <skill-dir>/validate-findings.cjs <output-dir>/findings.json
node <skill-dir>/validate-coverage-ledger.cjs <output-dir>/coverage-ledger.json
```
Both must exit 0 before proceeding.

### Phase 5: Independent Record & Diff Verification
Verify referenced line numbers against target code. Confirm test diffs are syntactically valid, minimal ($\le 40$ lines), and directly address the broken invariant.

### Phase 6: Reporting Deliverables (`VALIDATION-AND-REPORTING.md`)
- `REPORT.md`: Executive summary, test pyramid evaluation, health score, defect matrix.
- `FINDINGS-DETAIL.md`: Technical deep dive for confirmed findings with verified traces and surgical diffs.
- `QA-REMEDIATION-ROADMAP.md`: Prioritized remediation guide (P0 immediate, P1 planned, P2 hygiene).
