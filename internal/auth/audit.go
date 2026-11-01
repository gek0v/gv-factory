// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package auth

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// AuditStatus represents the outcome of an approval token verification and gate decision.
type AuditStatus string

const (
	AuditStatusSuccess          AuditStatus = "SUCCESS"
	AuditStatusInvalidSignature AuditStatus = "INVALID_SIGNATURE"
	AuditStatusExpired          AuditStatus = "EXPIRED"
	AuditStatusRunIDMismatch    AuditStatus = "RUN_ID_MISMATCH"
	AuditStatusInvalidDecision  AuditStatus = "INVALID_DECISION"
	AuditStatusMalformedToken   AuditStatus = "MALFORMED_TOKEN"
)

// AuditRecord captures immutable audit metadata for human-in-the-loop gate resolutions.
type AuditRecord struct {
	Timestamp       time.Time   `json:"timestamp"`
	RunID           string      `json:"run_id"`
	Decision        string      `json:"decision"`
	ClientIP        string      `json:"client_ip"`
	UserAgent       string      `json:"user_agent"`
	TokenHMACPrefix string      `json:"token_hmac_prefix"`
	Status          AuditStatus `json:"status"`
	Details         string      `json:"details,omitempty"`
}

// AuditLogger defines the interface for recording gate decision audit entries.
type AuditLogger interface {
	Record(record AuditRecord)
	GetRecords() []AuditRecord
}

// MemoryAuditLogger stores an append-only ring buffer of audit entries in-memory and logs structured records.
type MemoryAuditLogger struct {
	mu      sync.RWMutex
	records []AuditRecord
	maxSize int
	logger  *slog.Logger
}

// NewMemoryAuditLogger constructs a MemoryAuditLogger with the specified maximum history capacity.
func NewMemoryAuditLogger(capacity int) *MemoryAuditLogger {
	if capacity <= 0 {
		capacity = 1000
	}
	return &MemoryAuditLogger{
		records: make([]AuditRecord, 0, capacity),
		maxSize: capacity,
		logger:  slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

// Record appends an audit entry and emits a structured slog JSON log.
func (l *MemoryAuditLogger) Record(record AuditRecord) {
	l.mu.Lock()
	if len(l.records) >= l.maxSize {
		l.records = l.records[1:]
	}
	l.records = append(l.records, record)
	l.mu.Unlock()

	l.logger.Info("gate.audit",
		"timestamp", record.Timestamp.Format(time.RFC3339Nano),
		"run_id", record.RunID,
		"decision", record.Decision,
		"client_ip", record.ClientIP,
		"user_agent", record.UserAgent,
		"token_hmac_prefix", record.TokenHMACPrefix,
		"status", string(record.Status),
		"details", record.Details,
	)
}

// GetRecords returns a shallow copy of all recorded audit entries.
func (l *MemoryAuditLogger) GetRecords() []AuditRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := make([]AuditRecord, len(l.records))
	copy(res, l.records)
	return res
}

// ExtractTokenPrefix safely extracts the first 8 characters of the token HMAC signature
// without exposing the full secret signature.
func ExtractTokenPrefix(token string) string {
	parts := strings.Split(token, ":")
	if len(parts) == 3 && len(parts[2]) > 0 {
		sig := parts[2]
		if len(sig) > 8 {
			return sig[:8] + "..."
		}
		return sig
	}
	if len(token) > 8 {
		return token[:8] + "..."
	}
	return token
}

// MarshalAuditRecord serializes the audit record to JSON.
func (r AuditRecord) JSON() string {
	b, err := json.Marshal(r)
	if err != nil {
		return "{}"
	}
	return string(b)
}
