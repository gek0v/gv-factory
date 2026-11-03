#!/usr/bin/env node

/**
 * Unit tests for validate-findings.cjs in qa-audit.
 */

const assert = require("node:assert");
const path = require("node:path");
const fs = require("node:fs");
const { validateSchema, validateQAFindingInvariants } = require("./validate-findings.cjs");

const schema = JSON.parse(fs.readFileSync(path.join(__dirname, "report-schema.json"), "utf8"));

const validConfirmedFinding = {
  verdict: "confirmed",
  fingerprint: "QA-MOCK-01-billing-overmock",
  title: "Mock Drift in Stripe Webhook Signature Verification Test",
  description: "Test mocks stripe.webhooks.constructEvent to unconditionally return a hardcoded payload, masking changes in signature verification format.",
  qa_class: "mock_fidelity_and_isolation",
  subsystem: "billing_service",
  root_cause: "Stub returns static dummy object without asserting crypto signature headers or timestamp tolerance.",
  intended_behavior: "Test should verify that invalid signatures throw SignatureVerificationError using real test signing secrets.",
  trace: [
    {
      kind: "test_case",
      file: "test/unit/billing/webhook.test.ts",
      line: 34,
      symbol: "testWebhookSuccess",
      explanation: "Test injects mocked constructEvent returning valid payload for any header."
    },
    {
      kind: "fixture_or_mock",
      file: "test/fixtures/stripe-mock.ts",
      line: 12,
      symbol: "mockConstructEvent",
      explanation: "Stub bypasses real HMAC SHA-256 header computation."
    },
    {
      kind: "defect_site",
      file: "src/billing/webhook.ts",
      line: 45,
      symbol: "handleWebhook",
      explanation: "Controller relies on constructEvent signature contract which is completely unexercised in tests."
    }
  ],
  evidence: [
    {
      file: "test/unit/billing/webhook.test.ts",
      line: 36,
      description: "jest.spyOn(stripe.webhooks, 'constructEvent').mockReturnValue({ id: 'evt_123', type: 'invoice.paid' })"
    }
  ],
  test_failure_demonstration: {
    flaw_mode: "mock_drift",
    mechanism: "When Stripe SDK updated payload header requirements, test suite continued to pass while production webhook failed with 400 Bad Request.",
    impact_summary: "False positive test suite masking catastrophic payment processing outage in production."
  },
  remediation: {
    summary: "Use real Stripe test utility to sign webhook payloads with test secret instead of replacing constructEvent.",
    recommended_diff: "- jest.spyOn(stripe.webhooks, 'constructEvent').mockReturnValue(mockPayload);\n+ const signature = stripe.webhooks.generateTestHeaderString({ payload: JSON.stringify(mockPayload), secret: TEST_WEBHOOK_SECRET });",
    regression_test_plan: "Assert that tampering with the signature header throws SignatureVerificationError in integration test."
  },
  severity: "high",
  confidence: "certain"
};

const validNeedsValidationFinding = {
  verdict: "needs_validation",
  fingerprint: "QA-FLAKE-02-concurrent-db-isolation",
  title: "Potential Flakiness under Parallel Test Runners via Shared Postgres DB",
  description: "Integration tests use static tenant ID 'tenant_test_1' without transaction rollback or schema-per-worker isolation.",
  qa_class: "flakiness_and_concurrency",
  subsystem: "tenant_manager",
  claimed_root_cause: "Shared database state mutated by concurrent Jest worker threads.",
  trace: [
    {
      kind: "test_case",
      file: "test/integration/tenant.test.ts",
      line: 22,
      symbol: "testCreateTenant",
      explanation: "Test creates record with hardcoded ID 'tenant_test_1' without isolating DB per worker."
    }
  ],
  evidence: [
    {
      file: "test/integration/tenant.test.ts",
      line: 25,
      description: "await db.insert(tenants).values({ id: 'tenant_test_1', name: 'Acme' })"
    }
  ],
  blockers: [
    "Requires executing test runner with --maxWorkers=4 across 50 iterations to measure flakiness rate."
  ],
  validation_plan: "Run 'jest --maxWorkers=4 --runInBand=false --repeat=50' to observe unique constraint collision frequency.",
  provisional_severity: "medium"
};

