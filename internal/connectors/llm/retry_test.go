// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestIsTransientStatus(t *testing.T) {
	transient := []int{429, 500, 502, 503, 504}
	for _, code := range transient {
		if !llm.IsTransientStatus(code) {
			t.Errorf("expected status %d to be transient", code)
		}
	}

	nonTransient := []int{200, 201, 400, 401, 403, 404, 422}
	for _, code := range nonTransient {
		if llm.IsTransientStatus(code) {
			t.Errorf("expected status %d NOT to be transient", code)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Run("numeric seconds", func(t *testing.T) {
		dur := llm.ParseRetryAfter("15")
		if dur != 15*time.Second {
			t.Fatalf("expected 15s, got %v", dur)
		}
	})

	t.Run("http date", func(t *testing.T) {
		target := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
		dur := llm.ParseRetryAfter(target)
		if dur <= 0 || dur > 12*time.Second {
			t.Fatalf("unexpected duration from http date: %v", dur)
		}
	})

	t.Run("empty or invalid", func(t *testing.T) {
		if dur := llm.ParseRetryAfter(""); dur != 0 {
			t.Fatalf("expected 0 for empty header, got %v", dur)
		}
		if dur := llm.ParseRetryAfter("not-a-date"); dur != 0 {
			t.Fatalf("expected 0 for invalid header, got %v", dur)
		}
	})
}

func TestExecuteWithRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		policy := llm.FastTestRetryPolicy()
		var calls int32

		resp, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected response: %+v", resp)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Fatalf("expected 1 call, got %d", calls)
		}
	})

	t.Run("retries on transient status and succeeds", func(t *testing.T) {
		policy := llm.FastTestRetryPolicy()
		var calls int32

		resp, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			count := atomic.AddInt32(&calls, 1)
			if count == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": []string{"1"}},
					Body:       http.NoBody,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if atomic.LoadInt32(&calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", calls)
		}
	})

	t.Run("exceeds max attempts on persistent 500", func(t *testing.T) {
		policy := llm.FastTestRetryPolicy()
		policy.MaxAttempts = 3
		var calls int32

		resp, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       http.NoBody,
			}, nil
		})

		if err != nil {
			t.Fatalf("expected response with last status, got error: %v", err)
		}
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", resp.StatusCode)
		}
		if atomic.LoadInt32(&calls) != 3 {
			t.Fatalf("expected 3 calls, got %d", calls)
		}
	})

	t.Run("aborts immediately when context is canceled", func(t *testing.T) {
		policy := llm.RetryPolicy{
			MaxAttempts:     5,
			InitialInterval: 500 * time.Millisecond,
			MaxInterval:     2 * time.Second,
			BackoffFactor:   2.0,
		}

		ctx, cancel := context.WithCancel(context.Background())
		var calls int32

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		_, err := llm.ExecuteWithRetry(ctx, policy, func() (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       http.NoBody,
			}, nil
		})

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled error, got: %v", err)
		}
	})

	t.Run("does not retry on 400 Bad Request", func(t *testing.T) {
		policy := llm.FastTestRetryPolicy()
		var calls int32

		resp, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       http.NoBody,
			}, nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Fatalf("expected exactly 1 call without retry, got %d", calls)
		}
	})
}

func TestExecuteWithRetryNetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ts.Close() // Force connection refused

	policy := llm.FastTestRetryPolicy()
	policy.MaxAttempts = 2

	var calls int32
	_, err := llm.ExecuteWithRetry(context.Background(), policy, func() (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return http.Get(ts.URL)
	})

	if err == nil {
		t.Fatalf("expected network error, got nil")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}
