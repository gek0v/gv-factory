// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gv-factory/gv-factory/internal/registry"
)

func setupTestRegistryManager(t *testing.T) (*registry.Manager, string) {
	tempDir := t.TempDir()
	templatesDir := filepath.Join(tempDir, "templates")

	mgr := registry.NewManager(registry.ManagerOptions{
		WorkspaceRoot:   tempDir,
		IncludeUserHome: false,
		TemplatesDir:    templatesDir,
	})

	// Register sample skill
	mgr.Skills.Register(registry.SkillItem{
		Name:        "test-skill",
		Description: "A test skill for endpoints",
		Path:        "/skills/test",
		Source:      "workspace",
	})

	// Register sample plugin
	mgr.Plugins.Register(registry.PluginItem{
		ID:          "test-plugin",
		Name:        "Test Plugin",
		Description: "A test plugin for endpoints",
		Type:        "wasm",
		Path:        "/plugins/test",
		Enabled:     true,
	})

	return mgr, tempDir
}

func TestRegistryEndpoints_Skills(t *testing.T) {
	mux := http.NewServeMux()
	mgr, _ := setupTestRegistryManager(t)
	registerRegistryEndpoints(mux, mgr)

	// GET /api/v1/skills - Success
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var skills []registry.SkillItem
	if err := json.Unmarshal(rec.Body.Bytes(), &skills); err != nil {
		t.Fatalf("failed to decode skills JSON: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "test-skill" {
		t.Errorf("unexpected skills response: %+v", skills)
	}

	// POST /api/v1/skills - 405 Method Not Allowed
	req = httptest.NewRequest(http.MethodPost, "/api/v1/skills", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}

	// GET /api/v1/skills - Context Canceled Error (500)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/skills", nil).WithContext(ctx)
	// Force discover by clearing in-memory skills
	mgr.Skills = registry.NewSkillsRegistryWithOptions(registry.SkillsRegistryOptions{})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d", rec.Code)
	}
}

func TestRegistryEndpoints_Tools(t *testing.T) {
	mux := http.NewServeMux()
	mgr, _ := setupTestRegistryManager(t)
	registerRegistryEndpoints(mux, mgr)

	// GET /api/v1/tools - All tools
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var tools []registry.ToolItem
	if err := json.Unmarshal(rec.Body.Bytes(), &tools); err != nil {
		t.Fatalf("failed to decode tools JSON: %v", err)
	}
	if len(tools) != 11 {
		t.Errorf("expected 11 tools, got %d", len(tools))
	}

	// GET /api/v1/tools?category=builtin
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tools?category=builtin", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var builtinTools []registry.ToolItem
	if err := json.Unmarshal(rec.Body.Bytes(), &builtinTools); err != nil {
		t.Fatalf("failed to decode builtin tools JSON: %v", err)
	}
	if len(builtinTools) != 7 {
		t.Errorf("expected 7 builtin tools, got %d", len(builtinTools))
	}

	// POST /api/v1/tools - 405 Method Not Allowed
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tools", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestRegistryEndpoints_Plugins(t *testing.T) {
	mux := http.NewServeMux()
	mgr, _ := setupTestRegistryManager(t)
	registerRegistryEndpoints(mux, mgr)

	// GET /api/v1/plugins - Success
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var plugins []registry.PluginItem
	if err := json.Unmarshal(rec.Body.Bytes(), &plugins); err != nil {
		t.Fatalf("failed to decode plugins JSON: %v", err)
	}
	if len(plugins) != 1 || plugins[0].ID != "test-plugin" {
		t.Errorf("unexpected plugins response: %+v", plugins)
	}

	// DELETE /api/v1/plugins - 405 Method Not Allowed
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/plugins", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}

	// GET /api/v1/plugins - Context Canceled Error (500)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil).WithContext(ctx)
	mgr.Plugins = registry.NewPluginsRegistryWithOptions(registry.PluginsRegistryOptions{})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d", rec.Code)
	}
}

