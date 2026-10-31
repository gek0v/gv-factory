// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"net/http"
	"time"
)

// RetryPolicy specifies parameters for exponential backoff, retry counts, and per-request timeouts.
type RetryPolicy struct {
	InitialInterval time.Duration
	BackoffFactor   float64
	MaxInterval     time.Duration
	MaxAttempts     int
	RequestTimeout  time.Duration
	Jitter          bool
}

// DefaultRetryPolicy returns production-ready resilient retry defaults.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		InitialInterval: 1 * time.Second,
		BackoffFactor:   2.0,
		MaxInterval:     30 * time.Second,
		MaxAttempts:     5,
		RequestTimeout:  10 * time.Second,
		Jitter:          true,
	}
}

// FastTestRetryPolicy returns minimal delays suitable for fast unit and integration tests.
func FastTestRetryPolicy() RetryPolicy {
	return RetryPolicy{
		InitialInterval: 5 * time.Millisecond,
		BackoffFactor:   2.0,
		MaxInterval:     50 * time.Millisecond,
		MaxAttempts:     4,
		RequestTimeout:  2 * time.Second,
		Jitter:          false,
	}
}

// BackoffDelay computes the backoff duration for the given 1-based retry attempt.
func (p RetryPolicy) BackoffDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return p.applyJitter(p.InitialInterval)
	}

	factor := p.BackoffFactor
	if factor <= 1.0 {
		factor = 2.0
	}

	mult := math.Pow(factor, float64(attempt-1))
	delay := time.Duration(float64(p.InitialInterval) * mult)

	if p.MaxInterval > 0 && delay > p.MaxInterval {
		delay = p.MaxInterval
	}

	return p.applyJitter(delay)
}

// applyJitter adds deterministic/pseudo-randomized jitter between [0.75 * delay, 1.25 * delay].
func (p RetryPolicy) applyJitter(delay time.Duration) time.Duration {
	if !p.Jitter || delay <= 0 {
		return delay
	}

	var b [8]byte
	_, _ = rand.Read(b[:])
	r := float64(binary.LittleEndian.Uint64(b[:])) / float64(math.MaxUint64) // [0.0, 1.0)

	// Jitter range [0.75, 1.25]
	multiplier := 0.75 + (r * 0.50)
	jittered := time.Duration(float64(delay) * multiplier)
	if jittered <= 0 {
		return delay
	}
	return jittered
}

// IsTransientHTTPStatus returns true for temporary server errors or rate limits that warrant retrying.
// 5xx errors, 429 Too Many Requests, and 408 Request Timeout are treated as transient.
func IsTransientHTTPStatus(statusCode int) bool {
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout {
		return true
	}
	return statusCode >= 500 && statusCode <= 599
}

// IsPermanentClientError returns true for client errors (4xx) except rate limit (429) and timeout (408).
// These indicate misconfigurations, invalid signatures, non-existent paths, or forbidden resources.
func IsPermanentClientError(statusCode int) bool {
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout {
		return false
	}
	return statusCode >= 400 && statusCode < 500
}

// IsTransientError evaluates whether an HTTP status code or client-side error is temporary.
func IsTransientError(statusCode int, err error) bool {
	if err != nil {
		// Network errors, timeouts, connection refused, EOF
		return true
	}
	return IsTransientHTTPStatus(statusCode)
}
