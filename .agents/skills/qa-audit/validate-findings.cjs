#!/usr/bin/env node

/**
 * Validates findings.json against report-schema.json for qa-audit.
 * Usage: node validate-findings.cjs <path-to-findings.json> [path-to-schema.json]
 *
 * Dependency-free interpreter for JSON Schema keywords in report-schema.json,
 * plus QA-specific invariant checks, anti-nitpicking filters, and cross-platform safe file handling.
 */

const fs = require("node:fs");
const path = require("node:path");
const { TextDecoder } = require("node:util");

const hasOwn = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const SUPPORTED_TYPES = new Set(["object", "array", "string", "integer", "number", "boolean", "null"]);
const SUPPORTED_KEYWORDS = new Set([
  "$comment",
  "additionalProperties",
  "const",
  "description",
  "enum",
  "items",
  "minimum",
  "minItems",
  "minLength",
  "maxLength",
  "oneOf",
  "pattern",
  "properties",
  "required",
  "type",
  "uniqueItems",
  "visibleContent",
]);

const SEVERITY_RANK = new Map([
  ["informational", 0],
  ["low", 1],
  ["medium", 2],
  ["high", 3],
  ["critical", 4],
]);

const LIMITS = Object.freeze({
  inputBytes: 5 * 1024 * 1024,
  nestingDepth: 64,
  arrayItems: 1000,
  canonicalKeyBytes: 1024 * 1024,
  uniqueSetBytes: 5 * 1024 * 1024,
  validationErrors: 100,
});

const VISIBLE_CONTENT = /[^\p{White_Space}\p{Cc}\p{Cf}\p{Default_Ignorable_Code_Point}]/u;
const UTF8_DECODER = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
const UNSAFE_DIAGNOSTIC_CHARACTER = /[\p{Cc}\p{Cf}\p{Cs}\p{Zl}\p{Zp}\p{Default_Ignorable_Code_Point}]/gu;
const MAX_DIAGNOSTIC_STRING_LENGTH = 256;

class JsonStructureError extends Error {}
class SafeInputError extends Error {}

function escapeUnsafeDiagnosticCharacters(value) {
  return String(value).replace(UNSAFE_DIAGNOSTIC_CHARACTER, (character) => {
    const codePoint = character.codePointAt(0);
    return codePoint <= 0xffff
      ? `\\u${codePoint.toString(16).padStart(4, "0")}`
      : `\\u{${codePoint.toString(16)}}`;
  });
}

function safeQuote(value) {
  let serialized;
  if (typeof value === "string") {
    const clipped = value.length > MAX_DIAGNOSTIC_STRING_LENGTH
      ? `${value.slice(0, MAX_DIAGNOSTIC_STRING_LENGTH)}...`
      : value;
    serialized = JSON.stringify(clipped);
  } else if (value === null || typeof value === "boolean") {
    serialized = String(value);
  } else if (typeof value === "number" && Number.isFinite(value)) {
    serialized = String(value);
  } else {
    serialized = `"<${Array.isArray(value) ? "array" : typeof value}>"`;
  }
  return escapeUnsafeDiagnosticCharacters(serialized);
}

function propertyPath(base, key) {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(key)
    ? `${base}.${key}`
    : `${base}[${safeQuote(key)}]`;
}

function createErrorList() {
  const errors = [];
  Object.defineProperty(errors, "push", {
    value(...messages) {
      const remaining = LIMITS.validationErrors - this.length;
      if (remaining > 0) {
        Array.prototype.push.apply(this, messages.slice(0, remaining).map(escapeUnsafeDiagnosticCharacters));
      }
      return this.length;
    },
  });
  return errors;
}

function typeOf(value) {
  if (Array.isArray(value)) return "array";
  if (value === null) return "null";
  return typeof value;
}

function deepEqual(left, right) {
  if (left === right) return true;
  if (typeOf(left) !== typeOf(right)) return false;
  if (Array.isArray(left)) {
    return left.length === right.length && left.every((value, index) => deepEqual(value, right[index]));
  }
  if (left !== null && typeof left === "object") {
    const leftKeys = Object.keys(left);
    const rightKeys = Object.keys(right);
    return leftKeys.length === rightKeys.length &&
      leftKeys.every((key) => hasOwn(right, key) && deepEqual(left[key], right[key]));
  }
  return false;
}

