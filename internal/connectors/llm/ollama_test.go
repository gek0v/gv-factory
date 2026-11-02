// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestNativeOllamaChatCompletion(t *testing.T) {
	t.Run("successful non-streaming chat completion with token usage", func(t *testing.T) {
		var receivedReq map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/chat" {
				t.Errorf("expected path /api/chat, got %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}

			_ = json.NewDecoder(r.Body).Decode(&receivedReq)

			resp := map[string]any{
				"model":      "qwen2.5-coder:32b",
				"created_at": "2026-03-01T00:00:00Z",
				"message": map[string]any{
					"role":    "assistant",
					"content": "func main() { fmt.Println(\"Hello Ollama\") }",
				},
				"done":              true,
				"done_reason":       "stop",
				"prompt_eval_count": 42,
				"eval_count":        88,
				"total_duration":    1200000000,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		temp := 0.2
		topP := 0.95
		maxTokens := 1024
		req := &llm.ChatRequest{
			Model: "qwen2.5-coder:32b",
			Messages: []llm.ChatMessage{
				llm.NewSystemMessage("You are an expert Go developer."),
				llm.NewUserMessage("Write main func"),
			},
			Temperature: &temp,
			TopP:        &topP,
			MaxTokens:   &maxTokens,
			Stop:        []string{"func test"},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Model != "qwen2.5-coder:32b" {
			t.Errorf("expected model qwen2.5-coder:32b, got %s", resp.Model)
		}
		if resp.Message.Content != "func main() { fmt.Println(\"Hello Ollama\") }" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if resp.FinishReason != "stop" {
			t.Errorf("expected finish_reason stop, got %s", resp.FinishReason)
		}
		if resp.Usage.PromptTokens != 42 || resp.Usage.CompletionTokens != 88 || resp.Usage.TotalTokens != 130 {
			t.Errorf("unexpected usage metrics: %+v", resp.Usage)
		}

		// Verify received request format
		if receivedReq["model"] != "qwen2.5-coder:32b" {
			t.Errorf("expected model in payload, got %v", receivedReq["model"])
		}
		if streamVal, ok := receivedReq["stream"].(bool); !ok || streamVal {
			t.Errorf("expected stream: false, got %v", receivedReq["stream"])
		}
		msgs, ok := receivedReq["messages"].([]any)
		if !ok || len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %v", receivedReq["messages"])
		}
		firstMsg := msgs[0].(map[string]any)
		if firstMsg["role"] != "system" || firstMsg["content"] != "You are an expert Go developer." {
			t.Errorf("unexpected system message: %+v", firstMsg)
		}
		options, ok := receivedReq["options"].(map[string]any)
		if !ok {
			t.Fatalf("expected options object, got %v", receivedReq["options"])
		}
		if options["temperature"] != 0.2 || options["top_p"] != 0.95 || options["num_predict"] != float64(1024) {
			t.Errorf("unexpected options: %+v", options)
		}
	})

	t.Run("handles Ollama tool calls in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"model": "deepseek-coder:33b",
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{
						{
							"function": map[string]any{
								"name": "read_file",
								"arguments": map[string]any{
									"path": "main.go",
								},
							},
						},
					},
				},
				"done":              true,
				"done_reason":       "tool_calls",
				"prompt_eval_count": 20,
				"eval_count":        15,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "deepseek-coder:33b",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("read main.go"),
			},
			Tools: []llm.ToolDefinition{
				{
					Name:        "read_file",
					Description: "read file contents",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
						},
					},
				},
			},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(resp.Message.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
		}
		tc := resp.Message.ToolCalls[0]
		if tc.Function.Name != "read_file" {
			t.Errorf("expected tool read_file, got %s", tc.Function.Name)
		}
		if tc.Function.Arguments != `{"path":"main.go"}` {
			t.Errorf("unexpected arguments: %s", tc.Function.Arguments)
		}
	})

	t.Run("handles error response from Ollama API", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "model 'nonexistent-model' not found, try pulling it first",
			})
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		_, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "nonexistent-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hello"),
			},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("aborts on context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{"done": true})
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := client.ChatCompletion(ctx, &llm.ChatRequest{
			Model: "llama3",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hello"),
			},
		})
		if err == nil {
			t.Fatal("expected context error, got nil")
		}
	})

	t.Run("retries on transient 503 error", func(t *testing.T) {
		var attempts int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&attempts, 1)
			if count == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"model loading"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "llama3",
				"message": map[string]any{
					"role":    "assistant",
					"content": "Ready now",
				},
				"done": true,
			})
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		resp, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "llama3",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hello"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "Ready now" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if atomic.LoadInt32(&attempts) != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})
}

