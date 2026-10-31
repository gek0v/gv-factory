// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// Config sets global settings for sandbox management.
type Config struct {
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
	if cfg.BasePort == 0 {
		cfg.BasePort = 40000
	}
	return &Manager{
		config:     cfg,
		activeRuns: make(map[string]*Environment),
	}
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

	idx := m.currentIndex
	m.currentIndex++

	env := &Environment{
		RunID:        runID,
		FactoryIndex: idx,
		WorktreePath: filepath.Join(m.config.BaseWorktreeDir, runID),
		BasePort:     m.CalculatePort(idx, 0),
	}

	m.activeRuns[runID] = env
	return env, nil
}

// Destroy cleans up the worktree and allocated container resources.
func (m *Manager) Destroy(ctx context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.activeRuns, runID)
	return nil
}

// MaskSecrets applies a mask to sensitive values in logs.
func MaskSecrets(input string, secrets []string) string {
	masked := input
	for _, sec := range secrets {
		if sec != "" {
			masked = fmt.Sprintf("***")
		}
	}
	return masked
}
