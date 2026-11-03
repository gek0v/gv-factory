#!/usr/bin/env node

/**
 * Unit tests for validate-findings.cjs
 */

const assert = require("node:assert");
const path = require("node:path");
const fs = require("node:fs");
const { validateSchema, validateQualityFindingInvariants } = require("./validate-findings.cjs");

const schema = JSON.parse(fs.readFileSync(path.join(__dirname, "report-schema.json"), "utf8"));

const validConfirmedFinding = {
  verdict: "confirmed",
  fingerprint: "QC-PERF-01-user-order-nplusone",
  title: "N+1 Query Waterfall in Order History Fetching",
  description: "getUserOrderHistory executes one SQL query per order to resolve order items instead of batching or joining.",
  quality_class: "performance_and_algorithmic",
  subsystem: "order_service",
  root_cause: "Direct iteration over un-eager-loaded relation in a synchronous loop.",
  intended_design: "Batch load all items in a single IN-query or JOIN prior to mapping.",
  trace: [
    {
      kind: "entrypoint",
      file: "src/services/order.ts",
      line: 45,
      symbol: "getUserOrderHistory",
      explanation: "API route handler calls order retrieval method."
    },
    {
      kind: "defect_site",
      file: "src/services/order.ts",
      line: 62,
      symbol: "fetchItemsForOrder",
      explanation: "Iterates over orders invoking individual DB queries in loop."
    }
  ],
  evidence: [
    {
      file: "src/services/order.ts",
      line: 62,
      description: "await db.query('SELECT * FROM items WHERE order_id = ?', [order.id]) called within orders.map()"
    }
  ],
  conditions: [
    {
      kind: "large_dataset",
      description: "Users with > 50 orders trigger > 50 sequential DB round trips."
    }
  ],
  degradation_demonstration: {
    failure_mode: "algorithmic_complexity_cliff",
    mechanism: "Latency grows linearly O(N) with orders count; DB connection pool quickly saturates.",
    impact_summary: "Causes 1500ms+ response times on warm profiles and exhausts connection pool under 20 concurrent requests."
  },
  remediation: {
    summary: "Replace loop query with WHERE order_id IN (...) single query batch lookup.",
    recommended_diff: "- for (const o of orders) { o.items = await db.query(...); }\n+ const items = await db.query('SELECT * FROM items WHERE order_id IN (?)', [orders.map(o => o.id)]);",
    regression_test_plan: "Unit test with 10 mock orders asserting db.query is invoked exactly once."
  },
  severity: "high",
  confidence: "certain"
};

const validNeedsValidationFinding = {
  verdict: "needs_validation",
  fingerprint: "QC-CONCUR-02-session-cache-race",
  title: "Potential Race Condition in In-Memory Session Cache Invalidation",
  description: "SessionCache lacks mutex locking during concurrent write and invalidation operations.",
  quality_class: "concurrency_and_resources",
  subsystem: "session_manager",
  claimed_root_cause: "Map mutation during async tick without thread-safe guard.",
  trace: [
    {
      kind: "defect_site",
      file: "src/cache/session.ts",
      line: 88,
      symbol: "invalidateSession",
      explanation: "Deletes key without checking pending write lock."
    }
  ],
  evidence: [
    {
      file: "src/cache/session.ts",
      line: 88,
      description: "this.sessions.delete(id) occurs concurrently with this.sessions.set(id, data)"
    }
  ],
  blockers: [
    "Requires multi-threaded cluster benchmarking or stress testing under 1,000 rps to observe stale session read."
  ],
  validation_plan: "Execute concurrent stress test using autocannon with 50 parallel workers alternating reads and deletes."
};

const validRejectedFinding = {
  verdict: "rejected",
  fingerprint: "QC-ARCH-03-controller-db-access",
  title: "Direct Database Import in Auth Controller",
  description: "AuthController imports prisma instance directly rather than injecting AuthService.",
  quality_class: "architecture_and_coupling",
  subsystem: "auth",
  claimed_root_cause: "Bypassing domain layer separation.",
  trace: [
    {
      kind: "defect_site",
      file: "src/controllers/auth.ts",
      line: 12,
      symbol: "AuthController",
      explanation: "Imports prisma singleton."
    }
  ],
  evidence: [
    {
      file: "src/controllers/auth.ts",
      line: 12,
      description: "import { prisma } from '../db'"
    }
  ],
  rejection_reason: "intentional_tradeoff_with_demonstrated_benefit",
  refutation: "Repository architecture is intentionally vertical slice (Feature-Based Architecture) where controller and query live in the same slice to optimize developer velocity; not an accidental coupling bug."
};

console.log("Running validate-findings.cjs unit tests...");

// Test 1: Valid array of all 3 verdicts
{
  const errors = [];
  const findings = [validConfirmedFinding, validNeedsValidationFinding, validRejectedFinding];
  validateSchema(schema, findings, "$", errors);
  validateQualityFindingInvariants(findings, errors);
  assert.strictEqual(errors.length, 0, `Expected 0 errors, got: ${errors.join("; ")}`);
  console.log("  [PASS] Valid findings with confirmed, needs_validation, and rejected");
}

// Test 2: Reject absolute paths in trace
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  invalid.trace[0].file = "/etc/passwd";
  validateSchema(schema, [invalid], "$", errors);
  validateQualityFindingInvariants([invalid], errors);
  assert.ok(errors.some(e => e.includes("must be repository-relative")), "Should reject absolute path");
  console.log("  [PASS] Absolute file paths rejected");
}

// Test 3: Reject duplicate fingerprints
{
  const errors = [];
  const f1 = JSON.parse(JSON.stringify(validConfirmedFinding));
  const f2 = JSON.parse(JSON.stringify(validNeedsValidationFinding));
  f2.fingerprint = f1.fingerprint;
  validateSchema(schema, [f1, f2], "$", errors);
  validateQualityFindingInvariants([f1, f2], errors);
  assert.ok(errors.some(e => e.includes("duplicate fingerprint")), "Should reject duplicate fingerprint");
  console.log("  [PASS] Duplicate fingerprints rejected");
}

// Test 4: Reject missing required remediation in confirmed finding
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  delete invalid.remediation;
  validateSchema(schema, [invalid], "$", errors);
  assert.ok(errors.some(e => e.includes('missing required property "remediation"')), "Should fail missing remediation");
  console.log("  [PASS] Missing remediation rejected for confirmed finding");
}

// Test 5: Reject oversized diff (> 60 lines)
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  invalid.remediation.recommended_diff = Array(70).fill("+ line").join("\n");
  validateSchema(schema, [invalid], "$", errors);
  validateQualityFindingInvariants([invalid], errors);
  assert.ok(errors.some(e => e.includes("diff exceeds 60 lines")), "Should reject diff > 60 lines");
  console.log("  [PASS] Oversized diff (>60 lines) rejected");
}

// Test 6: Reject oversized trace (> 5 steps)
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  invalid.trace = Array(6).fill(invalid.trace[0]);
  validateSchema(schema, [invalid], "$", errors);
  validateQualityFindingInvariants([invalid], errors);
  assert.ok(errors.some(e => e.includes("exceeds 5 steps")), "Should reject trace > 5 steps");
  console.log("  [PASS] Oversized trace (>5 steps) rejected");
}

console.log("All validate-findings tests passed successfully!");
