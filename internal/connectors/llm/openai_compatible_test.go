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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestOpenAICompatibleChatCompletion(t *testing.T) {
	t.Run("non-streaming completion with custom base URL and token usage", func(t *testing.T) {
		var receivedAuth string
		var receivedReq map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
				t.Errorf("expected path ending in /chat/completions, got %s", r.URL.Path)
			}
			receivedAuth = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)

			resp := map[string]any{
				"id":    "vllm-cmpl-123",
				"model": "deepseek-ai/DeepSeek-Coder-V2-Lite-Instruct",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "package main\n\nfunc main() {}",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     35,
					"completion_tokens": 12,
					"total_tokens":      47,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		// Test baseURL without /v1 trailing
		client := llm.NewOpenAICompatibleClient(server.URL, "secret-token",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		temp := 0.3
		maxTokens := 2048
		req := &llm.ChatRequest{
			Model: "deepseek-ai/DeepSeek-Coder-V2-Lite-Instruct",
			Messages: []llm.ChatMessage{
				llm.NewSystemMessage("You are an expert coder."),
				llm.NewUserMessage("Create a main package"),
			},
			Temperature: &temp,
			MaxTokens:   &maxTokens,
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.ID != "vllm-cmpl-123" {
			t.Errorf("expected id vllm-cmpl-123, got %s", resp.ID)
		}
		if resp.Message.Content != "package main\n\nfunc main() {}" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if resp.Usage.PromptTokens != 35 || resp.Usage.CompletionTokens != 12 || resp.Usage.TotalTokens != 47 {
			t.Errorf("unexpected usage: %+v", resp.Usage)
		}
		if receivedAuth != "Bearer secret-token" {
			t.Errorf("expected Bearer secret-token, got %s", receivedAuth)
		}
	})

	t.Run("supports base URL already having /v1 and optional/empty API key", func(t *testing.T) {
		var receivedPath string
		var receivedAuth string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			receivedAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "localai-1",
				"model": "mistral-7b",
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": "Hello from LocalAI",
						},
						"finish_reason": "stop",
					},
				},
			})
		}))
		defer server.Close()

		// BaseURL ending with /v1
		client := llm.NewOpenAICompatibleClient(server.URL+"/v1", "",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		resp, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "mistral-7b",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Message.Content != "Hello from LocalAI" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if receivedPath != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", receivedPath)
		}
		if receivedAuth != "" {
			t.Errorf("expected no authorization header for empty apiKey, got %s", receivedAuth)
		}
	})

	t.Run("vLLM constructor helper", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "vllm-1",
				"model": "qwen2.5-coder",
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": "from vllm",
						},
					},
				},
			})
		}))
		defer server.Close()

		client := llm.NewVLLMClient(server.URL, llm.WithRetryPolicy(llm.FastTestRetryPolicy()))
		resp, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "qwen2.5-coder",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("test"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "from vllm" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
	})

	t.Run("handles error response and 401 unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"message": "Invalid API key provided",
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
				},
			})
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "bad-key",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		_, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "gpt-4",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("retries on 502 bad gateway and succeeds", func(t *testing.T) {
		var attempts int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&attempts, 1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":"bad gateway"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "ok",
				"model": "vllm-model",
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": "Recovered",
						},
					},
				},
			})
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		resp, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "vllm-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "Recovered" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}
		if atomic.LoadInt32(&attempts) != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})
}

func TestOpenAICompatibleStreaming(t *testing.T) {
	t.Run("streams SSE events and aggregates token usage", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			events := []string{
				`{"id":"c1","choices":[{"delta":{"role":"assistant","content":"Streamed "}}]}`,
				`{"id":"c2","choices":[{"delta":{"content":"vLLM "}}]}`,
				`{"id":"c3","choices":[{"delta":{"content":"output"}}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
				`[DONE]`,
			}

			for _, ev := range events {
				if ev == `[DONE]` {
					_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				} else {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", ev)
				}
				flusher.Flush()
			}
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "key",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		chunkCh, errCh := client.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "vllm-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("test stream"),
			},
		})

		var text string
		var usage *llm.UsageMetrics
		for chunk := range chunkCh {
			text += chunk.DeltaContent
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
		}

		for err := range errCh {
			if err != nil {
				t.Fatalf("stream error: %v", err)
			}
		}

		if text != "Streamed vLLM output" {
			t.Errorf("expected 'Streamed vLLM output', got %q", text)
		}
		if usage == nil || usage.TotalTokens != 13 {
			t.Errorf("expected usage total_tokens=13, got %+v", usage)
		}
	})

	t.Run("streaming canceled by context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"token"}}]}`)
			flusher.Flush()

			time.Sleep(500 * time.Millisecond)
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		chunkCh, _ := client.StreamChatCompletion(ctx, &llm.ChatRequest{
			Model: "vllm-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})

		for range chunkCh {
		}
	})
}
