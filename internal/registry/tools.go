// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry

import (
	"fmt"
	"sync"
)

// ToolItem represents an available tool in the Pi or Cerebro ecosystem.
type ToolItem struct {
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	Category         string         `json:"category"` // "builtin", "brain", "mcp", "custom"
	ParametersSchema map[string]any `json:"parameters_schema"`
}

// ToolsRegistry manages the catalog of built-in, brain MCP, and custom tools.
type ToolsRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolItem
}

// NewToolsRegistry initializes a ToolsRegistry pre-populated with standard built-in and Cerebro tools.
func NewToolsRegistry() *ToolsRegistry {
	r := &ToolsRegistry{
		tools: make(map[string]ToolItem),
	}
	r.registerDefaults()
	return r
}

// Register registers or updates a tool in the catalog.
func (r *ToolsRegistry) Register(tool ToolItem) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if tool.Category == "" {
		tool.Category = "custom"
	}
	if tool.ParametersSchema == nil {
		tool.ParametersSchema = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
	return nil
}

// Get returns a tool definition by name.
func (r *ToolsRegistry) Get(name string) (*ToolItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return &item, nil
}

// List returns a list of all registered tools.
func (r *ToolsRegistry) List() []ToolItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]ToolItem, 0, len(r.tools))
	for _, item := range r.tools {
		res = append(res, item)
	}
	return res
}

// ListByCategory returns tools filtered by category ("builtin", "brain", "mcp", etc.).
func (r *ToolsRegistry) ListByCategory(category string) []ToolItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var res []ToolItem
	for _, item := range r.tools {
		if item.Category == category {
			res = append(res, item)
		}
	}
	return res
}

// registerDefaults configures standard built-in and Brain MCP tools.
func (r *ToolsRegistry) registerDefaults() {
	defaults := []ToolItem{
		// Built-ins
		{
			Name:        "read",
			Description: "Read the contents of a file with optional line offsets and limit.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Path to the file to read"},
					"offset": map[string]any{"type": "integer", "description": "Line number to start reading from (1-indexed)"},
					"limit":  map[string]any{"type": "integer", "description": "Maximum number of lines to read"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write",
			Description: "Write or overwrite content to a file, creating parent directories automatically.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path to the file to write"},
					"content": map[string]any{"type": "string", "description": "Content to write into the file"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "edit",
			Description: "Edit a single file using exact non-overlapping text replacement chunks.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Path to the file to edit"},
					"edits": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"oldText": map[string]any{"type": "string", "description": "Exact text to replace"},
								"newText": map[string]any{"type": "string", "description": "Replacement text"},
							},
							"required": []string{"oldText", "newText"},
						},
					},
				},
				"required": []string{"path", "edits"},
			},
		},
		{
			Name:        "bash",
			Description: "Execute a command inside the workspace container or local shell.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Shell command to execute"},
					"timeout": map[string]any{"type": "integer", "description": "Execution timeout in seconds"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "todo",
			Description: "Manage a stateful task list for tracking multi-step progress and requirements.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":      map[string]any{"type": "string", "enum": []string{"create", "update", "list", "get", "delete", "clear"}},
					"id":          map[string]any{"type": "integer", "description": "Task identifier"},
					"subject":     map[string]any{"type": "string", "description": "Short imperative summary"},
					"description": map[string]any{"type": "string", "description": "Detailed task description"},
					"status":      map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "deleted"}},
					"activeForm":  map[string]any{"type": "string", "description": "Present-continuous spinner label"},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "ffgrep",
			Description: "Fast smart-case ripgrep file content search with path filters.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":       map[string]any{"type": "string", "description": "Search literal text or regex"},
					"path":          map[string]any{"type": "string", "description": "Path or glob constraint"},
					"exclude":       map[string]any{"type": "string", "description": "Exclude patterns"},
					"caseSensitive": map[string]any{"type": "boolean", "description": "Enforce case-sensitive matching"},
					"limit":         map[string]any{"type": "integer", "description": "Maximum matches to return"},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "fffind",
			Description: "Fuzzy filename and glob path search ranked by frecency.",
			Category:    "builtin",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Fuzzy filename search terms"},
					"path":    map[string]any{"type": "string", "description": "Path prefix or glob"},
					"exclude": map[string]any{"type": "string", "description": "Exclude paths"},
					"limit":   map[string]any{"type": "integer", "description": "Max results to return"},
				},
				"required": []string{"pattern"},
			},
		},

		// Cerebro MCP Tools
		{
			Name:        "brain_search",
			Description: "Semantic and hybrid vector search across ADRs, codebase knowledge, rules, and episodic memory.",
			Category:    "brain",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":    map[string]any{"type": "string", "description": "Natural language or keyword search query"},
					"category": map[string]any{"type": "string", "description": "Category: all, knowledge, rules, episodes", "default": "all"},
					"limit":    map[string]any{"type": "integer", "description": "Max items to return", "default": 10},
					"alpha":    map[string]any{"type": "number", "description": "Hybrid weighting between vector (1.0) and lexical (0.0)", "default": 0.7},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "brain_record_episode",
			Description: "Record task execution learnings, failures, and architectural insights into Cerebro persistent memory.",
			Category:    "brain",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id":      map[string]any{"type": "string", "description": "Identifier of the task"},
					"summary":      map[string]any{"type": "string", "description": "Summary of the outcome and learnings"},
					"decision":     map[string]any{"type": "string", "description": "Decision outcome: APPROVED, REJECTED, RETRY"},
					"lessons":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Key learnings"},
					"code_symbols": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Affected code symbols"},
				},
				"required": []string{"task_id", "summary", "decision"},
			},
		},
		{
			Name:        "brain_list_rules",
			Description: "List active architectural rules, constraints, and invariants indexed in Cerebro.",
			Category:    "brain",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"active_only": map[string]any{"type": "boolean", "description": "Only return active rules", "default": true},
					"category":    map[string]any{"type": "string", "description": "Optional category filter"},
				},
			},
		},
		{
			Name:        "git_diff",
			Description: "Inspect working tree file modifications and git diffs against the base branch.",
			Category:    "brain",
			ParametersSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"base_branch": map[string]any{"type": "string", "description": "Base branch to compare with (defaults to main)"},
					"stat_only":   map[string]any{"type": "boolean", "description": "Return summary statistics instead of full diff"},
					"paths":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional file path filters"},
				},
			},
		},
	}

	for _, tool := range defaults {
		r.tools[tool.Name] = tool
	}
}
