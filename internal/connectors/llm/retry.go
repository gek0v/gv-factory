// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy defines backoff and attempt boundaries.
type RetryPolicy struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	BackoffFactor   float64
}

// DefaultRetryPolicy returns production-ready retry defaults.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:     4,
		InitialInterval: 500 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		BackoffFactor:   2.0,
	}
}

// FastTestRetryPolicy returns minimal intervals for unit testing.
func FastTestRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:     3,
		InitialInterval: 5 * time.Millisecond,
		MaxInterval:     20 * time.Millisecond,
		BackoffFactor:   1.5,
	}
}

// IsTransientStatus returns true if the HTTP status code is temporary and worth retrying.
func IsTransientStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}

// IsTransientError returns true if the network/io error indicates a temporary failure.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// General network errors or connection refused from net/http
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return false
}

// ParseRetryAfter parses the Retry-After header into a time.Duration.
func ParseRetryAfter(headerVal string) time.Duration {
	if headerVal == "" {
		return 0
	}

	// Try integer seconds
	if secs, err := strconv.Atoi(headerVal); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}

	// Try HTTP date format
	if t, err := http.ParseTime(headerVal); err == nil {
		diff := time.Until(t)
		if diff > 0 {
			return diff
		}
	}

	return 0
}

// CalculateBackoff computes exponential backoff with jitter.
func CalculateBackoff(attempt int, policy RetryPolicy) time.Duration {
	if attempt <= 0 {
		return policy.InitialInterval
	}
	multiplier := math.Pow(policy.BackoffFactor, float64(attempt-1))
	interval := float64(policy.InitialInterval) * multiplier
	if interval > float64(policy.MaxInterval) {
		interval = float64(policy.MaxInterval)
	}

	// Apply 25% jitter: [0.75, 1.25]
	jitter := 0.75 + (rand.Float64() * 0.50)
	jittered := time.Duration(interval * jitter)
	if jittered > policy.MaxInterval {
		return policy.MaxInterval
	}
	return jittered
}

// ExecuteWithRetry executes an HTTP request operation with backoff retry on transient failures.
func ExecuteWithRetry(ctx context.Context, policy RetryPolicy, op func() (*http.Response, error)) (*http.Response, error) {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := op()
		if err == nil {
			if !IsTransientStatus(resp.StatusCode) {
				return resp, nil
			}
			// Transient HTTP status code
			lastResp = resp
			if attempt == policy.MaxAttempts {
				return lastResp, nil
			}

			// Parse Retry-After header if present
			retryDelay := ParseRetryAfter(resp.Header.Get("Retry-After"))
			if retryDelay <= 0 || retryDelay > policy.MaxInterval {
				retryDelay = CalculateBackoff(attempt, policy)
			}

			// Release response body before waiting to avoid socket/connection leaks
			_ = resp.Body.Close()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
			continue
		}

		// Error returned from op()
		lastErr = err
		if !IsTransientError(err) || attempt == policy.MaxAttempts {
			return nil, lastErr
		}

		delay := CalculateBackoff(attempt, policy)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, nil
}
