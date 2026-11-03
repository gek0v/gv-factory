// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

// Standard domain errors for PiRunner.
var (
	ErrPiBinaryNotFound = errors.New("runtime: pi-bolt binary not found in PATH or PI_BINARY_PATH")
	ErrEmptyUserPrompt  = errors.New("runtime: user prompt cannot be empty")
	ErrNilRunConfig     = errors.New("runtime: run configuration cannot be nil")
)

// Known binary candidates to search for in PATH in priority order.
var defaultBinaryCandidates = []string{"pb", "pi-bolt", "pi"}

// GatewayEventPublisher abstracts publishing task events to gv-factory communications gateway.
type GatewayEventPublisher interface {
	PublishTaskEvent(ctx context.Context, ev gateway.Event) error
}

// CommandRunner abstracts subprocess execution for testing and platform isolation.
type CommandRunner func(ctx context.Context, cmd *exec.Cmd, stdoutHandler func(io.Reader) error, stderrHandler func(io.Reader) error) error

// PiRunConfig specifies all parameters required for executing an autonomous agent through Pi-Bolt.
type PiRunConfig struct {
	TaskID             string            `json:"task_id,omitempty"`
	RunID              string            `json:"run_id,omitempty"`
	WorkspaceDir       string            `json:"workspace_dir"`
	Model              string            `json:"model,omitempty"`
	Thinking           string            `json:"thinking,omitempty"`
	Skills             []string          `json:"skills,omitempty"`
	Tools              []string          `json:"tools,omitempty"`
	Extensions         []string          `json:"extensions,omitempty"`
	SystemPrompt       string            `json:"system_prompt,omitempty"`
	AppendSystemPrompt string            `json:"append_system_prompt,omitempty"`
	UserPrompt         string            `json:"user_prompt"`
	Mode               string            `json:"mode,omitempty"` // "json" (default, JSONL streaming) or "text" / "print"
	NoSession          bool              `json:"no_session"`
	SessionDir         string            `json:"session_dir,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	Timeout            time.Duration     `json:"timeout,omitempty"`
	OnEvent            func(ev PiEvent)  `json:"-"`
}

// Normalize applies defaults to PiRunConfig fields.
func (c *PiRunConfig) Normalize() {
	if c.Mode == "" {
		c.Mode = "json"
	}
	c.WorkspaceDir = strings.TrimSpace(c.WorkspaceDir)
	c.Model = strings.TrimSpace(c.Model)
	c.Thinking = strings.TrimSpace(c.Thinking)
	c.SystemPrompt = strings.TrimSpace(c.SystemPrompt)
	c.AppendSystemPrompt = strings.TrimSpace(c.AppendSystemPrompt)
	c.UserPrompt = strings.TrimSpace(c.UserPrompt)
	c.SessionDir = strings.TrimSpace(c.SessionDir)
}

// PiEvent represents a structured JSONL event emitted by Pi-Bolt during streaming runs.
type PiEvent struct {
	Type                  string            `json:"type"`
	Version               int               `json:"version,omitempty"`
	ID                    string            `json:"id,omitempty"`
	Timestamp             any               `json:"timestamp,omitempty"`
	Cwd                   string            `json:"cwd,omitempty"`
	Message               *PiMessage        `json:"message,omitempty"`
	Usage                 *PiUsage          `json:"usage,omitempty"`
	AssistantMessageEvent *PiAssistantEvent `json:"assistantMessageEvent,omitempty"`
	ToolResults           []any             `json:"toolResults,omitempty"`
	Aborted               bool              `json:"aborted,omitempty"`
	Raw                   string            `json:"-"`
}

// PiMessage represents a chat message emitted in message_start, message_end, or turn_end.
type PiMessage struct {
	Role          string          `json:"role"`
	Content       []PiContentItem `json:"content"`
	Usage         *PiUsage        `json:"usage,omitempty"`
	StopReason    string          `json:"stopReason,omitempty"`
	DurationMS    int64           `json:"durationMs,omitempty"`
	ThinkingLevel string          `json:"thinkingLevel,omitempty"`
}

// PiContentItem represents a block inside a message's content array.
type PiContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// PiUsage tracks token consumption and duration metrics.
type PiUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	TotalTokens int64 `json:"totalTokens"`
	Reasoning   int64 `json:"reasoning,omitempty"`
}

// PiAssistantEvent models incremental streaming text/tool deltas.
type PiAssistantEvent struct {
	Type         string `json:"type"`
	Delta        string `json:"delta,omitempty"`
	Content      string `json:"content,omitempty"`
	ContentIndex int    `json:"contentIndex,omitempty"`
}

// PiRunResult contains the consolidated outcome and captured telemetry of a PiRunner execution.
type PiRunResult struct {
	Output       string        `json:"output"`
	Events       []PiEvent     `json:"events"`
	TotalTokens  int64         `json:"total_tokens"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	Duration     time.Duration `json:"duration"`
	ExitCode     int           `json:"exit_code"`
	Stderr       string        `json:"stderr,omitempty"`
}