func TestRegistryEndpoints_AgentTemplates_CRUD(t *testing.T) {
	mux := http.NewServeMux()
	mgr, _ := setupTestRegistryManager(t)
	registerRegistryEndpoints(mux, mgr)

	// 1. GET /api/v1/agent-templates - List default templates
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-templates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var templates []registry.AgentTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &templates); err != nil {
		t.Fatalf("failed to decode templates: %v", err)
	}
	if len(templates) != 7 {
		t.Errorf("expected 7 default templates, got %d", len(templates))
	}

	// 2. GET /api/v1/agent-templates/spec_writer - Detail
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent-templates/spec_writer", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var specWriter registry.AgentTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &specWriter); err != nil {
		t.Fatalf("failed to decode spec_writer: %v", err)
	}
	if specWriter.ID != "spec_writer" || specWriter.Role != "spec_writer" {
		t.Errorf("unexpected template data: %+v", specWriter)
	}

	// 3. POST /api/v1/agent-templates - Create new template
	newTpl := registry.AgentTemplate{
		ID:           "test_agent",
		Name:         "Test Agent Persona",
		Role:         "tester",
		Model:        "claude-3-7-sonnet",
		Thinking:     true,
		SystemPrompt: "You are a test agent.",
		Skills:       []string{"tdd"},
		Tools:        []string{"read", "bash"},
	}
	payload, _ := json.Marshal(newTpl)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent-templates", bytes.NewReader(payload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. GET /api/v1/agent-templates/test_agent - Verify retrieved
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent-templates/test_agent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var fetchedTpl registry.AgentTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &fetchedTpl); err != nil {
		t.Fatalf("failed to decode fetched template: %v", err)
	}
	if fetchedTpl.ID != "test_agent" || fetchedTpl.Name != "Test Agent Persona" {
		t.Errorf("fetched template mismatch: %+v", fetchedTpl)
	}

	// 5. GET /api/v1/agent-templates/non_existent - 404 Not Found
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent-templates/non_existent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", rec.Code)
	}

	// 6. POST /api/v1/agent-templates - Bad JSON
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent-templates", bytes.NewReader([]byte("{invalid-json")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on invalid json, got %d", rec.Code)
	}

	// 7. POST /api/v1/agent-templates - Validation failure (missing name)
	invalidTpl := registry.AgentTemplate{
		ID:   "missing_name",
		Role: "worker",
	}
	payload, _ = json.Marshal(invalidTpl)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent-templates", bytes.NewReader(payload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on validation failure, got %d", rec.Code)
	}

	// 8. PUT /api/v1/agent-templates - 405 Method Not Allowed
	req = httptest.NewRequest(http.MethodPut, "/api/v1/agent-templates", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on PUT /api/v1/agent-templates, got %d", rec.Code)
	}

	// 9. POST /api/v1/agent-templates/test_agent - 405 Method Not Allowed on sub-resource
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent-templates/test_agent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on POST /api/v1/agent-templates/{id}, got %d", rec.Code)
	}

	// 10. Empty ID on /api/v1/agent-templates/ -> 400 Bad Request
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent-templates/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on empty id, got %d", rec.Code)
	}

	// 11. DELETE /api/v1/agent-templates/test_agent -> 200 OK
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/agent-templates/test_agent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on DELETE /api/v1/agent-templates/test_agent, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify template is gone
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent-templates/test_agent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for deleted template, got %d", rec.Code)
	}

	// 12. DELETE non-existent template -> 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/agent-templates/test_agent", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found on deleting non-existent template, got %d", rec.Code)
	}

	// 13. POST /api/v1/agent-templates and DELETE with query param ?id=
	tpl2 := registry.AgentTemplate{
		ID:           "test_agent_query",
		Name:         "Query Deleted Agent",
		Role:         "cleaner",
		Model:        "claude-3-7-sonnet",
		SystemPrompt: "Clean up resources.",
	}
	payload, _ = json.Marshal(tpl2)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent-templates", bytes.NewReader(payload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/agent-templates?id=test_agent_query", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on DELETE /api/v1/agent-templates?id=test_agent_query, got %d: %s", rec.Code, rec.Body.String())
	}

	// 14. DELETE with empty id on /api/v1/agent-templates -> 400 Bad Request
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/agent-templates", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on DELETE without ID, got %d", rec.Code)
	}

	// 15. DELETE with body on /api/v1/agent-templates
	tpl3 := registry.AgentTemplate{
		ID:           "test_agent_body",
		Name:         "Body Deleted Agent",
		Role:         "tester",
		Model:        "claude-3-7-sonnet",
		SystemPrompt: "Test via body delete.",
	}
	payload, _ = json.Marshal(tpl3)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent-templates", bytes.NewReader(payload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rec.Code)
	}

	deleteBody := bytes.NewReader([]byte(`{"id":"test_agent_body"}`))
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/agent-templates", deleteBody)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on DELETE with body, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegistryEndpoints_IntegratedServerMux(t *testing.T) {
	// Construct full server via NewServerMux and ensure registry routes are mounted
	mux := NewServerMux(nil, nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from integrated server mux on /api/v1/tools, got %d", rec.Code)
	}
}
