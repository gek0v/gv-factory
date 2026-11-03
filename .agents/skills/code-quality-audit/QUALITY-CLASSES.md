# Quality Classes & Inspection Patterns

This document defines the core quality classes used across reconnaissance, hunting, and verification in `code-quality-audit`.

---

## 1. Architecture, Modularity & Coupling (`architecture_and_coupling`)

### Invariants & Contracts
- **Directional Dependencies**: Lower layers (domain models, core business rules) must NEVER depend on higher layers (HTTP controllers, UI components, CLI routers).
- **Interface Segregation & Inversion**: High-level modules must depend on abstractions, not concrete database or network clients.
- **Acyclic Graph**: Module and package dependency graphs must form a directed acyclic graph (DAG).
- **Bounded Contexts**: Subsystems should interact via explicit contracts, not by querying private internal tables or mutating internal memory structures.

### Critical Defect Patterns
- **Cyclic Module Dependencies**: Circular `import` / `require` loops that lead to partially initialized modules, undefined exports at runtime, or tight compile-time coupling.
- **Layer Bypassing**: Controllers invoking raw database SQL or low-level file I/O directly, bypassing authorization, validation, and domain invariants.
- **God Modules & Brain Classes**: Single files or classes aggregating thousands of lines of disparate responsibilities, preventing concurrent development and modular refactoring.
- **Leaky Abstractions**: Exposing ORM-specific entities or low-level HTTP headers directly into domain entities, tightly binding business logic to third-party frameworks.

### Excluded as Cosmetic
- Disagreements on whether to use functional programming vs object-oriented design when both maintain clear boundaries.
- Preference for directory naming schemes (e.g. `src/modules` vs `src/features`) where dependencies remain clean.

---

## 2. Concurrency, Async & Resource Lifecycles (`concurrency_and_resources`)

### Invariants & Contracts
- **Mutual Exclusion & State Integrity**: Shared mutable state accessed by concurrent threads or async ticks must be protected by appropriate primitives (locks, atomic operations, immutable data structures, or actor mailboxes).
- **Atomic Read-Modify-Write**: Multi-step state transitions must be executed atomically or guarded by optimistic concurrency control / database locks.
- **Guaranteed Resource Disposal**: File descriptors, network connections, database handles, timers, and worker threads must be deterministically released across both success and error/panic paths.
- **Lock Ordering**: Multiple locks must always be acquired in a globally consistent order to prevent deadlocks.

### Critical Defect Patterns
- **Time-of-Check to Time-of-Use (TOCTOU)**: Querying a balance or status in one step and updating it in a subsequent async step without holding a lock or transaction, enabling race conditions.
- **Unclosed Handles on Error**: Opening files, sockets, or database transactions without `try...finally` (or language equivalents like `defer`, `using`, `RAII`), causing connection pool starvation.
- **Fire-and-Forget Goroutines / Tasks**: Spawning background workers without cancellation tokens, error listeners, or lifecycle management, leading to zombie processes and unmonitored crashes.
- **Event Listener / Subscription Leaks**: Subscribing to event emitters or message brokers without unregistering on component destroy/cleanup, causing memory growth.

### Excluded as Cosmetic
- Choosing between `async/await` vs raw Promises when error propagation and execution order are identical.
- Preference for channels vs mutexes when both safely enforce isolation.

---

## 3. Algorithmic Complexity, Memory & Performance (`performance_and_algorithmic`)

### Invariants & Contracts
- **Scalable Time Complexity**: High-frequency or request-critical operations must scale at $O(1)$, $O(\log n)$, or $O(n)$. Avoid accidental $O(n^2)$ or $O(2^n)$ in unbounded paths.
- **I/O Batching**: Repeated data fetching across related items must be batched or eager-loaded, avoiding sequential round trips.
- **Bounded Memory Allocation**: In-memory collections, caches, buffers, and request bodies must have explicit size limits and eviction policies.

### Critical Defect Patterns
- **N+1 Query Cascades**: Fetching a list of entities and subsequently issuing a query per entity inside a loop rather than executing a single batched query or JOIN.
- **Hot-Loop Allocations & Deep Clones**: Performing expensive JSON serialization, regex compilation, or deep cloning inside tight iterations over large arrays.
- **Unbounded In-Memory Caches**: Storing items in static Maps or dictionaries without LRU eviction, TTL expiry, or maximum capacity, leading to eventual Out-Of-Memory (OOM) crashes.
- **Catastrophic Backtracking Regex**: Regular expressions with nested quantifiers (e.g. `(a+)+$`) evaluated against user inputs, causing CPU starvation (ReDoS).

