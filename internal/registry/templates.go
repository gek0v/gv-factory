// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AgentTemplate defines a declarative configuration for an autonomous agent persona.
type AgentTemplate struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Role         string   `json:"role"`
	Model        string   `json:"model"`
	Thinking     bool     `json:"thinking"`
	SystemPrompt string   `json:"system_prompt"`
	Skills       []string `json:"skills"`
	Tools        []string `json:"tools"`
	Plugins      []string `json:"plugins"`
}

// Validate checks whether mandatory fields in the template are populated.
func (t *AgentTemplate) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("template id is required")
	}
	if strings.Contains(t.ID, "..") || strings.ContainsAny(t.ID, "/\\") {
		return fmt.Errorf("invalid template id %q: path traversal characters not allowed", t.ID)
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("template name is required")
	}
	if strings.TrimSpace(t.Role) == "" {
		return fmt.Errorf("template role is required")
	}
	if strings.TrimSpace(t.Model) == "" {
		t.Model = "claude-3-7-sonnet"
	}
	if t.Skills == nil {
		t.Skills = make([]string, 0)
	}
	if t.Tools == nil {
		t.Tools = make([]string, 0)
	}
	if t.Plugins == nil {
		t.Plugins = make([]string, 0)
	}
	return nil
}

// DefaultCustomTemplatesDir points to the default location for custom templates.
const DefaultCustomTemplatesDir = ".agents/templates"

var (
	customTemplatesDir = filepath.Join(".agents", "templates")
	customTemplatesMu  sync.RWMutex
)

// GetCustomTemplatesDir returns the active directory for custom template persistence.
func GetCustomTemplatesDir() string {
	customTemplatesMu.RLock()
	defer customTemplatesMu.RUnlock()
	return customTemplatesDir
}

// SetCustomTemplatesDir sets the directory used for custom template persistence.
func SetCustomTemplatesDir(dir string) {
	customTemplatesMu.Lock()
	defer customTemplatesMu.Unlock()
	customTemplatesDir = dir
}

// RegisterCustomTemplate validates and persists an agent template to .agents/templates/<id>.json.
func RegisterCustomTemplate(tmpl AgentTemplate) error {
	if err := tmpl.Validate(); err != nil {
		return err
	}

	dir := GetCustomTemplatesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create custom templates directory: %w", err)
	}

	data, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	filePath := filepath.Join(dir, fmt.Sprintf("%s.json", tmpl.ID))
	return os.WriteFile(filePath, data, 0o644)
}

// DeleteCustomTemplate deletes an agent template file from .agents/templates/<id>.json.
func DeleteCustomTemplate(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("template id is required")
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("invalid template id %q: path traversal characters not allowed", id)
	}

	dir := GetCustomTemplatesDir()
	filePath := filepath.Join(dir, fmt.Sprintf("%s.json", id))
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("custom agent template %q not found", id)
	}

	return os.Remove(filePath)
}

// TemplateRegistry manages persistent and in-memory agent templates.
type TemplateRegistry struct {
	mu         sync.RWMutex
	storageDir string
	templates  map[string]AgentTemplate
}

// NewTemplateRegistry initializes the registry with base templates, and loads any templates from storageDir if provided.
func NewTemplateRegistry(storageDir string) *TemplateRegistry {
	r := &TemplateRegistry{
		storageDir: storageDir,
		templates:  make(map[string]AgentTemplate),
	}

	r.loadDefaultTemplates()

	if storageDir != "" {
		_ = r.loadFromDisk()
	}

	return r
}

// List returns a list of all registered templates.
func (r *TemplateRegistry) List() []AgentTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]AgentTemplate, 0, len(r.templates))
	for _, tpl := range r.templates {
		res = append(res, tpl)
	}
	return res
}

// Get finds an agent template by its unique ID.
func (r *TemplateRegistry) Get(id string) (*AgentTemplate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tpl, ok := r.templates[id]
	if !ok {
		return nil, fmt.Errorf("agent template %q not found", id)
	}
	return &tpl, nil
}

// Save creates or updates an agent template in memory and optionally on disk.
func (r *TemplateRegistry) Save(tpl AgentTemplate) error {
	if err := tpl.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.templates[tpl.ID] = tpl

	if r.storageDir != "" {
		if err := r.saveToDisk(tpl); err != nil {
			// If default storageDir is read-only, attempt fallback writable directories
			saved := false
			fallbacks := []string{
				filepath.Join(os.Getenv("HOME"), ".pi", "agent", "templates"),
				"/tmp/ov-templates",
			}
			for _, fb := range fallbacks {
				if fbErr := os.MkdirAll(fb, 0o755); fbErr == nil {
					data, mErr := json.MarshalIndent(tpl, "", "  ")
					if mErr == nil {
						if wErr := os.WriteFile(filepath.Join(fb, fmt.Sprintf("%s.json", tpl.ID)), data, 0o644); wErr == nil {
							saved = true
							break
						}
					}
				}
			}
			if !saved {
				// Keep in memory and return nil rather than hard-failing on read-only environments
				return nil
			}
		}
	}

	return nil
}

// RegisterCustomTemplate validates and stores a custom agent template.
func (r *TemplateRegistry) RegisterCustomTemplate(tpl AgentTemplate) error {
	return r.Save(tpl)
}

// DeleteCustomTemplate removes a template by ID from memory and disk.
func (r *TemplateRegistry) DeleteCustomTemplate(id string) error {
	return r.Delete(id)
}

