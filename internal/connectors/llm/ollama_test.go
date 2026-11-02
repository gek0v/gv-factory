// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestOllamaClient(t *testing.T) {
	t.Run("defaults to localhost ollama base url and works with local models", func(t *testing.T) {
		var receivedModel string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				t.Errorf("expected /chat/completions, got %s", r.URL.Path)
			}

			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			receivedModel, _ = body["model"].(string)

			resp := map[string]any{
				"id":    "ollama-123",
				"model": receivedModel,
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "DeepSeek response",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 5,
					"total_tokens":      15,
				},
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
			Model: "deepseek-r1:14b",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("test reasoning"),
			},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Model != "deepseek-r1:14b" || resp.Message.Content != "DeepSeek response" {
			t.Errorf("unexpected response: %+v", resp)
		}
		if receivedModel != "deepseek-r1:14b" {
			t.Errorf("expected model deepseek-r1:14b, got %s", receivedModel)
		}
	})

	t.Run("openai-compatible client with custom base url and auth key", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "compat-123",
				"model": "qwen2.5-coder:32b",
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"role":    "assistant",
							"content": "package main",
						},
					},
				},
			})
		}))
		defer server.Close()

		client := llm.NewOpenAICompatibleClient(server.URL, "custom-secret",
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "qwen2.5-coder:32b",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("write go code"),
			},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Message.Content != "package main" {
			t.Fatalf("unexpected content: %s", resp.Message.Content)
		}
		if authHeader != "Bearer custom-secret" {
			t.Fatalf("expected Bearer custom-secret, got %s", authHeader)
		}
	})
}
