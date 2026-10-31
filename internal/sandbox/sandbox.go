// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// CleanStaleLocks removes orphaned .git/index.lock and .git/worktrees/*/index.lock files.
func CleanStaleLocks(repoPath string) error {
	gitDir := filepath.Join(repoPath, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	actualGitDir := gitDir
	if !info.IsDir() {
		content, err := os.ReadFile(gitDir)
		if err != nil {
			return err
		}
		trimmed := strings.TrimSpace(string(content))
		if strings.HasPrefix(trimmed, "gitdir:") {
			target := strings.TrimSpace(strings.TrimPrefix(trimmed, "gitdir:"))
			if !filepath.IsAbs(target) {
				target = filepath.Join(repoPath, target)
			}
			actualGitDir = target
		}
	}

	mainLock := filepath.Join(actualGitDir, "index.lock")
	if err := os.Remove(mainLock); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove index.lock: %w", err)
	}

	worktreesDir := filepath.Join(actualGitDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				wtLock := filepath.Join(worktreesDir, entry.Name(), "index.lock")
				if err := os.Remove(wtLock); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove worktree lock for %s: %w", entry.Name(), err)
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// DefaultWorktreeDir returns the default base directory for worktrees.
// It checks the OV_WORKTREE_DIR environment variable and falls back to
// filepath.Join(os.TempDir(), "ov-worktrees").
func DefaultWorktreeDir() string {
	if env := os.Getenv("OV_WORKTREE_DIR"); env != "" {
		return env
	}
	return filepath.Join(os.TempDir(), "ov-worktrees")
}

// Config sets global settings for sandbox management.
type Config struct {
	RepoPath        string
	BaseBranch      string
	BaseWorktreeDir string
	BasePort        int
}

// Environment represents an isolated runtime environment for a factory run.
type Environment struct {
	RunID        string
	FactoryIndex int
	WorktreePath string
	BasePort     int
}

// Manager controls the lifecycle of Git Worktrees and rootless containers.
type Manager struct {
	mu           sync.Mutex
	config       Config
	activeRuns   map[string]*Environment
	currentIndex int
}

// NewManager creates a sandbox manager instance.
func NewManager(cfg Config) *Manager {
	if cfg.RepoPath == "" {
		cfg.RepoPath = "."
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if cfg.BaseWorktreeDir == "" {
		cfg.BaseWorktreeDir = DefaultWorktreeDir()
	}
	if cfg.BasePort == 0 {
		cfg.BasePort = 40000
	}
	return &Manager{
		config:     cfg,
		activeRuns: make(map[string]*Environment),
	}
}

// Config returns a copy of the manager's configuration.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config
}

// BaseWorktreeDir returns the configured base directory for worktrees.
func (m *Manager) BaseWorktreeDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config.BaseWorktreeDir
}

// CalculatePort implements the deterministic formula:
// P_service = 40000 + (Factory_Index * 10) + Offset_service
func (m *Manager) CalculatePort(factoryIndex int, offset int) int {
	return m.config.BasePort + (factoryIndex * 10) + offset
}

// ProvisionWorktree allocates an isolated worktree directory for a run.
func (m *Manager) ProvisionWorktree(ctx context.Context, runID string) (*Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if runID == "" {
		return nil, fmt.Errorf("runID cannot be empty")
	}

	if _, exists := m.activeRuns[runID]; exists {
		return nil, fmt.Errorf("run %q is already active", runID)
	}

	// 1. Pre-flight lock cleanup
	if err := CleanStaleLocks(m.config.RepoPath); err != nil {
		return nil, fmt.Errorf("pre-flight lock cleanup failed: %w", err)
	}

	// 2. Prepare paths and branch name
	worktreePath := filepath.Join(m.config.BaseWorktreeDir, runID)
	branch := fmt.Sprintf("ov-run/%s", runID)

	if err := os.MkdirAll(m.config.BaseWorktreeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base worktree directory: %w", err)
	}

	// 3. Execute git worktree add -b ov-run/<run_id> <worktree_path> <base_branch>
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, worktreePath, m.config.BaseBranch)
	cmd.Dir = m.config.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		// Rollback partial directory if created
		_ = os.RemoveAll(worktreePath)
		_ = CleanStaleLocks(m.config.RepoPath)
		return nil, fmt.Errorf("failed to provision git worktree: %w, output: %s", err, string(out))
	}

	// 4. Validate worktree directory exists and is cleanly attached
	info, err := os.Stat(worktreePath)
	if err != nil || !info.IsDir() {
		_ = exec.CommandContext(ctx, "git", "-C", m.config.RepoPath, "worktree", "remove", "--force", worktreePath).Run()
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("worktree directory was not created successfully")
	}

	// 5. Verify that active branch is locked to ov-run/<run_id> and not main
	checkBranchCmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := checkBranchCmd.CombinedOutput()
	if err != nil {
		_ = exec.CommandContext(ctx, "git", "-C", m.config.RepoPath, "worktree", "remove", "--force", worktreePath).Run()
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("failed to verify worktree branch: %w", err)
	}

	activeBranch := strings.TrimSpace(string(branchOut))
	if activeBranch != branch {
		_ = exec.CommandContext(ctx, "git", "-C", m.config.RepoPath, "worktree", "remove", "--force", worktreePath).Run()
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("worktree is attached to branch %q, expected %q", activeBranch, branch)
	}
	if activeBranch == m.config.BaseBranch {
		_ = exec.CommandContext(ctx, "git", "-C", m.config.RepoPath, "worktree", "remove", "--force", worktreePath).Run()
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("worktree cannot be on base branch %q", m.config.BaseBranch)
	}

	idx := m.currentIndex
	m.currentIndex++

	env := &Environment{
		RunID:        runID,
		FactoryIndex: idx,
		WorktreePath: worktreePath,
		BasePort:     m.CalculatePort(idx, 0),
	}

	m.activeRuns[runID] = env
	return env, nil
}