func TestNativeOllamaStreaming(t *testing.T) {
	t.Run("streams tokens with NDJSON format and parses final usage", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/chat" {
				t.Errorf("expected path /api/chat, got %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)

			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected flusher")
			}

			chunks := []map[string]any{
				{
					"model": "qwen2.5-coder",
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello",
					},
					"done": false,
				},
				{
					"model": "qwen2.5-coder",
					"message": map[string]any{
						"role":    "assistant",
						"content": " world",
					},
					"done": false,
				},
				{
					"model": "qwen2.5-coder",
					"message": map[string]any{
						"role":    "assistant",
						"content": "!",
					},
					"done":              true,
					"done_reason":       "stop",
					"prompt_eval_count": 12,
					"eval_count":        3,
				},
			}

			for _, chunk := range chunks {
				data, _ := json.Marshal(chunk)
				_, _ = w.Write(append(data, '\n'))
				flusher.Flush()
			}
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "qwen2.5-coder",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		}

		chunkCh, errCh := client.StreamChatCompletion(context.Background(), req)

		var accumulated string
		var finalUsage *llm.UsageMetrics
		var finalReason string

		for chunk := range chunkCh {
			accumulated += chunk.DeltaContent
			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}
			if chunk.FinishReason != "" {
				finalReason = chunk.FinishReason
			}
		}

		for err := range errCh {
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}

		if accumulated != "Hello world!" {
			t.Errorf("expected 'Hello world!', got %q", accumulated)
		}
		if finalReason != "stop" {
			t.Errorf("expected finish_reason stop, got %s", finalReason)
		}
		if finalUsage == nil || finalUsage.PromptTokens != 12 || finalUsage.CompletionTokens != 3 || finalUsage.TotalTokens != 15 {
			t.Errorf("unexpected usage: %+v", finalUsage)
		}
	})

	t.Run("stream context cancellation terminates channel", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			chunk := map[string]any{
				"model": "llama3",
				"message": map[string]any{
					"role":    "assistant",
					"content": "start",
				},
				"done": false,
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write(append(data, '\n'))
			flusher.Flush()

			time.Sleep(500 * time.Millisecond)
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		req := &llm.ChatRequest{
			Model: "llama3",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		}

		chunkCh, _ := client.StreamChatCompletion(ctx, req)

		for range chunkCh {
		}
		// Stream completes cleanly on cancellation
	})
}

func TestNativeOllamaGenerate(t *testing.T) {
	t.Run("generate endpoint for raw prompt completion", func(t *testing.T) {
		var receivedReq map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/generate" {
				t.Errorf("expected path /api/generate, got %s", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)

			resp := map[string]any{
				"model":             "deepseek-coder:6.7b",
				"created_at":        "2026-03-01T00:00:00Z",
				"response":          "println(\"hello world\")",
				"done":              true,
				"done_reason":       "stop",
				"prompt_eval_count": 15,
				"eval_count":        25,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		temp := 0.5
		genReq := &llm.GenerateRequest{
			Model:       "deepseek-coder:6.7b",
			Prompt:      "write a simple print",
			System:      "Kotlin assistant",
			Temperature: &temp,
		}

		resp, err := client.Generate(context.Background(), genReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Response != "println(\"hello world\")" {
			t.Errorf("unexpected response: %s", resp.Response)
		}
		if resp.Usage.PromptTokens != 15 || resp.Usage.CompletionTokens != 25 || resp.Usage.TotalTokens != 40 {
			t.Errorf("unexpected usage: %+v", resp.Usage)
		}
		if receivedReq["system"] != "Kotlin assistant" {
			t.Errorf("expected system Kotlin assistant, got %v", receivedReq["system"])
		}
		if receivedReq["prompt"] != "write a simple print" {
			t.Errorf("expected prompt, got %v", receivedReq["prompt"])
		}
	})
}