// PiRunnerConfig configures a PiRunner instance.
type PiRunnerConfig struct {
	BinaryPath    string
	Gateway       GatewayEventPublisher
	CommandRunner CommandRunner
}

// PiRunner orchestrates agent executions by delegating to the Pi-Bolt binary harness.
type PiRunner struct {
	binaryPath string
	gateway    GatewayEventPublisher
	cmdRunner  CommandRunner
	mu         sync.RWMutex
}

// NewPiRunner creates a new PiRunner with automatic binary detection and default options.
func NewPiRunner(cfg ...PiRunnerConfig) (*PiRunner, error) {
	var c PiRunnerConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}

	binPath := strings.TrimSpace(c.BinaryPath)
	if binPath == "" {
		detected, err := DetectPiBinary()
		if err != nil {
			return nil, err
		}
		binPath = detected
	}

	runner := c.CommandRunner
	if runner == nil {
		runner = DefaultSubprocessRunner
	}

	return &PiRunner{
		binaryPath: binPath,
		gateway:    c.Gateway,
		cmdRunner:  runner,
	}, nil
}

// DetectPiBinary searches environment variables and PATH for a valid Pi-Bolt binary.
func DetectPiBinary() (string, error) {
	// 1. Check explicit override via PI_BINARY_PATH
	if envPath := strings.TrimSpace(os.Getenv("PI_BINARY_PATH")); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
		// If LookPath resolves it (e.g. if envPath is just a command name in PATH)
		if resolved, err := exec.LookPath(envPath); err == nil {
			return resolved, nil
		}
		return "", fmt.Errorf("%w: PI_BINARY_PATH specified '%s' but file is not executable", ErrPiBinaryNotFound, envPath)
	}

	// 2. Search PATH for standard candidates: pb, pi-bolt, pi
	for _, candidate := range defaultBinaryCandidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}

	return "", ErrPiBinaryNotFound
}

// BinaryPath returns the configured Pi-Bolt binary path.
func (r *PiRunner) BinaryPath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.binaryPath
}

// SetGateway updates the attached gateway event publisher.
func (r *PiRunner) SetGateway(g GatewayEventPublisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gateway = g
}

// BuildArgs translates a PiRunConfig into the command-line arguments passed to pi-bolt.
func BuildArgs(cfg PiRunConfig) []string {
	args := make([]string, 0, 16)

	if strings.ToLower(cfg.Mode) == "json" {
		args = append(args, "--mode", "json", "-p")
	} else {
		args = append(args, "-p")
	}

	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.Thinking != "" {
		args = append(args, "--thinking", cfg.Thinking)
	}
	if cfg.SystemPrompt != "" {
		args = append(args, "--system-prompt", cfg.SystemPrompt)
	}
	if cfg.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.AppendSystemPrompt)
	}
	for _, skill := range cfg.Skills {
		s := strings.TrimSpace(skill)
		if s != "" {
			args = append(args, "--skill", s)
		}
	}
	if len(cfg.Tools) > 0 {
		cleaned := make([]string, 0, len(cfg.Tools))
		for _, tool := range cfg.Tools {
			t := strings.TrimSpace(tool)
			if t != "" {
				cleaned = append(cleaned, t)
			}
		}
		if len(cleaned) > 0 {
			args = append(args, "--tools", strings.Join(cleaned, ","))
		}
	}
	for _, ext := range cfg.Extensions {
		e := strings.TrimSpace(ext)
		if e != "" {
			args = append(args, "--extension", e)
		}
	}
	if cfg.NoSession {
		args = append(args, "--no-session")
	}
	if cfg.SessionDir != "" {
		args = append(args, "--session-dir", cfg.SessionDir)
	}

	if cfg.UserPrompt != "" {
		args = append(args, "--", cfg.UserPrompt)
	}

	return args
}

