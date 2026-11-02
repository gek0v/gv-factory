// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import (
	"encoding/json"

	"github.com/gv-factory/gv-factory/internal/runtime/mcp"
)

// MCPToolToLLMTool converts an MCP tool definition into an LLM tool definition.
// Sensitive execution fields (RequiresApproval, ApprovalTimeout) are stripped.
func MCPToolToLLMTool(def mcp.ToolDefinition) ToolDefinition {
	schemaType := def.InputSchema.Type
	if schemaType == "" {
		schemaType = "object"
	}

	props := make(map[string]any)
	if def.InputSchema.Properties != nil {
		// Deep copy through JSON roundtrip to ensure no alias or mutation leaks
		if data, err := json.Marshal(def.InputSchema.Properties); err == nil {
			_ = json.Unmarshal(data, &props)
		}
	}

	params := map[string]any{
		"type":       schemaType,
		"properties": props,
	}

	if len(def.InputSchema.Required) > 0 {
		requiredCopy := make([]string, len(def.InputSchema.Required))
		copy(requiredCopy, def.InputSchema.Required)
		params["required"] = requiredCopy
	}

	return ToolDefinition{
		Name:        def.Name,
		Description: def.Description,
		Parameters:  params,
	}
}

// MCPToolsToLLMTools converts a slice of MCP tool definitions into LLM tool definitions.
func MCPToolsToLLMTools(defs []mcp.ToolDefinition) []ToolDefinition {
	if len(defs) == 0 {
		return []ToolDefinition{}
	}

	result := make([]ToolDefinition, len(defs))
	for i, def := range defs {
		result[i] = MCPToolToLLMTool(def)
	}
	return result
}