### Excluded as Cosmetic
- Micro-optimizations on cold paths (e.g. setup scripts or one-time configuration loading at startup).
- Nitpicking array iteration methods (e.g. `for` loop vs `forEach` vs `map`) where differences are sub-millisecond and non-hot.

---

## 4. Error Handling, Resilience & Fault Tolerance (`error_handling_and_resilience`)

### Invariants & Contracts
- **No Swallowed Failures**: Exceptions, errors, and rejections must either be handled with appropriate recovery/logging or propagated to the caller.
- **Bounded Retry with Backoff**: Retrying failed external network operations must implement exponential backoff and jitter to prevent thundering herds.
- **Timeouts & Circuit Breaking**: All outbound I/O (network requests, DB calls, IPC) must have explicit deadlines/timeouts.
- **Safe Failure Modes**: In case of partial failure, operations must fail closed or rollback partial state.

### Critical Defect Patterns
- **Empty Catch Blocks**: Catching errors and doing nothing, silently masking operational failures, network drops, or data corruption.
- **Unhandled Async Rejections**: Missing `.catch()` or unhandled promise rejection in Node.js/browser environments that crash processes or leave callers hanging indefinitely.
- **Panic in Library / Middleware Code**: Using `panic`, `unwrap()`, or `process.exit()` in library code rather than returning typed error results.
- **Missing Rollback on Partial Failures**: Writing to a database, uploading an S3 file, and failing on an email dispatch without rolling back the database row or compensating.

### Excluded as Cosmetic
- Preferring custom Error subclasses vs standard Error objects unless callers depend on type discrimination for recovery.
- Log message phrasing or formatting style.

---

## 5. Maintainability, Testability & Cognitive Complexity (`maintainability_and_testability`)

### Invariants & Contracts
- **Isolated Testability**: Business logic must be executable in test harnesses without requiring live external infrastructure (databases, internet, cloud APIs).
- **Controlled Cognitive Complexity**: Complex branching and business decisions must be decomposed into readable, single-responsibility units.
- **Deterministic Execution**: Functions should avoid hidden dependencies on ambient system clocks, environment variables, or randomness unless injected.

### Critical Defect Patterns
- **Monolithic Functions with Extreme Nesting**: Single routines exceeding cognitive complexity > 25 with 5+ levels of nested `if`/`switch`/`try` blocks that make reasonability and edge-case coverage nearly impossible.
- **Hardcoded Singletons & Global State**: Hardcoding direct global singletons (`DB.getInstance()`) inside domain logic, rendering isolated unit testing impossible without bytecode manipulation or complex monkey-patching.
- **Side-Effects in Accessors**: Property getters or `toString()` implementations that trigger network calls, mutate database records, or alter global state.

### Excluded as Cosmetic
- Line length disagreements (e.g. 80 vs 100 vs 120 chars).
- Trailing commas, brace positions, or indentation.
- Variable naming nitpicks that do not obscure semantics.

---

## 6. Type Safety, Contracts & Invariant Drift (`type_safety_and_contracts`)

### Invariants & Contracts
- **Sound Boundary Validation**: Data crossing trust boundaries (HTTP bodies, query params, IPC payloads, configuration files) must be validated with runtime schemas (Zod, Pydantic, serde, etc.).
- **Honest Types**: Static type definitions must accurately reflect the possible values (including `null`, `undefined`, or error states).

### Critical Defect Patterns
- **Escape Hatch Proliferation**: Systemic abuse of `any`, `as unknown as Type`, `reinterpret_cast`, or `@ts-ignore` to silence compiler errors, bypassing safety guarantees and causing runtime crashes.
- **Unchecked Optional Traversal**: Accessing nested properties on optional or nullable objects without checking presence or using optional chaining, leading to `NullPointerException` or `TypeError: cannot read property of undefined`.
- **Deserialization Invariant Violations**: Deserializing external JSON directly into internal domain models without verifying that domain invariants (e.g. `price > 0`, `status in ValidStatuses`) hold true.

### Excluded as Cosmetic
- Explicit type annotations vs type inference where inference is 100% sound.
- Choosing `type` alias vs `interface` in TypeScript when behavior is equivalent.
