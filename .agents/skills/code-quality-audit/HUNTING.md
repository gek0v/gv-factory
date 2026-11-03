# Code Quality Hunting

## Phase 2: Run coverage-led hunting waves

The parent assigns `planned` ledger units to focused `general` hunter subagents. Each hunter **MUST be spawned in a fresh subagent with a clean context**—devoid of prior chat history, parent deliberations, or speculative bias.

One hunter may own closely related units within the same subsystem or boundary, but unrelated subsystems should be examined independently to maintain focus.

When a budget or profile limits hunter invocations, assign units by priority:
1. **Concurrency, async safety, and resource lifecycles** (potential deadlocks, race conditions, memory/handle leaks).
2. **Performance cliffs and algorithmic complexity** (N+1 queries, hot-loop allocations, unindexed operations).
3. **Resilience and error boundaries** (swallowed errors, panic points, missing fallbacks/timeouts).
4. **Architectural integrity and coupling** (cyclic dependencies, layering violations, leaky domain models).
5. **Maintainability and cognitive complexity** (complex monoliths, untestable side effects).

### Hunter Prompt Structure

Every hunter subagent prompt must contain:
1. **Role preamble**: Search for source-grounded engineering invariant failures in assigned units and return structured JSON.
2. **`architecture.md`** summary for context.
3. **Assigned coverage IDs, subsystem, boundary, and starting paths** from `coverage-ledger.json`.
4. **Progressive Disclosure**: The specific assigned quality class from `QUALITY-CLASSES.md` and AT MOST ONE relevant domain companion file (`WEB-AND-APIS.md`, `DATA-AND-STORAGE.md`, etc.). Never inject multiple companion files into a single hunter.
5. **The Core Quality Hunting Method** (below).
6. **The Anti-Nitpicking Rule**: Strictly ignore formatting, styling, cosmetic naming, and trivial linter warnings.
7. **The Structured Hunter Result schema** with bounded diff and trace constraints.

---

### Core Quality Hunting Method

```text
## Defensive Code Quality & Architecture Review Method

Your goal is to identify concrete, source-grounded engineering invariant failures that degrade
system correctness, performance, maintainability, concurrency, or resilience.

DO NOT REPORT COSMETIC ISSUES. Formatting, variable naming tastes, tabs vs spaces, and pedantic
linter opinions are strictly prohibited. A valid finding must demonstrate a measurable engineering
defect.

WORK FROM A CONCRETE INVARIANT:
1. Identify the architectural contract, data invariant, or concurrency expectation:
   - What does the interface or caller assume?
   - What guarantees should this component provide regarding state, memory, or execution?
2. Trace the exact source path where the invariant breaks:
   - Read code using targeted line-range reads (e.g. `path:20-60`) rather than reading whole files.
   - Directly verify line numbers and symbols. Never guess or hallucinate line numbers.
   - Ensure the defect site is reachable from an active entrypoint or public API.
3. Prove the concrete degradation mechanism:
   - Concurrency: under what interleaved execution does a race, lost update, or deadlock occur?
   - Performance: what is the algorithmic complexity curve? Under what input size does it degrade?
   - Resources: where is the unreleased handle, unclosed stream, or retained reference preventing garbage collection?
   - Architecture: what cyclic dependency or coupling prevents independent testing or modular refactoring?
   - Resilience: what unhandled exception causes silent data loss or service degradation?
   - If degradation depends on unmeasured production assumptions (e.g. table size > 100k, packet loss),
     mark as `needs_validation`, NOT confirmed.
4. Formulate the smallest effective refactoring:
   - Provide a clean, minimal code diff (maximum 25 lines) targeting the exact invariant site.
   - Never paste entire refactored files or boilerplate classes.
   - Describe a concise regression test verifying the fix.
```

---

### Structured Hunter Result Format

The hunter subagent must return its result as a single JSON object:

```json
{
  "reviewed_paths": [
    "src/services/billingService.ts",
    "src/repositories/invoiceRepository.ts"
  ],
  "inspected_invariants": [
    "Verified that invoice generation runs within an atomic database transaction.",
    "Checked concurrent retry handling during payment gateway webhook callbacks."
  ],
  "candidate_findings": [
    {
      "fingerprint": "QC-CONCUR-01-invoice-double-charge-race",
      "title": "Non-Atomic Status Check in Payment Webhook Allows Double Capture",
      "description": "Invoice status is queried in one step and updated in a later async step without row-level locking or optimistic concurrency control.",
      "quality_class": "concurrency_and_resources",
      "subsystem": "billing",
      "root_cause": "Time-of-check to time-of-use (TOCTOU) gap between select and update under concurrent webhook deliveries.",
      "intended_design": "Use SELECT ... FOR UPDATE or an atomic conditional UPDATE invoice SET status = 'paid' WHERE id = ? AND status = 'pending'.",
      "trace": [
        {
          "kind": "entrypoint",
          "file": "src/controllers/webhookController.ts",
          "line": 34,
          "symbol": "handleStripeWebhook",
          "explanation": "Receives payment_intent.succeeded event."
        },
        {
          "kind": "defect_site",
          "file": "src/services/billingService.ts",
          "line": 89,
          "symbol": "captureInvoice",
          "explanation": "Reads invoice without lock, awaits external receipt generation, then updates status."
        }
      ],
      "evidence": [
        {
          "file": "src/services/billingService.ts",
          "line": 89,
          "description": "const invoice = await db.invoices.findUnique(...); // no lock\nawait mailer.send(...);\nawait db.invoices.update(...);"
        }
      ],
      "conditions": [
        {
          "kind": "high_concurrency",
          "description": "Two webhook retries arriving within 150ms of each other."
        }
      ],
      "degradation_demonstration": {
        "failure_mode": "race_condition",
        "mechanism": "Interleaved execution allows both workers to observe status='pending' and trigger double capture.",
        "impact_summary": "Customers may be charged twice and duplicate fulfillment events are dispatched."
      },
      "remediation": {
        "summary": "Use conditional atomic update checking current status before dispatching fulfillment.",
        "recommended_diff": "- await db.invoices.update({ where: { id }, data: { status: 'paid' } });\n+ const updated = await db.invoices.updateMany({ where: { id, status: 'pending' }, data: { status: 'paid' } });\n+ if (updated.count === 0) return; // already processed",
        "regression_test_plan": "Simulate two concurrent invocations with Promise.all and assert fulfillment fires exactly once."
      },
      "severity": "critical",
      "confidence": "certain"
    }
  ],
  "blocked_units": [],
  "uncovered_leads": []
}
```

---

## Coverage Critics Between Waves

Between hunting waves (or at the end of Wave 1 in `quick` mode), the parent or a delegated critic agent examines `coverage-ledger.json` and hunter outputs:
1. Are there unreviewed subsystems or missing boundary handoffs?
2. Did any hunter uncover new boundaries that lack planned ledger units?
3. Were any high-risk concurrency or data layers skipped?

If gaps are identified, the critic creates new `planned` units for subsequent waves.
