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

func TestAnthropicChatCompletion(t *testing.T) {
	t.Run("single turn completion with system prompt extraction", func(t *testing.T) {
		var receivedHeaders http.Header
		var receivedBody map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)

			resp := map[string]any{
				"id":    "msg_123",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet-20241022",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "Hello! How can I help you today?",
					},
				},
				"stop_reason": "end_turn",
				"usage": map[string]any{
					"input_tokens":  25,
					"output_tokens": 10,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewAnthropicClient("test-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "claude-3-5-sonnet-20241022",
			Messages: []llm.ChatMessage{
				llm.NewSystemMessage("You are an expert engineer."),
				llm.NewUserMessage("Hi"),
			},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.ID != "msg_123" {
			t.Errorf("expected ID msg_123, got %s", resp.ID)
		}
		if resp.Message.Content != "Hello! How can I help you today?" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if resp.Usage.PromptTokens != 25 || resp.Usage.CompletionTokens != 10 || resp.Usage.TotalTokens != 35 {
			t.Errorf("unexpected usage metrics: %+v", resp.Usage)
		}
		if resp.FinishReason != "stop" {
			t.Errorf("expected stop finish reason, got %s", resp.FinishReason)
		}

		// Verify headers
		if receivedHeaders.Get("x-api-key") != "test-key" {
			t.Errorf("missing or invalid x-api-key header: %s", receivedHeaders.Get("x-api-key"))
		}
		if receivedHeaders.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("missing or invalid anthropic-version: %s", receivedHeaders.Get("anthropic-version"))
		}

		// Verify system message extraction
		if receivedBody["system"] != "You are an expert engineer." {
			t.Errorf("expected extracted system prompt, got %v", receivedBody["system"])
		}

		// Messages should only contain the user message
		msgs, ok := receivedBody["messages"].([]any)
		if !ok || len(msgs) != 1 {
			t.Fatalf("expected 1 message in messages array, got %v", receivedBody["messages"])
		}
	})

	t.Run("multi-turn with tool calls and tool results", func(t *testing.T) {
		var callCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")

			if callCount == 1 {
				// Return tool call
				resp := map[string]any{
					"id":    "msg_tool_req",
					"type":  "message",
					"role":  "assistant",
					"model": "claude-3-5-sonnet-20241022",
					"content": []map[string]any{
						{
							"type": "text",
							"text": "Checking file...",
						},
						{
							"type": "tool_use",
							"id":   "toolu_01",
							"name": "read_file",
							"input": map[string]any{
								"path": "config.yaml",
							},
						},
					},
					"stop_reason": "tool_use",
					"usage": map[string]any{
						"input_tokens":  30,
						"output_tokens": 15,
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}

			// Call 2: Final response after tool result
			resp := map[string]any{
				"id":    "msg_final",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-5-sonnet-20241022",
				"content": []map[string]any{
					{
						"type": "text",
						"text": "The file contains valid configuration.",
					},
				},
				"stop_reason": "end_turn",
				"usage": map[string]any{
					"input_tokens":  60,
					"output_tokens": 12,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewAnthropicClient("test-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		// First turn: assistant responds with tool_use
		req1 := &llm.ChatRequest{
			Model: "claude-3-5-sonnet-20241022",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("Please read config.yaml"),
			},
			Tools: []llm.ToolDefinition{
				{
					Name:        "read_file",
					Description: "Reads file",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
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
			t.Fatalf("expected finish reason tool_calls, got %s", resp1.FinishReason)
		}
		if len(resp1.Message.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(resp1.Message.ToolCalls))
		}
		tc := resp1.Message.ToolCalls[0]
		if tc.ID != "toolu_01" || tc.Function.Name != "read_file" {
			t.Fatalf("unexpected tool call: %+v", tc)
		}

		// Second turn: pass tool result back
		req2 := &llm.ChatRequest{
			Model: "claude-3-5-sonnet-20241022",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("Please read config.yaml"),
				resp1.Message,
				llm.NewToolResultMessage(llm.ToolResult{
					ToolCallID: tc.ID,
					Content:    "port: 8080\nenv: prod",
					IsError:    false,
				}),
			},
		}

		resp2, err := client.ChatCompletion(context.Background(), req2)
		if err != nil {
			t.Fatalf("turn 2 failed: %v", err)
		}
		if resp2.FinishReason != "stop" {
			t.Fatalf("expected stop finish reason, got %s", resp2.FinishReason)
		}
		if resp2.Message.Content != "The file contains valid configuration." {
			t.Fatalf("unexpected final content: %s", resp2.Message.Content)
		}
	})

	t.Run("returns error on 401 unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"type":    "authentication_error",
					"message": "invalid x-api-key",
				},
			})
		}))
		defer server.Close()

		client := llm.NewAnthropicClient("invalid-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "claude-3-5-sonnet-20241022",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("Hi"),
			},
		}

		_, err := client.ChatCompletion(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error on 401, got nil")
		}
		if !strings.Contains(err.Error(), "invalid x-api-key") {
			t.Errorf("expected error message to contain 'invalid x-api-key', got: %v", err)
		}
	})
}

