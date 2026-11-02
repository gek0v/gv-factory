// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestAdditionalCoverage(t *testing.T) {
	t.Run("retry backoff limits and negative attempts", func(t *testing.T) {
		p := llm.DefaultRetryPolicy()
		if p.MaxAttempts <= 0 {
			t.Errorf("expected positive max attempts")
		}

		b0 := llm.CalculateBackoff(0, p)
		if b0 != p.InitialInterval {
			t.Errorf("expected InitialInterval for attempt 0, got %v", b0)
		}

		// High attempt should clamp to MaxInterval
		bMax := llm.CalculateBackoff(100, p)
		if bMax > p.MaxInterval {
			t.Errorf("expected backoff capped at MaxInterval, got %v", bMax)
		}
	})

	t.Run("config options", func(t *testing.T) {
		client := &http.Client{Timeout: 5 * time.Second}
		c := llm.NewOpenAIClient("key",
			llm.WithHTTPClient(client),
			llm.WithHTTPClient(nil), // test nil guard
		)
		if c == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("anthropic error response with empty error body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		_, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("openai stream error on 500 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`internal server error`))
		}))
		defer server.Close()

		c := llm.NewOpenAIClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		chunksCh, errCh := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "gpt-4o",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		for range chunksCh {
		}
		if err := <-errCh; err == nil {
			t.Fatal("expected stream error on 500 response")
		}
	})

	t.Run("anthropic stream error on 500 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`internal server error`))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		chunksCh, errCh := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		for range chunksCh {
		}
		if err := <-errCh; err == nil {
			t.Fatal("expected stream error on 500 response")
		}
	})

	t.Run("anthropic validation errors", func(t *testing.T) {
		c := llm.NewAnthropicClient("key")

		if _, err := c.ChatCompletion(context.Background(), nil); err != llm.ErrNilRequest {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}
		if _, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{Model: "claude"}); err != llm.ErrEmptyMessages {
			t.Errorf("expected ErrEmptyMessages, got %v", err)
		}
		if _, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		}); err != llm.ErrMissingModel {
			t.Errorf("expected ErrMissingModel, got %v", err)
		}
	})

	t.Run("openai empty choices response error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","model":"gpt-4o","choices":[]}`))
		}))
		defer server.Close()

		c := llm.NewOpenAIClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)
		_, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "gpt-4o",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err == nil {
			t.Fatal("expected error on empty choices")
		}
	})

	t.Run("openai stream with usage only chunk", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"1\",\"model\":\"gpt-4o\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":5,\"total_tokens\":10}}\n\ndata: [DONE]\n\n"))
		}))
		defer server.Close()

		c := llm.NewOpenAIClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)
		chunksCh, errCh := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "gpt-4o",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		var receivedUsage *llm.UsageMetrics
		for chunk := range chunksCh {
			if chunk.Usage != nil {
				receivedUsage = chunk.Usage
			}
		}
		if err := <-errCh; err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedUsage == nil || receivedUsage.TotalTokens != 10 {
			t.Fatalf("unexpected usage: %+v", receivedUsage)
		}
	})
}
