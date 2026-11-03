# Domain Companion: Performance & Resilience Testing

This companion provides specialized inspection patterns for benchmark stability, load testing harnesses (k6, Locust, Artillery, JMeter), chaos/fault injection, and resource leak detection under test.

---

## 1. Load Harness Integrity & Realistic Modeling

### Invariant
Load and performance test scripts must model realistic production traffic patterns (arrival rates, think times, payload variance, connection reuse), not unrealistic single-loop flooding.

### Critical Failure Modes
1. **Unrealistic Zero-Think-Time Tight Loops**:
   A load test firing 10,000 requests in a `while(true)` loop without simulated user think time, testing TCP socket exhaustion on the test machine rather than real system throughput.
2. **Missing Dynamic Payload Variance**:
   Testing cache-heavy endpoints with the exact same query parameters (`/api/products?id=1`) for every virtual user, artificially inflating cache hit ratios to 100% and masking database bottlenecks.
3. **Unmeasured Client-Side Bottlenecks**:
   Running a 1,000 VU (virtual user) test on a single small CI container where CPU saturation on the test generator causes artificial response latency spikes.

### Anti-Nitpicking Filter
- Reject complaints about synthetic load scripts not exactly matching production analytics if the goal of the test is stress/breakpoint analysis.
- Reject requests to run heavy load tests inside standard unit test CI pipelines.

---

## 2. Resilience, Circuit Breakers & Chaos Verification

### Invariant
Systems relying on distributed dependencies (databases, third-party APIs, microservices) must have automated tests verifying graceful degradation when dependencies fail or experience latency spikes.

### Critical Failure Modes
1. **Missing Timeout & Cancellation Assertions**:
   A service client makes outbound HTTP calls to an external partner, but no test verifies that the client terminates after a 3-second timeout when the partner hangs.
2. **Unexercised Circuit Breaker Trips**:
   A circuit breaker library is installed, but no automated test simulates consecutive 500 errors to verify that the circuit trips to `OPEN` and fast-fails subsequent requests.
3. **Missing Fallback Response Checks**:
   When downstream services are unavailable, testing only that the primary call failed, without asserting that the service returned a valid cached or degraded fallback response.

### Anti-Nitpicking Filter
- Reject findings demanding chaos tests on systems with simple, single-tier monolithic architectures without external network dependencies.

---

## 3. Resource Leaks & Connection Pool Starvation Under Test

### Invariant
Performance and integration tests must not exhaust system resources (file descriptors, database connections, threads, memory) during repeated test executions.

### Critical Failure Modes
1. **Database Connection Pool Exhaustion**:
   Tests spawning parallel workers that each open 10 database connections against a database configured with `max_connections: 50`, causing tests to crash with `ConnectionPoolTimeoutException`.
2. **Dangling HTTP Keep-Alive Agents**:
   Integration tests creating new `http.Agent` instances per test without destroying them, leading to thousands of open TCP sockets and `EMFILE` (too many open files) errors.
3. **Memory Leaks in Repeated Benchmark Iterations**:
   Benchmark functions appending results to global arrays across 100,000 iterations, skewing garbage collection overhead and producing false benchmark regression alerts.

### Anti-Nitpicking Filter
- Reject micro-benchmarking complaints on utility functions that execute in sub-microsecond intervals.
