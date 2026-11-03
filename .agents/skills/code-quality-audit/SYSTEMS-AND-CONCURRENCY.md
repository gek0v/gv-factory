# Domain Companion: Systems & Low-Level Concurrency

Apply this companion when auditing native systems, runtimes, daemons, and concurrent infrastructure written in Rust, Go, C/C++, or multi-threaded Java/C#.

## Core Disciplines

1. **Lock Ordering & Deadlock Avoidance**:
   - Verify consistent lock hierarchy when acquiring multiple mutexes/rwlocks.
   - Guard against holding a lock across an I/O wait, channel receive, or cross-actor RPC call.
2. **Channel & Queue Backpressure**:
   - In Go/Rust, unbuffered channels can block indefinitely if receivers exit early.
   - Bounded channels must have explicit drop or backpressure policies; unbounded queues risk OOM under traffic spikes.
3. **Memory Safety & Leaks**:
   - In C/C++: check double free, use-after-free, buffer overruns, uninitialized reads.
   - In Rust: inspect `unsafe` blocks for undefined behavior, pointer aliasing, or unaligned memory access.
   - In Go/Java: identify long-lived pointers preventing GC of large slice/map allocations (slice backing array retention).
4. **Atomics & Memory Ordering**:
   - Check atomic operations for proper memory ordering (`Relaxed`, `Acquire`, `Release`, `SeqCst`).
   - Avoid data races on shared counters or flags.
