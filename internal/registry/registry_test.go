// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gv-factory/gv-factory/internal/registry"
)

func TestSkillsRegistry_DiscoveryAndPrecedence(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Setup workspace skills
	wsAgentsDir := filepath.Join(tempDir, "workspace", ".agents", "skills", "test-skill-1")
	if err := os.MkdirAll(wsAgentsDir, 0o755); err != nil {
		t.Fatalf("failed to create ws dir: %v", err)
	}
	skillContent := `---
name: test-skill-1
description: A workspace skill for testing
---
# Test Skill 1
This is a test skill.
`
	if err := os.WriteFile(filepath.Join(wsAgentsDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	// 2. Standalone .md skill
	wsPiDir := filepath.Join(tempDir, "workspace", ".pi", "skills")
	if err := os.MkdirAll(wsPiDir, 0o755); err != nil {
		t.Fatalf("failed to create pi skills dir: %v", err)
	}
	standaloneContent := `---
name: standalone-skill
description: A standalone markdown skill
---
`
	if err := os.WriteFile(filepath.Join(wsPiDir, "standalone-skill.md"), []byte(standaloneContent), 0o644); err != nil {
		t.Fatalf("failed to write standalone skill: %v", err)
	}

	// 3. User skill (overridden by workspace if same name)
	userDir := filepath.Join(tempDir, "user", ".agents", "skills", "test-skill-1")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("failed to create user dir: %v", err)
	}
	userSkillContent := `---
name: test-skill-1
description: A user skill that should be overridden
---
`
	if err := os.WriteFile(filepath.Join(userDir, "skill.md"), []byte(userSkillContent), 0o644); err != nil {
		t.Fatalf("failed to write user skill file: %v", err)
	}

	// 4. User-only skill with fallback description (no frontmatter)
	userDir2 := filepath.Join(tempDir, "user", ".agents", "skills", "fallback-skill")
	if err := os.MkdirAll(userDir2, 0o755); err != nil {
		t.Fatalf("failed to create user dir 2: %v", err)
	}
	fallbackContent := `# Fallback Skill Title

This is the extracted fallback description line for testing.
`
	if err := os.WriteFile(filepath.Join(userDir2, "SKILL.md"), []byte(fallbackContent), 0o644); err != nil {
		t.Fatalf("failed to write fallback skill: %v", err)
	}

	reg := registry.NewSkillsRegistryWithOptions(registry.SkillsRegistryOptions{
		WorkspaceRoot:   filepath.Join(tempDir, "workspace"),
		IncludeUserHome: false,
		ExtraPaths: []registry.SkillSearchPath{
			{Path: filepath.Join(tempDir, "user", ".agents", "skills"), Source: "user"},
		},
	})

	ctx := context.Background()
	skills, err := reg.Discover(ctx)
	if err != nil {
		t.Fatalf("expected no error on discover, got %v", err)
	}

	if len(skills) < 3 {
		t.Fatalf("expected at least 3 skills, got %d", len(skills))
	}

	// Verify test-skill-1 comes from workspace
	skill1, err := reg.Get(ctx, "test-skill-1")
	if err != nil {
		t.Fatalf("failed to get test-skill-1: %v", err)
	}
	if skill1.Source != "workspace" {
		t.Errorf("expected source workspace, got %s", skill1.Source)
	}
	if skill1.Description != "A workspace skill for testing" {
		t.Errorf("unexpected description: %s", skill1.Description)
	}

	// Verify standalone-skill
	skill2, err := reg.Get(ctx, "standalone-skill")
	if err != nil {
		t.Fatalf("failed to get standalone-skill: %v", err)
	}
	if skill2.Description != "A standalone markdown skill" {
		t.Errorf("unexpected description: %s", skill2.Description)
	}

	// Verify fallback skill description
	fallbackSkill, err := reg.Get(ctx, "fallback-skill")
	if err != nil {
		t.Fatalf("failed to get fallback-skill: %v", err)
	}
	if fallbackSkill.Description != "This is the extracted fallback description line for testing." {
		t.Errorf("unexpected fallback description: %q", fallbackSkill.Description)
	}

	// Register custom skill
	reg.Register(registry.SkillItem{
		Name:        "custom-mock",
		Description: "A dynamically registered skill",
		Path:        "/opt/custom",
	})

	customSkill, err := reg.Get(ctx, "custom-mock")
	if err != nil {
		t.Fatalf("failed to get custom-mock: %v", err)
	}
	if customSkill.Source != "custom" {
		t.Errorf("expected source custom, got %s", customSkill.Source)
	}

	// Test AddSearchPath
	extraSearchDir := filepath.Join(tempDir, "extra-skills")
	_ = os.MkdirAll(extraSearchDir, 0o755)
	reg.AddSearchPath(extraSearchDir, "custom")

	// Call default constructor to cover NewSkillsRegistry
	defaultReg := registry.NewSkillsRegistry(tempDir)
	if defaultReg == nil {
		t.Errorf("expected non-nil default registry")
	}

	// Test List
	allList, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("failed to list skills: %v", err)
	}
	if len(allList) != 4 {
		t.Errorf("expected 4 skills in list, got %d", len(allList))
	}

	// Test Get not found
	_, err = reg.Get(ctx, "non-existent")
	if err == nil {
		t.Errorf("expected error for non-existent skill")
	}

	// Test context cancelled
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = reg.Discover(cancelCtx)
	if err == nil {
		t.Errorf("expected context cancellation error")
	}
}

