# Domain Companion: API & Contract Testing

This companion provides specialized inspection patterns for REST, GraphQL, gRPC, and message contract testing, schema conformance, status matrices, and payload boundary verification.

---

## 1. Schema Conformance & Contract Drift

### Invariant
API tests must validate that runtime request and response bodies strictly adhere to published interface contracts (OpenAPI/Swagger schemas, GraphQL AST schemas, Protobuf definitions, or Pact pactfiles).

### Critical Failure Modes
1. **Unvalidated Response Bodies**:
   Tests asserting only `expect(res.status).toBe(200)` without validating the response payload structure against a JSON schema or contract validator. An API returning `{ "status": "ok" }` instead of `{ "items": [...] }` will pass this test.
2. **Ignoring Unknown or Leaked Fields**:
   Failing to assert `additionalProperties: false` or schema strictness, allowing sensitive internal fields (e.g. `password_hash`, `internal_trace_id`, `stripe_customer_id`) to leak into public API responses undetected.
3. **Pact Consumer/Provider Desynchronization**:
   Pact contract files stored statically in git without being verified against the provider service during provider CI builds.

### Anti-Nitpicking Filter
- Reject findings demanding JSON schema validation on trivial healthcheck endpoints (`/healthz`).
- Reject complaints about response field ordering in JSON objects.

---

## 2. HTTP Status & Error Matrix Coverage

### Invariant
Every endpoint must have explicit test coverage for both valid operations and expected failure modes across the HTTP status code matrix.

### Critical Failure Modes
1. **Missing Authentication & Authorization Matrices**:
   Testing endpoints only with a valid superadmin token. Zero test cases asserting:
   - Missing `Authorization` header $\to$ HTTP 401 Unauthorized.
   - Expired or malformed JWT token $\to$ HTTP 401 Unauthorized.
   - Valid token for User A accessing User B's resource $\to$ HTTP 403 Forbidden.
2. **Missing Validation Error Breakdown**:
   When testing invalid input, asserting only `expect(res.status).toBe(400)` without verifying that the response body points to the exact invalid field (e.g. `res.body.errors[0].field === "email"`).
3. **Idempotency Header Bypass**:
   Mutating POST endpoints (e.g. charge creation, order placement) tested only once, never verifying that submitting the same request twice with the same `Idempotency-Key` header returns the original response without duplicate side effects.

### Anti-Nitpicking Filter
- Reject requests to test every permutation of HTTP status codes (e.g. 418 I'm a teapot, 405 Method Not Allowed) unless the application explicitly defines custom handlers for them.

---

## 3. Payload Boundary & Mutation Fuzzing

### Invariant
Endpoints accepting user input must be tested against boundary values and invalid payloads to ensure graceful rejection rather than unhandled 500 exceptions.

### Critical Failure Modes
1. **Type Coercion & Injection Blindspots**:
   Testing only well-typed payloads, never verifying that sending an array `[1, 2]` where a string is expected, or an object `{}` where a number is expected, results in a clean 400 error rather than a server crash.
2. **Boundary Values ($0, -1, \text{MAX\_INT}, \text{empty string}, \text{whitespace}$)**:
   Financial or numerical APIs tested only with positive round numbers ($10, 50, 100$), never testing $0$ or negative numbers, allowing negative discounts or negative balances in production.

### Anti-Nitpicking Filter
- Reject generic demands to fuzz every endpoint if the framework already applies strict schema validation middleware (e.g. Zod, Joi, class-validator) proven by shared unit tests.
