# 03: Worktree teardown, prune and lifecycle integration tests

**What to build:**
Complete the lifecycle management in `internal/sandbox.Manager.Destroy` and `Prune` by implementing real worktree teardown and garbage collection.

The teardown flow must:
1. Safely remove the worktree via `git worktree remove --force <worktree_path>`.
2. Delete the associated transient branch `ov-run/<run_id>`.
3. Execute `git worktree prune` to clean up dangling worktree administrative metadata.
4. Clean up the internal state map in `Manager`.

Provide an end-to-end integration test in `internal/sandbox/sandbox_test.go` exercising the entire lifecycle: Provision -> Verify Isolated State -> Destroy -> Verify Full Disk & Git Reclamation.

**Blocked by:** 02-preflight-lock-cleanup-and-worktree-provisioning

**GitHub Issue:** https://github.com/gek0v/gv-factory/issues/3

**Status:** done

- [x] `Destroy` executes `git worktree remove --force` and deletes `ov-run/<run_id>` branch.
- [x] Garbage collection calls `git worktree prune` so no dangling worktree pointers remain in `.git/worktrees`.
- [x] Automated integration test in `sandbox_test.go` passes cleanly verifying creation, isolation, and complete cleanup.
- [x] `go test -v -race ./internal/sandbox/...` passes with zero leaks and zero race conditions.