const validRejectedFinding = {
  verdict: "rejected",
  fingerprint: "QA-STYLE-03-test-name-convention",
  title: "Test Descriptions Use 'should' Instead of 'given-when-then'",
  description: "Unit tests in auth.test.ts use describe('when logged in') / it('should redirect') style.",
  qa_class: "assertion_and_soundness",
  subsystem: "auth_service",
  claimed_defect: "Test naming convention does not follow BDD standard.",
  rejection_reason: "cosmetic_naming_or_style",
  refutation: "Naming convention is cosmetic. Test assertions are sound and deterministically verify authentication redirects."
};

console.log("Running validate-findings.cjs unit tests for qa-audit...");

// Test 1: Valid multi-verdict payload
{
  const errors = [];
  const findings = [validConfirmedFinding, validNeedsValidationFinding, validRejectedFinding];
  validateSchema(schema, findings, "root", errors);
  validateQAFindingInvariants(findings, errors);
  assert.strictEqual(errors.length, 0, `Expected 0 errors, got: ${errors.join("; ")}`);
  console.log("  [PASS] Test 1: Valid multi-verdict payload (confirmed, needs_validation, rejected)");
}

// Test 2: Absolute file paths rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  invalid.trace[0].file = "C:/abs/path/test.ts";
  validateSchema(schema, [invalid], "root", errors);
  validateQAFindingInvariants([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("must be repository-relative")),
    `Expected repository-relative error for absolute path, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 2: Absolute file paths rejected");
}

// Test 3: Duplicate fingerprints rejected
{
  const errors = [];
  const dup1 = JSON.parse(JSON.stringify(validConfirmedFinding));
  const dup2 = JSON.parse(JSON.stringify(validConfirmedFinding));
  validateSchema(schema, [dup1, dup2], "root", errors);
  validateQAFindingInvariants([dup1, dup2], errors);
  assert.ok(
    errors.some(e => e.includes("duplicate fingerprint")),
    `Expected duplicate fingerprint error, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 3: Duplicate fingerprints rejected");
}

// Test 4: Missing remediation in confirmed finding rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  delete invalid.remediation;
  validateSchema(schema, [invalid], "root", errors);
  validateQAFindingInvariants([invalid], errors);
  assert.ok(
    errors.length > 0,
    "Expected error for missing remediation in confirmed finding"
  );
  console.log("  [PASS] Test 4: Missing remediation in confirmed finding rejected");
}

// Test 5: Oversized diff (> 40 lines) rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  const longDiff = Array.from({ length: 45 }, (_, i) => `+ line ${i}`).join("\n");
  invalid.remediation.recommended_diff = longDiff;
  validateSchema(schema, [invalid], "root", errors);
  validateQAFindingInvariants([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("diff exceeds 40 lines")),
    `Expected diff exceeds 40 lines error, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 5: Oversized diff (> 40 lines) rejected");
}

// Test 6: Oversized trace (> 5 steps) rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  invalid.trace = Array.from({ length: 6 }, (_, i) => ({
    kind: "defect_site",
    file: `test/file${i}.ts`,
    line: 10 + i,
    explanation: `Step ${i}`
  }));
  validateSchema(schema, [invalid], "root", errors);
  validateQAFindingInvariants([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("exceeds 5 steps")),
    `Expected trace exceeds 5 steps error, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 6: Oversized trace (> 5 steps) rejected");
}

// Test 7: String exceeding maxLength rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validConfirmedFinding));
  invalid.title = "A".repeat(150); // maxLength is 120
  validateSchema(schema, [invalid], "root", errors);
  validateQAFindingInvariants([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("> maxLength 120")),
    `Expected maxLength error for oversized title, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 7: String exceeding maxLength rejected");
}

console.log("All 7 validate-findings.cjs unit tests passed successfully!");
