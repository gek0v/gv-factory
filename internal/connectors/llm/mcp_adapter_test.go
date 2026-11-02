// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
	"github.com/gv-factory/gv-factory/internal/runtime/mcp"
)

func TestMCPToolsToLLMTools(t *testing.T) {
	t.Run("converts standard mcp tool definitions", func(t *testing.T) {
		mcpDefs := []mcp.ToolDefinition{
			{
				Name:        "read_file",
				Description: "Read file contents from workspace",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Path to file",
						},
					},
					Required: []string{"path"},
				},
				RequiresApproval: true,
				ApprovalTimeout:  5 * time.Minute,
			},
		}

		llmDefs := llm.MCPToolsToLLMTools(mcpDefs)
		if len(llmDefs) != 1 {
			t.Fatalf("expected 1 tool definition, got %d", len(llmDefs))
		}

		def := llmDefs[0]
		if def.Name != "read_file" {
			t.Errorf("expected name read_file, got %s", def.Name)
		}
		if def.Description != "Read file contents from workspace" {
			t.Errorf("unexpected description: %s", def.Description)
		}

		params := def.Parameters
		if params["type"] != "object" {
			t.Errorf("expected parameters type 'object', got %v", params["type"])
		}

		props, ok := params["properties"].(map[string]any)
		if !ok || props["path"] == nil {
			t.Fatalf("expected properties with 'path', got %v", params["properties"])
		}

		req, ok := params["required"].([]string)
		if !ok || len(req) != 1 || req[0] != "path" {
			t.Errorf("expected required ['path'], got %v", params["required"])
		}
	})

	t.Run("defaults empty type to object and nil properties to empty map", func(t *testing.T) {
		mcpDefs := []mcp.ToolDefinition{
			{
				Name:        "ping",
				Description: "Pings server",
				InputSchema: mcp.ToolInputSchema{
					Type:       "",
					Properties: nil,
					Required:   nil,
				},
			},
		}

		llmDefs := llm.MCPToolsToLLMTools(mcpDefs)
		if len(llmDefs) != 1 {
			t.Fatalf("expected 1 tool definition, got %d", len(llmDefs))
		}

		params := llmDefs[0].Parameters
		if params["type"] != "object" {
			t.Errorf("expected default type 'object', got %v", params["type"])
		}
		props, ok := params["properties"].(map[string]any)
		if !ok || props == nil {
			t.Errorf("expected non-nil properties map, got %v", params["properties"])
		}
	})

	t.Run("handles empty or nil inputs gracefully", func(t *testing.T) {
		if res := llm.MCPToolsToLLMTools(nil); len(res) != 0 {
			t.Fatalf("expected empty slice for nil, got %v", res)
		}
		if res := llm.MCPToolsToLLMTools([]mcp.ToolDefinition{}); len(res) != 0 {
			t.Fatalf("expected empty slice for empty slice, got %v", res)
		}
	})

	t.Run("deep copies properties to prevent accidental mutation", func(t *testing.T) {
		origProps := map[string]any{
			"param": map[string]any{
				"type": "string",
			},
		}
		mcpDefs := []mcp.ToolDefinition{
			{
				Name: "sample",
				InputSchema: mcp.ToolInputSchema{
					Type:       "object",
					Properties: origProps,
				},
			},
		}

		llmDefs := llm.MCPToolsToLLMTools(mcpDefs)
		// mutate original map
		origProps["param2"] = "mutated"

		paramsProps := llmDefs[0].Parameters["properties"].(map[string]any)
		if _, exists := paramsProps["param2"]; exists {
			t.Fatalf("properties were not deep copied!")
		}
	})
}
