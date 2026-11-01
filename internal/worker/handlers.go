// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gv-factory/gv-factory/internal/sandbox"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
)

// WasmCaller abstracts execution of WebAssembly functions. Satisfied by *plugins.Host.
type WasmCaller interface {
	Call(ctx context.Context, pluginRef string, functionName string, input []byte) ([]byte, error)
	CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error)
}

// SandboxManager abstracts isolated Git Worktree management. Satisfied by *sandbox.Manager.
type SandboxManager interface {
	ProvisionWorktreeWithOptions(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error)
	GetWorktreePath(runID string) (string, bool)
	GetEnvironment(runID string) (*sandbox.Environment, bool)
	RecycleWorktree(ctx context.Context, runID string) error
	Destroy(ctx context.Context, runID string) error
}

// CommandExecutor abstracts OS process execution in sandboxes.
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, dir string, name string, args []string, env []string) (stdout string, stderr string, exitCode int, err error)
}

// StreamingCommandExecutor extends CommandExecutor to support real-time piping into io.Writer sinks.
type StreamingCommandExecutor interface {
	CommandExecutor
	ExecuteCommandStreaming(
		ctx context.Context,
		dir string,
		name string,
		args []string,
		env []string,
		stdoutWriter io.Writer,
		stderrWriter io.Writer,
	) (exitCode int, err error)
}

// RuntimeMetadataProvider optionally exposes execution engine details for task result metadata.
type RuntimeMetadataProvider interface {
	GetRuntimeMetadata() map[string]any
}

// DefaultCommandExecutor executes commands via standard OS subprocesses.
type DefaultCommandExecutor struct{}

// ExecuteCommandStreaming executes an OS command and streams stdout/stderr directly into writers.
func (e *DefaultCommandExecutor) ExecuteCommandStreaming(
	ctx context.Context,
	dir string,
	name string,
	args []string,
	env []string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return exitCode, err
}

