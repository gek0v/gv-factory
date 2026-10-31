# 01: Portable worktree path configuration and OS-agnostic defaults

**What to build:**
Eliminate hardcoded `/tmp/ov-worktrees` directory paths across `cmd/ov-server`, `cmd/ov-worker`, and `internal/sandbox`.
Replace them with cross-platform default path resolution that defaults to `filepath.Join(os.TempDir(), "ov-worktrees")` when not explicitly supplied via configuration, CLI flags, or environment variables.

Ensure all services cleanly initialize and locate worktree root folders across Windows (`%TEMP%\ov-worktrees`), macOS, and Linux without hardcoded POSIX slash assumptions.

**Blocked by:** None (can start immediately).

**GitHub Issue:** https://github.com/gek0v/gv-factory/issues/1

**Status:** done

- [x] `internal/sandbox.Config` defaults `BaseWorktreeDir` to a cross-platform location (`filepath.Join(os.TempDir(), "ov-worktrees")`) if empty.
- [x] `cmd/ov-server` and `cmd/ov-worker` pass portable default worktree paths or use the automatic fallback.
- [x] Existing unit tests pass across Windows and POSIX environments without path separator failures.
