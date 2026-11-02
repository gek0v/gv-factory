// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

// mockCommandRunner records command invocations and returns configured results.
type mockCommandRunner struct {
	mu       sync.Mutex
	calls    []mockCommandCall
	handlers []func(dir, cmd string, args ...string) (CommandResult, error)
}

type mockCommandCall struct {
	Dir  string
	Cmd  string
	Args []string
}

func newMockCommandRunner(handlers ...func(dir, cmd string, args ...string) (CommandResult, error)) *mockCommandRunner {
	return &mockCommandRunner{
		handlers: handlers,
	}
}

func (m *mockCommandRunner) RunCommand(ctx context.Context, dir string, cmd string, args ...string) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{ExitCode: -1}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	idx := len(m.calls)
	m.calls = append(m.calls, mockCommandCall{Dir: dir, Cmd: cmd, Args: args})

	if idx < len(m.handlers) && m.handlers[idx] != nil {
		return m.handlers[idx](dir, cmd, args...)
	}

	// Default fallback
	return CommandResult{
		ExitCode: 0,
		Stdout:   "ok",
		Duration: 5 * time.Millisecond,
	}, nil
}

func (m *mockCommandRunner) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// builderMockLLMClient implements llm.LLMClient for testing code synthesis.
type builderMockLLMClient struct {
	mu           sync.Mutex
	calls        []*llm.ChatRequest
	responseFunc func(req *llm.ChatRequest) (*llm.ChatResponse, error)
}

func (m *builderMockLLMClient) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.responseFunc != nil {
		return m.responseFunc(req)
	}
	return &llm.ChatResponse{
		ID:    "mock-resp-1",
		Model: "mock-model",
		Message: llm.ChatMessage{
			Role:    llm.RoleAssistant,
			Content: `{"path": "sample.go", "content": "package sample\n"}`,
		},
	}, nil
}

func (m *builderMockLLMClient) StreamChatCompletion(ctx context.Context, req *llm.ChatRequest) (<-chan llm.ChatChunk, <-chan error) {
	chunkCh := make(chan llm.ChatChunk)
	errCh := make(chan error, 1)
	close(chunkCh)
	close(errCh)
	return chunkCh, errCh
}

// TestNewBuilder verifies constructor defaults and configuration overrides.
func TestNewBuilder(t *testing.T) {
	tmpDir := t.TempDir()

	// Default configuration
	b1 := NewBuilder(BuilderConfig{
		WorkspaceDir: tmpDir,
	})
	if b1 == nil {
		t.Fatal("expected non-nil Builder")
	}
	if b1.MaxIterations != DefaultMaxIterations {
		t.Errorf("expected MaxIterations %d, got %d", DefaultMaxIterations, b1.MaxIterations)
	}
	if b1.CommandExecutor == nil {
		t.Error("expected non-nil default CommandExecutor")
	}
	if len(b1.TestCommand) == 0 {
		t.Error("expected non-empty default TestCommand")
	}

	// Custom configuration
	mockRunner := newMockCommandRunner()
	mockLLM := &builderMockLLMClient{}
	b2 := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: mockRunner,
		LLMClient:       mockLLM,
		MaxIterations:   5,
		TestCommand:     []string{"pytest", "-v"},
	})
	if b2.MaxIterations != 5 {
		t.Errorf("expected MaxIterations 5, got %d", b2.MaxIterations)
	}
	if b2.CommandExecutor != mockRunner {
		t.Error("expected custom CommandExecutor to be set")
	}
	if b2.LLMClient != mockLLM {
		t.Error("expected custom LLMClient to be set")
	}
	if len(b2.TestCommand) != 2 || b2.TestCommand[0] != "pytest" {
		t.Errorf("expected custom TestCommand, got %v", b2.TestCommand)
	}
}

