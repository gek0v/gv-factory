// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidApprovalToken is returned when an HMAC gate approval token fails validation or has expired.
	ErrInvalidApprovalToken = errors.New("auth: invalid or expired approval token")

	// ErrRunIDMismatch is returned when the approval token's run ID does not match the expected run ID.
	ErrRunIDMismatch = errors.New("auth: run ID mismatch")
)

// Service provides authentication and cryptographic verification for gate approvals.
type Service struct {
	secretKey   []byte
	auditLogger AuditLogger
}

// NewService constructs an Auth service with the provided signing key.
func NewService(secret string, auditLoggers ...AuditLogger) *Service {
	var al AuditLogger
	if len(auditLoggers) > 0 && auditLoggers[0] != nil {
		al = auditLoggers[0]
	} else {
		al = NewMemoryAuditLogger(1000)
	}
	return &Service{
		secretKey:   []byte(secret),
		auditLogger: al,
	}
}

// AuditLogger returns the configured audit logger instance.
func (s *Service) AuditLogger() AuditLogger {
	return s.auditLogger
}

// GenerateApprovalToken generates an opaque HMAC-signed token for Human-in-the-Loop approval.
func (s *Service) GenerateApprovalToken(runID string, expiresAt time.Time) string {
	payload := fmt.Sprintf("%s:%d", runID, expiresAt.Unix())
	h := hmac.New(sha256.New, s.secretKey)
	h.Write([]byte(payload))
	sig := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%s:%s", payload, sig)
}

// ValidateApprovalToken validates whether an approval token is authentic and not expired.
func (s *Service) ValidateApprovalToken(token string) (string, bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return "", false
	}
	runID := parts[0]
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", false
	}
	sig := parts[2]

	if time.Now().Unix() > exp {
		return "", false
	}

	payload := fmt.Sprintf("%s:%d", runID, exp)
	h := hmac.New(sha256.New, s.secretKey)
	h.Write([]byte(payload))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", false
	}

	return runID, true
}

// VerifyGateToken validates that an HMAC approval token is authentic, unexpired,
// and matches the specified runID (if runID is non-empty).
func (s *Service) VerifyGateToken(runID, token string) error {
	if s == nil {
		return errors.New("auth: service not configured")
	}
	if token == "" {
		return ErrInvalidApprovalToken
	}
	extractedRunID, valid := s.ValidateApprovalToken(token)
	if !valid {
		return ErrInvalidApprovalToken
	}
	if runID != "" && extractedRunID != runID {
		return ErrRunIDMismatch
	}
	return nil
}
