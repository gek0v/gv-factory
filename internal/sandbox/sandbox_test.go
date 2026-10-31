// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"context"
	"testing"
)

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
	mgr := NewManager(Config{BaseWorktreeDir: "/tmp/ov-worktrees", BasePort: 40000})
	ctx := context.Background()

	env, err := mgr.ProvisionWorktree(ctx, "run-101")
	if err != nil {
		t.Fatalf("unexpected error provisioning worktree: %v", err)
	}

	if env.FactoryIndex != 0 {
		t.Errorf("expected factory index 0, got %d", env.FactoryIndex)
	}

	if err := mgr.Destroy(ctx, "run-101"); err != nil {
		t.Fatalf("unexpected error destroying worktree: %v", err)
	}
}
