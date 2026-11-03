#!/usr/bin/env node

/**
 * Unit tests for validate-coverage-ledger.cjs in qa-audit.
 */

const assert = require("node:assert");
const { validateLedger, canonicalCoverageId } = require("./validate-coverage-ledger.cjs");

const validPlannedUnit = {
  coverage_id: "qa/unit_suite/unit/billing_service/mock_fidelity_and_isolation",
  canonical_refs: {
    surface: "unit_suite",
    level: "unit",
    subsystem: "billing_service",
    qa_class: "mock_fidelity_and_isolation"
  },
  surface: "unit_suite",
  level: "unit",
  subsystem: "billing_service",
  qa_class: "mock_fidelity_and_isolation",
  starting_paths: ["test/unit/billing/webhook.test.ts"],
  selected_companion_blocks: ["unit_and_integration"],
  excluded_blocks: [],
  prior_status: null,
  attempts: [],
  wave: 1,
  status: "planned",
  agent_id: null,
  reviewed_paths: [],
  inspected_invariants: [],
  result_fingerprints: [],
  unresolved: null
};

const validCoveredUnit = {
  coverage_id: "qa/integration_suite/integration/auth_service/assertion_and_soundness",
  canonical_refs: {
    surface: "integration_suite",
    level: "integration",
    subsystem: "auth_service",
    qa_class: "assertion_and_soundness"
  },
  surface: "integration_suite",
  level: "integration",
  subsystem: "auth_service",
  qa_class: "assertion_and_soundness",
  starting_paths: ["test/integration/auth.test.ts"],
  selected_companion_blocks: ["api_and_contract"],
  excluded_blocks: [],
  prior_status: null,
  attempts: [],
  wave: 1,
  status: "covered",
  agent_id: "hunter-qa-01",
  reviewed_paths: ["test/integration/auth.test.ts", "src/auth/jwt.ts"],
  inspected_invariants: ["Expired JWT token assertions check HTTP 401 status and error code header."],
  result_fingerprints: [],
  unresolved: null
};

console.log("Running validate-coverage-ledger.cjs unit tests for qa-audit...");

// Test 1: Valid planned and covered units
{
  const errors = [];
  validateLedger([validPlannedUnit, validCoveredUnit], errors);
  assert.strictEqual(errors.length, 0, `Expected 0 errors, got: ${errors.join("; ")}`);
  console.log("  [PASS] Test 1: Valid planned and covered units");
}

// Test 2: Mismatched canonical ID rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validPlannedUnit));
  invalid.coverage_id = "qa/wrong_surface/unit/billing_service/mock_fidelity_and_isolation";
  validateLedger([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("does not match canonical pattern")),
    `Expected canonical pattern error, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 2: Mismatched canonical ID rejected");
}

// Test 3: Incomplete covered status missing reviewed paths rejected
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validCoveredUnit));
  invalid.reviewed_paths = [];
  validateLedger([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("requires at least one reviewed path")),
    `Expected reviewed_paths error for covered status, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 3: Incomplete covered status missing reviewed paths rejected");
}

// Test 4: Exceeding 1 companion block rejected (Progressive Disclosure)
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validPlannedUnit));
  invalid.selected_companion_blocks = ["unit_and_integration", "test_data_and_environment"];
  validateLedger([invalid], errors);
  assert.ok(
    errors.some(e => e.includes("exceeds 1 companion block")),
    `Expected progressive disclosure error, got: ${errors.join("; ")}`
  );
  console.log("  [PASS] Test 4: Exceeding 1 companion block rejected (Progressive Disclosure)");
}

console.log("All 4 validate-coverage-ledger.cjs unit tests passed successfully!");