func TestToolsRegistry_DefaultsAndCustom(t *testing.T) {
	reg := registry.NewToolsRegistry()

	// Verify required built-ins
	builtins := []string{"read", "write", "edit", "bash", "todo", "ffgrep", "fffind"}
	for _, name := range builtins {
		tool, err := reg.Get(name)
		if err != nil {
			t.Errorf("expected built-in tool %q to exist: %v", name, err)
			continue
		}
		if tool.Category != "builtin" {
			t.Errorf("expected category builtin for %q, got %s", name, tool.Category)
		}
		if tool.ParametersSchema == nil {
			t.Errorf("expected ParametersSchema for %q", name)
		}
	}

	// Verify required Cerebro MCP tools
	brainTools := []string{"brain_search", "brain_record_episode", "brain_list_rules", "git_diff"}
	for _, name := range brainTools {
		tool, err := reg.Get(name)
		if err != nil {
			t.Errorf("expected brain tool %q to exist: %v", name, err)
			continue
		}
		if tool.Category != "brain" {
			t.Errorf("expected category brain for %q, got %s", name, tool.Category)
		}
	}

	// Test List
	allTools := reg.List()
	if len(allTools) != 11 {
		t.Errorf("expected 11 default tools, got %d", len(allTools))
	}

	// Test ListByCategory
	builtinTools := reg.ListByCategory("builtin")
	if len(builtinTools) != 7 {
		t.Errorf("expected 7 builtin tools, got %d", len(builtinTools))
	}
	brainCategory := reg.ListByCategory("brain")
	if len(brainCategory) != 4 {
		t.Errorf("expected 4 brain tools, got %d", len(brainCategory))
	}

	// Register custom tool
	err := reg.Register(registry.ToolItem{
		Name:        "custom_runner",
		Description: "Runs custom external scripts",
	})
	if err != nil {
		t.Fatalf("failed to register custom tool: %v", err)
	}

	customTool, err := reg.Get("custom_runner")
	if err != nil {
		t.Fatalf("failed to get custom_runner: %v", err)
	}
	if customTool.Category != "custom" {
		t.Errorf("expected category custom, got %s", customTool.Category)
	}
	if customTool.ParametersSchema == nil {
		t.Errorf("expected default schema for custom_runner")
	}

	// Validation on empty name
	if err := reg.Register(registry.ToolItem{}); err == nil {
		t.Errorf("expected error registering tool with empty name")
	}

	// Tool not found
	if _, err := reg.Get("non_existent_tool"); err == nil {
		t.Errorf("expected error for non existent tool")
	}
}

