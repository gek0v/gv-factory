// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"context"
	"os"
	"path/filepath"
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
	t.Setenv("OV_WORKTREE_DIR", "")
	mgr := NewManager(Config{BasePort: 40000})
	ctx := context.Background()

	env, err := mgr.ProvisionWorktree(ctx, "run-101")
	if err != nil {
		t.Fatalf("unexpected error provisioning worktree: %v", err)
	}

	if env.FactoryIndex != 0 {
		t.Errorf("expected factory index 0, got %d", env.FactoryIndex)
	}

	expectedPath := filepath.Join(os.TempDir(), "ov-worktrees", "run-101")
	if env.WorktreePath != expectedPath {
		t.Errorf("expected worktree path %q, got %q", expectedPath, env.WorktreePath)
	}

	if err := mgr.Destroy(ctx, "run-101"); err != nil {
		t.Fatalf("unexpected error destroying worktree: %v", err)
	}
}
