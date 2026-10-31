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
	"time"
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
	MaxConcurrent   int
}

// Environment represents an isolated runtime environment for a factory run.
type Environment struct {
	RunID        string    `json:"run_id"`
	FactoryIndex int       `json:"factory_index"`
	WorktreePath string    `json:"worktree_path"`
	BasePort     int       `json:"base_port"`
	Branch       string    `json:"branch"`
	CreatedAt    time.Time `json:"created_at"`
}

// WorktreeOptions configures isolated worktree allocation.
type WorktreeOptions struct {
	Branch       string // Target transient or feature branch
	BaseBranch   string // Base branch to branch off, defaults to Config.BaseBranch
	WorktreePath string // Explicit worktree path (optional override)
	MultiTenant  bool   // Whether to allocate path as ov-factory/runs/<run_id>/<branch>
}

var (
	repoLocksMu sync.Mutex
	repoLocks   = make(map[string]*sync.Mutex)
)

func getRepoLock(repoPath string) *sync.Mutex {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	repoLocksMu.Lock()
	defer repoLocksMu.Unlock()
	l, ok := repoLocks[abs]
	if !ok {
		l = &sync.Mutex{}
		repoLocks[abs] = l
	}
	return l
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
	return m.ProvisionWorktreeWithOptions(ctx, runID, WorktreeOptions{})
}

// ProvisionWorktreeWithOptions allocates an isolated worktree directory with custom branch and multitenancy options.
func (m *Manager) ProvisionWorktreeWithOptions(ctx context.Context, runID string, opts WorktreeOptions) (*Environment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if runID == "" {
		return nil, fmt.Errorf("runID cannot be empty")
	}

	if _, exists := m.activeRuns[runID]; exists {
		return nil, fmt.Errorf("run %q is already active", runID)
	}

	if m.config.MaxConcurrent > 0 && len(m.activeRuns) >= m.config.MaxConcurrent {
		return nil, fmt.Errorf("maximum concurrent sandboxes limit (%d) reached", m.config.MaxConcurrent)
	}

	branch := opts.Branch
	if branch == "" {
		branch = fmt.Sprintf("ov-run/%s", runID)
	}

	for _, r := range m.activeRuns {
		if r.Branch == branch {
			return nil, fmt.Errorf("branch %q is already in use by active run %q", branch, r.RunID)
		}
	}

	baseBranch := opts.BaseBranch
	if baseBranch == "" {
		baseBranch = m.config.BaseBranch
	}

	var worktreePath string
	if opts.WorktreePath != "" {
		worktreePath = opts.WorktreePath
	} else if opts.MultiTenant || opts.Branch != "" {
		cleanedBranch := filepath.FromSlash(strings.TrimPrefix(branch, "refs/heads/"))
		worktreePath = filepath.Join(m.config.BaseWorktreeDir, "ov-factory", "runs", runID, cleanedBranch)
	} else {
		worktreePath = filepath.Join(m.config.BaseWorktreeDir, runID)
	}

	repoLock := getRepoLock(m.config.RepoPath)
	repoLock.Lock()
	defer repoLock.Unlock()

	// 1. Pre-flight lock cleanup
	if err := CleanStaleLocks(m.config.RepoPath); err != nil {
		return nil, fmt.Errorf("pre-flight lock cleanup failed: %w", err)
	}

	// 2. Prepare paths
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create base worktree directory: %w", err)
	}

	// 3. Determine if branch already exists in repo
	checkCmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branch)
	checkCmd.Dir = m.config.RepoPath
	branchExists := checkCmd.Run() == nil

	// 4. Execute git worktree add with retry and lock recovery
	var out []byte
	var addErr error
	for attempt := 0; attempt < 3; attempt++ {
		var cmd *exec.Cmd
		if branchExists {
			cmd = exec.CommandContext(ctx, "git", "worktree", "add", worktreePath, branch)
		} else {
			cmd = exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, worktreePath, baseBranch)
		}
		cmd.Dir = m.config.RepoPath
		out, addErr = cmd.CombinedOutput()
		if addErr == nil {
			break
		}

		_ = os.RemoveAll(worktreePath)
		_ = CleanStaleLocks(m.config.RepoPath)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(50*(attempt+1)) * time.Millisecond):
		}
	}

	if addErr != nil {
		_ = os.RemoveAll(worktreePath)
		_ = CleanStaleLocks(m.config.RepoPath)
		return nil, fmt.Errorf("failed to provision git worktree: %w, output: %s", addErr, string(out))
	}

	// 5. Validate worktree directory exists and is cleanly attached
	info, err := os.Stat(worktreePath)
	if err != nil || !info.IsDir() {
		_ = exec.CommandContext(ctx, "git", "-C", m.config.RepoPath, "worktree", "remove", "--force", worktreePath).Run()
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("worktree directory was not created successfully")
	}

	// 6. Verify that active branch is locked to branch and not base branch
	checkBranchCmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := checkBranchCmd.CombinedOutput()
	if err != nil {
		_ = exec.CommandContext(ctx, "git", "-C", m.config.RepoPath, "worktree", "remove", "--force", worktreePath).Run()
		_ = os.RemoveAll(worktreePath)
		return nil, fmt.Errorf("failed to verify worktree branch: %w", err)
	}

	activeBranch := strings.TrimSpace(string(branchOut))
	expectedShortBranch := strings.TrimPrefix(branch, "refs/heads/")
	if activeBranch != expectedShortBranch && activeBranch != branch {
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
		Branch:       branch,
		CreatedAt:    time.Now(),
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
	branch := fmt.Sprintf("ov-run/%s", runID)
	if exists {
		if env.WorktreePath != "" {
			worktreePath = env.WorktreePath
		}
		if env.Branch != "" {
			branch = env.Branch
		}
	}

	repoLock := getRepoLock(m.config.RepoPath)
	repoLock.Lock()
	defer repoLock.Unlock()

	// 1. Safely remove the worktree via git worktree remove --force <worktree_path>
	cmdRemove := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", worktreePath)
	cmdRemove.Dir = m.config.RepoPath
	_ = cmdRemove.Run()

	// Guarantee disk cleanup if git worktree remove left residual files
	_ = os.RemoveAll(worktreePath)

	// Clean multi-tenant run directory if created
	runDir := filepath.Join(m.config.BaseWorktreeDir, "ov-factory", "runs", runID)
	_ = os.RemoveAll(runDir)

	// 2. Delete the associated transient branch ov-run/<run_id>
	if strings.HasPrefix(branch, "ov-run/") {
		cmdBranch := exec.CommandContext(ctx, "git", "branch", "-D", branch)
		cmdBranch.Dir = m.config.RepoPath
		_ = cmdBranch.Run()
	}

	// 3. Execute git worktree prune to clean administrative metadata
	_ = m.prune(ctx)

	// 4. Clean up internal state map
	delete(m.activeRuns, runID)
	return nil
}

// RecycleWorktree cleans up and recycles a worktree environment.
func (m *Manager) RecycleWorktree(ctx context.Context, runID string) error {
	return m.Destroy(ctx, runID)
}

// ActiveCount returns the number of currently active sandboxes.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.activeRuns)
}

// ActiveRuns returns a snapshot slice of all active environments.
func (m *Manager) ActiveRuns() []*Environment {
	m.mu.Lock()
	defer m.mu.Unlock()
	runs := make([]*Environment, 0, len(m.activeRuns))
	for _, env := range m.activeRuns {
		runs = append(runs, env)
	}
	return runs
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
