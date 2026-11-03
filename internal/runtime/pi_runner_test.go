// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/runtime"
)

// mockGateway records all published events for assertion.
type mockGateway struct {
	mu     sync.Mutex
	events []gateway.Event
}

func (m *mockGateway) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *mockGateway) Events() []gateway.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]gateway.Event, len(m.events))
	copy(copied, m.events)
	return copied
}

func (m *mockGateway) HasTopic(topic string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.Type == topic {
			return true
		}
	}
	return false
}

// createMockCommandRunner returns a CommandRunner that feeds specified stdout and stderr lines.
func createMockCommandRunner(stdoutLines []string, stderrLines []string, returnErr error) runtime.CommandRunner {
	return func(ctx context.Context, cmd *exec.Cmd, stdoutHandler func(io.Reader) error, stderrHandler func(io.Reader) error) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if stdoutHandler != nil && len(stdoutLines) > 0 {
			stdoutContent := strings.Join(stdoutLines, "\n") + "\n"
			if err := stdoutHandler(strings.NewReader(stdoutContent)); err != nil {
				return err
			}
		}

		if stderrHandler != nil && len(stderrLines) > 0 {
			stderrContent := strings.Join(stderrLines, "\n") + "\n"
			if err := stderrHandler(strings.NewReader(stderrContent)); err != nil {
				return err
			}
		}

		return returnErr
	}
}

func TestDetectPiBinary(t *testing.T) {
	t.Run("resolves explicit PI_BINARY_PATH", func(t *testing.T) {
		tempDir := t.TempDir()
		mockBin := filepath.Join(tempDir, "mock-pb.exe")
		if err := os.WriteFile(mockBin, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
			t.Fatalf("failed to create temp mock binary: %v", err)
		}

		t.Setenv("PI_BINARY_PATH", mockBin)
		detected, err := runtime.DetectPiBinary()
		if err != nil {
			t.Fatalf("expected binary detection, got error: %v", err)
		}
		if detected != mockBin {
			t.Errorf("expected %s, got %s", mockBin, detected)
		}
	})

	t.Run("returns error on invalid PI_BINARY_PATH", func(t *testing.T) {
		t.Setenv("PI_BINARY_PATH", filepath.Join(t.TempDir(), "non-existent-binary-12345"))
		_, err := runtime.DetectPiBinary()
		if err == nil {
			t.Fatalf("expected error for non-existent binary, got nil")
		}
		if !errors.Is(err, runtime.ErrPiBinaryNotFound) {
			t.Errorf("expected ErrPiBinaryNotFound, got %v", err)
		}
	})

	t.Run("fallback when PI_BINARY_PATH is unset", func(t *testing.T) {
		t.Setenv("PI_BINARY_PATH", "")
		// If pb, pi-bolt, or pi is in PATH, it will succeed, otherwise returns ErrPiBinaryNotFound
		detected, err := runtime.DetectPiBinary()
		if err != nil && !errors.Is(err, runtime.ErrPiBinaryNotFound) {
			t.Fatalf("expected ErrPiBinaryNotFound or found binary, got: %v", err)
		}
		if err == nil && detected == "" {
			t.Fatalf("detected binary path is empty")
		}
	})
}

func TestBuildArgs(t *testing.T) {
	t.Run("builds comprehensive JSONL arguments", func(t *testing.T) {
		cfg := runtime.PiRunConfig{
			Model:              "claude-3-7-sonnet",
			Thinking:           "high",
			SystemPrompt:       "You are a test agent.",
			AppendSystemPrompt: "Extra rules.",
			Skills:             []string{"tdd", "security-audit"},
			Tools:              []string{"read", "edit", "bash"},
			Extensions:         []string{"/path/to/ext.js"},
			Mode:               "json",
			NoSession:          true,
			SessionDir:         "/tmp/sessions",
			UserPrompt:         "Implement feature X",
		}

		args := runtime.BuildArgs(cfg)
		argsStr := strings.Join(args, " ")

		expectedFragments := []string{
			"--mode json -p",
			"--model claude-3-7-sonnet",
			"--thinking high",
			"--system-prompt You are a test agent.",
			"--append-system-prompt Extra rules.",
			"--skill tdd",
			"--skill security-audit",
			"--tools read,edit,bash",
			"--extension /path/to/ext.js",
			"--no-session",
			"--session-dir /tmp/sessions",
			"-- Implement feature X",
		}

		for _, frag := range expectedFragments {
			if !strings.Contains(argsStr, frag) {
				t.Errorf("expected args to contain '%s', got '%s'", frag, argsStr)
			}
		}
	})

	t.Run("builds text mode arguments", func(t *testing.T) {
		cfg := runtime.PiRunConfig{
			Mode:       "text",
			UserPrompt: "Hello world",
		}
		args := runtime.BuildArgs(cfg)
		if len(args) == 0 || args[0] != "-p" {
			t.Errorf("expected args to start with -p in text mode, got %v", args)
		}
	})
}

