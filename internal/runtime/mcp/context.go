// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import "context"

// ExecutionContext encapsulates runtime identifiers required for governance, security gates, and auditing.
type ExecutionContext struct {
	RunID     string         `json:"run_id"`
	CallerID  string         `json:"caller_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type mcpContextKey struct{}

// WithExecutionContext returns a copy of parent context in which the ExecutionContext is set.
func WithExecutionContext(ctx context.Context, ec ExecutionContext) context.Context {
	return context.WithValue(ctx, mcpContextKey{}, ec)
}

// ExecutionContextFromContext extracts the ExecutionContext from ctx, if present.
func ExecutionContextFromContext(ctx context.Context) (ExecutionContext, bool) {
	if ctx == nil {
		return ExecutionContext{}, false
	}
	ec, ok := ctx.Value(mcpContextKey{}).(ExecutionContext)
	return ec, ok
}