func TestPluginsRegistry_DiscoveryAndLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	extDir := filepath.Join(tempDir, ".pi", "extensions")

	// 1. Extension with plugin.json manifest
	p1Dir := filepath.Join(extDir, "my-wasm-plugin")
	if err := os.MkdirAll(p1Dir, 0o755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	manifest := map[string]any{
		"id":          "my-wasm-plugin",
		"name":        "My WASM Plugin",
		"description": "A high-performance WASM plugin",
		"type":        "wasm",
		"enabled":     true,
	}
	mBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(p1Dir, "plugin.json"), mBytes, 0o644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// 2. Single-file extension
	if err := os.WriteFile(filepath.Join(extDir, "ts-runner.ts"), []byte("// ts extension"), 0o644); err != nil {
		t.Fatalf("failed to write single ts file: %v", err)
	}

	// 3. Single-file wasm extension
	if err := os.WriteFile(filepath.Join(extDir, "fast-hasher.wasm"), []byte{0x00, 0x61, 0x73, 0x6d}, 0o644); err != nil {
		t.Fatalf("failed to write single wasm file: %v", err)
	}

	// 4. Directory without manifest containing a wasm file
	wasmSubDir := filepath.Join(extDir, "auto-wasm")
	_ = os.MkdirAll(wasmSubDir, 0o755)
	_ = os.WriteFile(filepath.Join(wasmSubDir, "module.wasm"), []byte{0x00, 0x61, 0x73, 0x6d}, 0o644)

	// 5. Directory without manifest containing a ts file
	tsSubDir := filepath.Join(extDir, "auto-ts")
	_ = os.MkdirAll(tsSubDir, 0o755)
	_ = os.WriteFile(filepath.Join(tsSubDir, "index.ts"), []byte("// ts code"), 0o644)

	reg := registry.NewPluginsRegistryWithOptions(registry.PluginsRegistryOptions{
		WorkspaceRoot:   tempDir,
		IncludeUserHome: false,
	})
	ctx := context.Background()

	// Cover AddSearchPath and default constructor
	reg.AddSearchPath(filepath.Join(tempDir, "extra-plugins"), "custom")
	defaultPluginsReg := registry.NewPluginsRegistry(tempDir)
	if defaultPluginsReg == nil {
		t.Errorf("expected default plugins registry non-nil")
	}

	plugins, err := reg.Discover(ctx)
	if err != nil {
		t.Fatalf("failed to discover plugins: %v", err)
	}
	if len(plugins) < 3 {
		t.Fatalf("expected at least 3 plugins, got %d", len(plugins))
	}

	// Verify my-wasm-plugin
	p1, err := reg.Get(ctx, "my-wasm-plugin")
	if err != nil {
		t.Fatalf("failed to get my-wasm-plugin: %v", err)
	}
	if p1.Type != "wasm" || !p1.Enabled {
		t.Errorf("unexpected plugin properties: %+v", p1)
	}

	// Toggle enabled
	if err := reg.SetEnabled("my-wasm-plugin", false); err != nil {
		t.Fatalf("failed to set enabled: %v", err)
	}
	p1Updated, _ := reg.Get(ctx, "my-wasm-plugin")
	if p1Updated.Enabled {
		t.Errorf("expected plugin to be disabled")
	}

	// Test SetEnabled on missing plugin
	if err := reg.SetEnabled("missing-plugin", true); err == nil {
		t.Errorf("expected error setting enabled on missing plugin")
	}

	// Custom registered plugin
	reg.Register(registry.PluginItem{
		ID:          "custom-ext",
		Name:        "Custom Extension",
		Description: "Manually registered",
		Type:        "native",
		Enabled:     true,
	})

	customP, err := reg.Get(ctx, "custom-ext")
	if err != nil {
		t.Fatalf("failed to get custom-ext: %v", err)
	}
	if customP.Type != "native" {
		t.Errorf("expected type native, got %s", customP.Type)
	}

	// List
	list, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("failed to list plugins: %v", err)
	}
	if len(list) < 4 {
		t.Errorf("expected at least 4 plugins, got %d", len(list))
	}

	// Plugin not found
	if _, err := reg.Get(ctx, "non-existent"); err == nil {
		t.Errorf("expected error for non-existent plugin")
	}
}