function canonicalJsonString(value) {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonicalJsonString).join(",")}]`;
  const keys = Object.keys(value).sort();
  return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJsonString(value[k])}`).join(",")}}`;
}

function readFileSafely(filePath) {
  const resolved = path.resolve(filePath);
  let lstat;
  try {
    lstat = fs.lstatSync(resolved);
  } catch (err) {
    throw new SafeInputError(`cannot access file: ${err.message}`);
  }
  if (lstat.isSymbolicLink()) {
    throw new SafeInputError("input must not be a symlink");
  }
  if (!lstat.isFile()) {
    throw new SafeInputError("input must be a regular file");
  }
  if (lstat.size > LIMITS.inputBytes) {
    throw new SafeInputError(`input exceeds ${LIMITS.inputBytes} byte limit`);
  }

  const rawBuffer = fs.readFileSync(resolved);
  if (rawBuffer.length > LIMITS.inputBytes) {
    throw new SafeInputError(`input exceeds ${LIMITS.inputBytes} byte limit`);
  }
  try {
    return UTF8_DECODER.decode(rawBuffer);
  } catch {
    throw new SafeInputError("input is not valid UTF-8");
  }
}

function validateSchema(schema, data, pointer, errors) {
  if (schema.$comment && typeof schema.$comment !== "string") {
    errors.push(`${pointer}: $comment must be string`);
  }

  if (schema.type) {
    const currentType = typeOf(data);
    if (schema.type === "integer") {
      if (typeof data !== "number" || !Number.isInteger(data)) {
        errors.push(`${pointer}: expected integer, got ${currentType}`);
        return;
      }
    } else if (currentType !== schema.type) {
      errors.push(`${pointer}: expected ${schema.type}, got ${currentType}`);
      return;
    }
  }

  if (schema.const !== undefined && !deepEqual(data, schema.const)) {
    errors.push(`${pointer}: must equal constant ${safeQuote(schema.const)}`);
  }

  if (schema.enum && !schema.enum.some((val) => deepEqual(val, data))) {
    errors.push(`${pointer}: must be one of [${schema.enum.map(safeQuote).join(", ")}]`);
  }

  if (typeof data === "string") {
    if (schema.minLength !== undefined && data.length < schema.minLength) {
      errors.push(`${pointer}: length ${data.length} < minLength ${schema.minLength}`);
    }
    if (schema.maxLength !== undefined && data.length > schema.maxLength) {
      errors.push(`${pointer}: length ${data.length} > maxLength ${schema.maxLength}`);
    }
    if (schema.pattern && !new RegExp(schema.pattern).test(data)) {
      errors.push(`${pointer}: does not match pattern ${schema.pattern}`);
    }
    if (schema.visibleContent && !VISIBLE_CONTENT.test(data)) {
      errors.push(`${pointer}: must contain visible non-whitespace characters`);
    }
  }

  if (typeof data === "number") {
    if (schema.minimum !== undefined && data < schema.minimum) {
      errors.push(`${pointer}: ${data} < minimum ${schema.minimum}`);
    }
  }

  if (Array.isArray(data)) {
    if (schema.minItems !== undefined && data.length < schema.minItems) {
      errors.push(`${pointer}: item count ${data.length} < minItems ${schema.minItems}`);
    }
    if (schema.uniqueItems) {
      const seen = new Set();
      for (let i = 0; i < data.length; i++) {
        const repr = canonicalJsonString(data[i]);
        if (seen.has(repr)) {
          errors.push(`${pointer}[${i}]: duplicate item in uniqueItems array`);
        }
        seen.add(repr);
      }
    }
    if (schema.items) {
      for (let i = 0; i < data.length; i++) {
        validateSchema(schema.items, data[i], `${pointer}[${i}]`, errors);
      }
    }
  }

  if (data !== null && typeof data === "object" && !Array.isArray(data)) {
    if (schema.required) {
      for (const reqKey of schema.required) {
        if (!hasOwn(data, reqKey)) {
          errors.push(`${pointer}: missing required property "${reqKey}"`);
        }
      }
    }
    if (schema.properties) {
      for (const key of Object.keys(data)) {
        if (hasOwn(schema.properties, key)) {
          validateSchema(schema.properties[key], data[key], propertyPath(pointer, key), errors);
        } else if (schema.additionalProperties === false) {
          errors.push(`${pointer}: unrecognized additional property "${key}"`);
        }
      }
    }
  }

  if (schema.oneOf) {
    const matchingIndices = [];
    const subErrors = [];
    for (let i = 0; i < schema.oneOf.length; i++) {
      const trialErrors = [];
      validateSchema(schema.oneOf[i], data, pointer, trialErrors);
      if (trialErrors.length === 0) {
        matchingIndices.push(i);
      } else {
        subErrors.push({ index: i, errs: trialErrors });
      }
    }
    if (matchingIndices.length === 0) {
      errors.push(`${pointer}: did not match any oneOf branch:\n` +
        subErrors.map((s) => `  Branch ${s.index}: ${s.errs.join("; ")}`).join("\n"));
    } else if (matchingIndices.length > 1) {
      errors.push(`${pointer}: matched multiple oneOf branches [${matchingIndices.join(", ")}], must match exactly one`);
    }
  }
}

function validateQAFindingInvariants(findings, errors) {
  const seenFingerprints = new Set();

  for (let i = 0; i < findings.length; i++) {
    const f = findings[i];
    const prefix = `findings[${i}]`;

    if (!f || typeof f !== "object") continue;

    // Fingerprint uniqueness
    if (typeof f.fingerprint === "string") {
      if (seenFingerprints.has(f.fingerprint)) {
        errors.push(`${prefix}.fingerprint: duplicate fingerprint "${f.fingerprint}" across findings`);
      }
      seenFingerprints.add(f.fingerprint);
    }

    // Relative and safe file paths in trace
    if (Array.isArray(f.trace)) {
      for (let t = 0; t < f.trace.length; t++) {
        const item = f.trace[t];
        if (item && typeof item.file === "string") {
          const filePath = item.file;
          if (path.isAbsolute(filePath) || filePath.startsWith("/") || /^[a-zA-Z]:/.test(filePath)) {
            errors.push(`${prefix}.trace[${t}].file: must be repository-relative, got absolute path "${filePath}"`);
          }
          if (filePath.includes("..")) {
            errors.push(`${prefix}.trace[${t}].file: contains traversal component ".."`);
          }
        }
      }
    }

    // Relative and safe file paths in evidence
    if (Array.isArray(f.evidence)) {
      for (let e = 0; e < f.evidence.length; e++) {
        const ev = f.evidence[e];
        if (ev && typeof ev.file === "string") {
          const filePath = ev.file;
          if (path.isAbsolute(filePath) || filePath.startsWith("/") || /^[a-zA-Z]:/.test(filePath)) {
            errors.push(`${prefix}.evidence[${e}].file: must be repository-relative, got absolute path "${filePath}"`);
          }
          if (filePath.includes("..")) {
            errors.push(`${prefix}.evidence[${e}].file: contains traversal component ".."`);
          }
        }
      }
    }

    // Anti-nitpicking & token efficiency rules:
    if (f.verdict === "confirmed") {
      if (f.remediation && typeof f.remediation.recommended_diff === "string") {
        const diff = f.remediation.recommended_diff;
        const lineCount = diff.split("\n").length;
        if (diff.trim().length === 0) {
          errors.push(`${prefix}.remediation.recommended_diff: cannot be empty for confirmed QA finding`);
        } else if (lineCount > 40) {
          errors.push(`${prefix}.remediation.recommended_diff: diff exceeds 40 lines (${lineCount} lines); must be a concise, surgical refactoring diff`);
        }
      }

      // Bounded trace and evidence for high-precision, token-lean reports
      if (Array.isArray(f.trace) && f.trace.length > 5) {
        errors.push(`${prefix}.trace: exceeds 5 steps (${f.trace.length} steps); keep trace focused on test_case, fixture_or_mock, defect_site, and impact_site`);
      }
      if (Array.isArray(f.evidence) && f.evidence.length > 3) {
        errors.push(`${prefix}.evidence: exceeds 3 items (${f.evidence.length} items); provide only the most decisive evidence`);
      }
    }

    // Needs validation must have non-empty blockers and clear validation plan
    if (f.verdict === "needs_validation") {
      if (Array.isArray(f.blockers) && f.blockers.length === 0) {
        errors.push(`${prefix}.blockers: must list at least one unresolved validation blocker`);
      }
      if (typeof f.validation_plan === "string" && f.validation_plan.trim().length === 0) {
        errors.push(`${prefix}.validation_plan: cannot be empty for needs_validation finding`);
      }
    }

    // Rejected must have rejection_reason and refutation
    if (f.verdict === "rejected") {
      if (typeof f.refutation === "string" && f.refutation.trim().length === 0) {
        errors.push(`${prefix}.refutation: cannot be empty for rejected finding`);
      }
    }
  }
}

function main() {
  const args = process.argv.slice(2);
  if (args.length === 0) {
    console.error("Usage: node validate-findings.cjs <path-to-findings.json> [path-to-schema.json]");
    process.exit(1);
  }

  const findingsPath = args[0];
  const schemaPath = args[1] || path.join(__dirname, "report-schema.json");

  let schemaContent;
  let findingsContent;

  try {
    schemaContent = readFileSafely(schemaPath);
  } catch (err) {
    console.error(`Error reading schema at ${schemaPath}: ${err.message}`);
    process.exit(1);
  }

  try {
    findingsContent = readFileSafely(findingsPath);
  } catch (err) {
    console.error(`Error reading findings at ${findingsPath}: ${err.message}`);
    process.exit(1);
  }

  let schema;
  let findings;

  try {
    schema = JSON.parse(schemaContent);
  } catch (err) {
    console.error(`Invalid JSON in schema: ${err.message}`);
    process.exit(1);
  }

  try {
    findings = JSON.parse(findingsContent);
  } catch (err) {
    console.error(`Invalid JSON in findings: ${err.message}`);
    process.exit(1);
  }

  const errors = createErrorList();

  validateSchema(schema, findings, "root", errors);
  if (Array.isArray(findings)) {
    validateQAFindingInvariants(findings, errors);
  }

  if (errors.length > 0) {
    console.error(`Validation failed with ${errors.length} error(s):`);
    for (const err of errors) {
      console.error(`  - ${err}`);
    }
    process.exit(1);
  }

  console.log(`Validation passed: ${Array.isArray(findings) ? findings.length : 0} finding(s) conform to schema and invariants.`);
  process.exit(0);
}

if (require.main === module) {
  main();
}

module.exports = {
  validateSchema,
  validateQAFindingInvariants,
  readFileSafely,
  main,
};
