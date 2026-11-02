// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

// Standard domain errors for Builder agent operations.
var (
	ErrEmptyWorkspaceDir = errors.New("agent: workspace directory is required")
	ErrNilSpecDocument   = errors.New("agent: spec document cannot be nil")
)

// Standard TDD lifecycle phase names.
const (
	PhaseRed      = "Red"
	PhaseGreen    = "Green"
	PhaseRefactor = "Refactor"

	DefaultMaxIterations = 3
)

// CommandResult represents the outcome of a command execution.
type CommandResult struct {
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Duration time.Duration `json:"duration"`
}

// CommandRunner defines the interface for running commands within a workspace.
type CommandRunner interface {
	RunCommand(ctx context.Context, dir string, cmd string, args ...string) (CommandResult, error)
}

// DefaultCommandRunner executes commands directly on the host using os/exec.
type DefaultCommandRunner struct{}

// RunCommand executes a command in the given directory using os/exec.CommandContext.
func (d *DefaultCommandRunner) RunCommand(ctx context.Context, dir string, cmd string, args ...string) (CommandResult, error) {
	start := time.Now()
	command := exec.CommandContext(ctx, cmd, args...)
	if dir != "" {
		command.Dir = dir
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf

	err := command.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return CommandResult{
				ExitCode: -1,
				Stdout:   stdoutBuf.String(),
				Stderr:   stderrBuf.String(),
				Duration: duration,
			}, err
		}
	}

	return CommandResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Duration: duration,
	}, nil
}

