// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"strings"
	"testing"
)

func TestSigner_ComputeAndVerify(t *testing.T) {
	secret := "super-secret-key-12345"
	payload := []byte(`{"id":"evt-1","event":"task.completed","run_id":"run-42"}`)

	sig := Sign(secret, payload)
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("expected signature to start with 'sha256=', got %s", sig)
	}

	// Verify valid signature with prefix
	if !VerifySignature(secret, payload, sig) {
		t.Errorf("expected signature verification to succeed")
	}

	// Verify valid signature without prefix
	rawHex := ComputeRawHMACSHA256(secret, payload)
	if !VerifySignature(secret, payload, rawHex) {
		t.Errorf("expected raw hex signature verification to succeed")
	}

	// Verify custom prefix builder
	customSig := BuildSignatureHeader("", secret, payload)
	if customSig != rawHex {
		t.Errorf("expected empty prefix to return raw hex, got %s", customSig)
	}

	// Tampered payload
	tamperedPayload := []byte(`{"id":"evt-1","event":"task.completed","run_id":"run-99"}`)
	if VerifySignature(secret, tamperedPayload, sig) {
		t.Errorf("expected tampered payload verification to fail")
	}

	// Wrong secret
	if VerifySignature("wrong-secret", payload, sig) {
		t.Errorf("expected verification with wrong secret to fail")
	}

	// Malformed signature
	if VerifySignature(secret, payload, "sha256=invalidhex") {
		t.Errorf("expected malformed signature verification to fail")
	}
}
