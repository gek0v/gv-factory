// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package auth

import (
	"testing"
	"time"
)

func TestApprovalToken(t *testing.T) {
	svc := NewService("test-secret-key-12345")
	runID := "run-test-abc"
	expiresAt := time.Now().Add(1 * time.Hour)

	token := svc.GenerateApprovalToken(runID, expiresAt)
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	extractedID, ok := svc.ValidateApprovalToken(token)
	if !ok {
		t.Fatalf("expected token validation to succeed for %s", token)
	}
	if extractedID != runID {
		t.Fatalf("expected run ID %s, got %s", runID, extractedID)
	}

	// Test expired token
	expiredToken := svc.GenerateApprovalToken(runID, time.Now().Add(-1*time.Minute))
	if _, ok := svc.ValidateApprovalToken(expiredToken); ok {
		t.Fatal("expected expired token validation to fail")
	}

	// Test invalid signature
	if _, ok := svc.ValidateApprovalToken(token + "corrupt"); ok {
		t.Fatal("expected corrupted token validation to fail")
	}

	// Test VerifyGateToken
	if err := svc.VerifyGateToken(runID, token); err != nil {
		t.Fatalf("expected VerifyGateToken to succeed, got: %v", err)
	}
	if err := svc.VerifyGateToken("", token); err != nil {
		t.Fatalf("expected VerifyGateToken with empty expected runID to succeed, got: %v", err)
	}
	if err := svc.VerifyGateToken("different-run-id", token); err != ErrRunIDMismatch {
		t.Fatalf("expected ErrRunIDMismatch, got: %v", err)
	}
	if err := svc.VerifyGateToken(runID, expiredToken); err != ErrInvalidApprovalToken {
		t.Fatalf("expected ErrInvalidApprovalToken for expired token, got: %v", err)
	}
	if err := svc.VerifyGateToken(runID, ""); err != ErrInvalidApprovalToken {
		t.Fatalf("expected ErrInvalidApprovalToken for empty token, got: %v", err)
	}
}
