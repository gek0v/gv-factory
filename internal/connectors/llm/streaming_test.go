// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestConcurrentStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		for i := 1; i <= 5; i++ {
			line := fmt.Sprintf(`data: {"id":"c-%d","choices":[{"delta":{"content":"token%d"}}]}`, i, i)
			_, _ = fmt.Fprintf(w, "%s\n\n", line)
			flusher.Flush()
			time.Sleep(2 * time.Millisecond)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("test-key",
		llm.WithBaseURL(server.URL),
		llm.WithRetryPolicy(llm.FastTestRetryPolicy()),
	)

	concurrency := 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()
			req := &llm.ChatRequest{
				Model: "gpt-4o",
				Messages: []llm.ChatMessage{
					llm.NewUserMessage(fmt.Sprintf("worker %d", workerID)),
				},
			}

			chunksCh, errCh := client.StreamChatCompletion(context.Background(), req)
			count := 0
			for range chunksCh {
				count++
			}
			if err := <-errCh; err != nil {
				t.Errorf("worker %d failed: %v", workerID, err)
			}
			if count != 5 {
				t.Errorf("worker %d expected 5 chunks, got %d", workerID, count)
			}
		}(i)
	}

	wg.Wait()
}

func TestValidationErrors(t *testing.T) {
	client := llm.NewOpenAIClient("key")

	t.Run("nil request", func(t *testing.T) {
		_, err := client.ChatCompletion(context.Background(), nil)
		if err != llm.ErrNilRequest {
			t.Errorf("expected ErrNilRequest, got: %v", err)
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		_, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model:    "gpt-4o",
			Messages: nil,
		})
		if err != llm.ErrEmptyMessages {
			t.Errorf("expected ErrEmptyMessages, got: %v", err)
		}
	})

	t.Run("missing model", func(t *testing.T) {
		_, err := client.ChatCompletion(context.Background(), &llm.ChatRequest{
			Model: "",
			Messages: []llm.ChatMessage{
				llm.NewUserMessage("hi"),
			},
		})
		if err != llm.ErrMissingModel {
			t.Errorf("expected ErrMissingModel, got: %v", err)
		}
	})
}