func TestPiRunner_Run(t *testing.T) {
	sampleJSONLLines := []string{
		`{"type":"session","id":"sess-123","version":3,"cwd":"/workspace"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","usage":{"totalTokens":10,"input":5,"output":5},"assistantMessageEvent":{"type":"text_delta","delta":"Building"}}`,
		`{"type":"message_update","usage":{"totalTokens":25,"input":10,"output":15},"assistantMessageEvent":{"type":"text_delta","delta":" spec..."}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Building spec completed successfully."}],"usage":{"totalTokens":50,"input":20,"output":30},"durationMs":1200}}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Building spec completed successfully."}]}}`,
		`{"type":"agent_settled","aborted":false}`,
	}

	t.Run("successfully runs and parses JSONL streaming events", func(t *testing.T) {
		mockGw := &mockGateway{}
		runner, err := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath:    "mock-pb",
			Gateway:       mockGw,
			CommandRunner: createMockCommandRunner(sampleJSONLLines, nil, nil),
		})
		if err != nil {
			t.Fatalf("unexpected NewPiRunner error: %v", err)
		}

		var eventTypes []string
		cfg := runtime.PiRunConfig{
			TaskID:       "task-01",
			RunID:        "run-999",
			WorkspaceDir: "/tmp/worktree",
			Model:        "sonnet",
			UserPrompt:   "Generate spec",
			OnEvent: func(ev runtime.PiEvent) {
				eventTypes = append(eventTypes, ev.Type)
			},
		}

		res, err := runner.Run(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected successful run, got %v", err)
		}

		if res.Output != "Building spec completed successfully." {
			t.Errorf("expected output 'Building spec completed successfully.', got '%s'", res.Output)
		}
		if res.TotalTokens != 50 {
			t.Errorf("expected 50 total tokens, got %d", res.TotalTokens)
		}
		if res.InputTokens != 20 || res.OutputTokens != 30 {
			t.Errorf("expected 20 input and 30 output tokens, got in=%d out=%d", res.InputTokens, res.OutputTokens)
		}
		if len(res.Events) != len(sampleJSONLLines) {
			t.Errorf("expected %d events, got %d", len(sampleJSONLLines), len(res.Events))
		}
		if len(eventTypes) != len(sampleJSONLLines) {
			t.Errorf("expected OnEvent callback to receive %d events, got %d", len(sampleJSONLLines), len(eventTypes))
		}

		// Gateway verification
		if !mockGw.HasTopic(gateway.TopicTaskStarted) {
			t.Errorf("expected gateway event for %s", gateway.TopicTaskStarted)
		}
		if !mockGw.HasTopic(gateway.TopicTaskLog) {
			t.Errorf("expected gateway event for %s", gateway.TopicTaskLog)
		}
		if !mockGw.HasTopic(gateway.TopicTaskSpan) {
			t.Errorf("expected gateway event for %s", gateway.TopicTaskSpan)
		}
		if !mockGw.HasTopic(gateway.TopicTaskCompleted) {
			t.Errorf("expected gateway event for %s", gateway.TopicTaskCompleted)
		}
	})

	t.Run("handles non-JSON plaintext stream gracefully", func(t *testing.T) {
		mockGw := &mockGateway{}
		textLines := []string{"Line 1", "Line 2", "Done"}
		runner, err := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath:    "mock-pb",
			Gateway:       mockGw,
			CommandRunner: createMockCommandRunner(textLines, nil, nil),
		})
		if err != nil {
			t.Fatalf("unexpected NewPiRunner error: %v", err)
		}

		cfg := runtime.PiRunConfig{
			Mode:       "text",
			UserPrompt: "Plain run",
		}

		res, err := runner.Run(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expectedOut := "Line 1\nLine 2\nDone"
		if res.Output != expectedOut {
			t.Errorf("expected output '%s', got '%s'", expectedOut, res.Output)
		}
	})

	t.Run("returns error when UserPrompt is empty", func(t *testing.T) {
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath:    "mock-pb",
			CommandRunner: createMockCommandRunner(nil, nil, nil),
		})
		_, err := runner.Run(context.Background(), runtime.PiRunConfig{})
		if !errors.Is(err, runtime.ErrEmptyUserPrompt) {
			t.Errorf("expected ErrEmptyUserPrompt, got %v", err)
		}
	})

	t.Run("captures stderr and returns execution error on non-zero exit", func(t *testing.T) {
		mockGw := &mockGateway{}
		cmdErr := errors.New("exit status 1")
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath:    "mock-pb",
			Gateway:       mockGw,
			CommandRunner: createMockCommandRunner(nil, []string{"fatal: command failed"}, cmdErr),
		})

		res, err := runner.Run(context.Background(), runtime.PiRunConfig{
			UserPrompt: "do failure",
		})
		if err == nil {
			t.Fatalf("expected error on failed execution, got nil")
		}
		if res.Stderr != "fatal: command failed" {
			t.Errorf("expected stderr 'fatal: command failed', got '%s'", res.Stderr)
		}
		if !mockGw.HasTopic(gateway.TopicTaskFailed) {
			t.Errorf("expected gateway event for %s", gateway.TopicTaskFailed)
		}
	})

	t.Run("respects context cancellation and timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath:    "mock-pb",
			CommandRunner: createMockCommandRunner(nil, nil, nil),
		})

		_, err := runner.Run(ctx, runtime.PiRunConfig{UserPrompt: "test"})
		if err == nil {
			t.Fatalf("expected context cancellation error, got nil")
		}
	})

	t.Run("supports custom environment variables and workspace directory", func(t *testing.T) {
		var capturedCmd *exec.Cmd
		customRunner := func(ctx context.Context, cmd *exec.Cmd, stdoutHandler func(io.Reader) error, stderrHandler func(io.Reader) error) error {
			capturedCmd = cmd
			return nil
		}

		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath:    "mock-pb",
			CommandRunner: customRunner,
		})

		cfg := runtime.PiRunConfig{
			WorkspaceDir: "/tmp/custom-worktree",
			UserPrompt:   "check env",
			Env: map[string]string{
				"CUSTOM_VAR": "secret-token",
			},
		}

		_, err := runner.Run(context.Background(), cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if capturedCmd.Dir != "/tmp/custom-worktree" {
			t.Errorf("expected dir '/tmp/custom-worktree', got '%s'", capturedCmd.Dir)
		}

		hasCustomVar := false
		for _, e := range capturedCmd.Env {
			if strings.HasPrefix(e, "CUSTOM_VAR=secret-token") {
				hasCustomVar = true
				break
			}
		}
		if !hasCustomVar {
			t.Errorf("expected custom env variable in command execution")
		}
	})
}

