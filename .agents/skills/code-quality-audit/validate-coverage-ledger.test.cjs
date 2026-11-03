#!/usr/bin/env node

/**
 * Unit tests for validate-coverage-ledger.cjs
 */

const assert = require("node:assert");
const { validateLedger, canonicalCoverageId } = require("./validate-coverage-ledger.cjs");

const validPlannedUnit = {
  coverage_id: "quality/api_entrypoint/client_to_controller/order_service/performance_and_algorithmic",
  canonical_refs: {
    surface: "api_entrypoint",
    boundary: "client_to_controller",
    subsystem: "order_service",
    quality_class: "performance_and_algorithmic"
  },
  surface: "api_entrypoint",
  boundary: "client_to_controller",
  subsystem: "order_service",
  quality_class: "performance_and_algorithmic",
  starting_paths: ["src/controllers/order.ts", "src/services/order.ts"],
  selected_companion_blocks: ["web_and_apis", "data_and_storage"],
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
  coverage_id: "quality/data_access/domain_to_persistence/user_repository/architecture_and_coupling",
  canonical_refs: {
    surface: "data_access",
    boundary: "domain_to_persistence",
    subsystem: "user_repository",
    quality_class: "architecture_and_coupling"
  },
  surface: "data_access",
  boundary: "domain_to_persistence",
  subsystem: "user_repository",
  quality_class: "architecture_and_coupling",
  starting_paths: ["src/repositories/user.ts"],
  selected_companion_blocks: ["data_and_storage"],
  excluded_blocks: [],
  prior_status: null,
  attempts: [],
  wave: 1,
  status: "covered",
  agent_id: "hunter-arch-01",
  reviewed_paths: ["src/repositories/user.ts", "src/domain/user.ts"],
  inspected_invariants: ["Repository implements domain interface without leaky SQL driver abstractions."],
  result_fingerprints: [],
  unresolved: null
};

console.log("Running validate-coverage-ledger.cjs unit tests...");

// Test 1: Valid units
{
  const errors = [];
  validateLedger([validPlannedUnit, validCoveredUnit], errors);
  assert.strictEqual(errors.length, 0, `Expected 0 errors, got: ${errors.join("; ")}`);
  console.log("  [PASS] Valid planned and covered ledger units");
}

// Test 2: Mismatched coverage ID
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validPlannedUnit));
  invalid.coverage_id = "quality/wrong/id";
  validateLedger([invalid], errors);
  assert.ok(errors.some(e => e.includes("does not match canonical pattern")), "Should flag mismatched coverage_id");
  console.log("  [PASS] Mismatched coverage_id rejected");
}

// Test 3: Covered status without reviewed_paths or agent_id
{
  const errors = [];
  const invalid = JSON.parse(JSON.stringify(validCoveredUnit));
  invalid.agent_id = "";
  invalid.reviewed_paths = [];
  validateLedger([invalid], errors);
  assert.ok(errors.some(e => e.includes("requires a non-empty agent_id")), "Should require agent_id");
  assert.ok(errors.some(e => e.includes("requires at least one reviewed path")), "Should require reviewed_paths");
  console.log("  [PASS] Incomplete covered status rejected");
}

console.log("All validate-coverage-ledger tests passed successfully!");