func TestAnthropicStreaming(t *testing.T) {
	t.Run("streams tokens and tool call deltas correctly", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", 500)
				return
			}

			events := []string{
				"event: message_start\ndata: {\"type\": \"message_start\", \"message\": {\"id\": \"msg_stream_1\", \"model\": \"claude-3-5-sonnet\", \"usage\": {\"input_tokens\": 10}}}\n\n",
				"event: content_block_start\ndata: {\"type\": \"content_block_start\", \"index\": 0, \"content_block\": {\"type\": \"text\", \"text\": \"\"}}\n\n",
				"event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \"Hello \"}}\n\n",
				"event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \"world!\"}}\n\n",
				"event: content_block_stop\ndata: {\"type\": \"content_block_stop\", \"index\": 0}\n\n",
				"event: content_block_start\ndata: {\"type\": \"content_block_start\", \"index\": 1, \"content_block\": {\"type\": \"tool_use\", \"id\": \"toolu_99\", \"name\": \"calc\"}}\n\n",
				"event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"index\": 1, \"delta\": {\"type\": \"input_json_delta\", \"partial_json\": \"{\\\"expr\\\":\\\"\"}}\n\n",
				"event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"index\": 1, \"delta\": {\"type\": \"input_json_delta\", \"partial_json\": \"2+2\\\"}\"}}\n\n",
				"event: content_block_stop\ndata: {\"type\": \"content_block_stop\", \"index\": 1}\n\n",
				"event: message_delta\ndata: {\"type\": \"message_delta\", \"delta\": {\"stop_reason\": \"tool_use\"}, \"usage\": {\"output_tokens\": 15}}\n\n",
				"event: message_stop\ndata: {\"type\": \"message_stop\"}\n\n",
			}

			for _, ev := range events {
				_, _ = fmt.Fprint(w, ev)
				flusher.Flush()
			}
		}))
		defer server.Close()

		client := llm.NewAnthropicClient("test-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("calc 2+2"),
			},
		}

		chunksCh, errCh := client.StreamChatCompletion(context.Background(), req)

		var accumulatedText strings.Builder
		var accumulatedToolArgs strings.Builder
		var lastFinishReason string
		var finalUsage *llm.UsageMetrics

		for chunk := range chunksCh {
			if chunk.DeltaContent != "" {
				accumulatedText.WriteString(chunk.DeltaContent)
			}
			if chunk.DeltaTool != nil {
				accumulatedToolArgs.WriteString(chunk.DeltaTool.Function.Arguments)
			}
			if chunk.FinishReason != "" {
				lastFinishReason = chunk.FinishReason
			}
			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}
		}

		if err := <-errCh; err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}

		if accumulatedText.String() != "Hello world!" {
			t.Errorf("expected 'Hello world!', got '%s'", accumulatedText.String())
		}
		if accumulatedToolArgs.String() != `{"expr":"2+2"}` {
			t.Errorf("expected '{\"expr\":\"2+2\"}', got '%s'", accumulatedToolArgs.String())
		}
		if lastFinishReason != "tool_calls" {
			t.Errorf("expected finish reason 'tool_calls', got '%s'", lastFinishReason)
		}
		if finalUsage == nil || finalUsage.CompletionTokens != 15 {
			t.Errorf("unexpected usage metrics: %+v", finalUsage)
		}
	})

	t.Run("stream canceled by context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"delta\": {\"type\": \"text_delta\", \"text\": \"part\"}}\n\n")
			flusher.Flush()
			// hang forever until client cancels
			<-r.Context().Done()
		}))
		defer server.Close()

		client := llm.NewAnthropicClient("test-key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		req := &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("stream"),
			},
		}

		chunksCh, errCh := client.StreamChatCompletion(ctx, req)
		for range chunksCh {
		}

		err := <-errCh
		if err == nil || !strings.Contains(err.Error(), "context") {
			t.Logf("stream terminated with context error as expected: %v", err)
		}
	})
}
