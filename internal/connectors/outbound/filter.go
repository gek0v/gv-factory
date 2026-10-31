// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"strings"
)

// Matches returns true if the subscription is active and has registered for the given event type.
func (s Subscription) Matches(eventType string) bool {
	if !s.Active {
		return false
	}
	if len(s.EventTypes) == 0 {
		return false
	}
	for _, pattern := range s.EventTypes {
		if MatchesEvent(pattern, eventType) {
			return true
		}
	}
	return false
}

// MatchesEvent evaluates whether an event type matches a subscription pattern.
// Supported patterns:
//   - Exact match: "task.completed" == "task.completed"
//   - Global wildcard: "*", ">", "all"
//   - Hierarchical category: "task.*" or "task.>" matches "task.completed", "task.started", etc.
//   - Dot-separated wildcard tokens: "a.*.c" matches "a.b.c"
func MatchesEvent(pattern, eventType string) bool {
	pattern = strings.TrimSpace(pattern)
	eventType = strings.TrimSpace(eventType)

	if pattern == "" || eventType == "" {
		return false
	}

	if pattern == "*" || pattern == ">" || strings.EqualFold(pattern, "all") {
		return true
	}

	if pattern == eventType {
		return true
	}

	pParts := strings.Split(pattern, ".")
	eParts := strings.Split(eventType, ".")

	for i := 0; i < len(pParts); i++ {
		if pParts[i] == ">" {
			return true
		}
		if i >= len(eParts) {
			return false
		}
		if pParts[i] != "*" && pParts[i] != eParts[i] {
			return false
		}
	}

	return len(pParts) == len(eParts)
}
