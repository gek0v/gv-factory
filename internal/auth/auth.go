// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Service provides authentication and cryptographic verification for gate approvals.
type Service struct {
	secretKey []byte
}

// NewService constructs an Auth service with the provided signing key.
func NewService(secret string) *Service {
	return &Service{
		secretKey: []byte(secret),
	}
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
