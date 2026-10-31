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

func TestPrune(t *testing.T) {
	repoDir := createTestGitRepo(t)
	mgr := NewManager(Config{
		RepoPath: repoDir,
	})

	ctx := context.Background()
	if err := mgr.Prune(ctx); err != nil {
		t.Fatalf("expected Prune to succeed, got %v", err)
	}
}

func TestLifecycle_FullReclamation(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BasePort:        40000,
	})
	ctx := context.Background()
	runID := "run-lifecycle"

	// 1. Provision
	env, err := mgr.ProvisionWorktree(ctx, runID)
	if err != nil {
		t.Fatalf("unexpected provision error: %v", err)
	}

	// 2. Verify Isolated State
	if _, err := os.Stat(env.WorktreePath); err != nil {
		t.Fatalf("worktree path %q should exist, err: %v", env.WorktreePath, err)
	}

	branches := runGit(t, repoDir, "branch", "--list", "ov-run/"+runID)
	if !strings.Contains(branches, "ov-run/"+runID) {
		t.Fatalf("expected branch ov-run/%s to exist in git repo", runID)
	}

	worktrees := runGit(t, repoDir, "worktree", "list")
	if !strings.Contains(filepath.ToSlash(worktrees), filepath.ToSlash(env.WorktreePath)) {
		t.Fatalf("expected worktree %q to appear in git worktree list: %s", env.WorktreePath, worktrees)
	}

	// 3. Destroy
	if err := mgr.Destroy(ctx, runID); err != nil {
		t.Fatalf("unexpected destroy error: %v", err)
	}

	// 4. Verify Full Reclamation
	if _, err := os.Stat(env.WorktreePath); !os.IsNotExist(err) {
		t.Errorf("expected worktree path %q to be deleted, but stat err: %v", env.WorktreePath, err)
	}

	branchesAfter := runGit(t, repoDir, "branch", "--list", "ov-run/"+runID)
	if strings.TrimSpace(branchesAfter) != "" {
		t.Errorf("expected branch ov-run/%s to be deleted, got %q", runID, branchesAfter)
	}

	worktreesAfter := runGit(t, repoDir, "worktree", "list")
	if strings.Contains(filepath.ToSlash(worktreesAfter), filepath.ToSlash(env.WorktreePath)) {
		t.Errorf("expected worktree list to not contain %q, got: %s", env.WorktreePath, worktreesAfter)
	}

	mgr.mu.Lock()
	_, exists := mgr.activeRuns[runID]
	mgr.mu.Unlock()
	if exists {
		t.Errorf("expected active run %q to be removed from map", runID)
	}
}

func TestDestroy_EdgeCases(t *testing.T) {
	repoDir := createTestGitRepo(t)
	worktreeBase := t.TempDir()

	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BasePort:        40000,
	})
	ctx := context.Background()

	// 1. Empty runID error
	if err := mgr.Destroy(ctx, ""); err == nil {
		t.Errorf("expected error when destroying empty runID, got nil")
	}

	// 2. Non-existent runID succeeds safely without panic
	if err := mgr.Destroy(ctx, "non-existent-run"); err != nil {
		t.Errorf("expected destroying non-existent run to succeed safely, got %v", err)
	}

	// 3. Worktree directory manually deleted before Destroy
	env, err := mgr.ProvisionWorktree(ctx, "run-manual-del")
	if err != nil {
		t.Fatalf("unexpected provision error: %v", err)
	}

	if err := os.RemoveAll(env.WorktreePath); err != nil {
		t.Fatalf("failed to delete worktree dir: %v", err)
	}

	if err := mgr.Destroy(ctx, "run-manual-del"); err != nil {
		t.Errorf("expected Destroy to handle pre-deleted worktree cleanly, got %v", err)
	}

	branches := runGit(t, repoDir, "branch", "--list", "ov-run/run-manual-del")
	if strings.TrimSpace(branches) != "" {
		t.Errorf("expected branch to be deleted even if worktree dir was already gone")
	}
}

func TestMaskSecrets(t *testing.T) {
	input := "User logged in with password=secret123 and token=my-api-token-456"
	secrets := []string{"secret123", "my-api-token-456", ""}
	expected := "User logged in with password=*** and token=***"

	actual := MaskSecrets(input, secrets)
	if actual != expected {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func TestGetEnvironmentAndPath(t *testing.T) {
	repoDir := createTestGitRepo(t)
	mgr := NewManager(Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: t.TempDir(),
	})
	ctx := context.Background()

	// Before provisioning
	if _, ok := mgr.GetEnvironment("run-test-get"); ok {
		t.Errorf("expected GetEnvironment to return false before provision")
	}
	if _, ok := mgr.GetWorktreePath("run-test-get"); ok {
		t.Errorf("expected GetWorktreePath to return false before provision")
	}

	env, err := mgr.ProvisionWorktree(ctx, "run-test-get")
	if err != nil {
		t.Fatalf("unexpected provision error: %v", err)
	}
	defer mgr.Destroy(ctx, "run-test-get")

	gotEnv, ok := mgr.GetEnvironment("run-test-get")
	if !ok || gotEnv.WorktreePath != env.WorktreePath {
		t.Errorf("GetEnvironment mismatch: got %v, want %v", gotEnv, env)
	}

	path, ok := mgr.GetWorktreePath("run-test-get")
	if !ok || path != env.WorktreePath {
		t.Errorf("GetWorktreePath mismatch: got %q, want %q", path, env.WorktreePath)
	}
}
