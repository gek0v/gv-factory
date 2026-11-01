// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package auth

import (
	"testing"
	"time"
)

func TestAuditLogger_RecordAndRingBuffer(t *testing.T) {
	logger := NewMemoryAuditLogger(3)

	for i := 1; i <= 5; i++ {
		logger.Record(AuditRecord{
			Timestamp:       time.Now(),
			RunID:           "run-" + string(rune('0'+i)),
			Decision:        "approved",
			ClientIP:        "127.0.0.1",
			UserAgent:       "test-agent",
			TokenHMACPrefix: "abc12345...",
			Status:          AuditStatusSuccess,
		})
	}

	records := logger.GetRecords()
	if len(records) != 3 {
		t.Fatalf("expected 3 records in ring buffer, got %d", len(records))
	}
	if records[0].RunID != "run-3" {
		t.Errorf("expected first record to be run-3, got %s", records[0].RunID)
	}
	if records[2].RunID != "run-5" {
		t.Errorf("expected last record to be run-5, got %s", records[2].RunID)
	}
}

func TestExtractTokenPrefix(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "valid 3-part token",
			token:    "run-123:1700000000:abcdef0123456789abcdef",
			expected: "abcdef01...",
		},
		{
			name:     "short signature",
			token:    "run-123:1700000000:abcd",
			expected: "abcd",
		},
		{
			name:     "opaque token long",
			token:    "1234567890abcdef",
			expected: "12345678...",
		},
		{
			name:     "opaque token short",
			token:    "12345",
			expected: "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTokenPrefix(tt.token)
			if got != tt.expected {
				t.Errorf("ExtractTokenPrefix(%q) = %q, want %q", tt.token, got, tt.expected)
			}
		})
	}
}