// ExecuteCommand runs an OS command inside the specified directory with given arguments and environment.
func (e *DefaultCommandExecutor) ExecuteCommand(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode, err := e.ExecuteCommandStreaming(ctx, dir, name, args, env, &stdoutBuf, &stderrBuf)
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// AgentStepExecutor abstracts agent cognition and step execution.
type AgentStepExecutor interface {
	ExecuteStep(ctx context.Context, step AgentStepPayload, task *TaskMessage) (*TaskResult, error)
}

// --- Payloads ---

// WasmPluginPayload holds parameters for WASM plugin invocation.
type WasmPluginPayload struct {
	PluginURI  string            `json:"plugin_uri,omitempty"`
	PluginRef  string            `json:"plugin_ref,omitempty"`
	Entrypoint string            `json:"entrypoint,omitempty"`
	Function   string            `json:"function,omitempty"`
	Configs    map[string]string `json:"configs,omitempty"`
	Arguments  map[string]any    `json:"arguments,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	Input      json.RawMessage   `json:"input,omitempty"`
}

// TargetPlugin returns the target plugin identifier or path.
func (p *WasmPluginPayload) TargetPlugin() string {
	if p.PluginURI != "" {
		return p.PluginURI
	}
	return p.PluginRef
}

// TargetFunction returns the entrypoint function to call.
func (p *WasmPluginPayload) TargetFunction() string {
	if p.Function != "" {
		return p.Function
	}
	if p.Entrypoint != "" {
		return p.Entrypoint
	}
	return "run_test"
}

// ResolvedConfigs merges Configs and string Arguments into a unified map.
func (p *WasmPluginPayload) ResolvedConfigs() map[string]string {
	res := make(map[string]string)
	for k, v := range p.Configs {
		res[k] = v
	}
	for k, v := range p.Arguments {
		if s, ok := v.(string); ok && res[k] == "" {
			res[k] = s
		}
	}
	return res
}

// GitSandboxPayload defines command execution parameters inside a sandbox.
type GitSandboxPayload struct {
	Repository   string            `json:"repository,omitempty"`
	RepoPath     string            `json:"repo_path,omitempty"`
	Branch       string            `json:"branch,omitempty"`
	Worktree     string            `json:"worktree,omitempty"`
	WorktreePath string            `json:"worktree_path,omitempty"`
	WorkingDir   string            `json:"working_dir,omitempty"`
	Command      string            `json:"command"`
	Arguments    []string          `json:"arguments,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Secrets      []string          `json:"secrets,omitempty"`
	TimeoutMS    int64             `json:"timeout_ms,omitempty"`
	MultiTenant  bool              `json:"multi_tenant,omitempty"`
	RecycleAfter bool              `json:"recycle_after,omitempty"`
	CleanLocks   bool              `json:"clean_locks,omitempty"`
}

// TargetCommand returns the command to execute.
func (p *GitSandboxPayload) TargetCommand() string {
	return strings.TrimSpace(p.Command)
}

// TargetArgs returns command line arguments.
func (p *GitSandboxPayload) TargetArgs() []string {
	if len(p.Args) > 0 {
		return p.Args
	}
	return p.Arguments
}

// TargetEnv returns environment variables as a slice of KEY=VALUE strings.
func (p *GitSandboxPayload) TargetEnv() []string {
	merged := make(map[string]string)
	for k, v := range p.Environment {
		merged[k] = v
	}
	for k, v := range p.Env {
		merged[k] = v
	}
	list := make([]string, 0, len(merged))
	for k, v := range merged {
		list = append(list, fmt.Sprintf("%s=%s", k, v))
	}
	return list
}

// AgentStepPayload describes a structured cognitive agent step.
type AgentStepPayload struct {
	StepName    string            `json:"step_name,omitempty"`
	Role        string            `json:"role,omitempty"`   // "spec_writer", "tdd", "reviewer", "gate"
	Action      string            `json:"action,omitempty"` // "plan", "execute", "validate", "refactor"
	Prompt      string            `json:"prompt,omitempty"`
	Context     map[string]any    `json:"context,omitempty"`
	Model       string            `json:"model,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	TimeoutMS   int64             `json:"timeout_ms,omitempty"`
}

// --- Handlers ---

// WasmPluginHandler executes WASM plugin tasks via WasmCaller.
type WasmPluginHandler struct {
	caller WasmCaller
}

// NewWasmPluginHandler creates an initialized WasmPluginHandler.
func NewWasmPluginHandler(caller WasmCaller) *WasmPluginHandler {
	return &WasmPluginHandler{caller: caller}
}

// HandleTask executes the WASM plugin according to TaskMessage payload.
func (h *WasmPluginHandler) HandleTask(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
	if h.caller == nil {
		return nil, errors.New("wasm caller not initialized")
	}

	if len(task.Payload) == 0 || strings.TrimSpace(string(task.Payload)) == "" {
		return &TaskResult{
			Success: true,
			Output:  fmt.Sprintf("wasm plugin completed (noop) for task %s", task.TaskID),
		}, nil
	}

	var payload WasmPluginPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wasm plugin payload: %w", err)
	}

	pluginRef := payload.TargetPlugin()
	if pluginRef == "" {
		return &TaskResult{
			Success: true,
			Output:  fmt.Sprintf("wasm plugin completed (noop) for task %s", task.TaskID),
		}, nil
	}

	functionName := payload.TargetFunction()

	// If raw Input is provided, execute Call directly
	if len(payload.Input) > 0 {
		outBytes, err := h.caller.Call(ctx, pluginRef, functionName, payload.Input)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
				return nil, NewRetryableError(fmt.Errorf("wasm plugin execution timed out: %w", err), 500*time.Millisecond)
			}
			return nil, fmt.Errorf("wasm plugin call failed: %w", err)
		}
		return &TaskResult{
			Success: true,
			Output:  string(outBytes),
			Data:    map[string]any{"raw_output": string(outBytes)},
		}, nil
	}

	// Standard PDK invocation
	req := pdk.Context{
		RunID:   task.RunID,
		TaskID:  task.TaskID,
		Configs: payload.ResolvedConfigs(),
	}

	res, err := h.caller.CallPDK(ctx, pluginRef, functionName, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
			return nil, NewRetryableError(fmt.Errorf("wasm plugin execution timed out: %w", err), 500*time.Millisecond)
		}
		return nil, fmt.Errorf("wasm plugin pdk invocation failed: %w", err)
	}

	if res == nil {
		return &TaskResult{
			Success: true,
			Output:  fmt.Sprintf("wasm plugin %s executed successfully", pluginRef),
		}, nil
	}

	taskRes := &TaskResult{
		Success: res.Success,
		Output:  res.Output,
		Data:    res.Data,
	}
	if !res.Success {
		taskRes.Error = res.Output
	}

	return taskRes, nil
}

// GitSandboxHandler executes commands in isolated Git Sandboxes.
type GitSandboxHandler struct {
	mgr       SandboxManager
	executor  CommandExecutor
	publisher EventPublisher
	workerID  string
	chunkSize int
}

// SandboxHandlerOption configures GitSandboxHandler.
type SandboxHandlerOption func(*GitSandboxHandler)

// WithCommandExecutor configures a custom CommandExecutor.
func WithCommandExecutor(exec CommandExecutor) SandboxHandlerOption {
	return func(h *GitSandboxHandler) {
		h.executor = exec
	}
}

// WithSandboxPublisher configures an EventPublisher for real-time log chunk streaming.
func WithSandboxPublisher(pub EventPublisher) SandboxHandlerOption {
	return func(h *GitSandboxHandler) {
		h.publisher = pub
	}
}

// WithSandboxWorkerID configures the WorkerID reported in streamed log chunks.
func WithSandboxWorkerID(workerID string) SandboxHandlerOption {
	return func(h *GitSandboxHandler) {
		h.workerID = workerID
	}
}

// WithSandboxChunkSize configures the buffer threshold size for log chunking.
func WithSandboxChunkSize(size int) SandboxHandlerOption {
	return func(h *GitSandboxHandler) {
		h.chunkSize = size
	}
}

// NewGitSandboxHandler creates an initialized GitSandboxHandler.
func NewGitSandboxHandler(mgr SandboxManager, opts ...SandboxHandlerOption) *GitSandboxHandler {
	h := &GitSandboxHandler{
		mgr:      mgr,
		executor: &DefaultCommandExecutor{},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleTask executes a command within an isolated worktree sandbox.
func (h *GitSandboxHandler) HandleTask(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
	if h.mgr == nil {
		return nil, errors.New("sandbox manager not initialized")
	}

	if len(task.Payload) == 0 || strings.TrimSpace(string(task.Payload)) == "" {
		return &TaskResult{
			Success: true,
			Output:  fmt.Sprintf("git sandbox completed (noop) for task %s", task.TaskID),
		}, nil
	}

	var payload GitSandboxPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal git sandbox payload: %w", err)
	}

	command := payload.TargetCommand()
	if command == "" {
		return &TaskResult{
			Success: true,
			Output:  fmt.Sprintf("git sandbox completed (noop) for task %s", task.TaskID),
		}, nil
	}

	execCtx := ctx
	if payload.TimeoutMS > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(payload.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	// Resolve target directory
	worktreeDir := payload.WorktreePath
	if worktreeDir == "" {
		worktreeDir = payload.WorkingDir
	}
	if worktreeDir == "" {
		existingPath, found := h.mgr.GetWorktreePath(task.RunID)
		if found && existingPath != "" {
			worktreeDir = existingPath
		} else {
			opts := sandbox.WorktreeOptions{
				Branch:      payload.Branch,
				MultiTenant: payload.MultiTenant,
			}
			env, provErr := h.mgr.ProvisionWorktreeWithOptions(execCtx, task.RunID, opts)
			if provErr != nil {
				if isTransientGitError(provErr) {
					return nil, NewRetryableError(fmt.Errorf("transient worktree provisioning error: %w", provErr), 1*time.Second)
				}
				return nil, fmt.Errorf("failed to provision worktree: %w", provErr)
			}
			worktreeDir = env.WorktreePath
		}
	}

	if payload.CleanLocks && worktreeDir != "" {
		_ = sandbox.CleanStaleLocks(worktreeDir)
	}

	// Prepare sensitive secrets
	secrets := append([]string{}, payload.Secrets...)
	for _, envVar := range payload.TargetEnv() {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) == 2 && isSensitiveKey(parts[0]) && parts[1] != "" {
			secrets = append(secrets, parts[1])
		}
	}

	var stdout, stderr string
	var exitCode int
	var cmdErr error

	if h.publisher != nil {
		seqCounter := new(atomic.Int64)
		stdoutChunker := NewStreamChunker(StreamChunkerConfig{
			TaskID:     task.TaskID,
			StepID:     task.StepID,
			RunID:      task.RunID,
			WorkerID:   h.workerID,
			Stream:     "stdout",
			Publisher:  h.publisher,
			Secrets:    secrets,
			ChunkSize:  h.chunkSize,
			SeqCounter: seqCounter,
		})
		stderrChunker := NewStreamChunker(StreamChunkerConfig{
			TaskID:     task.TaskID,
			StepID:     task.StepID,
			RunID:      task.RunID,
			WorkerID:   h.workerID,
			Stream:     "stderr",
			Publisher:  h.publisher,
			Secrets:    secrets,
			ChunkSize:  h.chunkSize,
			SeqCounter: seqCounter,
		})

		if streamingExec, ok := h.executor.(StreamingCommandExecutor); ok {
			exitCode, cmdErr = streamingExec.ExecuteCommandStreaming(
				execCtx,
				worktreeDir,
				command,
				payload.TargetArgs(),
				payload.TargetEnv(),
				stdoutChunker,
				stderrChunker,
			)
		} else {
			var outRaw, errRaw string
			outRaw, errRaw, exitCode, cmdErr = h.executor.ExecuteCommand(execCtx, worktreeDir, command, payload.TargetArgs(), payload.TargetEnv())
			_, _ = stdoutChunker.Write([]byte(outRaw))
			_, _ = stderrChunker.Write([]byte(errRaw))
		}

		_ = stdoutChunker.Flush()
		_ = stderrChunker.Flush()

		stdout = stdoutChunker.FullOutput()
		stderr = stderrChunker.FullOutput()
	} else {
		stdout, stderr, exitCode, cmdErr = h.executor.ExecuteCommand(execCtx, worktreeDir, command, payload.TargetArgs(), payload.TargetEnv())
		stdout = sandbox.MaskSecrets(stdout, secrets)
		stderr = sandbox.MaskSecrets(stderr, secrets)
	}

	if payload.RecycleAfter {
		_ = h.mgr.RecycleWorktree(ctx, task.RunID)
	}

	// Check for transient lock or connection errors
	if strings.Contains(stderr, "index.lock") || strings.Contains(stdout, "index.lock") || isTransientGitError(cmdErr) {
		return nil, NewRetryableError(fmt.Errorf("transient git conflict in sandbox: %s (err: %v)", stderr, cmdErr), 1*time.Second)
	}

	success := (exitCode == 0 && cmdErr == nil)
	output := stdout
	if output == "" && stderr != "" {
		output = stderr
	}

	res := &TaskResult{
		Success: success,
		Output:  output,
		Data: map[string]any{
			"exit_code":     exitCode,
			"stdout":        stdout,
			"stderr":        stderr,
			"worktree_path": worktreeDir,
		},
	}

	// Populate runtime metadata (oci/host, engine, cgroups limits)
	if metaProv, ok := h.executor.(RuntimeMetadataProvider); ok {
		for k, v := range metaProv.GetRuntimeMetadata() {
			res.Data[k] = v
		}
	} else {
		res.Data["sandbox_runtime"] = "host"
		res.Data["runtime_engine"] = "host"
	}

	if !success {
		errMsg := stderr
		if errMsg == "" && cmdErr != nil {
			errMsg = cmdErr.Error()
		}
		res.Error = fmt.Sprintf("command exited with code %d: %s", exitCode, errMsg)
	}

	return res, nil
}

// AgentStepConfig provides dependencies for AgentStepHandler.
type AgentStepConfig struct {
	Caller    WasmCaller
	Sandbox   SandboxManager
	Executor  AgentStepExecutor
	Command   CommandExecutor
	Publisher EventPublisher
	WorkerID  string
}

// AgentStepHandler processes cognitive agent tasks.
type AgentStepHandler struct {
	cfg AgentStepConfig
}

// NewAgentStepHandler creates an initialized AgentStepHandler.
func NewAgentStepHandler(cfg AgentStepConfig) *AgentStepHandler {
	return &AgentStepHandler{cfg: cfg}
}

// HandleTask executes the agent step with timeout and error classification.
func (h *AgentStepHandler) HandleTask(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
	if len(task.Payload) == 0 || strings.TrimSpace(string(task.Payload)) == "" {
		return &TaskResult{
			Success: true,
			Output:  fmt.Sprintf("agent step completed for task %s", task.TaskID),
		}, nil
	}

	var payload AgentStepPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent step payload: %w", err)
	}

	stepCtx := ctx
	if payload.TimeoutMS > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, time.Duration(payload.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	if h.cfg.Executor != nil {
		return h.cfg.Executor.ExecuteStep(stepCtx, payload, task)
	}

	// Default agent step execution behavior
	role := payload.Role
	if role == "" {
		role = "orchestrator"
	}
	action := payload.Action
	if action == "" {
		action = "execute"
	}

	output := fmt.Sprintf("agent step [%s:%s] completed for task %s", role, action, task.TaskID)
	if payload.Prompt != "" {
		output = fmt.Sprintf("executed prompt for task %s (%s)", task.TaskID, role)
	}

	if h.cfg.Publisher != nil {
		seqCounter := new(atomic.Int64)
		chunker := NewStreamChunker(StreamChunkerConfig{
			TaskID:     task.TaskID,
			StepID:     task.StepID,
			RunID:      task.RunID,
			WorkerID:   h.cfg.WorkerID,
			Stream:     "stdout",
			Publisher:  h.cfg.Publisher,
			SeqCounter: seqCounter,
		})
		_, _ = chunker.Write([]byte(output + "\n"))
		_ = chunker.Flush()
	}

	res := &TaskResult{
		Success: true,
		Output:  output,
		Data: map[string]any{
			"step_name": payload.StepName,
			"role":      role,
			"action":    action,
			"context":   payload.Context,
		},
	}

	if h.cfg.Command != nil {
		if metaProv, ok := h.cfg.Command.(RuntimeMetadataProvider); ok {
			for k, v := range metaProv.GetRuntimeMetadata() {
				res.Data[k] = v
			}
		} else {
			res.Data["sandbox_runtime"] = "host"
			res.Data["runtime_engine"] = "host"
		}
	}

	return res, nil
}

func isTransientGitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "index.lock") ||
		strings.Contains(msg, "resource temporarily unavailable") ||
		strings.Contains(msg, "could not lock config file")
}

func isSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "PASS") ||
		strings.Contains(upper, "AUTH")
}