// TestExecuteTask_Validation checks parameter validation on ExecuteTask.
func TestExecuteTask_Validation(t *testing.T) {
	b := NewBuilder(BuilderConfig{
		WorkspaceDir: "",
	})

	ctx := context.Background()

	// Empty workspace
	_, err := b.ExecuteTask(ctx, AtomicTask{ID: "task-1", Title: "Valid"})
	if !errors.Is(err, ErrEmptyWorkspaceDir) {
		t.Errorf("expected ErrEmptyWorkspaceDir, got %v", err)
	}

	b.WorkspaceDir = t.TempDir()

	// Empty task ID
	_, err = b.ExecuteTask(ctx, AtomicTask{ID: "", Title: "Valid"})
	if !errors.Is(err, ErrEmptyTaskID) {
		t.Errorf("expected ErrEmptyTaskID, got %v", err)
	}

	// Canceled context
	cancCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = b.ExecuteTask(cancCtx, AtomicTask{ID: "task-1", Title: "Valid"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestExecuteTask_FullRedGreenRefactor_Success verifies the end-to-end standard TDD loop.
func TestExecuteTask_FullRedGreenRefactor_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Handlers sequence:
	// 1. Red phase: test execution exits with 1 (verified failure).
	// 2. Green phase (iteration 1): test execution exits with 0 (green!).
	// 3. Refactor phase: format/cleanup exits with 0.
	// 4. Refactor phase: re-run tests exits with 0 (remains green).
	// 5. Git diff: git diff command exits with 0 and returns diff.
	runner := newMockCommandRunner(
		// 1. Red phase test
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{
				ExitCode: 1,
				Stdout:   "FAIL: TestMathAdd (compilation error: undefined: Add)",
				Stderr:   "exit status 1",
				Duration: 10 * time.Millisecond,
			}, nil
		},
		// 2. Green phase test
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{
				ExitCode: 0,
				Stdout:   "PASS: TestMathAdd (0.00s)",
				Stderr:   "",
				Duration: 12 * time.Millisecond,
			}, nil
		},
		// 3. Refactor phase formatting
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{
				ExitCode: 0,
				Stdout:   "math.go",
				Duration: 5 * time.Millisecond,
			}, nil
		},
		// 4. Refactor phase re-testing
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{
				ExitCode: 0,
				Stdout:   "PASS: TestMathAdd (0.00s)",
				Duration: 10 * time.Millisecond,
			}, nil
		},
		// 5. Git diff
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{
				ExitCode: 0,
				Stdout:   "+func Add(a, b int) int { return a + b }",
				Duration: 3 * time.Millisecond,
			}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		MaxIterations:   3,
	})

	task := AtomicTask{
		ID:          "TASK-001",
		Title:       "Implement Add function",
		Description: "Add function in math package",
	}

	tc := TestCase{
		Name:            "TestMathAdd",
		Phase:           "Red",
		Description:     "Verify Add(2, 3) == 5",
		ExpectedOutcome: "PASS",
	}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected build success, got failure: %s", res.ErrorMessage)
	}
	if res.TaskID != "TASK-001" {
		t.Errorf("expected TaskID TASK-001, got %s", res.TaskID)
	}
	if len(res.Phases) != 3 {
		t.Fatalf("expected 3 phases (Red, Green, Refactor), got %d", len(res.Phases))
	}

	if res.Phases[0].Phase != PhaseRed || res.Phases[0].ExitCode != 1 {
		t.Errorf("expected Red phase exit code 1, got %+v", res.Phases[0])
	}
	if res.Phases[1].Phase != PhaseGreen || res.Phases[1].ExitCode != 0 {
		t.Errorf("expected Green phase exit code 0, got %+v", res.Phases[1])
	}
	if res.Phases[2].Phase != PhaseRefactor || res.Phases[2].ExitCode != 0 {
		t.Errorf("expected Refactor phase exit code 0, got %+v", res.Phases[2])
	}

	if !strings.Contains(res.GitDiff, "+func Add") {
		t.Errorf("expected GitDiff to contain addition, got: %s", res.GitDiff)
	}
	if len(res.FilesModified) == 0 {
		t.Error("expected modified files to be recorded")
	}
	if res.TotalDuration <= 0 {
		t.Error("expected non-zero total duration")
	}
}

