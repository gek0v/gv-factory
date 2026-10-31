// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMultitenancy_WorktreePath(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})
	ctx := context.Background()

	runID := "run-multi-1"
	branch := "feat/user-auth"

	env, err := mgr.ProvisionWorktreeWithOptions(ctx, runID, WorktreeOptions{
		Branch:      branch,
		MultiTenant: true,
	})
	if err != nil {
		t.Fatalf("unexpected provision error: %v", err)
	}

	expectedPath := filepath.Join(worktreeBase, "ov-factory", "runs", runID, filepath.FromSlash(branch))
	if env.WorktreePath != expectedPath {
		t.Fatalf("expected worktree path %q, got %q", expectedPath, env.WorktreePath)
	}

	if env.Branch != branch {
		t.Fatalf("expected branch %q, got %q", branch, env.Branch)
	}

	// Verify directory exists
	info, err := os.Stat(env.WorktreePath)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected directory at %q", env.WorktreePath)
	}

	// Clean up
	if err := mgr.RecycleWorktree(ctx, runID); err != nil {
		t.Fatalf("unexpected recycle error: %v", err)
	}

	// Verify path was cleaned up
	if _, err := os.Stat(env.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree path to be deleted after recycle")
	}
}

func TestMultitenancy_BranchCollisionGuard(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})
	ctx := context.Background()

	branch := "feat/shared-feature"

	_, err := mgr.ProvisionWorktreeWithOptions(ctx, "run-1", WorktreeOptions{
		Branch: branch,
	})
	if err != nil {
		t.Fatalf("first provision error: %v", err)
	}

	// Second run attempting same branch should fail
	_, err = mgr.ProvisionWorktreeWithOptions(ctx, "run-2", WorktreeOptions{
		Branch: branch,
	})
	if err == nil {
		t.Fatalf("expected branch collision error, got nil")
	}

	_ = mgr.Destroy(ctx, "run-1")
}

func TestMultitenancy_MaxConcurrentLimit(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
		MaxConcurrent:   2,
	})
	ctx := context.Background()

	_, err := mgr.ProvisionWorktreeWithOptions(ctx, "run-1", WorktreeOptions{Branch: "feat/branch-1"})
	if err != nil {
		t.Fatalf("unexpected error for run-1: %v", err)
	}
	_, err = mgr.ProvisionWorktreeWithOptions(ctx, "run-2", WorktreeOptions{Branch: "feat/branch-2"})
	if err != nil {
		t.Fatalf("unexpected error for run-2: %v", err)
	}

	if mgr.ActiveCount() != 2 {
		t.Fatalf("expected 2 active sandboxes, got %d", mgr.ActiveCount())
	}

	// 3rd run should exceed max concurrent
	_, err = mgr.ProvisionWorktreeWithOptions(ctx, "run-3", WorktreeOptions{Branch: "feat/branch-3"})
	if err == nil {
		t.Fatalf("expected error for exceeding max concurrent limit, got nil")
	}

	_ = mgr.Destroy(ctx, "run-1")
	_ = mgr.Destroy(ctx, "run-2")
}

func TestMultitenancy_ConcurrentProvisionAndDestroy(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const concurrentCount = 5
	var wg sync.WaitGroup
	errCh := make(chan error, concurrentCount*2)

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runID := fmt.Sprintf("concurrent-run-%d", id)
			branch := fmt.Sprintf("ov-run/branch-%d", id)

			env, err := mgr.ProvisionWorktreeWithOptions(ctx, runID, WorktreeOptions{
				Branch:      branch,
				MultiTenant: true,
			})
			if err != nil {
				errCh <- fmt.Errorf("run %d provision error: %w", id, err)
				return
			}

			// Ensure directory exists
			if _, err := os.Stat(env.WorktreePath); err != nil {
				errCh <- fmt.Errorf("run %d directory does not exist: %w", id, err)
				return
			}

			time.Sleep(50 * time.Millisecond)

			if err := mgr.Destroy(ctx, runID); err != nil {
				errCh <- fmt.Errorf("run %d destroy error: %w", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent test failure: %v", err)
	}

	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active runs, got %d", mgr.ActiveCount())
	}
}
