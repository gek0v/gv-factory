# Domain Companion: Cloud & Serverless

Apply this companion when auditing AWS Lambda, Cloudflare Workers, Google Cloud Functions, Azure Functions, Kubernetes microservices, or event-driven serverless architectures.

## Core Disciplines

1. **Stateless Contract & Global Scope**:
   - Serverless functions must not rely on in-memory global variables for persistent state across invocations (container recycling).
   - Global DB connection pools must be initialized outside the handler for container reuse, but bounded to prevent exhausting database max connections during traffic bursts.
2. **Cold Start & Heavy Imports**:
   - Heavy initialization or unused dependencies imported synchronously at module root that inflate cold-start duration.
3. **Idempotency in Queue & Event Handlers**:
   - SQS, Kafka, Pub/Sub, and EventBridge messages provide at-least-once delivery; consumers must be idempotent to prevent duplicate processing.
4. **Execution Deadlines & Resource Bounds**:
   - Ensure timeouts on external network calls are strictly lower than the lambda/worker maximum execution timeout.
