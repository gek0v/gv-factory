#!/usr/bin/env node

/**
 * Validates coverage-ledger.json and its canonical coverage IDs for qa-audit.
 * Usage: node validate-coverage-ledger.cjs <path-to-coverage-ledger.json>
 */

const fs = require("node:fs");
const path = require("node:path");
const { TextDecoder } = require("node:util");

const LIMITS = Object.freeze({
  inputBytes: 5 * 1024 * 1024,
  units: 10000,
  collectionItems: 1000,
  objectFields: 1000,
  nestingDepth: 64,
  validationErrors: 100,
});

const MAX_INPUT_BYTES = LIMITS.inputBytes;
const MAX_UNITS = LIMITS.units;

const REQUIRED_FIELDS = [
  "coverage_id",
  "canonical_refs",
  "surface",
  "level",
  "subsystem",
  "qa_class",
  "starting_paths",
  "selected_companion_blocks",
  "excluded_blocks",
  "prior_status",
  "attempts",
  "wave",
  "status",
  "agent_id",
  "reviewed_paths",
  "inspected_invariants",
  "result_fingerprints",
  "unresolved",
];

const REF_FIELDS = ["surface", "level", "subsystem", "qa_class"];

const STATUSES = new Set([
  "planned",
  "not_applicable",
  "out_of_scope",
  "in_progress",
  "covered",
  "candidate",
  "blocked",
  "deferred",
]);

const TEST_SURFACES = new Set([
  "unit_suite",
  "integration_suite",
  "e2e_suite",
  "api_contract",
  "performance_suite",
  "ci_pipeline",
]);

const TEST_LEVELS = new Set([
  "unit",
  "integration",
  "system_e2e",
  "smoke",
  "acceptance",
]);

const QA_CLASSES = new Set([
  "assertion_and_soundness",
  "mock_fidelity_and_isolation",
  "flakiness_and_concurrency",
  "boundary_and_coverage_gaps",
  "test_architecture_and_performance",
  "test_data_and_lifecycle",
  "domain_specific",
]);

const COMPANION_BLOCKS = new Set([
  "unit_and_integration",
  "e2e_and_ui",
  "api_and_contract",
  "performance_and_resilience",
  "test_data_and_environment",
]);

const UTF8_DECODER = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });

function readFileSafely(filePath) {
  const resolved = path.resolve(filePath);
  let lstat;
  try {
    lstat = fs.lstatSync(resolved);
  } catch (err) {
    throw new Error(`cannot access file: ${err.message}`);
  }
  if (lstat.isSymbolicLink()) {
    throw new Error("input must not be a symlink");
  }
  if (!lstat.isFile()) {
    throw new Error("input must be a regular file");
  }
  if (lstat.size > MAX_INPUT_BYTES) {
    throw new Error(`input exceeds ${MAX_INPUT_BYTES} byte limit`);
  }

  const rawBuffer = fs.readFileSync(resolved);
  if (rawBuffer.length > MAX_INPUT_BYTES) {
    throw new Error(`input exceeds ${MAX_INPUT_BYTES} byte limit`);
  }
  try {
    return UTF8_DECODER.decode(rawBuffer);
  } catch {
    throw new Error("input is not valid UTF-8");
  }
}

function canonicalCoverageId(surface, level, subsystem, qaClass) {
  return `qa/${surface}/${level}/${subsystem}/${qaClass}`;
}