func TestPiAdapters(t *testing.T) {
	specJSON := `{
  "title": "Auth JWT Feature",
  "architecture_overview": "Adds JWT token validation",
  "impacted_files": ["internal/auth/jwt.go"],
  "architectural_rules_applied": ["Rule A"],
  "past_lessons_considered": [],
  "tdd_plan": {
    "summary": "TDD for JWT",
    "test_cases": [
      {
        "name": "TestValidToken",
        "description": "validates signature",
        "type": "unit",
        "target_file": "internal/auth/jwt_test.go",
        "expected": "nil error"
      }
    ]
  },
  "atomic_tasks": [
    {
      "id": "jwt-01",
      "title": "Implement JWT parser",
      "description": "Parse HMAC tokens",
      "dependencies": [],
      "acceptance_criteria": ["All tests pass"]
    }
  ]
}`

	t.Run("PiSpecWriter synthesizes SpecDocument from JSON output", func(t *testing.T) {
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, specJSON),
			}, nil, nil),
		})

		writer, err := runtime.NewPiSpecWriter(runner, runtime.DefaultSpecWriterTemplate(), "/tmp/ws")
		if err != nil {
			t.Fatalf("failed to create PiSpecWriter: %v", err)
		}

		task := agent.TaskContext{
			TaskID:      "TASK-100",
			Title:       "Auth JWT Feature",
			Description: "Implement JWT validation",
		}

		spec, err := writer.GenerateSpec(context.Background(), task)
		if err != nil {
			t.Fatalf("expected spec generation success, got %v", err)
		}
		if spec.Title != "Auth JWT Feature" {
			t.Errorf("expected title 'Auth JWT Feature', got '%s'", spec.Title)
		}
		if len(spec.AtomicTasks) != 1 || spec.AtomicTasks[0].ID != "jwt-01" {
			t.Errorf("expected 1 atomic task with id jwt-01, got %v", spec.AtomicTasks)
		}
	})

	t.Run("PiSpecWriter extracts JSON from markdown code fences", func(t *testing.T) {
		fencedOutput := "Here is the plan:\n```json\n" + specJSON + "\n```\nGood luck!"
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, fencedOutput),
			}, nil, nil),
		})

		writer, _ := runtime.NewPiSpecWriter(runner, runtime.DefaultSpecWriterTemplate(), "/tmp/ws")
		spec, err := writer.GenerateSpec(context.Background(), agent.TaskContext{
			TaskID: "TASK-101",
			Title:  "Fenced Task",
		})
		if err != nil {
			t.Fatalf("failed to extract fenced JSON: %v", err)
		}
		if spec.Title != "Auth JWT Feature" {
			t.Errorf("expected spec title 'Auth JWT Feature', got '%s'", spec.Title)
		}
	})

	t.Run("PiSpecWriter falls back gracefully on unstructured output", func(t *testing.T) {
		plainText := "I will create internal/auth/jwt.go and add tests."
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, plainText),
			}, nil, nil),
		})

		writer, _ := runtime.NewPiSpecWriter(runner, runtime.DefaultSpecWriterTemplate(), "/tmp/ws")
		spec, err := writer.GenerateSpec(context.Background(), agent.TaskContext{
			TaskID:      "TASK-102",
			Title:       "Unstructured Spec",
			Description: "Freeform output description",
		})
		if err != nil {
			t.Fatalf("expected fallback spec generation, got error: %v", err)
		}
		if spec.Title != "Unstructured Spec" {
			t.Errorf("expected title 'Unstructured Spec', got '%s'", spec.Title)
		}
		if len(spec.AtomicTasks) != 1 {
			t.Errorf("expected 1 fallback atomic task, got %d", len(spec.AtomicTasks))
		}
	})

	t.Run("PiBuilder executes atomic tasks through PiRunner", func(t *testing.T) {
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"TDD tests implemented and passing."}]}}`,
			}, nil, nil),
		})

		builder, err := runtime.NewPiBuilder(runner, runtime.DefaultBuilderTemplate(), "/tmp/ws")
		if err != nil {
			t.Fatalf("failed to create PiBuilder: %v", err)
		}

		spec := &agent.SpecDocument{
			Title: "Test Spec",
			AtomicTasks: []agent.AtomicTask{
				{
					ID:          "task-1",
					Title:       "Task 1",
					Description: "Do task 1",
				},
				{
					ID:          "task-2",
					Title:       "Task 2",
					Description: "Do task 2",
				},
			},
		}

		results, err := builder.BuildSpec(context.Background(), spec)
		if err != nil {
			t.Fatalf("expected build success, got %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 build results, got %d", len(results))
		}
		if !results[0].Success || !results[1].Success {
			t.Errorf("expected both tasks to succeed, got %v", results)
		}
		if results[0].TaskID != "task-1" || results[1].TaskID != "task-2" {
			t.Errorf("expected task IDs task-1 and task-2, got %s and %s", results[0].TaskID, results[1].TaskID)
		}
	})

	t.Run("PiReviewer audits diffs and returns structured ReviewResult", func(t *testing.T) {
		reviewJSON := `{
  "reviewer_name": "Security Reviewer",
  "passed": true,
  "score": 0.95,
  "severity": "LOW",
  "findings": [],
  "summary": "No security defects detected."
}`
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, reviewJSON),
			}, nil, nil),
		})

		reviewer, err := runtime.NewPiReviewer(runner, runtime.DefaultSecurityReviewerTemplate(), "/tmp/ws")
		if err != nil {
			t.Fatalf("failed to create PiReviewer: %v", err)
		}

		if reviewer.Name() != "Security Reviewer" {
			t.Errorf("expected name 'Security Reviewer', got '%s'", reviewer.Name())
		}

		req := agent.ReviewRequest{
			RunID:        "run-10",
			Diff:         "--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n+func Safe() {}",
			FilesChanged: []string{"file.go"},
		}

		res, err := reviewer.Review(context.Background(), req)
		if err != nil {
			t.Fatalf("expected review success, got %v", err)
		}
		if !res.Passed {
			t.Errorf("expected passed to be true")
		}
		if res.Score != 0.95 {
			t.Errorf("expected score 0.95, got %f", res.Score)
		}
	})

	t.Run("PiReviewer falls back on unstructured review text", func(t *testing.T) {
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Architecture looks good and respects all module boundaries."}]}}`,
			}, nil, nil),
		})

		reviewer, _ := runtime.NewPiReviewer(runner, runtime.DefaultArchitectureReviewerTemplate(), "/tmp/ws")
		res, err := reviewer.Review(context.Background(), agent.ReviewRequest{
			RunID: "run-11",
			Diff:  "+package foo",
		})
		if err != nil {
			t.Fatalf("expected review fallback success, got %v", err)
		}
		if !res.Passed {
			t.Errorf("expected review to pass without negative keywords")
		}
		if res.ReviewerName != "Architecture Reviewer" {
			t.Errorf("expected reviewer name 'Architecture Reviewer', got '%s'", res.ReviewerName)
		}
	})

	t.Run("PiCoordinatorAdapter wires FactoryCoordinator seamlessly", func(t *testing.T) {
		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				fmt.Sprintf(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, specJSON),
			}, nil, nil),
		})

		adapter, err := runtime.NewPiCoordinatorAdapter(runner, "/tmp/ws",
			runtime.WithSpecTemplate(runtime.DefaultSpecWriterTemplate()),
			runtime.WithBuilderTemplate(runtime.DefaultBuilderTemplate()),
			runtime.WithReviewerTemplates(
				runtime.DefaultSecurityReviewerTemplate(),
				runtime.DefaultArchitectureReviewerTemplate(),
				runtime.DefaultCodeReviewerTemplate(),
			),
		)
		if err != nil {
			t.Fatalf("failed to construct adapter: %v", err)
		}

		if adapter.SpecWriter() == nil {
			t.Errorf("expected non-nil SpecWriter")
		}
		if adapter.Builder() == nil {
			t.Errorf("expected non-nil Builder")
		}
		if len(adapter.Reviewers()) != 3 {
			t.Errorf("expected 3 reviewers, got %d", len(adapter.Reviewers()))
		}
		if adapter.Approver() == nil {
			t.Errorf("expected non-nil Approver")
		}

		coordCfg := adapter.CoordinatorConfig(agent.CoordinatorConfig{MaxRetries: 5})
		if coordCfg.MaxRetries != 5 {
			t.Errorf("expected MaxRetries to be preserved as 5, got %d", coordCfg.MaxRetries)
		}
		if coordCfg.SpecWriter == nil || coordCfg.Builder == nil || coordCfg.Approver == nil {
			t.Fatalf("expected fully populated CoordinatorConfig: %+v", coordCfg)
		}

		approverWithCfg := adapter.Approver(agent.ApproverConfig{MinApprovalScore: 0.90})
		if approverWithCfg == nil {
			t.Errorf("expected non-nil Approver with custom config")
		}

		// Instantiate FactoryCoordinator to ensure full interface satisfaction
		coordinator := agent.NewFactoryCoordinator(coordCfg)
		if coordinator == nil {
			t.Fatalf("expected non-nil FactoryCoordinator")
		}
	})

	t.Run("validates nil parameters and error paths across adapters", func(t *testing.T) {
		if _, err := runtime.NewPiSpecWriter(nil, runtime.AgentTemplate{}, ""); !errors.Is(err, runtime.ErrNilRunner) {
			t.Errorf("expected ErrNilRunner, got %v", err)
		}
		if _, err := runtime.NewPiBuilder(nil, runtime.AgentTemplate{}, ""); !errors.Is(err, runtime.ErrNilRunner) {
			t.Errorf("expected ErrNilRunner, got %v", err)
		}
		if _, err := runtime.NewPiReviewer(nil, runtime.AgentTemplate{}, ""); !errors.Is(err, runtime.ErrNilRunner) {
			t.Errorf("expected ErrNilRunner, got %v", err)
		}
		if _, err := runtime.NewPiCoordinatorAdapter(nil, ""); !errors.Is(err, runtime.ErrNilRunner) {
			t.Errorf("expected ErrNilRunner, got %v", err)
		}

		runner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner(nil, nil, errors.New("simulated crash")),
		})

		writer, _ := runtime.NewPiSpecWriter(runner, runtime.AgentTemplate{}, "")
		builder, _ := runtime.NewPiBuilder(runner, runtime.AgentTemplate{}, "")
		reviewer, _ := runtime.NewPiReviewer(runner, runtime.AgentTemplate{}, "")

		// Canceled contexts
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := writer.GenerateSpec(canceledCtx, agent.TaskContext{TaskID: "T1", Title: "Title"}); err == nil {
			t.Errorf("expected error on canceled context in SpecWriter")
		}
		if _, err := writer.GenerateSpec(context.Background(), agent.TaskContext{}); err == nil {
			t.Errorf("expected validation error on invalid TaskContext")
		}
		if _, err := writer.GenerateSpec(context.Background(), agent.TaskContext{TaskID: "T1", Title: "Title"}); err == nil {
			t.Errorf("expected runner error in SpecWriter")
		}

		if _, err := builder.BuildSpec(canceledCtx, &agent.SpecDocument{}); err == nil {
			t.Errorf("expected error on canceled context in Builder")
		}
		if _, err := builder.BuildSpec(context.Background(), nil); !errors.Is(err, runtime.ErrNilSpec) {
			t.Errorf("expected ErrNilSpec, got %v", err)
		}
		if _, err := builder.BuildSpec(context.Background(), &agent.SpecDocument{}); err == nil {
			t.Errorf("expected runner error in Builder")
		}

		if _, err := reviewer.Review(canceledCtx, agent.ReviewRequest{}); err == nil {
			t.Errorf("expected error on canceled context in Reviewer")
		}
		if _, err := reviewer.Review(context.Background(), agent.ReviewRequest{}); err == nil {
			t.Errorf("expected runner error in Reviewer")
		}

		// Negative review feedback detection in unstructured fallback
		failingReviewerRunner, _ := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "mock-pb",
			CommandRunner: createMockCommandRunner([]string{
				`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Critical vulnerability and defect detected in diff!"}]}}`,
			}, nil, nil),
		})
		secReviewer, _ := runtime.NewPiReviewer(failingReviewerRunner, runtime.DefaultSecurityReviewerTemplate(), "")
		res, err := secReviewer.Review(context.Background(), agent.ReviewRequest{RunID: "run-bad", Diff: "+bad code"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Passed {
			t.Errorf("expected review to fail when vulnerability is mentioned")
		}
		if res.Score != 0.50 {
			t.Errorf("expected score 0.50 for failed review, got %f", res.Score)
		}
	})
}