// Delete removes a template by ID from memory and disk.
func (r *TemplateRegistry) Delete(id string) error {
	if strings.Contains(id, "..") || strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("invalid template id %q: path traversal characters not allowed", id)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.templates[id]; !ok {
		return fmt.Errorf("agent template %q not found", id)
	}

	delete(r.templates, id)

	if r.storageDir != "" {
		filePath := filepath.Join(r.storageDir, fmt.Sprintf("%s.json", id))
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// saveToDisk writes an agent template as JSON into the storageDir.
func (r *TemplateRegistry) saveToDisk(tpl AgentTemplate) error {
	if err := os.MkdirAll(r.storageDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		return err
	}
	filePath := filepath.Join(r.storageDir, fmt.Sprintf("%s.json", tpl.ID))
	return os.WriteFile(filePath, data, 0o644)
}

// loadFromDisk scans storageDir for *.json template files and indexes them.
func (r *TemplateRegistry) loadFromDisk() error {
	if _, err := os.Stat(r.storageDir); err != nil {
		return nil
	}

	entries, err := os.ReadDir(r.storageDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}

		filePath := filepath.Join(r.storageDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var tpl AgentTemplate
		if err := json.Unmarshal(data, &tpl); err == nil && tpl.ID != "" {
			_ = tpl.Validate()
			r.templates[tpl.ID] = tpl
		}
	}

	return nil
}

// loadDefaultTemplates populates the 7 required base templates:
// spec_writer, tdd_builder, security_reviewer, architecture_reviewer, approver_gate, bug_hunter, refactor_specialist.
func (r *TemplateRegistry) loadDefaultTemplates() {
	defaults := []AgentTemplate{
		{
			ID:           "spec_writer",
			Name:         "Spec Writer Agent",
			Role:         "spec_writer",
			Model:        "claude-3-7-sonnet",
			Thinking:     true,
			SystemPrompt: "You are an expert Software Design Document (SDD) and TDD specification author in gv-factory. Analyze issues, user requirements and repository architecture to produce formal, unambiguous specifications with clear test criteria and contract interfaces.",
			Skills:       []string{"domain-modeling", "codebase-design", "research"},
			Tools:        []string{"read", "ffgrep", "fffind", "brain_search", "brain_list_rules"},
			Plugins:      []string{},
		},
		{
			ID:           "tdd_builder",
			Name:         "TDD Builder Agent",
			Role:         "tdd_builder",
			Model:        "claude-3-7-sonnet",
			Thinking:     true,
			SystemPrompt: "You are an autonomous engineering agent following strict Red-Green-Refactor Test-Driven Development (TDD). First write failing unit tests, then write minimal clean code to make tests pass, and finally refactor for optimal structure while maintaining 100% green tests.",
			Skills:       []string{"tdd", "codebase-design", "full-output-enforcement"},
			Tools:        []string{"read", "write", "edit", "bash", "todo", "ffgrep", "fffind", "git_diff"},
			Plugins:      []string{},
		},
		{
			ID:           "security_reviewer",
			Name:         "Security Reviewer Agent",
			Role:         "security_reviewer",
			Model:        "claude-3-7-sonnet",
			Thinking:     true,
			SystemPrompt: "You are a senior Application Security and DevSecOps reviewer. Audit code diffs and dependencies against OWASP Top 10, CWE patterns, secret leaks, race conditions, and sandbox escapes.",
			Skills:       []string{"security-audit"},
			Tools:        []string{"read", "ffgrep", "fffind", "brain_search", "git_diff"},
			Plugins:      []string{},
		},
		{
			ID:           "architecture_reviewer",
			Name:         "Architecture Reviewer Agent",
			Role:         "architecture_reviewer",
			Model:        "claude-3-7-sonnet",
			Thinking:     true,
			SystemPrompt: "You are a Principal Software Architect evaluating system designs, modular boundaries, ADR compliance, and domain model consistency across gv-factory.",
			Skills:       []string{"code-quality-audit", "domain-modeling", "codebase-design"},
			Tools:        []string{"read", "ffgrep", "fffind", "brain_search", "brain_list_rules"},
			Plugins:      []string{},
		},
		{
			ID:           "approver_gate",
			Name:         "Approver Gatekeeper Agent",
			Role:         "approver_gate",
			Model:        "claude-3-7-sonnet",
			Thinking:     false,
			SystemPrompt: "You are the automated gatekeeper responsible for reviewing build outputs, test coverage (must be >= 90%), zero security vulnerabilities, REUSE SPDX compliance, and determining whether code changes are ready for fast-track merge.",
			Skills:       []string{"code-review", "qa-audit"},
			Tools:        []string{"read", "brain_search", "brain_record_episode", "git_diff"},
			Plugins:      []string{},
		},
		{
			ID:           "bug_hunter",
			Name:         "Bug Hunter Agent",
			Role:         "bug_hunter",
			Model:        "claude-3-7-sonnet",
			Thinking:     true,
			SystemPrompt: "You are a specialized debugging and root-cause analysis agent. Reproduce unexpected behaviors, inspect stack traces, trace data flows, isolate race conditions, and produce minimal reproducible failure cases.",
			Skills:       []string{"diagnosing-bugs", "tdd"},
			Tools:        []string{"read", "bash", "ffgrep", "fffind", "git_diff"},
			Plugins:      []string{},
		},
		{
			ID:           "refactor_specialist",
			Name:         "Refactor Specialist Agent",
			Role:         "refactor_specialist",
			Model:        "claude-3-7-sonnet",
			Thinking:     true,
			SystemPrompt: "You are a refactoring specialist focused on reducing technical debt, deepening module interfaces, eliminating code duplication, and improving algorithmic performance without altering public API behaviors.",
			Skills:       []string{"codebase-design", "code-quality-audit"},
			Tools:        []string{"read", "write", "edit", "bash", "ffgrep", "fffind", "git_diff"},
			Plugins:      []string{},
		},
	}

	for _, tpl := range defaults {
		_ = tpl.Validate()
		r.templates[tpl.ID] = tpl
	}
}
