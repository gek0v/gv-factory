// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gv-factory/gv-factory/internal/registry"
)

func TestRegisterAndDeleteCustomTemplate_PackageFunctions(t *testing.T) {
	tempDir := t.TempDir()
	originalDir := registry.GetCustomTemplatesDir()
	registry.SetCustomTemplatesDir(tempDir)
	defer registry.SetCustomTemplatesDir(originalDir)

	tpl := registry.AgentTemplate{
		ID:           "custom_spec_pro",
		Name:         "Spec Architect Pro",
		Description:  "Designs robust specifications and architectural contracts",
		Role:         "spec_writer",
		Model:        "claude-3-7-sonnet",
		Thinking:     true,
		SystemPrompt: "You are a master architect.",
		Skills:       []string{"domain-modeling", "codebase-design"},
		Tools:        []string{"read", "ffgrep"},
	}

	// 1. Register custom template
	if err := registry.RegisterCustomTemplate(tpl); err != nil {
		t.Fatalf("unexpected error registering custom template: %v", err)
	}

	// 2. Verify file on disk
	filePath := filepath.Join(tempDir, "custom_spec_pro.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read persisted template file: %v", err)
	}

	var readTpl registry.AgentTemplate
	if err := json.Unmarshal(data, &readTpl); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if readTpl.ID != tpl.ID || readTpl.Name != tpl.Name || readTpl.Description != tpl.Description {
		t.Errorf("read template mismatch: %+v", readTpl)
	}
	if !readTpl.Thinking {
		t.Errorf("expected thinking=true, got %v", readTpl.Thinking)
	}

	// 3. Validation errors
	invalidCases := []struct {
		name string
		tpl  registry.AgentTemplate
	}{
		{"missing id", registry.AgentTemplate{Name: "Name", Role: "Role"}},
		{"missing name", registry.AgentTemplate{ID: "valid_id", Role: "Role"}},
		{"missing role", registry.AgentTemplate{ID: "valid_id", Name: "Name"}},
		{"path traversal dots", registry.AgentTemplate{ID: "../evil", Name: "Name", Role: "Role"}},
		{"path traversal slash", registry.AgentTemplate{ID: "sub/evil", Name: "Name", Role: "Role"}},
		{"path traversal backslash", registry.AgentTemplate{ID: "sub\\evil", Name: "Name", Role: "Role"}},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := registry.RegisterCustomTemplate(tc.tpl); err == nil {
				t.Errorf("expected error for case %q, got nil", tc.name)
			}
		})
	}

	// 4. Delete custom template
	if err := registry.DeleteCustomTemplate("custom_spec_pro"); err != nil {
		t.Fatalf("unexpected error deleting custom template: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected file %q to be deleted, but stat returned %v", filePath, err)
	}

	// 5. Delete non-existent
	if err := registry.DeleteCustomTemplate("custom_spec_pro"); err == nil {
		t.Errorf("expected error deleting non-existent template, got nil")
	}

	// 6. Delete invalid id
	if err := registry.DeleteCustomTemplate(""); err == nil {
		t.Errorf("expected error deleting with empty id, got nil")
	}
	if err := registry.DeleteCustomTemplate("../outside"); err == nil {
		t.Errorf("expected error deleting with path traversal, got nil")
	}
}

func TestRegisterAndDeleteCustomTemplate_ManagerAndRegistry(t *testing.T) {
	tempDir := t.TempDir()
	templatesDir := filepath.Join(tempDir, "templates")

	mgr := registry.NewManager(registry.ManagerOptions{
		WorkspaceRoot: tempDir,
		TemplatesDir:  templatesDir,
	})

	tpl := registry.AgentTemplate{
		ID:           "builder_v2",
		Name:         "Advanced TDD Builder",
		Description:  "TDD builder with enhanced refactoring heuristics",
		Role:         "tdd_builder",
		Model:        "claude-3-7-sonnet",
		Thinking:     true,
		SystemPrompt: "Refactor aggressively while keeping tests green.",
		Skills:       []string{"tdd", "codebase-design"},
		Tools:        []string{"read", "write", "edit", "bash"},
	}

	// Register via Manager
	if err := mgr.RegisterCustomTemplate(tpl); err != nil {
		t.Fatalf("failed to register template via manager: %v", err)
	}

	// Retrieve via Manager
	got, err := mgr.GetTemplate("builder_v2")
	if err != nil {
		t.Fatalf("failed to get template via manager: %v", err)
	}
	if got.Name != tpl.Name || got.Description != tpl.Description {
		t.Errorf("template mismatch: %+v", got)
	}

	// Verify disk file
	filePath := filepath.Join(templatesDir, "builder_v2.json")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected template file on disk at %s: %v", filePath, err)
	}

	// Delete via Manager
	if err := mgr.DeleteCustomTemplate("builder_v2"); err != nil {
		t.Fatalf("failed to delete template via manager: %v", err)
	}

	// Verify removed
	if _, err := mgr.GetTemplate("builder_v2"); err == nil {
		t.Errorf("expected template to be deleted from manager")
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected template file to be deleted from disk")
	}
}
