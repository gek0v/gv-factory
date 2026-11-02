// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestLocalLLMComprehensiveCoverage(t *testing.T) {
	t.Run("config options WithModel and WithFallbackProvider", func(t *testing.T) {
		client, err := llm.NewClient(
			llm.ProviderOllama,
			llm.WithBaseURL("http://localhost:11434"),
			llm.WithModel("qwen2.5-coder"),
			llm.WithFallbackProvider(llm.ProviderOpenAICompatible, "http://localhost:8000/v1", "token"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client with fallback")
		}
	})

	t.Run("openai compatible helper constructors and baseURL normalization", func(t *testing.T) {
		// Test empty baseURL in NewOpenAICompatibleClient
		cEmpty := llm.NewOpenAICompatibleClient("", "")
		if cEmpty == nil {
			t.Fatal("expected non-nil client with empty baseURL")
		}

		vllmDef := llm.NewVLLMClient("")
		if vllmDef == nil {
			t.Fatal("expected non-nil default vllm client")
		}

		localDef := llm.NewLocalAIClient("")
		if localDef == nil {
			t.Fatal("expected non-nil default localai client")
		}

		localCustom := llm.NewLocalAIClient("http://custom-localai:9090")
		if localCustom == nil {
			t.Fatal("expected non-nil custom localai client")
		}
	})

	t.Run("factory single client creation variants", func(t *testing.T) {
		// OpenAI provider
		cOpenAI, err := llm.NewClient(llm.ProviderOpenAI, llm.WithAPIKey("test-openai-key"))
		if err != nil || cOpenAI == nil {
			t.Fatalf("failed to create openai client: %v", err)
		}

		// OpenAI missing key
		_, err = llm.NewClient(llm.ProviderOpenAI)
		if err == nil {
			t.Fatal("expected error on missing openai key")
		}

		// ProviderVLLM
		cVllm, err := llm.NewClient(llm.ProviderVLLM, llm.WithBaseURL("http://localhost:8000"))
		if err != nil || cVllm == nil {
			t.Fatalf("failed to create vllm client: %v", err)
		}

		// Fallback client nil guards
		fbNil1 := llm.NewFallbackClient(nil, cVllm)
		if fbNil1 == nil {
			t.Fatal("expected non-nil fallback when primary is nil")
		}
		fbNil2 := llm.NewFallbackClient(cVllm, nil)
		if fbNil2 == nil {
			t.Fatal("expected non-nil fallback when fallback is nil")
		}
	})

	t.Run("factory auto detection additional envs", func(t *testing.T) {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("OLLAMA_BASE_URL")
		_ = os.Unsetenv("OPENAI_COMPATIBLE_BASE_URL")
		_ = os.Unsetenv("VLLM_BASE_URL")
		_ = os.Unsetenv("LLM_PROVIDER")

		// LLM_PROVIDER env
		_ = os.Setenv("LLM_PROVIDER", "ollama")
		c, err := llm.NewClientFromEnv()
		if err != nil || c == nil {
			t.Fatalf("expected client from LLM_PROVIDER: %v", err)
		}
		_ = os.Unsetenv("LLM_PROVIDER")

		// ANTHROPIC_API_KEY env
		_ = os.Setenv("ANTHROPIC_API_KEY", "sk-ant-123")
		c, err = llm.NewClientFromEnv()
		if err != nil || c == nil {
			t.Fatalf("expected client from ANTHROPIC_API_KEY: %v", err)
		}
		_ = os.Unsetenv("ANTHROPIC_API_KEY")

		// OPENAI_API_KEY env
		_ = os.Setenv("OPENAI_API_KEY", "sk-openai-123")
		c, err = llm.NewClientFromEnv()
		if err != nil || c == nil {
			t.Fatalf("expected client from OPENAI_API_KEY: %v", err)
		}
		_ = os.Unsetenv("OPENAI_API_KEY")

		// VLLM_BASE_URL env
		_ = os.Setenv("VLLM_BASE_URL", "http://localhost:8000/v1")
		c, err = llm.NewClientFromEnv()
		if err != nil || c == nil {
			t.Fatalf("expected client from VLLM_BASE_URL: %v", err)
		}
		_ = os.Unsetenv("VLLM_BASE_URL")

		// ProviderAuto through NewClient
		_ = os.Setenv("OLLAMA_BASE_URL", "http://localhost:11434")
		cAuto, err := llm.NewClient(llm.ProviderAuto)
		if err != nil || cAuto == nil {
			t.Fatalf("expected client from ProviderAuto: %v", err)
		}
		_ = os.Unsetenv("OLLAMA_BASE_URL")
	})

	t.Run("ollama validation errors and tool message conversion", func(t *testing.T) {
		client := llm.NewOllamaClient()

		// ChatCompletion validation errors
		_, err := client.ChatCompletion(context.Background(), nil)
		if err != llm.ErrNilRequest {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}

		_, err = client.ChatCompletion(context.Background(), &llm.ChatRequest{Model: ""})
		if err != llm.ErrEmptyMessages {
			t.Errorf("expected ErrEmptyMessages, got %v", err)
		}

		_, err = client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err != llm.ErrMissingModel {
			t.Errorf("expected ErrMissingModel, got %v", err)
		}

		// Generate validation errors
		_, err = client.Generate(context.Background(), nil)
		if err != llm.ErrNilRequest {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}

		_, err = client.Generate(context.Background(), &llm.GenerateRequest{Model: ""})
		if err != llm.ErrMissingModel {
			t.Errorf("expected ErrMissingModel, got %v", err)
		}

		// Context cancellation on ChatCompletion and Generate
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = client.ChatCompletion(canceledCtx, &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err == nil {
			t.Fatal("expected error on canceled context")
		}
		_, err = client.Generate(canceledCtx, &llm.GenerateRequest{Model: "m", Prompt: "p"})
		if err == nil {
			t.Fatal("expected error on canceled context")
		}
	})

	t.Run("ollama chat with tool message and assistant message with tool calls", func(t *testing.T) {
		var receivedReq map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "qwen2.5-coder",
				"message": map[string]any{
					"role":    "assistant",
					"content": "Done with tools",
				},
				"done": true,
			})
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithAPIKey("test-key"),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		req := &llm.ChatRequest{
			Model: "qwen2.5-coder",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("run tool"),
				llm.NewAssistantMessage("", []llm.ToolCall{
					{
						Function: llm.FunctionCall{
							Name:      "test_tool",
							Arguments: `{"arg":"val"}`,
						},
					},
				}),
				llm.NewToolResultMessage(llm.ToolResult{
					ToolCallID: "call_0",
					Content:    "result data",
				}),
			},
		}

		resp, err := client.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Message.Content != "Done with tools" {
			t.Errorf("unexpected content: %s", resp.Message.Content)
		}

		msgs := receivedReq["messages"].([]any)
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		toolMsg := msgs[2].(map[string]any)
		if toolMsg["role"] != "tool" || toolMsg["content"] != "result data" {
			t.Errorf("unexpected tool message: %+v", toolMsg)
		}
	})

	t.Run("ollama api error handling and bad JSON", func(t *testing.T) {
		// Non-JSON 500 error
		server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal crash"))
		}))
		defer server500.Close()

		client500 := llm.NewOllamaClient(
			llm.WithBaseURL(server500.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		_, err := client500.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err == nil {
			t.Fatal("expected error on 500 non-JSON")
		}

		_, err = client500.Generate(context.Background(), &llm.GenerateRequest{
			Model:  "m",
			Prompt: "p",
		})
		if err == nil {
			t.Fatal("expected error on 500 generate")
		}

		// Malformed JSON on 200
		serverBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid-json"))
		}))
		defer serverBadJSON.Close()

		clientBad := llm.NewOllamaClient(
			llm.WithBaseURL(serverBadJSON.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		_, err = clientBad.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err == nil {
			t.Fatal("expected error on malformed JSON")
		}

		_, err = clientBad.Generate(context.Background(), &llm.GenerateRequest{
			Model:  "m",
			Prompt: "p",
		})
		if err == nil {
			t.Fatal("expected error on malformed JSON generate")
		}
	})

	t.Run("streaming context cancellation and APIKey coverage for ollama", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			chunk := map[string]any{
				"model":    "qwen",
				"response": "first chunk",
				"done":     false,
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write(append(data, '\n'))
			flusher.Flush()

			time.Sleep(200 * time.Millisecond)
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithAPIKey("secret-ollama-key"),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		genReq := &llm.GenerateRequest{
			Model:  "qwen",
			Prompt: "hi",
		}
		ch, _ := client.StreamGenerate(ctx, genReq)
		for range ch {
		}

		// StreamChatCompletion canceled context before start
		canceledCtx, cancelNow := context.WithCancel(context.Background())
		cancelNow()

		_, errCh := client.StreamChatCompletion(canceledCtx, &llm.ChatRequest{
			Model:    "qwen",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err := <-errCh; err == nil {
			t.Fatal("expected error on pre-canceled context")
		}

		_, errCh = client.StreamGenerate(canceledCtx, genReq)
		if err := <-errCh; err == nil {
			t.Fatal("expected error on pre-canceled context stream generate")
		}

		// Fallback StreamChatCompletion pre-canceled context
		primary := llm.NewOllamaClient(llm.WithBaseURL(server.URL))
		fb := llm.NewFallbackClient(primary, primary)
		_, errCh = fb.StreamChatCompletion(canceledCtx, &llm.ChatRequest{
			Model:    "qwen",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err := <-errCh; err == nil {
			t.Fatal("expected error on pre-canceled context for fallback stream")
		}
	})

	t.Run("ollama native stream generate and error paths", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			lines := []map[string]any{
				{
					"model":    "qwen",
					"response": "Hello ",
					"done":     false,
				},
				{
					"model":             "qwen",
					"response":          "stream generate",
					"done":              true,
					"done_reason":       "stop",
					"prompt_eval_count": 5,
					"eval_count":        2,
				},
			}
			for _, line := range lines {
				data, _ := json.Marshal(line)
				_, _ = w.Write(append(data, '\n'))
				flusher.Flush()
			}
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		temp := 0.7
		maxTok := 50
		genReq := &llm.GenerateRequest{
			Model:       "qwen",
			Prompt:      "prompt",
			Temperature: &temp,
			MaxTokens:   &maxTok,
			Stop:        []string{"stop-word"},
		}

		chunkCh, errCh := client.StreamGenerate(context.Background(), genReq)
		var output string
		for chunk := range chunkCh {
			output += chunk.DeltaContent
		}
		for err := range errCh {
			if err != nil {
				t.Fatalf("unexpected stream generate error: %v", err)
			}
		}
		if output != "Hello stream generate" {
			t.Errorf("unexpected output: %s", output)
		}

		// StreamGenerate validation errors
		_, errCh = client.StreamGenerate(context.Background(), nil)
		if err := <-errCh; err != llm.ErrNilRequest {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}

		_, errCh = client.StreamGenerate(context.Background(), &llm.GenerateRequest{Model: ""})
		if err := <-errCh; err != llm.ErrMissingModel {
			t.Errorf("expected ErrMissingModel, got %v", err)
		}

		// StreamChatCompletion validation errors
		_, errCh = client.StreamChatCompletion(context.Background(), nil)
		if err := <-errCh; err != llm.ErrNilRequest {
			t.Errorf("expected ErrNilRequest, got %v", err)
		}

		// Server 500 error on StreamChatCompletion and StreamGenerate
		server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("stream init error"))
		}))
		defer server500.Close()

		c500 := llm.NewOllamaClient(llm.WithBaseURL(server500.URL))
		_, errCh = c500.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err := <-errCh; err == nil {
			t.Fatal("expected error on stream chat completion 500")
		}

		_, errCh = c500.StreamGenerate(context.Background(), &llm.GenerateRequest{
			Model:  "m",
			Prompt: "p",
		})
		if err := <-errCh; err == nil {
			t.Fatal("expected error on stream generate 500")
		}
	})

	t.Run("streaming primary client success passes through in fallback client", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			chunk := map[string]any{
				"model": "primary-model",
				"message": map[string]any{
					"role":    "assistant",
					"content": "chunk from primary",
				},
				"done": true,
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write(append(data, '\n'))
			flusher.Flush()
		}))
		defer server.Close()

		primary := llm.NewOllamaClient(llm.WithBaseURL(server.URL))
		fallback := llm.NewOllamaClient(llm.WithBaseURL("http://localhost:11434"))
		fbClient := llm.NewFallbackClient(primary, fallback)

		chunkCh, errCh := fbClient.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		var text string
		for chunk := range chunkCh {
			text += chunk.DeltaContent
		}
		for err := range errCh {
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}

		if text != "chunk from primary" {
			t.Errorf("expected chunk from primary, got %s", text)
		}
	})

	t.Run("ollama 200 responses with error field", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "ollama error field returned",
			})
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		_, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})
		if err == nil {
			t.Fatal("expected error on chat completion with error field")
		}

		_, err = client.Generate(context.Background(), &llm.GenerateRequest{
			Model:  "m",
			Prompt: "p",
		})
		if err == nil {
			t.Fatal("expected error on generate with error field")
		}
	})

	t.Run("ollama streaming blank lines, malformed json, and default stop finish reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			_, _ = w.Write([]byte("\n\n{invalid-json\n"))
			flusher.Flush()

			doneChunk := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role":    "assistant",
					"content": "done content",
				},
				"done": true, // doneReason empty -> defaults to stop
			}
			data, _ := json.Marshal(doneChunk)
			_, _ = w.Write(append(data, '\n'))
			flusher.Flush()
		}))
		defer server.Close()

		client := llm.NewOllamaClient(
			llm.WithBaseURL(server.URL),
			llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
		)

		chunkCh, _ := client.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		var finishReason string
		for ch := range chunkCh {
			if ch.FinishReason != "" {
				finishReason = ch.FinishReason
			}
		}
		if finishReason != "stop" {
			t.Errorf("expected finish_reason 'stop', got %q", finishReason)
		}
	})

	t.Run("factory env variables for openai compatible and vllm in createSingleClient", func(t *testing.T) {
		_ = os.Setenv("OPENAI_COMPATIBLE_BASE_URL", "http://localhost:8000/v1")
		_ = os.Setenv("OPENAI_COMPATIBLE_API_KEY", "compat-key")
		defer func() {
			_ = os.Unsetenv("OPENAI_COMPATIBLE_BASE_URL")
			_ = os.Unsetenv("OPENAI_COMPATIBLE_API_KEY")
		}()

		client, err := llm.NewClient(llm.ProviderOpenAICompatible)
		if err != nil || client == nil {
			t.Fatalf("expected client from env for openai compatible: %v", err)
		}

		_ = os.Unsetenv("OPENAI_COMPATIBLE_BASE_URL")
		_ = os.Setenv("VLLM_BASE_URL", "http://localhost:8000/v1")
		defer os.Unsetenv("VLLM_BASE_URL")

		clientVllm, err := llm.NewClient(llm.ProviderVLLM)
		if err != nil || clientVllm == nil {
			t.Fatalf("expected client from env for vllm: %v", err)
		}

		// baseURL already having /v1/custom
		cCustom := llm.NewOpenAICompatibleClient("http://localhost:8000/v1/custom", "")
		if cCustom == nil {
			t.Fatal("expected client")
		}
	})

	t.Run("fallback stream error emission from primary and fallback", func(t *testing.T) {
		// Test case 1: primary stream emits error after initial chunk
		server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)

			chunk := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role":    "assistant",
					"content": "first chunk",
				},
				"done": false,
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write(append(data, '\n'))
			flusher.Flush()

			// Hijack or close connection abruptly
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
		}))
		defer server1.Close()

		primary1 := llm.NewOllamaClient(llm.WithBaseURL(server1.URL))
		fb1 := llm.NewOllamaClient(llm.WithBaseURL("http://localhost:11434"))
		client1 := llm.NewFallbackClient(primary1, fb1)

		chunkCh1, errCh1 := client1.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		for range chunkCh1 {
		}
		for range errCh1 {
		}

		// Test case 2: primary fails immediately, fallback stream emits error
		serverFallbackErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("fallback stream err"))
		}))
		defer serverFallbackErr.Close()

		primaryDown := llm.NewOllamaClient(llm.WithBaseURL("http://invalid-host-primary:9999"))
		fbErrClient := llm.NewOllamaClient(llm.WithBaseURL(serverFallbackErr.URL))
		client2 := llm.NewFallbackClient(primaryDown, fbErrClient)

		chunkCh2, errCh2 := client2.StreamChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "m",
			Messages: []llm.ChatMessage{llm.NewUserMessage("hi")},
		})

		for range chunkCh2 {
		}
		var receivedErr error
		for err := range errCh2 {
			if err != nil {
				receivedErr = err
			}
		}
		if receivedErr == nil {
			t.Fatal("expected error forwarded from fallback stream")
		}
	})
}
