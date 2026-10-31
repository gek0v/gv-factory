# 02: Pre-flight lock cleanup and atomic git worktree provisioning

**What to build:**
Implement concrete Git worktree provisioning in `internal/sandbox.Manager.ProvisionWorktree` to allocate an isolated worktree directory per run.

The provisioning flow must:
1. Perform pre-flight inspection for orphaned `.git/index.lock` or `.git/worktrees/<id>/index.lock` locks caused by crashes and safely release/clean them.
2. Execute `git worktree add -b ov-run/<run_id> <worktree_path> main` to create the real filesystem worktree and dedicated branch.
3. Validate that the worktree directory exists and is cleanly attached before returning the `Environment`.
4. Enforce that the active branch is locked to `ov-run/<run_id>` and direct mutation of `main` is prevented.

**Blocked by:** 01-portable-worktree-path-configuration

**GitHub Issue:** https://github.com/gek0v/gv-factory/issues/2

**Status:** done

- [x] Pre-flight lock cleaner safely discovers and removes stale lock files before allocating.
- [x] `ProvisionWorktree` creates a real filesystem directory and a dedicated git worktree on branch `ov-run/<run_id>`.
- [x] Failures during provisioning cleanly roll back any partial directories or locks.
- [x] Unit tests with a temporary git repo fixture assert worktree directory creation and correct git branch attachment.