// Run executes an agent session with streaming output parsing and telemetry capture.
func (r *PiRunner) Run(ctx context.Context, cfg PiRunConfig) (*PiRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg.Normalize()
	if cfg.UserPrompt == "" {
		return nil, ErrEmptyUserPrompt
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if cfg.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	args := BuildArgs(cfg)

	r.mu.RLock()
	binPath := r.binaryPath
	cmdRunner := r.cmdRunner
	gw := r.gateway
	r.mu.RUnlock()

	cmd := exec.CommandContext(runCtx, binPath, args...)
	if cfg.WorkspaceDir != "" {
		cmd.Dir = cfg.WorkspaceDir
	}
	if len(cfg.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	startTime := time.Now()
	result := &PiRunResult{
		Events: make([]PiEvent, 0, 32),
	}

	// Notify gateway that task execution has started
	if gw != nil {
		_ = gw.PublishTaskEvent(runCtx, gateway.Event{
			Type:      gateway.TopicTaskStarted,
			RunID:     cfg.RunID,
			TaskID:    cfg.TaskID,
			Timestamp: startTime.UnixMilli(),
			Payload: map[string]any{
				"status":    "running",
				"harness":   "pi-bolt",
				"workspace": cfg.WorkspaceDir,
				"model":     cfg.Model,
			},
		})
	}

	var accumulatedDelta strings.Builder
	var lastAssistantText string
	var mu sync.Mutex
	var stderrBuf bytes.Buffer

	stdoutHandler := func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		// Allocate a generous 2MB buffer to handle large JSON lines (e.g. tool results or AST dumps)
		const maxScanTokenSize = 2 * 1024 * 1024
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, maxScanTokenSize)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			if strings.ToLower(cfg.Mode) == "json" {
				var ev PiEvent
				if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Type != "" {
					ev.Raw = line
					mu.Lock()
					result.Events = append(result.Events, ev)

					r.processStreamingEvent(runCtx, gw, cfg, ev, &accumulatedDelta, &lastAssistantText, result)
					mu.Unlock()

					if cfg.OnEvent != nil {
						cfg.OnEvent(ev)
					}
					continue
				}
			}

			// Plaintext / fallback line handling
			mu.Lock()
			if accumulatedDelta.Len() > 0 {
				accumulatedDelta.WriteString("\n")
			}
			accumulatedDelta.WriteString(line)
			mu.Unlock()

			if gw != nil {
				_ = gw.PublishTaskEvent(runCtx, gateway.Event{
					Type:      gateway.TopicTaskLog,
					RunID:     cfg.RunID,
					TaskID:    cfg.TaskID,
					Timestamp: time.Now().UnixMilli(),
					Payload: map[string]any{
						"stream":  "stdout",
						"message": line,
					},
				})
			}
		}
		return scanner.Err()
	}

	stderrHandler := func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			if stderrBuf.Len() > 0 {
				stderrBuf.WriteString("\n")
			}
			stderrBuf.WriteString(line)
			mu.Unlock()

			if gw != nil {
				_ = gw.PublishTaskEvent(runCtx, gateway.Event{
					Type:      gateway.TopicTaskLog,
					RunID:     cfg.RunID,
					TaskID:    cfg.TaskID,
					Timestamp: time.Now().UnixMilli(),
					Payload: map[string]any{
						"stream":  "stderr",
						"message": line,
					},
				})
			}
		}
		return scanner.Err()
	}

	execErr := cmdRunner(runCtx, cmd, stdoutHandler, stderrHandler)
	result.Duration = time.Since(startTime)
	result.Stderr = stderrBuf.String()

	// Consolidate final output text
	if lastAssistantText != "" {
		result.Output = lastAssistantText
	} else {
		result.Output = strings.TrimSpace(accumulatedDelta.String())
	}

	if execErr != nil {
		var exitErr *exec.ExitError
		if errors.As(execErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}

		if gw != nil {
			_ = gw.PublishTaskEvent(runCtx, gateway.Event{
				Type:      gateway.TopicTaskFailed,
				RunID:     cfg.RunID,
				TaskID:    cfg.TaskID,
				Timestamp: time.Now().UnixMilli(),
				Payload: map[string]any{
					"error":     execErr.Error(),
					"exit_code": result.ExitCode,
					"duration":  result.Duration.Milliseconds(),
				},
			})
		}
		return result, fmt.Errorf("runtime: agent execution failed with exit code %d: %w", result.ExitCode, execErr)
	}

	if gw != nil {
		_ = gw.PublishTaskEvent(runCtx, gateway.Event{
			Type:      gateway.TopicTaskCompleted,
			RunID:     cfg.RunID,
			TaskID:    cfg.TaskID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"total_tokens": result.TotalTokens,
				"duration_ms":  result.Duration.Milliseconds(),
			},
		})
	}

	return result, nil
}