func TestTemplateRegistry_DefaultsAndPersistence(t *testing.T) {
	tempDir := t.TempDir()

	reg := registry.NewTemplateRegistry(tempDir)

	// Check 7 required default templates
	expectedIDs := []string{
		"spec_writer",
		"tdd_builder",
		"security_reviewer",
		"architecture_reviewer",
		"approver_gate",
		"bug_hunter",
		"refactor_specialist",
	}

	for _, id := range expectedIDs {
		tpl, err := reg.Get(id)
		if err != nil {
			t.Errorf("expected template %q to exist: %v", id, err)
			continue
		}
		if tpl.Role == "" || tpl.Model == "" || tpl.SystemPrompt == "" {
			t.Errorf("template %q has missing required fields: %+v", id, tpl)
		}
	}

	templates := reg.List()
	if len(templates) != 7 {
		t.Errorf("expected 7 templates, got %d", len(templates))
	}

	// Save a new template
	newTpl := registry.AgentTemplate{
		ID:           "e2e_tester",
		Name:         "E2E Tester Agent",
		Role:         "e2e_tester",
		Model:        "claude-3-7-sonnet",
		Thinking:     true,
		SystemPrompt: "You generate end-to-end integration and browser tests.",
		Skills:       []string{"qa-audit"},
		Tools:        []string{"read", "bash"},
	}

	if err := reg.Save(newTpl); err != nil {
		t.Fatalf("failed to save template: %v", err)
	}

	// Verify file was persisted to disk
	diskFile := filepath.Join(tempDir, "e2e_tester.json")
	if _, err := os.Stat(diskFile); err != nil {
		t.Fatalf("expected template file on disk, got %v", err)
	}

	// Reload from new registry instance using the same storage directory
	regReloaded := registry.NewTemplateRegistry(tempDir)
	loadedTpl, err := regReloaded.Get("e2e_tester")
	if err != nil {
		t.Fatalf("failed to get reloaded template: %v", err)
	}
	if loadedTpl.Name != newTpl.Name || loadedTpl.Role != newTpl.Role {
		t.Errorf("reloaded template mismatch: %+v", loadedTpl)
	}

	// Delete template
	if err := regReloaded.Delete("e2e_tester"); err != nil {
		t.Fatalf("failed to delete template: %v", err)
	}

	if _, err := regReloaded.Get("e2e_tester"); err == nil {
		t.Errorf("expected deleted template to be missing")
	}

	// Verify disk file is removed
	if _, err := os.Stat(diskFile); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted from disk, got %v", err)
	}

	// Delete non-existent
	if err := regReloaded.Delete("non_existent"); err == nil {
		t.Errorf("expected error deleting non-existent template")
	}

	// Validation errors
	if err := reg.Save(registry.AgentTemplate{Name: "Missing ID"}); err == nil {
		t.Errorf("expected error on missing ID")
	}
	if err := reg.Save(registry.AgentTemplate{ID: "test", Role: "role"}); err == nil {
		t.Errorf("expected error on missing Name")
	}
	if err := reg.Save(registry.AgentTemplate{ID: "test", Name: "name"}); err == nil {
		t.Errorf("expected error on missing Role")
	}
}

func TestManager_UnifiedCoordination(t *testing.T) {
	tempDir := t.TempDir()

	mgr := registry.NewManager(registry.ManagerOptions{
		WorkspaceRoot: tempDir,
		TemplatesDir:  filepath.Join(tempDir, "templates"),
	})

	ctx := context.Background()

	// Skills
	skills, err := mgr.ListSkills(ctx)
	if err != nil {
		t.Fatalf("failed to list skills: %v", err)
	}
	_ = skills

	// Tools
	tools := mgr.ListTools()
	if len(tools) != 11 {
		t.Errorf("expected 11 tools, got %d", len(tools))
	}

	// Plugins
	plugins, err := mgr.ListPlugins(ctx)
	if err != nil {
		t.Fatalf("failed to list plugins: %v", err)
	}
	_ = plugins

	// Templates
	templates := mgr.ListTemplates()
	if len(templates) != 7 {
		t.Errorf("expected 7 default templates, got %d", len(templates))
	}

	// Save and Get template via manager
	customTpl := registry.AgentTemplate{
		ID:           "custom_bot",
		Name:         "Custom Bot",
		Role:         "bot",
		Model:        "claude-3-7-sonnet",
		SystemPrompt: "Helpful bot",
	}
	if err := mgr.SaveTemplate(customTpl); err != nil {
		t.Fatalf("failed to save template: %v", err)
	}

	got, err := mgr.GetTemplate("custom_bot")
	if err != nil {
		t.Fatalf("failed to get template: %v", err)
	}
	if got.Name != customTpl.Name {
		t.Errorf("name mismatch: %s", got.Name)
	}

	if err := mgr.DeleteTemplate("custom_bot"); err != nil {
		t.Fatalf("failed to delete template: %v", err)
	}
	if _, err := mgr.GetTemplate("custom_bot"); err == nil {
		t.Errorf("expected custom_bot to be deleted")
	}
}