// TestExecuteTask_RedPhase_FailsWhenTestPassesImmediately verifies that Red phase
// fails if tests pass before any implementation changes.
func TestExecuteTask_RedPhase_FailsWhenTestPassesImmediately(t *testing.T) {
	tmpDir := t.TempDir()

	runner := newMockCommandRunner(
		// Red phase test unexpectedly exits with 0
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{
				ExitCode: 0,
				Stdout:   "PASS: All tests pass",
				Duration: 5 * time.Millisecond,
			}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
	})

	task := AtomicTask{
		ID:    "TASK-RED-FAIL",
		Title: "Add duplicate test",
	}

	tc := TestCase{
		Name:  "TestDuplicate",
		Phase: "Red",
	}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if res.Success {
		t.Fatal("expected failure when Red phase unexpectedly passes")
	}
	if !strings.Contains(strings.ToLower(res.ErrorMessage), "red phase") {
		t.Errorf("expected ErrorMessage to mention red phase failure, got: %s", res.ErrorMessage)
	}
	if len(res.Phases) != 1 || res.Phases[0].Phase != PhaseRed {
		t.Errorf("expected only Red phase recorded, got %+v", res.Phases)
	}
}

// TestExecuteTask_GreenPhase_RetriesUntilSuccess tests iterations up to MaxIterations.
func TestExecuteTask_GreenPhase_RetriesUntilSuccess(t *testing.T) {
	tmpDir := t.TempDir()

	// Red phase: fails (exit 1)
	// Green iteration 1: fails (exit 1)
	// Green iteration 2: fails (exit 1)
	// Green iteration 3: succeeds (exit 0)
	// Refactor format: exit 0
	// Refactor test: exit 0
	// Git diff: exit 0
	runner := newMockCommandRunner(
		// Red
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Red"}, nil
		},
		// Green iter 1
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Iter 1"}, nil
		},
		// Green iter 2
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Iter 2"}, nil
		},
		// Green iter 3 (success)
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS Iter 3"}, nil
		},
		// Refactor format
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "formatted"}, nil
		},
		// Refactor re-test
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS Refactor"}, nil
		},
		// Git diff
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "diff content"}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		MaxIterations:   3,
	})

	task := AtomicTask{ID: "TASK-RETRY", Title: "Retry task"}
	tc := TestCase{Name: "TestRetry"}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected build success after 3 iterations, got failure: %s", res.ErrorMessage)
	}
}

// TestExecuteTask_GreenPhase_ExceedsMaxIterations verifies failure when all iterations fail.
func TestExecuteTask_GreenPhase_ExceedsMaxIterations(t *testing.T) {
	tmpDir := t.TempDir()

	runner := newMockCommandRunner(
		// Red
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Red"}, nil
		},
		// Green iter 1
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Iter 1"}, nil
		},
		// Green iter 2
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Iter 2"}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		MaxIterations:   2,
	})

	task := AtomicTask{ID: "TASK-FAIL-GREEN", Title: "Unresolvable task"}
	tc := TestCase{Name: "TestUnresolvable"}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Success {
		t.Fatal("expected failure when max iterations exceeded")
	}
	if !strings.Contains(strings.ToLower(res.ErrorMessage), "max iterations") {
		t.Errorf("expected ErrorMessage to mention max iterations, got: %s", res.ErrorMessage)
	}
}

// TestExecuteTask_RefactorPhase_Regression verifies failure if refactoring breaks the build.
func TestExecuteTask_RefactorPhase_Regression(t *testing.T) {
	tmpDir := t.TempDir()

	runner := newMockCommandRunner(
		// Red
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Red"}, nil
		},
		// Green iter 1
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS Green"}, nil
		},
		// Refactor format
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "formatted"}, nil
		},
		// Refactor re-test fails!
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Regression after refactor"}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		MaxIterations:   3,
	})

	task := AtomicTask{ID: "TASK-REFACTOR-FAIL", Title: "Refactor regression"}
	tc := TestCase{Name: "TestRefactor"}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Success {
		t.Fatal("expected failure when refactor breaks tests")
	}
	if !strings.Contains(strings.ToLower(res.ErrorMessage), "refactor") {
		t.Errorf("expected ErrorMessage to mention refactor, got: %s", res.ErrorMessage)
	}
}

