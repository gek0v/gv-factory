// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"path"
	"strings"
	"time"
)

// ToolSecurityPolicy establishes Human-in-the-Loop decision constraints for tools.
type ToolSecurityPolicy struct {
	NamePattern      string        `json:"name_pattern"`
	RequiresApproval bool          `json:"requires_approval"`
	ApprovalTimeout  time.Duration `json:"approval_timeout"`
	Description      string        `json:"description,omitempty"`
}

// Matches returns true if toolName satisfies the policy's name pattern.
// It supports exact matching, path.Match glob patterns, and prefix wildcards.
func (p ToolSecurityPolicy) Matches(toolName string) bool {
	if p.NamePattern == "" {
		return false
	}
	if p.NamePattern == toolName {
		return true
	}
	// Try standard path.Match (e.g. "fs_*", "exec_?")
	if matched, err := path.Match(p.NamePattern, toolName); err == nil && matched {
		return true
	}
	// Fallback simple prefix check if pattern ends with *
	if strings.HasSuffix(p.NamePattern, "*") {
		prefix := strings.TrimSuffix(p.NamePattern, "*")
		if strings.HasPrefix(toolName, prefix) {
			return true
		}
	}
	return false
}
