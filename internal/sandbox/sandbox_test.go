// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	t.Setenv("OV_WORKTREE_DIR", "")

	mgr := NewManager(Config{})
	expected := filepath.Join(os.TempDir(), "ov-worktrees")
	if mgr.BaseWorktreeDir() != expected {
		t.Fatalf("expected default worktree dir %q, got %q", expected, mgr.BaseWorktreeDir())
	}
}

func TestEnvOverride(t *testing.T) {
	customDir := filepath.Join(os.TempDir(), "custom-env-worktrees")
	t.Setenv("OV_WORKTREE_DIR", customDir)

	mgr := NewManager(Config{})
	if mgr.BaseWorktreeDir() != customDir {
		t.Fatalf("expected worktree dir from env %q, got %q", customDir, mgr.BaseWorktreeDir())
	}
}

func TestExplicitConfigOverride(t *testing.T) {
	t.Setenv("OV_WORKTREE_DIR", filepath.Join(os.TempDir(), "env-worktrees"))

	explicitDir := filepath.Join(os.TempDir(), "explicit-worktrees")
	mgr := NewManager(Config{BaseWorktreeDir: explicitDir})
	if mgr.BaseWorktreeDir() != explicitDir {
		t.Fatalf("expected explicit worktree dir %q, got %q", explicitDir, mgr.BaseWorktreeDir())
	}
}

func TestCleanStaleLocks(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	worktreesDir := filepath.Join(gitDir, "worktrees", "wt-test")

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatalf("failed to create fixture dirs: %v", err)
	}

	mainLock := filepath.Join(gitDir, "index.lock")
	wtLock := filepath.Join(worktreesDir, "index.lock")
	gitConfig := filepath.Join(gitDir, "config")

	if err := os.WriteFile(mainLock, []byte("lock"), 0644); err != nil {
		t.Fatalf("failed to write main lock: %v", err)
	}
	if err := os.WriteFile(wtLock, []byte("lock"), 0644); err != nil {
		t.Fatalf("failed to write worktree lock: %v", err)
	}
	if err := os.WriteFile(gitConfig, []byte("[core]"), 0644); err != nil {
		t.Fatalf("failed to write git config: %v", err)
	}

	if err := CleanStaleLocks(tempDir); err != nil {
		t.Fatalf("CleanStaleLocks failed: %v", err)
	}

	if _, err := os.Stat(mainLock); !os.IsNotExist(err) {
		t.Errorf("expected main lock to be removed, but stat err: %v", err)
	}
	if _, err := os.Stat(wtLock); !os.IsNotExist(err) {
		t.Errorf("expected worktree lock to be removed, but stat err: %v", err)
	}
	if _, err := os.Stat(gitConfig); err != nil {
		t.Errorf("expected git config to still exist: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed in %s: %v, output: %s", args, dir, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func createTestGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()

	runGit(t, repoDir, "init")
	runGit(t, repoDir, "checkout", "-B", "main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")

	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "initial commit")

	return repoDir
}

func TestProvisionWorktree_GitIntegration(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	// Place a stale lock to test that ProvisionWorktree cleans it up in pre-flight
	staleLock := filepath.Join(repoDir, ".git", "index.lock")
	if err := os.WriteFile(staleLock, []byte("stale"), 0644); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseBranch:      "main",
		BaseWorktreeDir: worktreeBase,
		BasePort:        40000,
	})

	ctx := context.Background()
	env, err := mgr.ProvisionWorktree(ctx, "run-abc")
	if err != nil {
		t.Fatalf("unexpected error provisioning worktree: %v", err)
	}

	// 1. Directory must exist
	info, err := os.Stat(env.WorktreePath)
	if err != nil {
		t.Fatalf("worktree path does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("worktree path is not a directory")
	}

	// 2. Active branch must be ov-run/run-abc and not main
	branch := runGit(t, env.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "ov-run/run-abc" {
		t.Errorf("expected branch ov-run/run-abc, got %q", branch)
	}

	// 3. Stale lock should have been cleaned
	if _, err := os.Stat(staleLock); !os.IsNotExist(err) {
		t.Errorf("expected stale lock to be cleaned up, but it still exists")
	}
}

func TestCalculatePort(t *testing.T) {
	mgr := NewManager(Config{BasePort: 40000})

	// Formula: 40000 + (Factory_Index * 10) + Offset
	port0 := mgr.CalculatePort(0, 0)
	if port0 != 40000 {
		t.Fatalf("expected port 40000, got %d", port0)
	}

	port1 := mgr.CalculatePort(1, 3)
	if port1 != 40013 {
		t.Fatalf("expected port 40013, got %d", port1)
	}
}

func TestProvisionAndDestroy(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BasePort:        40000,
	})
	ctx := context.Background()

	env, err := mgr.ProvisionWorktree(ctx, "run-101")
	if err != nil {
		t.Fatalf("unexpected error provisioning worktree: %v", err)
	}

	if env.FactoryIndex != 0 {
		t.Errorf("expected factory index 0, got %d", env.FactoryIndex)
	}

	expectedPath := filepath.Join(worktreeBase, "run-101")
	if env.WorktreePath != expectedPath {
		t.Errorf("expected worktree path %q, got %q", expectedPath, env.WorktreePath)
	}

	if err := mgr.Destroy(ctx, "run-101"); err != nil {
		t.Fatalf("unexpected error destroying worktree: %v", err)
	}
}

func TestProvisionWorktree_RollbackOnFailure(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	// Configure with invalid BaseBranch to force git worktree add to fail
	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseBranch:      "non-existent-branch",
		BaseWorktreeDir: worktreeBase,
		BasePort:        40000,
	})

	ctx := context.Background()
	env, err := mgr.ProvisionWorktree(ctx, "fail-run")
	if err == nil {
		t.Fatalf("expected error provisioning with invalid branch, got nil")
	}
	if env != nil {
		t.Errorf("expected nil environment on error, got %v", env)
	}

	// Verify target worktree path was rolled back and does not exist
	targetPath := filepath.Join(worktreeBase, "fail-run")
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("expected target path %q to not exist, but err is: %v", targetPath, err)
	}

	// Verify no active run registered
	if _, exists := mgr.activeRuns["fail-run"]; exists {
		t.Errorf("expected fail-run to not be in active runs")
	}
}

func TestProvisionWorktree_ValidationErrors(t *testing.T) {
	repoDir := createTestGitRepo(t)
	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: t.TempDir(),
	})
	ctx := context.Background()

	// Empty runID
	if _, err := mgr.ProvisionWorktree(ctx, ""); err == nil {
		t.Errorf("expected error for empty runID, got nil")
	}

	// First provision
	if _, err := mgr.ProvisionWorktree(ctx, "dup-run"); err != nil {
		t.Fatalf("unexpected error on first provision: %v", err)
	}

	// Duplicate runID
	if _, err := mgr.ProvisionWorktree(ctx, "dup-run"); err == nil {
		t.Errorf("expected error for duplicate runID, got nil")
	}
}
