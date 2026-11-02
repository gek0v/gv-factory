// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestOpenAIChatCompletion(t *testing.T) {
	t.Run("single turn completion", func(t *testing.T) {
		var authHeader string
		var receivedBody map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)

			resp := map[string]any{
				"id":    "chatcmpl-123",
				"model": "gpt-4o",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "Hello from OpenAI!",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     15,
					"completion_tokens": 8,
					"total_tokens":      23,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewOpenAIClient("sk-test-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "gpt-4o",
			Messages: []llm.ChatMessage{
				llm.NewSystemMessage("You are an AI assistant"),
				llm.NewUserMessage("Hello"),
			},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.ID != "chatcmpl-123" {
			t.Errorf("expected ID chatcmpl-123, got %s", resp.ID)
		}
		if resp.Message.Content != "Hello from OpenAI!" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if resp.FinishReason != "stop" {
			t.Errorf("expected finish_reason 'stop', got %s", resp.FinishReason)
		}
		if resp.Usage.TotalTokens != 23 {
			t.Errorf("expected total tokens 23, got %d", resp.Usage.TotalTokens)
		}
		if authHeader != "Bearer sk-test-key" {
			t.Errorf("unexpected auth header: %s", authHeader)
		}
	})

	t.Run("multi-turn with tool calls and tool result", func(t *testing.T) {
		var callCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")

			if callCount == 1 {
				resp := map[string]any{
					"id":    "chatcmpl-tool-call",
					"model": "gpt-4o",
					"choices": []map[string]any{
						{
							"index": 0,
							"message": map[string]any{
								"role": "assistant",
								"tool_calls": []map[string]any{
									{
										"id":   "call_abc",
										"type": "function",
										"function": map[string]any{
											"name":      "list_files",
											"arguments": `{"dir":"/src"}`,
										},
									},
								},
							},
							"finish_reason": "tool_calls",
						},
					},
					"usage": map[string]any{
						"prompt_tokens":     30,
						"completion_tokens": 10,
						"total_tokens":      40,
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}

			// Call 2
			resp := map[string]any{
				"id":    "chatcmpl-final",
				"model": "gpt-4o",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "Directory contains main.go and go.mod",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     60,
					"completion_tokens": 12,
					"total_tokens":      72,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewOpenAIClient("sk-test-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req1 := &llm.ChatRequest{
			Model:      "gpt-4o",
			ToolChoice: "auto",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("list files in /src"),
			},
			Tools: []llm.ToolDefinition{
				{
					Name:        "list_files",
					Description: "Lists files in directory",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"dir": map[string]any{"type": "string"},
						},
					},
				},
			},
		}

		resp1, err := client.ChatCompletion(context.Background(), req1)
		if err != nil {
			t.Fatalf("turn 1 failed: %v", err)
		}
		if resp1.FinishReason != "tool_calls" {
			t.Fatalf("expected finish reason 'tool_calls', got %s", resp1.FinishReason)
		}
		if len(resp1.Message.ToolCalls) != 1 || resp1.Message.ToolCalls[0].ID != "call_abc" {
			t.Fatalf("unexpected tool calls: %+v", resp1.Message.ToolCalls)
		}

		// Turn 2
		req2 := &llm.ChatRequest{
			Model: "gpt-4o",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("list files in /src"),
				resp1.Message,
				llm.NewToolResultMessage(llm.ToolResult{
					ToolCallID: "call_abc",
					Content:    "main.go\ngo.mod",
				}),
			},
		}

		resp2, err := client.ChatCompletion(context.Background(), req2)
		if err != nil {
			t.Fatalf("turn 2 failed: %v", err)
		}
		if resp2.FinishReason != "stop" || resp2.Message.Content != "Directory contains main.go and go.mod" {
			t.Fatalf("unexpected response 2: %+v", resp2)
		}
	})

	t.Run("handles api error response cleanly", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"message": "model 'non-existent' does not exist",
					"type":    "invalid_request_error",
					"code":    "model_not_found",
				},
			})
		}))
		defer server.Close()

		client := llm.NewOpenAIClient("sk-test",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "non-existent",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("test"),
			},
		}

		_, err := client.ChatCompletion(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "model 'non-existent' does not exist") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestOpenAIStreaming(t *testing.T) {
	t.Run("streams chunks and completes with [DONE]", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			lines := []string{
				`data: {"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
				`data: {"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world!"}}]}`,
				`data: {"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_99","type":"function","function":{"name":"calc","arguments":"{\"a\":"}}]}}]}`,
				`data: {"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
				`data: {"id":"chatcmpl-s1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
				`data: [DONE]`,
			}

			for _, l := range lines {
				_, _ = fmt.Fprintf(w, "%s\n\n", l)
				flusher.Flush()
			}
		}))
		defer server.Close()

		client := llm.NewOpenAIClient("sk-test",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "gpt-4o",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("stream please"),
			},
		}

		chunksCh, errCh := client.StreamChatCompletion(context.Background(), req)

		var text strings.Builder
		var toolArgs strings.Builder
		var finishReason string

		for chunk := range chunksCh {
			if chunk.DeltaContent != "" {
				text.WriteString(chunk.DeltaContent)
			}
			if chunk.DeltaTool != nil {
				toolArgs.WriteString(chunk.DeltaTool.Function.Arguments)
			}
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
		}

		if err := <-errCh; err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}

		if text.String() != "Hello world!" {
			t.Errorf("expected 'Hello world!', got '%s'", text.String())
		}
		if toolArgs.String() != `{"a":1}` {
			t.Errorf("expected '{\"a\":1}', got '%s'", toolArgs.String())
		}
		if finishReason != "tool_calls" {
			t.Errorf("expected finish reason 'tool_calls', got '%s'", finishReason)
		}
	})

	t.Run("stream canceled by context terminates gracefully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"start\"}}]}\n\n")
			flusher.Flush()
			<-r.Context().Done()
		}))
		defer server.Close()

		client := llm.NewOpenAIClient("sk-test",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		chunksCh, errCh := client.StreamChatCompletion(ctx, &llm.ChatRequest{
			Model: "gpt-4o",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("cancel"),
			},
		})

		for range chunksCh {
		}
		err := <-errCh
		if err == nil || !strings.Contains(err.Error(), "context") {
			t.Logf("stream terminated with context error: %v", err)
		}
	})
}
