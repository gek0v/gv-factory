// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestFactoryClientCreation(t *testing.T) {
	t.Run("creates ollama client from provider type", func(t *testing.T) {
		client, err := llm.NewClient(llm.ProviderOllama, llm.WithBaseURL("http://localhost:11434"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("creates openai compatible client from provider type", func(t *testing.T) {
		client, err := llm.NewClient(llm.ProviderOpenAICompatible,
			llm.WithBaseURL("http://localhost:8000/v1"),
			llm.WithAPIKey("test-token"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("creates anthropic client from provider type", func(t *testing.T) {
		client, err := llm.NewClient(llm.ProviderAnthropic, llm.WithAPIKey("test-anthropic-key"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("returns error on missing required credentials", func(t *testing.T) {
		_, err := llm.NewClient(llm.ProviderAnthropic)
		if err == nil {
			t.Fatal("expected error on missing API key for anthropic")
		}
	})

	t.Run("returns error on unsupported provider", func(t *testing.T) {
		_, err := llm.NewClient(llm.ProviderType("unknown"))
		if err == nil {
			t.Fatal("expected error on unknown provider")
		}
	})
}

func TestFactoryAutoDetection(t *testing.T) {
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origOllama := os.Getenv("OLLAMA_BASE_URL")
	origCompat := os.Getenv("OPENAI_COMPATIBLE_BASE_URL")
	origProvider := os.Getenv("LLM_PROVIDER")
	defer func() {
		_ = os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		_ = os.Setenv("OPENAI_API_KEY", origOpenAI)
		_ = os.Setenv("OLLAMA_BASE_URL", origOllama)
		_ = os.Setenv("OPENAI_COMPATIBLE_BASE_URL", origCompat)
		_ = os.Setenv("LLM_PROVIDER", origProvider)
	}()

	t.Run("detects ollama when OLLAMA_BASE_URL set", func(t *testing.T) {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Setenv("OLLAMA_BASE_URL", "http://localhost:11434")

		client, err := llm.NewClientFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("detects openai compatible when OPENAI_COMPATIBLE_BASE_URL set", func(t *testing.T) {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("OLLAMA_BASE_URL")
		_ = os.Setenv("OPENAI_COMPATIBLE_BASE_URL", "http://localhost:8000/v1")

		client, err := llm.NewClientFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("returns error when no providers are configured in env", func(t *testing.T) {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("OLLAMA_BASE_URL")
		_ = os.Unsetenv("OPENAI_COMPATIBLE_BASE_URL")
		_ = os.Unsetenv("LLM_PROVIDER")

		_, err := llm.NewClientFromEnv()
		if err == nil {
			t.Fatal("expected error when no provider found")
		}
	})
}

func TestFallbackClient(t *testing.T) {
	t.Run("returns primary response when primary succeeds", func(t *testing.T) {
		var primaryCalled, fallbackCalled int32

		primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&primaryCalled, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "primary-model",
				"message": map[string]any{
					"role":    "assistant",
					"content": "from primary",
				},
				"done": true,
			})
		}))
		defer primaryServer.Close()

		fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&fallbackCalled, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "fallback-model",
				"message": map[string]any{
					"role":    "assistant",
					"content": "from fallback",
				},
				"done": true,
			})
		}))
		defer fallbackServer.Close()

		primary := llm.NewOllamaClient(
			llm.WithBaseURL(primaryServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)
		fallback := llm.NewOllamaClient(
			llm.WithBaseURL(fallbackServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		client := llm.NewFallbackClient(primary, fallback)

		resp, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "test-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Message.Content != "from primary" {
			t.Errorf("expected from primary, got %s", resp.Message.Content)
		}
		if atomic.LoadInt32(&primaryCalled) != 1 {
			t.Errorf("expected primary called once")
		}
		if atomic.LoadInt32(&fallbackCalled) != 0 {
			t.Errorf("expected fallback not called")
		}
	})

	t.Run("switches to fallback when primary fails", func(t *testing.T) {
		var fallbackCalled int32

		primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"primary down"}`))
		}))
		defer primaryServer.Close()

		fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&fallbackCalled, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "fallback-model",
				"message": map[string]any{
					"role":    "assistant",
					"content": "from fallback",
				},
				"done": true,
			})
		}))
		defer fallbackServer.Close()

		primary := llm.NewOllamaClient(
			llm.WithBaseURL(primaryServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)
		fallback := llm.NewOllamaClient(
			llm.WithBaseURL(fallbackServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		client := llm.NewFallbackClient(primary, fallback)

		resp, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "test-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Message.Content != "from fallback" {
			t.Errorf("expected from fallback, got %s", resp.Message.Content)
		}
		if atomic.LoadInt32(&fallbackCalled) != 1 {
			t.Errorf("expected fallback called once")
		}
	})

	t.Run("does not invoke fallback if context was canceled", func(t *testing.T) {
		var fallbackCalled int32

		primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
		}))
		defer primaryServer.Close()

		fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&fallbackCalled, 1)
		}))
		defer fallbackServer.Close()

		primary := llm.NewOllamaClient(
			llm.WithBaseURL(primaryServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)
		fallback := llm.NewOllamaClient(
			llm.WithBaseURL(fallbackServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		client := llm.NewFallbackClient(primary, fallback)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := client.ChatCompletion(ctx, &llm.ChatRequest{
			Model: "test-model",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Logf("expected context error, got %v", err)
		}
		if atomic.LoadInt32(&fallbackCalled) != 0 {
			t.Errorf("fallback should not be invoked on context cancellation")
		}
	})

	t.Run("streaming fallback switches when primary fails", func(t *testing.T) {
		primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer primaryServer.Close()

		fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			chunk := map[string]any{
				"model": "fallback-model",
				"message": map[string]any{
					"role":    "assistant",
					"content": "streamed fallback",
				},
				"done": true,
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write(append(data, '\n'))
			flusher.Flush()
		}))
		defer fallbackServer.Close()

		primary := llm.NewOllamaClient(
			llm.WithBaseURL(primaryServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)
		fallback := llm.NewOllamaClient(
			llm.WithBaseURL(fallbackServer.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		client := llm.NewFallbackClient(primary, fallback)

		chunkCh, errCh := client.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "test",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})

		var content string
		for ch := range chunkCh {
			content += ch.DeltaContent
		}
		for err := range errCh {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}

		if content != "streamed fallback" {
			t.Errorf("expected 'streamed fallback', got %q", content)
		}
	})
}
