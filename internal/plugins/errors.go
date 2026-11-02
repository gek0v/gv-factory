// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrorCode represents a structured category of plugin runtime or configuration error.
type ErrorCode string

const (
	ErrCodeInvalidManifest  ErrorCode = "INVALID_MANIFEST"
	ErrCodePluginNotFound   ErrorCode = "PLUGIN_NOT_FOUND"
	ErrCodeFunctionNotFound ErrorCode = "FUNCTION_NOT_FOUND"
	ErrCodeExecutionTimeout ErrorCode = "EXECUTION_TIMEOUT"
	ErrCodeMemoryFault      ErrorCode = "MEMORY_FAULT"
	ErrCodeTrap             ErrorCode = "TRAP"
	ErrCodeRuntime          ErrorCode = "RUNTIME_ERROR"
	ErrCodeClosed           ErrorCode = "PLUGIN_CLOSED"
	ErrCodeInvalidSource    ErrorCode = "INVALID_SOURCE"
)

var (
	ErrInvalidManifest  = errors.New("invalid plugin manifest")
	ErrPluginNotFound   = errors.New("plugin not found")
	ErrFunctionNotFound = errors.New("function not found")
	ErrExecutionTimeout = errors.New("plugin execution timeout")
	ErrMemoryFault      = errors.New("wasm memory fault")
	ErrTrap             = errors.New("wasm execution trap")
	ErrPluginClosed     = errors.New("plugin instance is closed")
	ErrInvalidSource    = errors.New("invalid plugin source")
	ErrPluginRuntime    = errors.New("plugin runtime error")
)

// PluginError wraps runtime, memory, and lifecycle failures with structured metadata.
type PluginError struct {
	Code     ErrorCode `json:"code"`
	Plugin   string    `json:"plugin,omitempty"`
	Function string    `json:"function,omitempty"`
	Message  string    `json:"message"`
	Err      error     `json:"-"`
}

func (e *PluginError) Error() string {
	if e.Plugin != "" && e.Function != "" {
		return fmt.Sprintf("[%s] plugin %q function %q: %s", e.Code, e.Plugin, e.Function, e.Message)
	}
	if e.Plugin != "" {
		return fmt.Sprintf("[%s] plugin %q: %s", e.Code, e.Plugin, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *PluginError) Unwrap() error {
	return e.Err
}

func (e *PluginError) Is(target error) bool {
	if target == nil {
		return false
	}
	if errors.Is(e.Err, target) {
		return true
	}
	switch target {
	case ErrInvalidManifest:
		return e.Code == ErrCodeInvalidManifest
	case ErrPluginNotFound:
		return e.Code == ErrCodePluginNotFound
	case ErrFunctionNotFound:
		return e.Code == ErrCodeFunctionNotFound
	case ErrExecutionTimeout:
		return e.Code == ErrCodeExecutionTimeout
	case ErrMemoryFault:
		return e.Code == ErrCodeMemoryFault
	case ErrTrap:
		return e.Code == ErrCodeTrap
	case ErrPluginClosed:
		return e.Code == ErrCodeClosed
	case ErrInvalidSource:
		return e.Code == ErrCodeInvalidSource
	case ErrPluginRuntime:
		return e.Code == ErrCodeRuntime
	default:
		return false
	}
}

func classifyError(plugin, function string, exitCode uint32, err error, ctx context.Context) error {
	if err == nil {
		return nil
	}

	errMsg := err.Error()
	errLower := strings.ToLower(errMsg)

	if (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) ||
		errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(errLower, "deadline exceeded") ||
		strings.Contains(errLower, "timed out") ||
		strings.Contains(errLower, "timeout") {
		return &PluginError{
			Code:     ErrCodeExecutionTimeout,
			Plugin:   plugin,
			Function: function,
			Message:  fmt.Sprintf("execution timeout exceeded: %v", err),
			Err:      ErrExecutionTimeout,
		}
	}

	if strings.Contains(errLower, "memory") ||
		strings.Contains(errLower, "out of bounds") ||
		strings.Contains(errLower, "grow") ||
		strings.Contains(errLower, "pages") ||
		strings.Contains(errLower, "over limit") {
		return &PluginError{
			Code:     ErrCodeMemoryFault,
			Plugin:   plugin,
			Function: function,
			Message:  fmt.Sprintf("wasm memory fault: %v", err),
			Err:      ErrMemoryFault,
		}
	}

	if strings.Contains(errLower, "trap") ||
		strings.Contains(errLower, "unreachable") ||
		strings.Contains(errLower, "integer divide by zero") ||
		strings.Contains(errLower, "invalid conversion to integer") {
		return &PluginError{
			Code:     ErrCodeTrap,
			Plugin:   plugin,
			Function: function,
			Message:  fmt.Sprintf("wasm trap: %v", err),
			Err:      ErrTrap,
		}
	}

	return &PluginError{
		Code:     ErrCodeRuntime,
		Plugin:   plugin,
		Function: function,
		Message:  errMsg,
		Err:      err,
	}
}
