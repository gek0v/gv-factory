// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// Provider abstracts inbound webhook verification and payload normalization for specific platforms.
type Provider interface {
	// Name returns the provider slug (e.g. "github", "gitlab", "sentry").
	Name() string

	// Verify validates cryptographic signatures or authorization tokens.
	Verify(r *http.Request, body []byte) error

	// Normalize parses raw payload bytes into a canonical schema.TriggerEvent.
	Normalize(r *http.Request, body []byte) (*schema.TriggerEvent, error)
}

// ConstantTimeCompareString performs a constant-time comparison of two strings
// to guard against timing side-channel attacks.
func ConstantTimeCompareString(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// VerifyHMACSHA256 computes HMAC-SHA256 over body with secret and verifies providedSig in constant time.
// It handles optional "sha256=" prefix if present.
func VerifyHMACSHA256(body []byte, secret []byte, providedSig string) bool {
	trimmedSig := strings.TrimPrefix(providedSig, "sha256=")
	trimmedSig = strings.TrimSpace(trimmedSig)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expectedBytes := mac.Sum(nil)
	expectedHex := hex.EncodeToString(expectedBytes)

	// Constant-time comparison between hex strings
	return ConstantTimeCompareString(expectedHex, strings.ToLower(trimmedSig))
}

// ValidateFreshness validates that timestamp ts is within maxDrift of the current time.
// Prevents replay attacks when webhooks provide timestamps.
func ValidateFreshness(ts time.Time, maxDrift time.Duration) error {
	if ts.IsZero() || maxDrift <= 0 {
		return nil
	}
	now := time.Now().UTC()
	if ts.Before(now.Add(-maxDrift)) {
		return ErrTimestampExpired
	}
	if ts.After(now.Add(maxDrift)) {
		return ErrTimestampInFuture
	}
	return nil
}