// TestExecuteTask_WithLLMClient verifies code generation via LLMClient.
func TestExecuteTask_WithLLMClient(t *testing.T) {
	tmpDir := t.TempDir()

	runner := newMockCommandRunner(
		// Red
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL"}, nil
		},
		// Green
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS"}, nil
		},
		// Refactor format
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		// Refactor test
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		// Git diff
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "+package test"}, nil
		},
	)

	mockLLM := &builderMockLLMClient{
		responseFunc: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			// REUSE-IgnoreStart
			content := "{\"path\": \"generated.go\", \"content\": \"// SPDX-License-Identifier: Apache-2.0\\npackage main\\n\"}"
			// REUSE-IgnoreEnd
			return &llm.ChatResponse{
				ID: "llm-test-1",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: content,
				},
			}, nil
		},
	}

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		LLMClient:       mockLLM,
	})

	task := AtomicTask{ID: "TASK-LLM", Title: "LLM synthesis"}
	tc := TestCase{Name: "TestLLM"}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success with LLMClient, got: %s", res.ErrorMessage)
	}
	if len(mockLLM.calls) == 0 {
		t.Error("expected LLMClient to be invoked for code synthesis")
	}
}

// TestExecuteTask_WithLLMClient_FallbackOnError verifies that when LLM returns error,
// the builder safely falls back to deterministic generator without panic.
func TestExecuteTask_WithLLMClient_FallbackOnError(t *testing.T) {
	tmpDir := t.TempDir()

	runner := newMockCommandRunner(
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Red"}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS Green"}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "diff"}, nil
		},
	)

	mockLLM := &builderMockLLMClient{
		responseFunc: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			return nil, errors.New("provider rate limited")
		},
	}

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		LLMClient:       mockLLM,
	})

	task := AtomicTask{ID: "TASK-LLM-FALLBACK", Title: "LLM fallback test"}
	tc := TestCase{Name: "TestFallback"}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success with deterministic fallback, got: %s", res.ErrorMessage)
	}
}

// TestBuildSpec_SuccessAndPartialFailure tests the full SpecDocument execution.
func TestBuildSpec_SuccessAndPartialFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Scenario 1: Nil spec
	b := NewBuilder(BuilderConfig{WorkspaceDir: tmpDir})
	_, err := b.BuildSpec(context.Background(), nil)
	if !errors.Is(err, ErrNilSpecDocument) {
		t.Errorf("expected ErrNilSpecDocument, got %v", err)
	}

	// Scenario 2: Valid spec with 2 tasks
	runner := newMockCommandRunner(
		// Task 1: Red (fail) -> Green (pass) -> Refactor fmt -> Refactor test -> diff
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL Red 1"}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS Green 1"}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "diff 1"}, nil
		},

		// Task 2: Red passes immediately (fails TDD invariant)
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS Red 2"}, nil
		},
	)

	b2 := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
	})

	spec := &SpecDocument{
		Title: "Test Multi-Task Spec",
		AtomicTasks: []AtomicTask{
			{ID: "TASK-1", Title: "Task 1"},
			{ID: "TASK-2", Title: "Task 2"},
		},
		TDDPlan: TDDPlan{
			TestCases: []TestCase{
				{Name: "TestCase1", Phase: "Red"},
				{Name: "TestCase2", Phase: "Red"},
			},
		},
	}

	results, err := b2.BuildSpec(context.Background(), spec)
	if err != nil {
		t.Fatalf("unexpected error from BuildSpec: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success {
		t.Errorf("expected task 1 to succeed, got %s", results[0].ErrorMessage)
	}
	if results[1].Success {
		t.Errorf("expected task 2 to fail, got success")
	}
}

