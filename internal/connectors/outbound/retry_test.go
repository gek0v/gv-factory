// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryPolicy_BackoffDelay(t *testing.T) {
	policy := RetryPolicy{
		InitialInterval: 10 * time.Millisecond,
		BackoffFactor:   2.0,
		MaxInterval:     100 * time.Millisecond,
		MaxAttempts:     5,
		Jitter:          false,
	}

	d1 := policy.BackoffDelay(1)
	if d1 != 10*time.Millisecond {
		t.Errorf("expected attempt 1 delay to be 10ms, got %v", d1)
	}

	d2 := policy.BackoffDelay(2)
	if d2 != 20*time.Millisecond {
		t.Errorf("expected attempt 2 delay to be 20ms, got %v", d2)
	}

	d3 := policy.BackoffDelay(3)
	if d3 != 40*time.Millisecond {
		t.Errorf("expected attempt 3 delay to be 40ms, got %v", d3)
	}

	d5 := policy.BackoffDelay(5) // 10 * 2^4 = 160ms, capped at 100ms
	if d5 != 100*time.Millisecond {
		t.Errorf("expected attempt 5 delay capped at 100ms, got %v", d5)
	}
}

func TestRetryPolicy_Jitter(t *testing.T) {
	policy := RetryPolicy{
		InitialInterval: 100 * time.Millisecond,
		BackoffFactor:   2.0,
		MaxInterval:     500 * time.Millisecond,
		MaxAttempts:     3,
		Jitter:          true,
	}

	delay := policy.BackoffDelay(1)
	if delay < 75*time.Millisecond || delay > 125*time.Millisecond {
		t.Errorf("expected jittered delay within [75ms, 125ms], got %v", delay)
	}
}

func TestIsTransientHTTPStatus(t *testing.T) {
	transientCodes := []int{
		http.StatusTooManyRequests, // 429
		http.StatusRequestTimeout,  // 408
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}

	for _, code := range transientCodes {
		if !IsTransientHTTPStatus(code) {
			t.Errorf("expected HTTP %d to be transient", code)
		}
	}

	permanentCodes := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusUnprocessableEntity,
	}

	for _, code := range permanentCodes {
		if IsTransientHTTPStatus(code) {
			t.Errorf("expected HTTP %d NOT to be transient", code)
		}
	}
}

func TestIsPermanentClientError(t *testing.T) {
	if !IsPermanentClientError(http.StatusBadRequest) {
		t.Errorf("expected 400 Bad Request to be permanent client error")
	}
	if !IsPermanentClientError(http.StatusNotFound) {
		t.Errorf("expected 404 Not Found to be permanent client error")
	}
	if IsPermanentClientError(http.StatusTooManyRequests) {
		t.Errorf("expected 429 Too Many Requests NOT to be permanent client error")
	}
	if IsPermanentClientError(http.StatusServiceUnavailable) {
		t.Errorf("expected 503 Service Unavailable NOT to be permanent client error")
	}
}

func TestIsTransientError(t *testing.T) {
	if !IsTransientError(0, errors.New("connection refused")) {
		t.Errorf("expected network error to be transient")
	}
	if !IsTransientError(http.StatusServiceUnavailable, nil) {
		t.Errorf("expected 503 to be transient error")
	}
	if IsTransientError(http.StatusBadRequest, nil) {
		t.Errorf("expected 400 not to be transient error")
	}
}