func TestPiRunner_AccessorsAndSubprocess(t *testing.T) {
	t.Run("BinaryPath and SetGateway methods", func(t *testing.T) {
		runner, err := runtime.NewPiRunner(runtime.PiRunnerConfig{
			BinaryPath: "/usr/local/bin/pb",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if runner.BinaryPath() != "/usr/local/bin/pb" {
			t.Errorf("expected binary path '/usr/local/bin/pb', got '%s'", runner.BinaryPath())
		}

		gw := &mockGateway{}
		runner.SetGateway(gw)
	})

	t.Run("DefaultSubprocessRunner executes real command", func(t *testing.T) {
		cmd := exec.Command("go", "version")
		var capturedStdout string
		stdoutHandler := func(r io.Reader) error {
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			capturedStdout = string(data)
			return nil
		}

		err := runtime.DefaultSubprocessRunner(context.Background(), cmd, stdoutHandler, nil)
		if err != nil {
			t.Fatalf("DefaultSubprocessRunner failed: %v", err)
		}
		if !strings.Contains(capturedStdout, "go version") {
			t.Errorf("expected output to contain 'go version', got '%s'", capturedStdout)
		}
	})

	t.Run("NewPiRunner with empty config finds binary or returns error", func(t *testing.T) {
		t.Setenv("PI_BINARY_PATH", "")
		// If pb/pi/pi-bolt exists on test machine, it succeeds; if not, it returns ErrPiBinaryNotFound
		_, _ = runtime.NewPiRunner()
	})
}