// PhaseOutcome details the execution output of a single TDD phase.
type PhaseOutcome struct {
	Phase    string        `json:"phase"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Duration time.Duration `json:"duration"`
}

// BuildResult captures the complete telemetry and output of a TDD task build.
type BuildResult struct {
	TaskID        string         `json:"task_id"`
	Success       bool           `json:"success"`
	Phases        []PhaseOutcome `json:"phases"`
	FilesModified []string       `json:"files_modified"`
	GitDiff       string         `json:"git_diff"`
	TotalDuration time.Duration  `json:"total_duration"`
	ErrorMessage  string         `json:"error_message,omitempty"`
}

// GeneratedFile represents a synthesized code or test file.
type GeneratedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// CodeSynthesizer generates code and test fixtures for TDD phases.
type CodeSynthesizer interface {
	GenerateTest(ctx context.Context, task AtomicTask, tc TestCase) (GeneratedFile, error)
	GenerateImplementation(ctx context.Context, task AtomicTask, tc TestCase, failureOutput string, iteration int) (GeneratedFile, error)
	RefactorCode(ctx context.Context, task AtomicTask, tc TestCase) ([]GeneratedFile, error)
}

// BuilderConfig configures the autonomous TDD Builder agent.
type BuilderConfig struct {
	WorkspaceDir    string
	CommandExecutor CommandRunner
	LLMClient       llm.LLMClient
	CodeSynthesizer CodeSynthesizer
	MaxIterations   int
	TestCommand     []string
	FormatCommand   []string
}

// Builder orchestrates the Red-Green-Refactor development loop within an isolated workspace.
type Builder struct {
	WorkspaceDir    string
	CommandExecutor CommandRunner
	LLMClient       llm.LLMClient
	Synthesizer     CodeSynthesizer
	MaxIterations   int
	TestCommand     []string
	FormatCommand   []string
	mu              sync.RWMutex
}

// NewBuilder initializes a new Builder with sensible defaults for missing configuration.
func NewBuilder(cfg BuilderConfig) *Builder {
	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = DefaultMaxIterations
	}

	executor := cfg.CommandExecutor
	if executor == nil {
		executor = &DefaultCommandRunner{}
	}

	testCmd := cfg.TestCommand
	if len(testCmd) == 0 {
		testCmd = []string{"go", "test", "./..."}
	}

	formatCmd := cfg.FormatCommand
	if len(formatCmd) == 0 {
		formatCmd = []string{"go", "fmt", "./..."}
	}

	synth := cfg.CodeSynthesizer
	if synth == nil {
		synth = &defaultCodeSynthesizer{
			llmClient: cfg.LLMClient,
		}
	}

	return &Builder{
		WorkspaceDir:    cfg.WorkspaceDir,
		CommandExecutor: executor,
		LLMClient:       cfg.LLMClient,
		Synthesizer:     synth,
		MaxIterations:   maxIterations,
		TestCommand:     testCmd,
		FormatCommand:   formatCmd,
	}
}

// ExecuteTask executes the Red-Green-Refactor cycle for an atomic task and its test cases.
func (b *Builder) ExecuteTask(ctx context.Context, task AtomicTask, testCases ...TestCase) (*BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.RLock()
	workspaceDir := b.WorkspaceDir
	executor := b.CommandExecutor
	synthesizer := b.Synthesizer
	maxIter := b.MaxIterations
	testCmd := append([]string{}, b.TestCommand...)
	formatCmd := append([]string{}, b.FormatCommand...)
	b.mu.RUnlock()

	if strings.TrimSpace(workspaceDir) == "" {
		return nil, ErrEmptyWorkspaceDir
	}
	if strings.TrimSpace(task.ID) == "" {
		return nil, ErrEmptyTaskID
	}

	startTime := time.Now()
	result := &BuildResult{
		TaskID:        task.ID,
		Phases:        make([]PhaseOutcome, 0),
		FilesModified: make([]string, 0),
	}

	modifiedMap := make(map[string]bool)

	// Ensure at least one test case exists
	cases := testCases
	if len(cases) == 0 {
		cases = []TestCase{
			{
				Name:            fmt.Sprintf("Test_%s", sanitizeIdentifier(task.ID)),
				Phase:           PhaseRed,
				Description:     task.Description,
				ExpectedOutcome: "PASS",
			},
		}
	}

	for _, tc := range cases {
		// ==========================================
		// Phase 1: RED (Write test and verify failure)
		// ==========================================
		testFile, err := synthesizer.GenerateTest(ctx, task, tc)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("failed to synthesize test file: %v", err)
			result.TotalDuration = time.Since(startTime)
			return result, nil
		}

		if err := b.writeFile(workspaceDir, testFile.Path, testFile.Content); err != nil {
			result.ErrorMessage = fmt.Sprintf("failed to write test file %s: %v", testFile.Path, err)
			result.TotalDuration = time.Since(startTime)
			return result, nil
		}
		modifiedMap[testFile.Path] = true

		redCmdRes, err := executor.RunCommand(ctx, workspaceDir, testCmd[0], testCmd[1:]...)
		if err != nil && redCmdRes.ExitCode == 0 {
			redCmdRes.ExitCode = 1
		}

		result.Phases = append(result.Phases, PhaseOutcome{
			Phase:    PhaseRed,
			ExitCode: redCmdRes.ExitCode,
			Stdout:   redCmdRes.Stdout,
			Stderr:   redCmdRes.Stderr,
			Duration: redCmdRes.Duration,
		})

		// Verifies failure (exit code != 0)
		if redCmdRes.ExitCode == 0 {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("red phase verification failed: expected test failure for %s, but test passed with exit code 0", tc.Name)
			result.TotalDuration = time.Since(startTime)
			result.FilesModified = mapToSortedSlice(modifiedMap)
			return result, nil
		}

		// ==========================================
		// Phase 2: GREEN (Implement and repeat up to MaxIterations)
		// ==========================================
		var greenSuccess bool
		lastFailureOutput := redCmdRes.Stdout + "\n" + redCmdRes.Stderr

		for iter := 1; iter <= maxIter; iter++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			implFile, err := synthesizer.GenerateImplementation(ctx, task, tc, lastFailureOutput, iter)
			if err != nil {
				result.ErrorMessage = fmt.Sprintf("failed to synthesize implementation: %v", err)
				result.TotalDuration = time.Since(startTime)
				result.FilesModified = mapToSortedSlice(modifiedMap)
				return result, nil
			}

			if err := b.writeFile(workspaceDir, implFile.Path, implFile.Content); err != nil {
				result.ErrorMessage = fmt.Sprintf("failed to write implementation file %s: %v", implFile.Path, err)
				result.TotalDuration = time.Since(startTime)
				result.FilesModified = mapToSortedSlice(modifiedMap)
				return result, nil
			}
			modifiedMap[implFile.Path] = true

			greenCmdRes, _ := executor.RunCommand(ctx, workspaceDir, testCmd[0], testCmd[1:]...)

			result.Phases = append(result.Phases, PhaseOutcome{
				Phase:    PhaseGreen,
				ExitCode: greenCmdRes.ExitCode,
				Stdout:   greenCmdRes.Stdout,
				Stderr:   greenCmdRes.Stderr,
				Duration: greenCmdRes.Duration,
			})

			if greenCmdRes.ExitCode == 0 {
				greenSuccess = true
				break
			}

			lastFailureOutput = greenCmdRes.Stdout + "\n" + greenCmdRes.Stderr
		}

		if !greenSuccess {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("green phase failed: test did not pass within max iterations (%d)", maxIter)
			result.TotalDuration = time.Since(startTime)
			result.FilesModified = mapToSortedSlice(modifiedMap)
			return result, nil
		}

		// ==========================================
		// Phase 3: REFACTOR (Format code and verify tests still pass)
		// ==========================================
		refactoredFiles, err := synthesizer.RefactorCode(ctx, task, tc)
		if err == nil {
			for _, rf := range refactoredFiles {
				if rf.Path != "" && rf.Content != "" {
					_ = b.writeFile(workspaceDir, rf.Path, rf.Content)
					modifiedMap[rf.Path] = true
				}
			}
		}

		// Execute formatting command if configured
		if len(formatCmd) > 0 {
			_, _ = executor.RunCommand(ctx, workspaceDir, formatCmd[0], formatCmd[1:]...)
		}

		// Re-run test suite to ensure green state is preserved
		refactorCmdRes, _ := executor.RunCommand(ctx, workspaceDir, testCmd[0], testCmd[1:]...)

		result.Phases = append(result.Phases, PhaseOutcome{
			Phase:    PhaseRefactor,
			ExitCode: refactorCmdRes.ExitCode,
			Stdout:   refactorCmdRes.Stdout,
			Stderr:   refactorCmdRes.Stderr,
			Duration: refactorCmdRes.Duration,
		})

		if refactorCmdRes.ExitCode != 0 {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("refactor phase failed: tests broke during refactoring: %s", refactorCmdRes.Stdout)
			result.TotalDuration = time.Since(startTime)
			result.FilesModified = mapToSortedSlice(modifiedMap)
			return result, nil
		}
	}

	// Capture git diff if git is available in workspace
	diffRes, err := executor.RunCommand(ctx, workspaceDir, "git", "diff")
	if err == nil && diffRes.ExitCode == 0 {
		result.GitDiff = diffRes.Stdout
	}

	result.Success = true
	result.FilesModified = mapToSortedSlice(modifiedMap)
	result.TotalDuration = time.Since(startTime)

	return result, nil
}

// BuildSpec iterates through each AtomicTask and TestCase in the SpecDocument.
func (b *Builder) BuildSpec(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
	if spec == nil {
		return nil, ErrNilSpecDocument
	}

	results := make([]*BuildResult, 0, len(spec.AtomicTasks))

	for i, task := range spec.AtomicTasks {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		// Associate test cases by index or fallback to all
		var matchingCases []TestCase
		if i < len(spec.TDDPlan.TestCases) {
			matchingCases = append(matchingCases, spec.TDDPlan.TestCases[i])
		} else if len(spec.TDDPlan.TestCases) > 0 {
			matchingCases = spec.TDDPlan.TestCases
		}

		res, err := b.ExecuteTask(ctx, task, matchingCases...)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}

	return results, nil
}

func (b *Builder) writeFile(workspaceDir, relPath, content string) error {
	fullPath := filepath.Join(workspaceDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, []byte(content), 0o644)
}

func mapToSortedSlice(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sanitizeIdentifier(s string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	clean := reg.ReplaceAllString(s, "_")
	return strings.Trim(clean, "_")
}

// =========================================================================
// Default / Fallback Code Synthesizer (LLM or Deterministic Template)
// =========================================================================

type defaultCodeSynthesizer struct {
	llmClient llm.LLMClient
}

func (s *defaultCodeSynthesizer) GenerateTest(ctx context.Context, task AtomicTask, tc TestCase) (GeneratedFile, error) {
	if s.llmClient != nil {
		file, err := s.synthesizeWithLLM(ctx, "test", task, tc, "", 0)
		if err == nil && file.Path != "" && file.Content != "" {
			return file, nil
		}
	}

	// Deterministic fallback
	id := sanitizeIdentifier(tc.Name)
	if id == "" {
		id = sanitizeIdentifier(task.ID)
	}
	fileName := strings.ToLower(id) + "_test.go"
	content := fmt.Sprintf(`// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import "testing"

func %s(t *testing.T) {
	// Red phase test assertion
	t.Fatalf("red phase failing test for %s")
}
`, tc.Name, task.Title)

	return GeneratedFile{
		Path:    fileName,
		Content: content,
	}, nil
}

func (s *defaultCodeSynthesizer) GenerateImplementation(ctx context.Context, task AtomicTask, tc TestCase, failureOutput string, iteration int) (GeneratedFile, error) {
	if s.llmClient != nil {
		file, err := s.synthesizeWithLLM(ctx, "implementation", task, tc, failureOutput, iteration)
		if err == nil && file.Path != "" && file.Content != "" {
			return file, nil
		}
	}

	// Deterministic fallback
	id := sanitizeIdentifier(tc.Name)
	if id == "" {
		id = sanitizeIdentifier(task.ID)
	}
	fileName := strings.ToLower(id) + ".go"
	content := fmt.Sprintf(`// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

// %s satisfies task %s
func %s() bool {
	return true
}
`, tc.Name, task.ID, id)

	return GeneratedFile{
		Path:    fileName,
		Content: content,
	}, nil
}

func (s *defaultCodeSynthesizer) RefactorCode(ctx context.Context, task AtomicTask, tc TestCase) ([]GeneratedFile, error) {
	if s.llmClient != nil {
		file, err := s.synthesizeWithLLM(ctx, "refactor", task, tc, "", 0)
		if err == nil && file.Path != "" && file.Content != "" {
			return []GeneratedFile{file}, nil
		}
	}
	return nil, nil
}

func (s *defaultCodeSynthesizer) synthesizeWithLLM(ctx context.Context, kind string, task AtomicTask, tc TestCase, failureOutput string, iteration int) (GeneratedFile, error) {
	prompt := fmt.Sprintf(`You are an autonomous TDD expert. Generate a %s file for:
Task: %s (%s)
Test Case: %s
Description: %s
Previous Failure Output: %s
Iteration: %d

Respond ONLY with a valid JSON object with keys "path" (relative file path) and "content" (file contents).`,
		kind, task.ID, task.Title, tc.Name, tc.Description, failureOutput, iteration,
	)

	model := os.Getenv("MODEL")
	if model == "" {
		model = os.Getenv("OPENAI_COMPATIBLE_MODEL")
	}
	if model == "" {
		model = os.Getenv("OPENAI_MODEL")
	}
	if model == "" {
		model = "gpt-4o"
	}

	req := &llm.ChatRequest{
		Model: model,
		Messages: []llm.ChatMessage{
			llm.NewSystemMessage("You are an autonomous TDD code generation agent. Output only JSON."),
			llm.NewUserMessage(prompt),
		},
	}

	resp, err := s.llmClient.ChatCompletion(ctx, req)
	if err != nil {
		return GeneratedFile{}, err
	}

	content := strings.TrimSpace(resp.Message.Content)
	content = extractJSONContent(content)

	var gf GeneratedFile
	if err := json.Unmarshal([]byte(content), &gf); err != nil {
		return GeneratedFile{}, err
	}

	return gf, nil
}

func extractJSONContent(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			if strings.HasPrefix(lines[0], "```") {
				lines = lines[1:]
			}
			if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "```") {
				lines = lines[:len(lines)-1]
			}
			return strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return trimmed
}