// TestDefaultCommandRunner_Execution tests running commands with the default OS runner.
func TestDefaultCommandRunner_Execution(t *testing.T) {
	runner := &DefaultCommandRunner{}
	tmpDir := t.TempDir()

	// Execute a standard tool like 'go' version
	res, err := runner.RunCommand(context.Background(), tmpDir, "go", "version")
	if err != nil {
		t.Fatalf("failed to run go version: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "go version") {
		t.Errorf("unexpected stdout: %s", res.Stdout)
	}
	if res.Duration <= 0 {
		t.Errorf("expected positive duration, got %v", res.Duration)
	}

	// Command not found
	_, err = runner.RunCommand(context.Background(), tmpDir, "non_existent_binary_xyz_12345")
	if err == nil {
		t.Error("expected error for non-existent command")
	}
}

// TestBuilder_Concurrent_RaceSafety verifies that Builder instances are race-safe.
func TestBuilder_Concurrent_RaceSafety(t *testing.T) {
	tmpDir := t.TempDir()
	runner := newMockCommandRunner()
	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			task := AtomicTask{ID: fmt.Sprintf("TASK-%d", id), Title: "Concurrent"}
			tc := TestCase{Name: fmt.Sprintf("Test-%d", id)}
			// Red fails immediately because mock returns exit 0 by default, which is expected
			_, _ = b.ExecuteTask(context.Background(), task, tc)
		}(i)
	}
	wg.Wait()
}

// mockErrorSynthesizer simulates errors in code synthesis.
type mockErrorSynthesizer struct {
	errTest error
	errImpl error
}

func (m *mockErrorSynthesizer) GenerateTest(ctx context.Context, task AtomicTask, tc TestCase) (GeneratedFile, error) {
	if m.errTest != nil {
		return GeneratedFile{}, m.errTest
	}
	return GeneratedFile{Path: "test.go", Content: "package main\n"}, nil
}

func (m *mockErrorSynthesizer) GenerateImplementation(ctx context.Context, task AtomicTask, tc TestCase, failureOutput string, iteration int) (GeneratedFile, error) {
	if m.errImpl != nil {
		return GeneratedFile{}, m.errImpl
	}
	return GeneratedFile{Path: "impl.go", Content: "package main\n"}, nil
}

func (m *mockErrorSynthesizer) RefactorCode(ctx context.Context, task AtomicTask, tc TestCase) ([]GeneratedFile, error) {
	return nil, nil
}

// TestExecuteTask_SynthesizerErrors tests error handling when synthesizer fails.
func TestExecuteTask_SynthesizerErrors(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Synthesizer fails on GenerateTest
	b1 := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CodeSynthesizer: &mockErrorSynthesizer{errTest: errors.New("cannot generate test")},
	})
	res1, err := b1.ExecuteTask(context.Background(), AtomicTask{ID: "TASK-ERR1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1.Success || !strings.Contains(res1.ErrorMessage, "cannot generate test") {
		t.Errorf("expected test synthesis error, got: %s", res1.ErrorMessage)
	}

	// 2. Synthesizer fails on GenerateImplementation
	runner := newMockCommandRunner(
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL"}, nil
		},
	)
	b2 := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		CodeSynthesizer: &mockErrorSynthesizer{errImpl: errors.New("cannot generate impl")},
	})
	res2, err := b2.ExecuteTask(context.Background(), AtomicTask{ID: "TASK-ERR2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Success || !strings.Contains(res2.ErrorMessage, "cannot generate impl") {
		t.Errorf("expected impl synthesis error, got: %s", res2.ErrorMessage)
	}
}

