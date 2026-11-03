# Domain Companion: Web, Services & APIs

Apply this companion when auditing HTTP APIs, microservices, RPC endpoints, and web backends.

## Core Disciplines

1. **Request Lifecycle & Middleware Safety**:
   - Middleware order must ensure authentication, correlation ID generation, body parsing, and error-catching happen in the correct sequence.
   - An unhandled error in middleware must not hang the HTTP request (client socket remaining open until gateway timeout).
2. **Streaming & Buffering**:
   - File uploads and large payloads must use streaming or chunked handling rather than buffering the entire payload into RAM (`Buffer.concat`, `req.read()`).
3. **Idempotency & Pagination**:
   - Mutating endpoints (`POST`, `PUT`, `DELETE`) expected to handle network retries must enforce idempotency via idempotency keys or unique transaction tokens.
   - List endpoints must enforce pagination limits (max page size) to prevent unbounded memory allocation and DB saturation.
4. **Connection Pooling & Downstream I/O**:
   - HTTP client instances (`fetch`, `axios`, `HttpClient`) must be reused to leverage TCP keep-alive and connection pooling, not instantiated per request.
   - Explicit timeouts must be set on all outbound calls.

## Universal Moves
- Check if request handlers pass cancellation contexts (`context.Context`, `AbortSignal`) to downstream DB and HTTP queries.
- Inspect rate-limiting and input payload size caps (`body-parser` limits).
- Verify error response consistency: do internal stack traces or database errors leak to clients?