// Destroy cleans up the worktree and allocated container resources.
func (m *Manager) Destroy(ctx context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if runID == "" {
		return fmt.Errorf("runID cannot be empty")
	}

	env, exists := m.activeRuns[runID]
	worktreePath := filepath.Join(m.config.BaseWorktreeDir, runID)
	if exists && env.WorktreePath != "" {
		worktreePath = env.WorktreePath
	}
	branch := fmt.Sprintf("ov-run/%s", runID)

	// 1. Safely remove the worktree via git worktree remove --force <worktree_path>
	cmdRemove := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", worktreePath)
	cmdRemove.Dir = m.config.RepoPath
	_ = cmdRemove.Run()

	// Guarantee disk cleanup if git worktree remove left residual files
	_ = os.RemoveAll(worktreePath)

	// 2. Delete the associated transient branch ov-run/<run_id>
	cmdBranch := exec.CommandContext(ctx, "git", "branch", "-D", branch)
	cmdBranch.Dir = m.config.RepoPath
	_ = cmdBranch.Run()

	// 3. Execute git worktree prune to clean administrative metadata
	_ = m.prune(ctx)

	// 4. Clean up internal state map
	delete(m.activeRuns, runID)
	return nil
}

func (m *Manager) prune(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	cmd.Dir = m.config.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune failed: %w, output: %s", err, string(out))
	}
	return nil
}

// Prune cleans up dangling worktree administrative metadata.
func (m *Manager) Prune(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prune(ctx)
}

// GetEnvironment retrieves an active environment by its runID.
func (m *Manager) GetEnvironment(runID string) (*Environment, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	env, ok := m.activeRuns[runID]
	return env, ok
}

// GetWorktreePath returns the worktree path for a given runID if it exists.
func (m *Manager) GetWorktreePath(runID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	env, ok := m.activeRuns[runID]
	if !ok || env.WorktreePath == "" {
		return "", false
	}
	return env.WorktreePath, true
}

// MaskSecrets applies a mask to sensitive values in logs.
func MaskSecrets(input string, secrets []string) string {
	masked := input
	for _, sec := range secrets {
		if sec != "" {
			masked = strings.ReplaceAll(masked, sec, "***")
		}
	}
	return masked
}