// TestExecuteTask_FileWriteErrors tests handling of invalid file paths.
func TestExecuteTask_FileWriteErrors(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Synthesizer returns invalid path that fails writeFile on test
	b := NewBuilder(BuilderConfig{
		WorkspaceDir: tmpDir,
		CodeSynthesizer: &mockCustomSynth{
			testFile: GeneratedFile{Path: string([]byte{0}), Content: "invalid"},
		},
	})
	res, err := b.ExecuteTask(context.Background(), AtomicTask{ID: "TASK-BADPATH"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success || !strings.Contains(res.ErrorMessage, "failed to write test file") {
		t.Errorf("expected write error message, got: %s", res.ErrorMessage)
	}

	// 1b. MkdirAll failure
	blockingFile := filepath.Join(tmpDir, "blocking_file")
	_ = os.WriteFile(blockingFile, []byte("x"), 0o644)
	bMkdir := NewBuilder(BuilderConfig{
		WorkspaceDir: tmpDir,
		CodeSynthesizer: &mockCustomSynth{
			testFile: GeneratedFile{Path: filepath.Join("blocking_file", "sub", "test.go"), Content: "x"},
		},
	})
	resMkdir, _ := bMkdir.ExecuteTask(context.Background(), AtomicTask{ID: "TASK-MKDIR-ERR"})
	if resMkdir.Success || !strings.Contains(resMkdir.ErrorMessage, "failed to write test file") {
		t.Errorf("expected write error message for MkdirAll failure, got: %s", resMkdir.ErrorMessage)
	}

	// 2. Synthesizer returns invalid path that fails writeFile on implementation
	runner := newMockCommandRunner(
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL"}, nil
		},
	)
	b2 := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
		CodeSynthesizer: &mockCustomSynth{
			testFile: GeneratedFile{Path: "test.go", Content: "package main\n"},
			implFile: GeneratedFile{Path: string([]byte{0}), Content: "invalid"},
		},
	})
	res2, err := b2.ExecuteTask(context.Background(), AtomicTask{ID: "TASK-BADPATH-IMPL"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Success || !strings.Contains(res2.ErrorMessage, "failed to write implementation file") {
		t.Errorf("expected write error message, got: %s", res2.ErrorMessage)
	}
}

type mockCustomSynth struct {
	testFile GeneratedFile
	implFile GeneratedFile
}

func (m *mockCustomSynth) GenerateTest(ctx context.Context, task AtomicTask, tc TestCase) (GeneratedFile, error) {
	return m.testFile, nil
}

func (m *mockCustomSynth) GenerateImplementation(ctx context.Context, task AtomicTask, tc TestCase, failureOutput string, iteration int) (GeneratedFile, error) {
	return m.implFile, nil
}

func (m *mockCustomSynth) RefactorCode(ctx context.Context, task AtomicTask, tc TestCase) ([]GeneratedFile, error) {
	return nil, nil
}

// TestExecuteTask_ContextCanceledDuringGreenLoop tests cancellation between iterations.
func TestExecuteTask_ContextCanceledDuringGreenLoop(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())

	runner := newMockCommandRunner(
		// Red phase
		func(dir, cmd string, args ...string) (CommandResult, error) {
			cancel() // cancel right after red phase
			return CommandResult{ExitCode: 1, Stdout: "FAIL"}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
	})

	_, err := b.ExecuteTask(ctx, AtomicTask{ID: "TASK-CANCEL-GREEN"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestBuildSpec_ContextCanceledAndTaskError tests BuildSpec loop cancellation and task failure.
func TestBuildSpec_ContextCanceledAndTaskError(t *testing.T) {
	tmpDir := t.TempDir()

	// Context canceled during loop
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b := NewBuilder(BuilderConfig{WorkspaceDir: tmpDir})
	spec := &SpecDocument{
		AtomicTasks: []AtomicTask{
			{ID: "TASK-1"},
		},
	}
	_, err := b.BuildSpec(ctx, spec)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Task error on invalid workspace during BuildSpec
	bInvalid := NewBuilder(BuilderConfig{WorkspaceDir: ""})
	_, err = bInvalid.BuildSpec(context.Background(), spec)
	if !errors.Is(err, ErrEmptyWorkspaceDir) {
		t.Errorf("expected ErrEmptyWorkspaceDir, got %v", err)
	}
}

// TestExtractJSONContent verifies markdown code fence stripping and raw JSON preservation.
func TestExtractJSONContent(t *testing.T) {
	// Raw JSON
	raw := `{"path": "a.go", "content": "package a"}`
	if got := extractJSONContent(raw); got != raw {
		t.Errorf("expected %s, got %s", raw, got)
	}

	// Markdown fence
	markdown := "```json\n{\"path\": \"b.go\"}\n```"
	expected := "{\"path\": \"b.go\"}"
	if got := extractJSONContent(markdown); got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}

	// Single line
	single := "```"
	if got := extractJSONContent(single); got != single {
		t.Errorf("expected %s, got %s", single, got)
	}
}

// TestDefaultCodeSynthesizer_LLMRefactorAndInvalidJSON tests LLM refactor and invalid JSON handling.
func TestDefaultCodeSynthesizer_LLMRefactorAndInvalidJSON(t *testing.T) {
	// Invalid JSON causes fallback
	mockLLM := &builderMockLLMClient{
		responseFunc: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			return &llm.ChatResponse{
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "invalid json string here",
				},
			}, nil
		},
	}

	synth := &defaultCodeSynthesizer{llmClient: mockLLM}
	gfTest, err := synth.GenerateTest(context.Background(), AtomicTask{ID: "TASK-INV"}, TestCase{Name: "TestInv"})
	if err != nil {
		t.Fatalf("expected fallback on invalid JSON, got error: %v", err)
	}
	if !strings.Contains(gfTest.Content, "TestInv") {
		t.Errorf("expected fallback content with test name, got: %s", gfTest.Content)
	}

	gfImpl, err := synth.GenerateImplementation(context.Background(), AtomicTask{ID: "TASK-INV"}, TestCase{Name: "TestInv"}, "err", 1)
	if err != nil {
		t.Fatalf("expected fallback on invalid JSON, got error: %v", err)
	}
	if !strings.Contains(gfImpl.Content, "TASK-INV") {
		t.Errorf("expected fallback content with task ID, got: %s", gfImpl.Content)
	}

	// LLM Refactor
	mockLLMRefactor := &builderMockLLMClient{
		responseFunc: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			return &llm.ChatResponse{
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: `{"path": "refactored.go", "content": "package main\n"}`,
				},
			}, nil
		},
	}
	synthRefactor := &defaultCodeSynthesizer{llmClient: mockLLMRefactor}
	refactored, err := synthRefactor.RefactorCode(context.Background(), AtomicTask{ID: "TASK-REF"}, TestCase{Name: "TestRef"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refactored) != 1 || refactored[0].Path != "refactored.go" {
		t.Errorf("expected refactored file, got: %+v", refactored)
	}
}

// TestBuilder_RealFileSystemModifications verifies actual files are written to workspace.
func TestBuilder_RealFileSystemModifications(t *testing.T) {
	tmpDir := t.TempDir()

	runner := newMockCommandRunner(
		// Red
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "FAIL"}, nil
		},
		// Green
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "PASS"}, nil
		},
		// Refactor format
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		// Refactor test
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
		// Git diff
		func(dir, cmd string, args ...string) (CommandResult, error) {
			return CommandResult{ExitCode: 0, Stdout: "diff"}, nil
		},
	)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir:    tmpDir,
		CommandExecutor: runner,
	})

	task := AtomicTask{ID: "TASK-FS", Title: "FS task"}
	tc := TestCase{Name: "TestFileSystem"}

	res, err := b.ExecuteTask(context.Background(), task, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected build success, got %s", res.ErrorMessage)
	}

	// Verify at least one file was created in tmpDir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed reading workspace dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected generated files in workspace directory")
	}

	// Verify file path recorded in FilesModified
	var found bool
	for _, f := range res.FilesModified {
		fullPath := filepath.Join(tmpDir, f)
		if _, err := os.Stat(fullPath); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected modified files to exist on disk: %v", res.FilesModified)
	}
}
