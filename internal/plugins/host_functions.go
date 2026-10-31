// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

// ErrAccessDenied is returned when an operation attempts to breach security boundaries.
var ErrAccessDenied = errors.New("access denied")

// Context keys for plugin execution.
type contextKey string

const (
	// RunIDContextKey stores the active run ID within context.Context.
	RunIDContextKey contextKey = "gv_run_id"
)

// WithRunID associates a run identifier with the context.
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, RunIDContextKey, runID)
}

// RunIDFromContext extracts the run identifier from context.
func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(RunIDContextKey).(string); ok {
		return v
	}
	return ""
}

// BrainService defines the subset of brain capabilities required by host functions.
type BrainService interface {
	ListRules(ctx context.Context, limit int) ([]brain.ArchitecturalRule, error)
	QueryRelevantRulesByVector(ctx context.Context, embedding []float32, limit int) ([]brain.ArchitecturalRule, error)
	GetRuleByCode(ctx context.Context, ruleCode string) (*brain.ArchitecturalRule, error)
}

// WorktreeResolver resolves a runID to its absolute worktree filesystem root.
type WorktreeResolver func(runID string) (string, error)

// LogSink handles output of scrubbed log events.
type LogSink func(event LogEvent)

// LogEvent represents a structured log payload emitted by a WASM plugin.
type LogEvent struct {
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Timestamp string         `json:"timestamp,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// LogResponse is sent back to WASM guests acknowledging log emission.
type LogResponse struct {
	Status string `json:"status"`
	Masked bool   `json:"masked"`
}

// BrainQueryRequest specifies criteria for querying architectural rules.
type BrainQueryRequest struct {
	RuleCode  string    `json:"rule_code,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// WorkspaceReadRequest represents an input payload for reading a workspace file.
type WorkspaceReadRequest struct {
	RunID string `json:"run_id,omitempty"`
	Path  string `json:"path"`
}

// WorkspaceReadResponse holds the outcome of a workspace file read attempt.
type WorkspaceReadResponse struct {
	Success bool   `json:"success"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HostFunctionRegistry manages secure host functions exposed to Extism WASM plugins.
type HostFunctionRegistry struct {
	brainSvc         BrainService
	secrets          []string
	worktreeResolver WorktreeResolver
	staticWorktrees  map[string]string
	logSink          LogSink
	defaultRunID     string
	namespace        string
	mu               sync.RWMutex
}

// RegistryOption configures the HostFunctionRegistry.
type RegistryOption func(*HostFunctionRegistry)

// WithBrainService injects the brain service dependency.
func WithBrainService(svc BrainService) RegistryOption {
	return func(r *HostFunctionRegistry) {
		r.brainSvc = svc
	}
}

// WithSecrets configures sensitive credentials to mask in plugin logs.
func WithSecrets(secrets ...string) RegistryOption {
	return func(r *HostFunctionRegistry) {
		r.secrets = append(r.secrets, secrets...)
	}
}

// WithWorktreeResolver configures a custom function to resolve worktrees.
func WithWorktreeResolver(resolver WorktreeResolver) RegistryOption {
	return func(r *HostFunctionRegistry) {
		r.worktreeResolver = resolver
	}
}

// WithSandboxManager bridges a sandbox Manager for worktree resolution.
func WithSandboxManager(mgr *sandbox.Manager) RegistryOption {
	return func(r *HostFunctionRegistry) {
		if mgr != nil {
			r.worktreeResolver = func(runID string) (string, error) {
				path, ok := mgr.GetWorktreePath(runID)
				if !ok {
					return "", fmt.Errorf("worktree for run %q not found", runID)
				}
				return path, nil
			}
		}
	}
}

// WithLogSink configures a custom sink for scrubbed log events.
func WithLogSink(sink LogSink) RegistryOption {
	return func(r *HostFunctionRegistry) {
		r.logSink = sink
	}
}

// WithDefaultRunID configures a fallback runID when not specified in context or payload.
func WithDefaultRunID(runID string) RegistryOption {
	return func(r *HostFunctionRegistry) {
		r.defaultRunID = runID
	}
}

// WithNamespace overrides the default Extism host function namespace.
func WithNamespace(ns string) RegistryOption {
	return func(r *HostFunctionRegistry) {
		r.namespace = ns
	}
}

// NewHostFunctionRegistry creates a new registry configured with options.
func NewHostFunctionRegistry(opts ...RegistryOption) *HostFunctionRegistry {
	r := &HostFunctionRegistry{
		secrets:         make([]string, 0),
		staticWorktrees: make(map[string]string),
		namespace:       "extism:host/user",
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RegisterSecret adds a sensitive value to the active mask list.
func (r *HostFunctionRegistry) RegisterSecret(secret string) {
	if strings.TrimSpace(secret) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets = append(r.secrets, secret)
}

// RegisterWorktree statically registers a worktree path for a specific runID.
func (r *HostFunctionRegistry) RegisterWorktree(runID, path string) {
	if strings.TrimSpace(runID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.staticWorktrees[runID] = path
}

// SetDefaultRunID updates the default runID for the registry.
func (r *HostFunctionRegistry) SetDefaultRunID(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultRunID = runID
}

// BrainQueryRules executes a rules query against the brain service and returns a JSON array.
func (r *HostFunctionRegistry) BrainQueryRules(ctx context.Context, input []byte) ([]byte, error) {
	r.mu.RLock()
	brainSvc := r.brainSvc
	r.mu.RUnlock()

	if brainSvc == nil {
		return []byte("[]"), nil
	}

	var req BrainQueryRequest
	if len(input) > 0 {
		_ = json.Unmarshal(input, &req)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	var rules []brain.ArchitecturalRule
	var err error

	if req.RuleCode != "" {
		var rule *brain.ArchitecturalRule
		rule, err = brainSvc.GetRuleByCode(ctx, req.RuleCode)
		if err == nil && rule != nil {
			rules = []brain.ArchitecturalRule{*rule}
		} else {
			rules = []brain.ArchitecturalRule{}
			if errors.Is(err, brain.ErrRuleNotFound) || (err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")) {
				err = nil
			}
		}
	} else if len(req.Embedding) > 0 {
		rules, err = brainSvc.QueryRelevantRulesByVector(ctx, req.Embedding, limit)
	} else {
		rules, err = brainSvc.ListRules(ctx, limit)
	}

	if err != nil {
		return []byte("[]"), err
	}

	if rules == nil {
		rules = []brain.ArchitecturalRule{}
	}

	return json.Marshal(rules)
}

// LogEvent sanitizes secrets from log events and forwards them to the configured sink or logger.
func (r *HostFunctionRegistry) LogEvent(ctx context.Context, input []byte) ([]byte, error) {
	var event LogEvent
	if err := json.Unmarshal(input, &event); err != nil {
		event = LogEvent{
			Level:   "INFO",
			Message: string(input),
		}
	}

	if strings.TrimSpace(event.Level) == "" {
		event.Level = "INFO"
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	r.mu.RLock()
	secrets := append([]string(nil), r.secrets...)
	sink := r.logSink
	r.mu.RUnlock()

	// Mask secrets from message and field values
	event.Message = sandbox.MaskSecrets(event.Message, secrets)
	if event.Fields != nil {
		maskedFields := make(map[string]any, len(event.Fields))
		for k, v := range event.Fields {
			if strVal, ok := v.(string); ok {
				maskedFields[k] = sandbox.MaskSecrets(strVal, secrets)
			} else {
				maskedFields[k] = v
			}
		}
		event.Fields = maskedFields
	}

	if sink != nil {
		sink(event)
	} else {
		log.Printf("[%s] (wasm-plugin) %s", strings.ToUpper(event.Level), event.Message)
	}

	return json.Marshal(LogResponse{Status: "ok", Masked: true})
}

// WorkspaceReadFile securely reads a file strictly within the assigned runID worktree boundary.
func (r *HostFunctionRegistry) WorkspaceReadFile(ctx context.Context, input []byte) ([]byte, error) {
	var req WorkspaceReadRequest
	if len(input) > 0 {
		if err := json.Unmarshal(input, &req); err != nil {
			req.Path = string(input)
		}
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		err := fmt.Errorf("%w: path cannot be empty", ErrAccessDenied)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: err.Error()})
		return resp, err
	}

	if strings.ContainsRune(req.Path, 0) {
		err := fmt.Errorf("%w: invalid null byte in path", ErrAccessDenied)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: err.Error()})
		return resp, err
	}

	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = RunIDFromContext(ctx)
	}
	if runID == "" {
		r.mu.RLock()
		runID = r.defaultRunID
		r.mu.RUnlock()
	}

	if runID == "" {
		err := fmt.Errorf("%w: run_id is required", ErrAccessDenied)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: err.Error()})
		return resp, err
	}

	worktreeRoot, err := r.resolveWorktreeRoot(runID)
	if err != nil {
		accessErr := fmt.Errorf("%w: %v", ErrAccessDenied, err)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: accessErr.Error()})
		return resp, accessErr
	}

	rootAbs, err := filepath.Abs(worktreeRoot)
	if err != nil {
		accessErr := fmt.Errorf("%w: invalid worktree root: %v", ErrAccessDenied, err)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: accessErr.Error()})
		return resp, accessErr
	}

	cleanReq := filepath.Clean(req.Path)

	// Explicit path traversal pattern rejection (e.g. "../" or "..\")
	if cleanReq == ".." || strings.HasPrefix(cleanReq, ".."+string(filepath.Separator)) || strings.HasPrefix(cleanReq, "../") || strings.HasPrefix(cleanReq, `..\`) {
		accessErr := fmt.Errorf("%w: path traversal detected: %q", ErrAccessDenied, req.Path)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: accessErr.Error()})
		return resp, accessErr
	}

	var targetAbs string
	if filepath.IsAbs(cleanReq) {
		targetAbs = cleanReq
	} else {
		targetAbs = filepath.Join(rootAbs, cleanReq)
	}

	targetAbs, err = filepath.Abs(targetAbs)
	if err != nil {
		accessErr := fmt.Errorf("%w: invalid target path: %v", ErrAccessDenied, err)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: accessErr.Error()})
		return resp, accessErr
	}

	// Boundary verification: targetAbs must be strictly contained within rootAbs
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		accessErr := fmt.Errorf("%w: path %q escapes worktree boundaries %q", ErrAccessDenied, req.Path, rootAbs)
		resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: accessErr.Error()})
		return resp, accessErr
	}

	// Symlink evaluation check: ensure real target path does not escape worktree
	realTarget, err := filepath.EvalSymlinks(targetAbs)
	if err == nil {
		realRoot, errRoot := filepath.EvalSymlinks(rootAbs)
		if errRoot == nil {
			relReal, errRel := filepath.Rel(realRoot, realTarget)
			if errRel != nil || strings.HasPrefix(relReal, "..") || relReal == ".." {
				accessErr := fmt.Errorf("%w: symlink target escapes worktree boundaries", ErrAccessDenied)
				resp, _ := json.Marshal(WorkspaceReadResponse{Success: false, Error: accessErr.Error()})
				return resp, accessErr
			}
		}
	}

	content, err := os.ReadFile(targetAbs)
	if err != nil {
		resp, _ := json.Marshal(WorkspaceReadResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to read file: %v", err),
		})
		return resp, err
	}

	resp, _ := json.Marshal(WorkspaceReadResponse{
		Success: true,
		Content: string(content),
	})
	return resp, nil
}

func (r *HostFunctionRegistry) resolveWorktreeRoot(runID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if staticPath, ok := r.staticWorktrees[runID]; ok && staticPath != "" {
		return staticPath, nil
	}

	if r.worktreeResolver != nil {
		return r.worktreeResolver(runID)
	}

	return "", fmt.Errorf("no worktree registered for run %q", runID)
}

// BrainQueryRulesHostFunction constructs the typed Extism host function for brain_query_rules.
func (r *HostFunctionRegistry) BrainQueryRulesHostFunction() extism.HostFunction {
	fn := extism.NewHostFunctionWithStack(
		"brain_query_rules",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			var input []byte
			offset := stack[0]
			if offset != 0 {
				var err error
				input, err = p.ReadBytes(offset)
				if err == nil {
					_ = p.Free(offset)
				}
			}

			outBytes, _ := r.BrainQueryRules(ctx, input)
			outOffset, err := p.WriteBytes(outBytes)
			if err != nil {
				stack[0] = 0
				return
			}
			stack[0] = outOffset
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
	if r.namespace != "" {
		fn.SetNamespace(r.namespace)
	}
	return fn
}

// LogEventHostFunction constructs the typed Extism host function for log_event.
func (r *HostFunctionRegistry) LogEventHostFunction() extism.HostFunction {
	fn := extism.NewHostFunctionWithStack(
		"log_event",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			var input []byte
			offset := stack[0]
			if offset != 0 {
				var err error
				input, err = p.ReadBytes(offset)
				if err == nil {
					_ = p.Free(offset)
				}
			}

			outBytes, _ := r.LogEvent(ctx, input)
			outOffset, err := p.WriteBytes(outBytes)
			if err != nil {
				stack[0] = 0
				return
			}
			stack[0] = outOffset
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
	if r.namespace != "" {
		fn.SetNamespace(r.namespace)
	}
	return fn
}

// WorkspaceReadFileHostFunction constructs the typed Extism host function for workspace_read_file.
func (r *HostFunctionRegistry) WorkspaceReadFileHostFunction() extism.HostFunction {
	fn := extism.NewHostFunctionWithStack(
		"workspace_read_file",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			var input []byte
			offset := stack[0]
			if offset != 0 {
				var err error
				input, err = p.ReadBytes(offset)
				if err == nil {
					_ = p.Free(offset)
				}
			}

			outBytes, _ := r.WorkspaceReadFile(ctx, input)
			outOffset, err := p.WriteBytes(outBytes)
			if err != nil {
				stack[0] = 0
				return
			}
			stack[0] = outOffset
		},
		[]extism.ValueType{extism.ValueTypePTR},
		[]extism.ValueType{extism.ValueTypePTR},
	)
	if r.namespace != "" {
		fn.SetNamespace(r.namespace)
	}
	return fn
}

// HostFunctions returns all registered host function definitions ready for Extism instantiation.
func (r *HostFunctionRegistry) HostFunctions() []extism.HostFunction {
	return []extism.HostFunction{
		r.BrainQueryRulesHostFunction(),
		r.LogEventHostFunction(),
		r.WorkspaceReadFileHostFunction(),
	}
}

// RegisterWithHost registers all host functions from this registry with the specified Host instance.
func (r *HostFunctionRegistry) RegisterWithHost(h *Host) {
	if h == nil {
		return
	}
	for _, fn := range r.HostFunctions() {
		h.RegisterHostFunction(fn)
	}
}