// processStreamingEvent updates accumulated metrics and dispatches events to the gateway.
func (r *PiRunner) processStreamingEvent(
	ctx context.Context,
	gw GatewayEventPublisher,
	cfg PiRunConfig,
	ev PiEvent,
	accumulatedDelta *strings.Builder,
	lastAssistantText *string,
	result *PiRunResult,
) {
	switch ev.Type {
	case "message_update":
		if ev.AssistantMessageEvent != nil {
			if ev.AssistantMessageEvent.Delta != "" {
				accumulatedDelta.WriteString(ev.AssistantMessageEvent.Delta)
				if gw != nil {
					_ = gw.PublishTaskEvent(ctx, gateway.Event{
						Type:      gateway.TopicTaskLog,
						RunID:     cfg.RunID,
						TaskID:    cfg.TaskID,
						Timestamp: time.Now().UnixMilli(),
						Payload: map[string]any{
							"stream":  "stdout",
							"message": ev.AssistantMessageEvent.Delta,
						},
					})
				}
			}
			if ev.AssistantMessageEvent.Content != "" {
				*lastAssistantText = ev.AssistantMessageEvent.Content
			}
		}
		if ev.Usage != nil {
			result.TotalTokens = ev.Usage.TotalTokens
			result.InputTokens = ev.Usage.Input
			result.OutputTokens = ev.Usage.Output
		}

	case "message_end", "turn_end":
		if ev.Message != nil {
			if ev.Message.Role == "assistant" {
				var sb strings.Builder
				for _, part := range ev.Message.Content {
					if part.Type == "text" && part.Text != "" {
						sb.WriteString(part.Text)
					}
				}
				if sb.Len() > 0 {
					*lastAssistantText = sb.String()
				}
			}
			if ev.Message.Usage != nil {
				result.TotalTokens = ev.Message.Usage.TotalTokens
				result.InputTokens = ev.Message.Usage.Input
				result.OutputTokens = ev.Message.Usage.Output

				if gw != nil {
					_ = gw.PublishTaskEvent(ctx, gateway.Event{
						Type:      gateway.TopicTaskSpan,
						RunID:     cfg.RunID,
						TaskID:    cfg.TaskID,
						Timestamp: time.Now().UnixMilli(),
						Payload: map[string]any{
							"duration_ms": ev.Message.DurationMS,
							"tokens":      ev.Message.Usage.TotalTokens,
							"status":      "ok",
						},
					})
				}
			}
		}
	}
}

// DefaultSubprocessRunner starts the command, streams stdout and stderr concurrently, and waits for completion.
func DefaultSubprocessRunner(
	ctx context.Context,
	cmd *exec.Cmd,
	stdoutHandler func(io.Reader) error,
	stderrHandler func(io.Reader) error,
) error {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("runtime: failed to attach stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		return fmt.Errorf("runtime: failed to attach stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("runtime: failed to start process: %w", err)
	}

	var wg sync.WaitGroup
	var stdoutErr, stderrErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		if stdoutHandler != nil {
			stdoutErr = stdoutHandler(stdoutPipe)
		}
	}()

	go func() {
		defer wg.Done()
		if stderrHandler != nil {
			stderrErr = stderrHandler(stderrPipe)
		}
	}()

	wg.Wait()
	waitErr := cmd.Wait()

	if stdoutErr != nil {
		return fmt.Errorf("runtime: stdout processing error: %w", stdoutErr)
	}
	if stderrErr != nil {
		return fmt.Errorf("runtime: stderr processing error: %w", stderrErr)
	}
	return waitErr
}