function validateLedger(ledger, errors) {
  if (!Array.isArray(ledger)) {
    errors.push("Ledger root must be an array of coverage units");
    return;
  }

  if (ledger.length > MAX_UNITS) {
    errors.push(`Ledger exceeds maximum units limit of ${MAX_UNITS}`);
    return;
  }

  const seenCoverageIds = new Set();

  for (let i = 0; i < ledger.length; i++) {
    const unit = ledger[i];
    const prefix = `ledger[${i}]`;

    if (!unit || typeof unit !== "object" || Array.isArray(unit)) {
      errors.push(`${prefix}: unit must be an object`);
      continue;
    }

    for (const req of REQUIRED_FIELDS) {
      if (!(req in unit)) {
        errors.push(`${prefix}: missing required field "${req}"`);
      }
    }

    if (unit.coverage_id) {
      if (seenCoverageIds.has(unit.coverage_id)) {
        errors.push(`${prefix}: duplicate coverage_id "${unit.coverage_id}"`);
      }
      seenCoverageIds.add(unit.coverage_id);
    }

    if (unit.surface && !TEST_SURFACES.has(unit.surface)) {
      errors.push(`${prefix}: invalid surface "${unit.surface}". Allowed: [${Array.from(TEST_SURFACES).join(", ")}]`);
    }

    if (unit.level && !TEST_LEVELS.has(unit.level)) {
      errors.push(`${prefix}: invalid level "${unit.level}". Allowed: [${Array.from(TEST_LEVELS).join(", ")}]`);
    }

    if (unit.qa_class && !QA_CLASSES.has(unit.qa_class)) {
      errors.push(`${prefix}: invalid qa_class "${unit.qa_class}". Allowed: [${Array.from(QA_CLASSES).join(", ")}]`);
    }

    if (unit.status && !STATUSES.has(unit.status)) {
      errors.push(`${prefix}: invalid status "${unit.status}". Allowed: [${Array.from(STATUSES).join(", ")}]`);
    }

    if (unit.canonical_refs && typeof unit.canonical_refs === "object") {
      for (const rf of REF_FIELDS) {
        if (unit.canonical_refs[rf] !== unit[rf]) {
          errors.push(`${prefix}: canonical_refs.${rf} ("${unit.canonical_refs[rf]}") does not match unit.${rf} ("${unit[rf]}")`);
        }
      }

      const expectedId = canonicalCoverageId(unit.surface, unit.level, unit.subsystem, unit.qa_class);
      if (unit.coverage_id !== expectedId) {
        errors.push(`${prefix}: coverage_id "${unit.coverage_id}" does not match canonical pattern "${expectedId}"`);
      }
    } else {
      errors.push(`${prefix}: canonical_refs must be an object`);
    }

    // Progressive Disclosure rule: selected_companion_blocks must be <= 1
    if (Array.isArray(unit.selected_companion_blocks)) {
      if (unit.selected_companion_blocks.length > 1) {
        errors.push(`${prefix}.selected_companion_blocks: exceeds 1 companion block (${unit.selected_companion_blocks.length}); must enforce progressive disclosure (max 1 companion block per hunter)`);
      }
      for (const block of unit.selected_companion_blocks) {
        if (typeof block === "string" && !COMPANION_BLOCKS.has(block)) {
          errors.push(`${prefix}.selected_companion_blocks: unknown companion block "${block}". Allowed: [${Array.from(COMPANION_BLOCKS).join(", ")}]`);
        }
      }
    } else {
      errors.push(`${prefix}: selected_companion_blocks must be an array`);
    }

    if (Array.isArray(unit.starting_paths)) {
      for (let p = 0; p < unit.starting_paths.length; p++) {
        const itemPath = unit.starting_paths[p];
        if (typeof itemPath === "string") {
          if (path.isAbsolute(itemPath) || itemPath.startsWith("/") || /^[a-zA-Z]:/.test(itemPath)) {
            errors.push(`${prefix}.starting_paths[${p}]: path must be repository-relative ("${itemPath}")`);
          }
          if (itemPath.includes("..")) {
            errors.push(`${prefix}.starting_paths[${p}]: path cannot contain ".."`);
          }
        }
      }
    } else {
      errors.push(`${prefix}: starting_paths must be an array`);
    }

    if (Array.isArray(unit.reviewed_paths)) {
      for (let p = 0; p < unit.reviewed_paths.length; p++) {
        const itemPath = unit.reviewed_paths[p];
        if (typeof itemPath === "string") {
          if (path.isAbsolute(itemPath) || itemPath.startsWith("/") || /^[a-zA-Z]:/.test(itemPath)) {
            errors.push(`${prefix}.reviewed_paths[${p}]: path must be repository-relative ("${itemPath}")`);
          }
        }
      }
    }

    // Status invariant checks
    if (unit.status === "covered" || unit.status === "candidate") {
      if (!unit.agent_id || typeof unit.agent_id !== "string" || unit.agent_id.trim() === "") {
        errors.push(`${prefix}: status "${unit.status}" requires a non-empty agent_id`);
      }
      if (!Array.isArray(unit.reviewed_paths) || unit.reviewed_paths.length === 0) {
        errors.push(`${prefix}: status "${unit.status}" requires at least one reviewed path`);
      }
      if (!Array.isArray(unit.inspected_invariants) || unit.inspected_invariants.length === 0) {
        errors.push(`${prefix}: status "${unit.status}" requires at least one inspected invariant`);
      }
    }

    if (unit.status === "candidate") {
      if (!Array.isArray(unit.result_fingerprints) || unit.result_fingerprints.length === 0) {
        errors.push(`${prefix}: status "candidate" requires at least one result_fingerprint`);
      }
    }

    if (unit.status === "blocked") {
      if (!unit.unresolved || typeof unit.unresolved !== "string" || unit.unresolved.trim() === "") {
        errors.push(`${prefix}: status "blocked" requires a non-empty unresolved reason`);
      }
    }
  }
}

function main() {
  const args = process.argv.slice(2);
  if (args.length === 0) {
    console.error("Usage: node validate-coverage-ledger.cjs <path-to-coverage-ledger.json>");
    process.exit(1);
  }

  const ledgerPath = args[0];
  let content;
  try {
    content = readFileSafely(ledgerPath);
  } catch (err) {
    console.error(`Error reading coverage ledger: ${err.message}`);
    process.exit(1);
  }

  let ledger;
  try {
    ledger = JSON.parse(content);
  } catch (err) {
    console.error(`Invalid JSON in coverage ledger: ${err.message}`);
    process.exit(1);
  }

  const errors = [];
  validateLedger(ledger, errors);

  if (errors.length > 0) {
    console.error(`Coverage ledger validation failed with ${errors.length} error(s):`);
    for (const err of errors) {
      console.error(`  - ${err}`);
    }
    process.exit(1);
  }

  const statusCounts = {};
  for (const unit of ledger) {
    statusCounts[unit.status] = (statusCounts[unit.status] || 0) + 1;
  }

  const statusSummary = Object.entries(statusCounts)
    .map(([s, c]) => `${s}: ${c}`)
    .join(", ");

  console.log(`Coverage ledger validation PASSED: ${ledger.length} units (${statusSummary}).`);
  process.exit(0);
}

if (require.main === module) {
  main();
}

module.exports = {
  validateLedger,
  canonicalCoverageId,
  readFileSafely,
  main,
};
