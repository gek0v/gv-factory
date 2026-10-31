// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"strconv"
	"time"
)

// getString traverses nested maps and extracts a string value.
func getString(m map[string]any, keys ...string) string {
	curr := m
	for i, k := range keys {
		val, exists := curr[k]
		if !exists || val == nil {
			return ""
		}
		if i == len(keys)-1 {
			if s, ok := val.(string); ok {
				return s
			}
			return ""
		}
		next, ok := val.(map[string]any)
		if !ok {
			return ""
		}
		curr = next
	}
	return ""
}

// getInt traverses nested maps and extracts an integer value (handling float64 from JSON).
func getInt(m map[string]any, keys ...string) int {
	curr := m
	for i, k := range keys {
		val, exists := curr[k]
		if !exists || val == nil {
			return 0
		}
		if i == len(keys)-1 {
			switch v := val.(type) {
			case float64:
				return int(v)
			case int:
				return v
			case int64:
				return int(v)
			case string:
				n, _ := strconv.Atoi(v)
				return n
			default:
				return 0
			}
		}
		next, ok := val.(map[string]any)
		if !ok {
			return 0
		}
		curr = next
	}
	return 0
}

// getBool traverses nested maps and extracts a boolean value.
func getBool(m map[string]any, keys ...string) (bool, bool) {
	curr := m
	for i, k := range keys {
		val, exists := curr[k]
		if !exists || val == nil {
			return false, false
		}
		if i == len(keys)-1 {
			if b, ok := val.(bool); ok {
				return b, true
			}
			return false, false
		}
		next, ok := val.(map[string]any)
		if !ok {
			return false, false
		}
		curr = next
	}
	return false, false
}

// parseTime tries parsing common ISO 8601 / RFC 3339 timestamps.
func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05 UTC",
		"2006-01-02 15:04:05",
	}
	for _, fmtStr := range formats {
		if t, err := time.Parse(fmtStr, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
