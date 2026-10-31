// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package schema

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTriggerEvent_Serialization(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ev := TriggerEvent{
		ID:          "trig-123",
		Source:      SourceGitHub,
		EventType:   EventIssueOpened,
		Repository:  "gek0v/gv-factory",
		Reference:   "main",
		IssueNumber: 33,
		Sender:      "gek0v",
		Payload: map[string]any{
			"title": "feat(connectors): multi-provider webhook",
		},
		RawPayload: []byte(`{"title":"feat(connectors): multi-provider webhook"}`),
		Timestamp:  now,
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("failed to marshal TriggerEvent: %v", err)
	}

	var decoded TriggerEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal TriggerEvent: %v", err)
	}

	if decoded.Source != SourceGitHub {
		t.Errorf("expected source %q, got %q", SourceGitHub, decoded.Source)
	}
	if decoded.EventType != EventIssueOpened {
		t.Errorf("expected event_type %q, got %q", EventIssueOpened, decoded.EventType)
	}
	if decoded.Repository != "gek0v/gv-factory" {
		t.Errorf("expected repository %q, got %q", "gek0v/gv-factory", decoded.Repository)
	}
	if decoded.IssueNumber != 33 {
		t.Errorf("expected issue number 33, got %d", decoded.IssueNumber)
	}
	if decoded.Sender != "gek0v" {
		t.Errorf("expected sender %q, got %q", "gek0v", decoded.Sender)
	}
	if decoded.UnixMilli() != now.UnixMilli() {
		t.Errorf("expected UnixMilli %d, got %d", now.UnixMilli(), decoded.UnixMilli())
	}
}
