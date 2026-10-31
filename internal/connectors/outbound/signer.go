// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// DefaultSignaturePrefix is the standard prefix applied to HMAC-SHA256 signature headers.
const DefaultSignaturePrefix = "sha256="

// ComputeRawHMACSHA256 calculates the raw hexadecimal HMAC-SHA256 hash of payload using secret.
func ComputeRawHMACSHA256(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Sign computes the HMAC-SHA256 signature prefixed with "sha256=" for standard outbound requests.
func Sign(secret string, payload []byte) string {
	return DefaultSignaturePrefix + ComputeRawHMACSHA256(secret, payload)
}

// BuildSignatureHeader builds the signature header using an optional prefix.
// If prefix is empty, it emits the raw hexadecimal signature.
func BuildSignatureHeader(prefix string, secret string, payload []byte) string {
	rawHex := ComputeRawHMACSHA256(secret, payload)
	if prefix == "" {
		return rawHex
	}
	return prefix + rawHex
}

// VerifySignature validates that providedSig matches the HMAC-SHA256 calculation for payload in constant time.
// It gracefully handles presence or absence of the "sha256=" prefix.
func VerifySignature(secret string, payload []byte, providedSig string) bool {
	trimmedSig := strings.TrimPrefix(strings.TrimSpace(providedSig), DefaultSignaturePrefix)
	trimmedSig = strings.TrimSpace(trimmedSig)
	expectedHex := ComputeRawHMACSHA256(secret, payload)

	return subtle.ConstantTimeCompare([]byte(expectedHex), []byte(strings.ToLower(trimmedSig))) == 1
}
