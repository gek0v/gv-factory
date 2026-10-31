// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"testing"
)

func TestMatchesEvent(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		eventType string
		expected  bool
	}{
		{"exact match task.completed", "task.completed", "task.completed", true},
		{"exact match pr.ready_for_review", "pr.ready_for_review", "pr.ready_for_review", true},
		{"mismatch", "task.completed", "task.started", false},
		{"wildcard star", "*", "task.completed", true},
		{"wildcard star on gate", "*", "gate.approval_required", true},
		{"wildcard gt", ">", "factory.failed", true},
		{"keyword all", "all", "task.failed", true},
		{"category wildcard task.* match", "task.*", "task.completed", true},
		{"category wildcard task.* match started", "task.*", "task.started", true},
		{"category wildcard task.* mismatch pr", "task.*", "pr.ready_for_review", false},
		{"category wildcard pr.* match", "pr.*", "pr.ready_for_review", true},
		{"category wildcard gate.* match", "gate.*", "gate.approval_required", true},
		{"hierarchical category task.>", "task.>", "task.sub.completed", true},
		{"empty pattern", "", "task.completed", false},
		{"empty event", "task.completed", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesEvent(tt.pattern, tt.eventType)
			if got != tt.expected {
				t.Errorf("MatchesEvent(%q, %q) = %v; want %v", tt.pattern, tt.eventType, got, tt.expected)
			}
		})
	}
}

func TestSubscription_Matches(t *testing.T) {
	activeSub := Subscription{
		ID:         "sub-1",
		Active:     true,
		EventTypes: []string{"task.completed", "gate.*"},
	}

	if !activeSub.Matches("task.completed") {
		t.Errorf("expected active subscription to match exact event")
	}
	if !activeSub.Matches("gate.approval_required") {
		t.Errorf("expected active subscription to match wildcard pattern")
	}
	if activeSub.Matches("pr.ready_for_review") {
		t.Errorf("expected active subscription not to match unsubscribed event")
	}

	inactiveSub := Subscription{
		ID:         "sub-2",
		Active:     false,
		EventTypes: []string{"task.*"},
	}
	if inactiveSub.Matches("task.completed") {
		t.Errorf("expected inactive subscription not to match any events")
	}

	emptyEventsSub := Subscription{
		ID:         "sub-3",
		Active:     true,
		EventTypes: []string{},
	}
	if emptyEventsSub.Matches("task.completed") {
		t.Errorf("expected subscription with empty event types not to match")
	}
}
