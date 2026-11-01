// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"testing"
	"time"
)

func TestExecutionContext(t *testing.T) {
	// Nil context
	if _, ok := ExecutionContextFromContext(nil); ok {
		t.Fatalf("expected no execution context from nil context")
	}

	ctx := context.Background()

	// Empty context
	if _, ok := ExecutionContextFromContext(ctx); ok {
		t.Fatalf("expected no execution context from empty context")
	}

	ec := ExecutionContext{
		RunID:     "run-123",
		CallerID:  "agent-alpha",
		SessionID: "sess-999",
		Metadata: map[string]any{
			"env": "production",
		},
	}

	ctxWithEC := WithExecutionContext(ctx, ec)
	extracted, ok := ExecutionContextFromContext(ctxWithEC)
	if !ok {
		t.Fatalf("expected execution context to be present")
	}

	if extracted.RunID != "run-123" {
		t.Errorf("expected RunID run-123, got %s", extracted.RunID)
	}
	if extracted.CallerID != "agent-alpha" {
		t.Errorf("expected CallerID agent-alpha, got %s", extracted.CallerID)
	}
	if extracted.SessionID != "sess-999" {
		t.Errorf("expected SessionID sess-999, got %s", extracted.SessionID)
	}
	if extracted.Metadata["env"] != "production" {
		t.Errorf("expected env production, got %v", extracted.Metadata["env"])
	}
}

func TestToolSecurityPolicy_Matches(t *testing.T) {
	tests := []struct {
		pattern  string
		toolName string
		matches  bool
	}{
		{"deploy_prod", "deploy_prod", true},
		{"deploy_prod", "deploy_staging", false},
		{"fs_*", "fs_write", true},
		{"fs_*", "fs_delete_all", true},
		{"fs_*", "exec_cmd", false},
		{"*delete*", "user_delete_item", true},
		{"*delete*", "user_read_item", false},
	}

	for _, tc := range tests {
		policy := ToolSecurityPolicy{
			NamePattern:      tc.pattern,
			RequiresApproval: true,
			ApprovalTimeout:  2 * time.Minute,
		}
		got := policy.Matches(tc.toolName)
		if got != tc.matches {
			t.Errorf("pattern %q on tool %q: expected match=%v, got=%v", tc.pattern, tc.toolName, tc.matches, got)
		}
	}
}
