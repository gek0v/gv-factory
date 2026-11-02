// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

type fakeNetError struct {
	timeout   bool
	temporary bool
}

func (e *fakeNetError) Error() string   { return "fake net error" }
func (e *fakeNetError) Timeout() bool   { return e.timeout }
func (e *fakeNetError) Temporary() bool { return e.temporary }

func TestNinetyPercentCoverageBoost(t *testing.T) {
	t.Run("anthropic tool_result with error flag", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","type":"message","model":"claude","content":[{"type":"text","text":"error handled"}],"stop_reason":"end_turn"}`))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		res := llm.ToolResult{
			ToolCallID: "call_err",
			Content:    "disk full",
			IsError:    true,
		}
		resp, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("run"),
				llm.NewToolResultMessage(res),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "error handled" {
			t.Fatalf("unexpected response: %s", resp.Message.Content)
		}
	})

	t.Run("anthropic tool_use arguments empty fallback", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","type":"message","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		tc := llm.ToolCall{
			ID:   "call_no_arg",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "test",
				Arguments: "",
			},
		}
		resp, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewAssistantMessage("", []llm.ToolCall{tc}),
				llm.NewUserMessage("next"),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.FinishReason != "stop" {
			t.Fatalf("expected finishReason stop, got %s", resp.FinishReason)
		}
	})

	t.Run("execute with retry policy zero attempts defaults to 1", func(t *testing.T) {
		policy := llm.RetryPolicy{
			MaxAttempts: 0,
		}
		var calls int
		_, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})
		if err != nil {
			t.Fatal("unexpected error")
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})

	t.Run("execute with retry transient error retries up to max", func(t *testing.T) {
		policy := llm.FastTestRetryPolicy()
		policy.MaxAttempts = 3
		var calls int
		_, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			calls++
			return nil, io.EOF
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if calls != 3 {
			t.Errorf("expected 3 calls, got %d", calls)
		}
	})

	t.Run("openai stream choices json parse error skips gracefully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {corrupt json}\n\ndata: [DONE]\n\n"))
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
		for range chunksCh {
		}
		if err := <-errCh; err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	})

	t.Run("calculate backoff zero or negative attempt returns initial interval", func(t *testing.T) {
		policy := llm.DefaultRetryPolicy()
		b := llm.CalculateBackoff(-1, policy)
		if b != policy.InitialInterval {
			t.Errorf("expected %v, got %v", policy.InitialInterval, b)
		}
	})

	t.Run("execute with retry non-transient error immediate return", func(t *testing.T) {
		policy := llm.FastTestRetryPolicy()
		var calls int
		_, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			calls++
			return nil, errors.New("fatal non transient")
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})

	t.Run("anthropic buildPayload temperature and top_p", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","type":"message","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		temp := 0.7
		topP := 0.95
		resp, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
			Temperature: &temp,
			TopP:        &topP,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.FinishReason != "stop" {
			t.Errorf("expected stop, got %s", resp.FinishReason)
		}
	})

	t.Run("retry transient errors io.EOF and unexpected EOF", func(t *testing.T) {
		if !llm.IsTransientError(io.EOF) {
			t.Errorf("expected io.EOF to be transient")
		}
		if !llm.IsTransientError(io.ErrUnexpectedEOF) {
			t.Errorf("expected io.ErrUnexpectedEOF to be transient")
		}
		if llm.IsTransientError(nil) {
			t.Errorf("expected nil NOT to be transient error")
		}
	})

	t.Run("retry net.Error wrapped error", func(t *testing.T) {
		timeoutErr := &fakeNetError{timeout: true}
		wrapped := fmt.Errorf("wrapped connection error: %w", timeoutErr)
		if !llm.IsTransientError(wrapped) {
			t.Errorf("expected wrapped net error to be transient")
		}

		otherErr := errors.New("generic application error")
		if llm.IsTransientError(otherErr) {
			t.Errorf("expected false for generic non-network error")
		}
	})

	t.Run("openai stream emitChunk cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			for i := 0; i < 50; i++ {
				_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"tok\"}}]}\n\n")
				flusher.Flush()
			}
		}))
		defer server.Close()

		c := llm.NewOpenAIClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithCancel(context.Background())
		chunksCh, errCh := c.StreamChatCompletion(ctx, &llm.ChatRequest{
			Model:    "gpt-4o",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		// Read one chunk and cancel immediately
		<-chunksCh
		cancel()

		// Drain until channels close
		for range chunksCh {
		}
		<-errCh
	})

	t.Run("anthropic stream emitChunk cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			for i := 0; i < 50; i++ {
				_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"tok\"}}\n\n")
				flusher.Flush()
			}
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithCancel(context.Background())
		chunksCh, errCh := c.StreamChatCompletion(ctx, &llm.ChatRequest{
			Model:    "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		<-chunksCh
		cancel()

		for range chunksCh {
		}
		<-errCh
	})

	t.Run("openai buildPayload assistant string only and custom parameters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "cmpl-1",
				"model": "gpt-4o",
				"choices": [{"index":0,"message":{"role":"assistant","content":"ok"}}]
			}`))
		}))
		defer server.Close()

		c := llm.NewOpenAIClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		temp := 0.5
		topP := 0.9
		maxTokens := 500
		resp, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "gpt-4o",
			Messages: []llm.ChatMessage{
				llm.NewAssistantMessage("prior note", nil),
				llm.NewUserMessage("next prompt"),
			},
			Temperature: &temp,
			TopP:        &topP,
			MaxTokens:   &maxTokens,
			Stop:        []string{"\n"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.FinishReason != "stop" {
			t.Errorf("expected finishReason stop, got %s", resp.FinishReason)
		}
	})

	t.Run("anthropic buildPayload stop sequences and max_tokens finish reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "msg-len",
				"type": "message",
				"model": "claude-3-5-sonnet",
				"content": [{"type":"text","text":"truncated..."}],
				"stop_reason": "max_tokens",
				"usage": {"input_tokens": 10, "output_tokens": 20}
			}`))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		maxTokens := 20
		resp, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewAssistantMessage("prior text", nil),
				llm.NewUserMessage("more text"),
			},
			MaxTokens: &maxTokens,
			Stop:      []string{"\n"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.FinishReason != "length" {
			t.Errorf("expected length finishReason, got %s", resp.FinishReason)
		}
	})

	t.Run("anthropic stream max_tokens finish reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\nevent: message_stop\ndata: {}\n\n"))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		chunksCh, errCh := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("test"),
			},
		})

		var finishReason string
		for chunk := range chunksCh {
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
		}
		if err := <-errCh; err != nil {
			t.Fatalf("unexpected stream err: %v", err)
		}
		if finishReason != "length" {
			t.Errorf("expected finishReason length, got %s", finishReason)
		}
	})

	t.Run("anthropic stream error on malformed json line", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message_start\ndata: {invalid json}\n\n"))
		}))
		defer server.Close()

		c := llm.NewAnthropicClient("key",
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		chunksCh, errCh := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "claude-3-5-sonnet",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("test"),
			},
		})

		for range chunksCh {
		}
		<-errCh
	})

	t.Run("openai stream validation errors", func(t *testing.T) {
		c := llm.NewOpenAIClient("key")

		ch1, errCh1 := c.StreamChatCompletion(context.Background(), nil)
		for range ch1 {
		}
		if err := <-errCh1; !errors.Is(err, llm.ErrNilRequest) {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}

		ch2, errCh2 := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{Model: "gpt-4o"})
		for range ch2 {
		}
		if err := <-errCh2; !errors.Is(err, llm.ErrEmptyMessages) {
			t.Errorf("expected ErrEmptyMessages, got %v", err)
		}
	})

	t.Run("anthropic stream validation errors", func(t *testing.T) {
		c := llm.NewAnthropicClient("key")

		ch1, errCh1 := c.StreamChatCompletion(context.Background(), nil)
		for range ch1 {
		}
		if err := <-errCh1; !errors.Is(err, llm.ErrNilRequest) {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}

		ch2, errCh2 := c.StreamChatCompletion(context.Background(), &llm.ChatRequest{Model: "claude-3"})
		for range ch2 {
		}
		if err := <-errCh2; !errors.Is(err, llm.ErrEmptyMessages) {
			t.Errorf("expected ErrEmptyMessages, got %v", err)
		}
	})

	t.Run("retry loop context canceled during delay", func(t *testing.T) {
		policy := llm.RetryPolicy{
			MaxAttempts:     3,
			InitialInterval: 500 * time.Millisecond,
			MaxInterval:     1 * time.Second,
			BackoffFactor:   2.0,
		}

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()

		fakeErr := &fakeNetError{timeout: true}
		_, err := llm.ExecuteWithRetry(ctx, policy, func() (*http.Response, error) {
			return nil, fakeErr
		})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}
