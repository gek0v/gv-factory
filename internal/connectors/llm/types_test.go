// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestChatMessageSerialization(t *testing.T) {
	t.Run("user message", func(t *testing.T) {
		msg := llm.NewUserMessage("hello world")
		if msg.Role != llm.RoleUser {
			t.Fatalf("expected role %s, got %s", llm.RoleUser, msg.Role)
		}
		if msg.Content != "hello world" {
			t.Fatalf("expected content 'hello world', got %s", msg.Content)
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("unexpected marshal error: %v", err)
		}

		var decoded llm.ChatMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unexpected unmarshal error: %v", err)
		}
		if decoded.Role != llm.RoleUser || decoded.Content != "hello world" {
			t.Fatalf("decoded mismatch: %+v", decoded)
		}
	})

	t.Run("system message", func(t *testing.T) {
		msg := llm.NewSystemMessage("system prompt")
		if msg.Role != llm.RoleSystem || msg.Content != "system prompt" {
			t.Fatalf("unexpected message: %+v", msg)
		}
	})

	t.Run("assistant message with tool calls", func(t *testing.T) {
		msg := llm.NewAssistantMessage("thinking...", []llm.ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			},
		})

		if msg.Role != llm.RoleAssistant {
			t.Fatalf("expected role assistant, got %s", msg.Role)
		}
		if len(msg.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
		}
		if msg.ToolCalls[0].Function.Name != "read_file" {
			t.Fatalf("expected tool name 'read_file', got %s", msg.ToolCalls[0].Function.Name)
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded llm.ChatMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if len(decoded.ToolCalls) != 1 || decoded.ToolCalls[0].ID != "call_123" {
			t.Fatalf("unmarshal mismatch: %+v", decoded)
		}
	})

	t.Run("tool result message", func(t *testing.T) {
		res := llm.ToolResult{
			ToolCallID: "call_123",
			Content:    "file contents here",
			IsError:    false,
		}
		msg := llm.NewToolResultMessage(res)
		if msg.Role != llm.RoleTool {
			t.Fatalf("expected role %s, got %s", llm.RoleTool, msg.Role)
		}
		if msg.ToolResult == nil || msg.ToolResult.ToolCallID != "call_123" {
			t.Fatalf("unexpected tool result: %+v", msg.ToolResult)
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded llm.ChatMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if decoded.ToolResult == nil || decoded.ToolResult.Content != "file contents here" {
			t.Fatalf("decoded mismatch: %+v", decoded)
		}
	})
}

func TestChatRequestAndResponseSerialization(t *testing.T) {
	temp := 0.7
	maxTokens := 1024
	req := llm.ChatRequest{
		Model: "claude-3-5-sonnet",
		Messages: []llm.ChatMessage{
			llm.NewUserMessage("hello"),
		},
		Tools: []llm.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Gets current weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var decodedReq llm.ChatRequest
	if err := json.Unmarshal(data, &decodedReq); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}
	if decodedReq.Model != "claude-3-5-sonnet" || len(decodedReq.Tools) != 1 {
		t.Fatalf("mismatch in decoded request: %+v", decodedReq)
	}

	resp := llm.ChatResponse{
		ID:           "resp_123",
		Model:        "claude-3-5-sonnet",
		Message:      llm.NewAssistantMessage("result", nil),
		FinishReason: "stop",
		Usage: llm.UsageMetrics{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}
	var decodedResp llm.ChatResponse
	if err := json.Unmarshal(respData, &decodedResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if decodedResp.Usage.TotalTokens != 30 || decodedResp.FinishReason != "stop" {
		t.Fatalf("mismatch in decoded response: %+v", decodedResp)
	}
}

func TestChatChunkSerialization(t *testing.T) {
	chunk := llm.ChatChunk{
		ID:           "chunk_1",
		Model:        "gpt-4o",
		DeltaContent: "token",
		FinishReason: "stop",
		Usage: &llm.UsageMetrics{
			PromptTokens:     5,
			CompletionTokens: 1,
			TotalTokens:      6,
		},
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("failed to marshal chunk: %v", err)
	}

	var decoded llm.ChatChunk
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal chunk: %v", err)
	}
	if decoded.DeltaContent != "token" || decoded.Usage.TotalTokens != 6 {
		t.Fatalf("mismatch in decoded chunk: %+v", decoded)
	}
}
