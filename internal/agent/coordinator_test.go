// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =========================================================================
// Mocks for SpecGenerator, TaskBuilder, GateApprover
// =========================================================================

type mockSpecGenerator struct {
	specFn    func(ctx context.Context, task TaskContext) (*SpecDocument, error)
	callCount int32
}

func (m *mockSpecGenerator) GenerateSpec(ctx context.Context, task TaskContext) (*SpecDocument, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.specFn != nil {
		return m.specFn(ctx, task)
	}
	return &SpecDocument{
		Title: task.Title,
		AtomicTasks: []AtomicTask{
			{ID: "task-1", Title: "Implement feature"},
		},
		TDDPlan: TDDPlan{
			TestCases: []TestCase{
				{Name: "TestCase1", Phase: PhaseRed},
			},
		},
	}, nil
}

type mockTaskBuilder struct {
	buildFn   func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error)
	callCount int32
}

func (m *mockTaskBuilder) BuildSpec(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.buildFn != nil {
		return m.buildFn(ctx, spec)
	}
	return []*BuildResult{
		{
			TaskID:        "task-1",
			Success:       true,
			FilesModified: []string{"pkg/feature.go"},
			GitDiff:       "diff --git a/pkg/feature.go b/pkg/feature.go\n+func Feature() {}",
			TotalDuration: 10 * time.Millisecond,
		},
	}, nil
}

type mockGateApprover struct {
	decideFn  func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error)
	callCount int32
}

func (m *mockGateApprover) Decide(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.decideFn != nil {
		return m.decideFn(ctx, req)
	}
	return &ApprovalDecision{
		DecisionType:      DecisionApproved,
		ConsolidatedScore: 1.0,
		FeedbackSummary:   "All checks passed",
	}, nil
}

// =========================================================================
// Unit Tests
// =========================================================================

func TestFactoryCoordinator_Success_FirstAttempt(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{}
	approverMock := &mockGateApprover{}

	var eventsMu sync.Mutex
	var stageEvents []string

	cfg := CoordinatorConfig{
		SpecWriter:  specMock,
		Builder:     buildMock,
		Approver:    approverMock,
		MaxRetries:  3,
		MinCoverage: 0.90,
		EventHook: func(stage string, payload any) {
			eventsMu.Lock()
			stageEvents = append(stageEvents, stage)
			eventsMu.Unlock()
		},
	}

	coordinator := NewFactoryCoordinator(cfg)

	task := TaskContext{
		TaskID: "task-100",
		Title:  "Implement user profile",
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome == nil {
		t.Fatal("expected non-nil outcome")
	}

	if !outcome.Success {
		t.Errorf("expected success=true, got %v", outcome.Success)
	}
	if outcome.Decision != DecisionApproved {
		t.Errorf("expected decision APPROVED, got %s", outcome.Decision)
	}
	if !outcome.IsApproved() {
		t.Errorf("expected IsApproved() to be true")
	}
	if outcome.IsHumanGate() || outcome.IsFailed() {
		t.Errorf("expected IsHumanGate and IsFailed to be false")
	}
	if outcome.TaskID != "task-100" {
		t.Errorf("expected TaskID task-100, got %s", outcome.TaskID)
	}
	if outcome.TotalIterations != 1 {
		t.Errorf("expected TotalIterations=1, got %d", outcome.TotalIterations)
	}
	if outcome.Spec == nil {
		t.Error("expected non-nil Spec in outcome")
	}
	if outcome.BuildResult == nil {
		t.Error("expected non-nil BuildResult in outcome")
	}
	if outcome.Approval == nil {
		t.Error("expected non-nil Approval in outcome")
	}
	if outcome.Duration < 0 {
		t.Errorf("expected non-negative Duration, got %v", outcome.Duration)
	}

	// Verify event progression
	expectedStages := []string{
		EventSpecGenerated,
		EventBuildCompleted,
		EventReviewDecided,
		EventPipelineCompleted,
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(stageEvents) < len(expectedStages) {
		t.Fatalf("expected at least %d stages, got %d: %v", len(expectedStages), len(stageEvents), stageEvents)
	}
	for i, expected := range expectedStages {
		if stageEvents[i] != expected {
			t.Errorf("stage[%d] expected %s, got %s", i, expected, stageEvents[i])
		}
	}
}

func TestFactoryCoordinator_RetryLoop_SuccessOnSecondAttempt(t *testing.T) {
	specMock := &mockSpecGenerator{}

	buildAttempts := 0
	buildMock := &mockTaskBuilder{
		buildFn: func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
			buildAttempts++
			return []*BuildResult{
				{
					TaskID:        fmt.Sprintf("task-retry-%d", buildAttempts),
					Success:       true,
					FilesModified: []string{"pkg/auth.go"},
					GitDiff:       "+func Auth() {}",
				},
			}, nil
		},
	}

	decideAttempts := 0
	approverMock := &mockGateApprover{
		decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
			decideAttempts++
			if decideAttempts == 1 {
				return &ApprovalDecision{
					DecisionType:      DecisionRetry,
					ConsolidatedScore: 0.70,
					FeedbackSummary:   "Coverage below 90% and missing docstrings",
				}, nil
			}
			return &ApprovalDecision{
				DecisionType:      DecisionApproved,
				ConsolidatedScore: 0.95,
				FeedbackSummary:   "All checks passed after retry",
			}, nil
		},
	}

	var eventsMu sync.Mutex
	var stageEvents []string

	cfg := CoordinatorConfig{
		SpecWriter:  specMock,
		Builder:     buildMock,
		Approver:    approverMock,
		MaxRetries:  3,
		MinCoverage: 0.90,
		EventHook: func(stage string, payload any) {
			eventsMu.Lock()
			stageEvents = append(stageEvents, stage)
			eventsMu.Unlock()
		},
	}

	coordinator := NewFactoryCoordinator(cfg)

	task := TaskContext{
		TaskID:      "task-retry-test",
		Title:       "Implement auth service",
		Constraints: []string{"Must be stateless"},
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !outcome.Success {
		t.Errorf("expected success=true after retry, got %v", outcome.Success)
	}
	if outcome.Decision != DecisionApproved {
		t.Errorf("expected DecisionApproved, got %s", outcome.Decision)
	}
	if outcome.TotalIterations != 2 {
		t.Errorf("expected TotalIterations=2, got %d", outcome.TotalIterations)
	}
	if buildAttempts != 2 {
		t.Errorf("expected 2 build attempts, got %d", buildAttempts)
	}
	if decideAttempts != 2 {
		t.Errorf("expected 2 decide attempts, got %d", decideAttempts)
	}

	// Verify feedback was applied to Spec
	if outcome.Spec == nil {
		t.Fatal("expected non-nil Spec")
	}
	if len(outcome.Spec.PastLessonsConsidered) == 0 {
		t.Error("expected PastLessonsConsidered to include retry feedback")
	}
	foundFixTask := false
	for _, at := range outcome.Spec.AtomicTasks {
		if at.ID == "fix-retry-1" {
			foundFixTask = true
			break
		}
	}
	if !foundFixTask {
		t.Errorf("expected targeted fix task 'fix-retry-1' in spec, got: %+v", outcome.Spec.AtomicTasks)
	}

	// Verify retry event was emitted
	eventsMu.Lock()
	defer eventsMu.Unlock()
	foundRetryEvent := false
	for _, ev := range stageEvents {
		if ev == EventRetryStarted {
			foundRetryEvent = true
			break
		}
	}
	if !foundRetryEvent {
		t.Errorf("expected %s event to be emitted", EventRetryStarted)
	}
}

func TestFactoryCoordinator_RetryLoop_MaxRetriesExhausted(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{}

	decideCount := 0
	approverMock := &mockGateApprover{
		decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
			decideCount++
			if req.RetryCount >= 2 {
				return &ApprovalDecision{
					DecisionType:      DecisionHumanGate,
					ConsolidatedScore: 0.60,
					RequiresHumanSign: true,
					GateReason:        "Max retries exhausted with failing tests",
					FeedbackSummary:   "Unresolvable test failures after retries",
				}, nil
			}
			return &ApprovalDecision{
				DecisionType:      DecisionRetry,
				ConsolidatedScore: 0.60,
				FeedbackSummary:   "Fixable test failures",
			}, nil
		},
	}

	cfg := CoordinatorConfig{
		SpecWriter:  specMock,
		Builder:     buildMock,
		Approver:    approverMock,
		MaxRetries:  2,
		MinCoverage: 0.90,
	}

	coordinator := NewFactoryCoordinator(cfg)

	task := TaskContext{
		TaskID: "task-exhausted",
		Title:  "Flaky feature",
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Success {
		t.Errorf("expected success=false on exhausted retries, got %v", outcome.Success)
	}
	if outcome.Decision != DecisionHumanGate {
		t.Errorf("expected DecisionHumanGate, got %s", outcome.Decision)
	}
	if !outcome.IsHumanGate() {
		t.Error("expected IsHumanGate()=true")
	}
	if outcome.TotalIterations != 3 { // attempt 0, 1, 2
		t.Errorf("expected TotalIterations=3, got %d", outcome.TotalIterations)
	}
}

func TestFactoryCoordinator_HumanGate_DirectSensitivePath(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{
		buildFn: func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
			return []*BuildResult{
				{
					TaskID:        "task-sensitive",
					Success:       true,
					FilesModified: []string{"migrations/001_init.sql"},
					GitDiff:       "+CREATE TABLE users();",
				},
			}, nil
		},
	}
	approverMock := &mockGateApprover{
		decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
			return &ApprovalDecision{
				DecisionType:      DecisionHumanGate,
				ConsolidatedScore: 0.90,
				RequiresHumanSign: true,
				GateReason:        "Sensitive path touched: migrations/",
				FeedbackSummary:   "Requires human review for DB migration",
			}, nil
		},
	}

	cfg := CoordinatorConfig{
		SpecWriter:  specMock,
		Builder:     buildMock,
		Approver:    approverMock,
		MaxRetries:  3,
		MinCoverage: 0.90,
	}

	coordinator := NewCoordinator(cfg)

	task := TaskContext{
		TaskID: "task-sensitive",
		Title:  "Add database migration",
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Success {
		t.Errorf("expected success=false for human gate, got %v", outcome.Success)
	}
	if outcome.Decision != DecisionHumanGate {
		t.Errorf("expected DecisionHumanGate, got %s", outcome.Decision)
	}
	if outcome.TotalIterations != 1 {
		t.Errorf("expected TotalIterations=1, got %d", outcome.TotalIterations)
	}
}

func TestFactoryCoordinator_SpecWriter_Failure(t *testing.T) {
	specMock := &mockSpecGenerator{
		specFn: func(ctx context.Context, task TaskContext) (*SpecDocument, error) {
			return nil, errors.New("LLM timeout during spec synthesis")
		},
	}
	buildMock := &mockTaskBuilder{}
	approverMock := &mockGateApprover{}

	cfg := CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	}
	coordinator := NewFactoryCoordinator(cfg)

	task := TaskContext{
		TaskID: "task-spec-err",
		Title:  "Broken spec task",
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if outcome == nil {
		t.Fatal("expected non-nil outcome on error")
	}
	if outcome.Success {
		t.Error("expected outcome.Success=false")
	}
	if outcome.Decision != DecisionFailed {
		t.Errorf("expected DecisionFailed, got %s", outcome.Decision)
	}
	if !outcome.IsFailed() {
		t.Error("expected IsFailed()=true")
	}
	if buildMock.callCount != 0 {
		t.Errorf("builder should not be called on spec failure, called %d times", buildMock.callCount)
	}
	if approverMock.callCount != 0 {
		t.Errorf("approver should not be called on spec failure, called %d times", approverMock.callCount)
	}
}

func TestFactoryCoordinator_Builder_Failure(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{
		buildFn: func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
			return nil, errors.New("command runner exited with code 127")
		},
	}
	approverMock := &mockGateApprover{}

	cfg := CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	}
	coordinator := NewFactoryCoordinator(cfg)

	task := TaskContext{
		TaskID: "task-build-err",
		Title:  "Broken build task",
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if outcome == nil {
		t.Fatal("expected non-nil outcome on build error")
	}
	if outcome.Success {
		t.Error("expected outcome.Success=false")
	}
	if outcome.Decision != DecisionFailed {
		t.Errorf("expected DecisionFailed, got %s", outcome.Decision)
	}
	if approverMock.callCount != 0 {
		t.Errorf("approver should not be called on build error, called %d times", approverMock.callCount)
	}
}

func TestFactoryCoordinator_Approver_Failure(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{}
	approverMock := &mockGateApprover{
		decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
			return nil, errors.New("approver reviewer panic")
		},
	}

	cfg := CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	}
	coordinator := NewFactoryCoordinator(cfg)

	task := TaskContext{
		TaskID: "task-approver-err",
		Title:  "Broken approver task",
	}

	outcome, err := coordinator.RunPipeline(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if outcome == nil {
		t.Fatal("expected non-nil outcome on approver error")
	}
	if outcome.Decision != DecisionFailed {
		t.Errorf("expected DecisionFailed, got %s", outcome.Decision)
	}
}

func TestFactoryCoordinator_ContextCanceled(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{}
	approverMock := &mockGateApprover{}

	cfg := CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	}
	coordinator := NewFactoryCoordinator(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	task := TaskContext{
		TaskID: "task-canceled",
		Title:  "Canceled task",
	}

	outcome, err := coordinator.RunPipeline(ctx, task)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
	if outcome != nil {
		t.Error("expected nil outcome when context canceled before start")
	}
}

func TestFactoryCoordinator_ValidationAndDefaults(t *testing.T) {
	// 1. Nil coordinator check
	var nilCoord *FactoryCoordinator
	_, err := nilCoord.RunPipeline(context.Background(), TaskContext{TaskID: "t1", Title: "T1"})
	if !errors.Is(err, ErrNilCoordinator) {
		t.Errorf("expected ErrNilCoordinator, got %v", err)
	}

	// 2. Missing components validation
	cEmpty := NewFactoryCoordinator(CoordinatorConfig{})
	if _, err := cEmpty.RunPipeline(context.Background(), TaskContext{TaskID: "t1", Title: "T1"}); !errors.Is(err, ErrNilSpecWriter) {
		t.Errorf("expected ErrNilSpecWriter, got %v", err)
	}

	cWithSpec := NewFactoryCoordinator(CoordinatorConfig{SpecWriter: &mockSpecGenerator{}})
	if _, err := cWithSpec.RunPipeline(context.Background(), TaskContext{TaskID: "t1", Title: "T1"}); !errors.Is(err, ErrNilBuilder) {
		t.Errorf("expected ErrNilBuilder, got %v", err)
	}

	cWithBuilder := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: &mockSpecGenerator{},
		Builder:    &mockTaskBuilder{},
	})
	if _, err := cWithBuilder.RunPipeline(context.Background(), TaskContext{TaskID: "t1", Title: "T1"}); !errors.Is(err, ErrNilApprover) {
		t.Errorf("expected ErrNilApprover, got %v", err)
	}

	// 3. TaskContext validation
	validCoord := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: &mockSpecGenerator{},
		Builder:    &mockTaskBuilder{},
		Approver:   &mockGateApprover{},
	})

	if _, err := validCoord.RunPipeline(context.Background(), TaskContext{}); !errors.Is(err, ErrEmptyTaskID) {
		t.Errorf("expected ErrEmptyTaskID, got %v", err)
	}
	if _, err := validCoord.RunPipeline(context.Background(), TaskContext{TaskID: "id"}); !errors.Is(err, ErrEmptyTaskTitle) {
		t.Errorf("expected ErrEmptyTaskTitle, got %v", err)
	}

	// 4. Config normalization defaults
	cfg := CoordinatorConfig{}
	cfg.Normalize()
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected default MaxRetries=%d, got %d", DefaultMaxRetries, cfg.MaxRetries)
	}
	if cfg.MinCoverage != DefaultMinCoverage {
		t.Errorf("expected default MinCoverage=%.2f, got %.2f", DefaultMinCoverage, cfg.MinCoverage)
	}
}

func TestFactoryCoordinator_CoverageCalculation(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{
		buildFn: func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
			return []*BuildResult{
				{
					TaskID:  "task-cov",
					Success: true,
					Phases: []PhaseOutcome{
						{Phase: PhaseGreen, Stdout: "coverage: 88.5% of statements"},
					},
				},
			}, nil
		},
	}

	var recordedCoverage float32
	approverMock := &mockGateApprover{
		decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
			recordedCoverage = req.Coverage
			return &ApprovalDecision{
				DecisionType: DecisionApproved,
			}, nil
		},
	}

	coord := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	})

	// 1. Extracted from stdout
	task := TaskContext{TaskID: "task-cov", Title: "Check stdout coverage"}
	_, err := coord.RunPipeline(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recordedCoverage < 0.884 || recordedCoverage > 0.886 {
		t.Errorf("expected coverage around 0.885, got %f", recordedCoverage)
	}

	// 2. Overridden via task.Metadata["coverage"]
	taskWithMeta := TaskContext{
		TaskID: "task-cov-meta",
		Title:  "Metadata coverage",
		Metadata: map[string]any{
			"coverage": float32(0.94),
		},
	}
	_, err = coord.RunPipeline(context.Background(), taskWithMeta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recordedCoverage != 0.94 {
		t.Errorf("expected coverage 0.94, got %f", recordedCoverage)
	}

	// 3. Custom CoverageCalculator
	customCoord := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
		CoverageCalculator: func(ctx context.Context, build *BuildResult, task TaskContext) float32 {
			return 0.99
		},
	})
	_, err = customCoord.RunPipeline(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recordedCoverage != 0.99 {
		t.Errorf("expected custom coverage 0.99, got %f", recordedCoverage)
	}
}

func TestFactoryCoordinator_MultipleBuildResultsConsolidation(t *testing.T) {
	specMock := &mockSpecGenerator{
		specFn: func(ctx context.Context, task TaskContext) (*SpecDocument, error) {
			return &SpecDocument{
				Title: task.Title,
				AtomicTasks: []AtomicTask{
					{ID: "task-1", Title: "Subtask 1"},
					{ID: "task-2", Title: "Subtask 2"},
				},
			}, nil
		},
	}

	buildMock := &mockTaskBuilder{
		buildFn: func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
			return []*BuildResult{
				{
					TaskID:        "task-1",
					Success:       true,
					FilesModified: []string{"pkg/a.go", "pkg/shared.go"},
					GitDiff:       "+diff a",
					TotalDuration: 5 * time.Millisecond,
				},
				{
					TaskID:        "task-2",
					Success:       true,
					FilesModified: []string{"pkg/b.go", "pkg/shared.go"},
					GitDiff:       "+diff b",
					TotalDuration: 7 * time.Millisecond,
				},
			}, nil
		},
	}

	var reqDiff string
	var reqFiles []string
	approverMock := &mockGateApprover{
		decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
			reqDiff = req.Diff
			reqFiles = req.FilesChanged
			return &ApprovalDecision{
				DecisionType: DecisionApproved,
			}, nil
		},
	}

	coord := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	})

	outcome, err := coord.RunPipeline(context.Background(), TaskContext{
		TaskID: "multi-task",
		Title:  "Multiple subtasks",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.BuildResult == nil {
		t.Fatal("expected non-nil consolidated BuildResult")
	}

	// Check deduplicated files: pkg/a.go, pkg/shared.go, pkg/b.go (total 3)
	if len(outcome.BuildResult.FilesModified) != 3 {
		t.Errorf("expected 3 deduplicated files, got %d: %v", len(outcome.BuildResult.FilesModified), outcome.BuildResult.FilesModified)
	}
	if len(reqFiles) != 3 {
		t.Errorf("expected 3 files passed to approval, got %d: %v", len(reqFiles), reqFiles)
	}

	// Check combined diff
	if !strings.Contains(reqDiff, "+diff a") || !strings.Contains(reqDiff, "+diff b") {
		t.Errorf("expected combined diff to contain both changes, got %q", reqDiff)
	}
}

func TestFactoryCoordinator_Concurrent_RaceSafety(t *testing.T) {
	specMock := &mockSpecGenerator{}
	buildMock := &mockTaskBuilder{}
	approverMock := &mockGateApprover{}

	coordinator := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: specMock,
		Builder:    buildMock,
		Approver:   approverMock,
	})

	var wg sync.WaitGroup
	numRoutines := 10

	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			task := TaskContext{
				TaskID: fmt.Sprintf("task-concurrent-%d", idx),
				Title:  fmt.Sprintf("Concurrent task %d", idx),
			}
			outcome, err := coordinator.RunPipeline(context.Background(), task)
			if err != nil {
				t.Errorf("concurrent run failed: %v", err)
			}
			if outcome == nil || !outcome.Success {
				t.Errorf("expected success for concurrent task %d", idx)
			}
		}(i)
	}

	wg.Wait()
}

func TestFactoryCoordinator_EdgeCasesAndBranchCoverage(t *testing.T) {
	// 1. applyFeedbackToSpec nil guards
	coord := NewFactoryCoordinator(CoordinatorConfig{})
	coord.applyFeedbackToSpec(nil, 0, nil, 0.90)
	coord.applyFeedbackToSpec(&SpecDocument{}, 0, nil, 0.90)

	// 2. consolidateBuildResults edge cases
	resEmpty := consolidateBuildResults("t-empty", nil)
	if resEmpty == nil || resEmpty.TaskID != "t-empty" || !resEmpty.Success {
		t.Errorf("unexpected resEmpty: %+v", resEmpty)
	}

	resWithNilsAndErrors := consolidateBuildResults("t-err", []*BuildResult{
		nil,
		{
			TaskID:        "sub-1",
			Success:       false,
			ErrorMessage:  "compiler error",
			TotalDuration: 5 * time.Millisecond,
		},
		{
			TaskID:        "sub-2",
			Success:       true,
			ErrorMessage:  "lint warning",
			TotalDuration: 3 * time.Millisecond,
		},
	})
	if resWithNilsAndErrors.Success {
		t.Error("expected consolidated Success=false when a subtask failed")
	}
	if !strings.Contains(resWithNilsAndErrors.ErrorMessage, "compiler error") {
		t.Errorf("expected combined error message, got %s", resWithNilsAndErrors.ErrorMessage)
	}

	// 3. calculateCoverage edge cases: float64 and int in metadata, failed build, nil build
	covFloat64 := coord.calculateCoverage(context.Background(), nil, TaskContext{
		Metadata: map[string]any{"coverage": float64(0.91)},
	})
	if covFloat64 < 0.909 || covFloat64 > 0.911 {
		t.Errorf("expected float64 coverage 0.91, got %f", covFloat64)
	}

	covInt := coord.calculateCoverage(context.Background(), nil, TaskContext{
		Metadata: map[string]any{"coverage": 85},
	})
	if covInt < 0.849 || covInt > 0.851 {
		t.Errorf("expected int coverage 0.85, got %f", covInt)
	}

	covFailedBuild := coord.calculateCoverage(context.Background(), &BuildResult{Success: false}, TaskContext{})
	if covFailedBuild != 0.0 {
		t.Errorf("expected 0.0 coverage for failed build, got %f", covFailedBuild)
	}

	covNilBuild := coord.calculateCoverage(context.Background(), nil, TaskContext{})
	if covNilBuild != 0.0 {
		t.Errorf("expected 0.0 coverage for nil build, got %f", covNilBuild)
	}

	// 4. Context canceled during retry loop
	ctxCancel, cancel := context.WithCancel(context.Background())
	loopAttempts := 0
	coordWithCancel := NewFactoryCoordinator(CoordinatorConfig{
		SpecWriter: &mockSpecGenerator{},
		Builder: &mockTaskBuilder{
			buildFn: func(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error) {
				loopAttempts++
				if loopAttempts >= 1 {
					cancel()
				}
				return []*BuildResult{{Success: true}}, nil
			},
		},
		Approver: &mockGateApprover{
			decideFn: func(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
				return &ApprovalDecision{DecisionType: DecisionRetry}, nil
			},
		},
		MaxRetries: 3,
	})

	_, err := coordWithCancel.RunPipeline(ctxCancel, TaskContext{TaskID: "t-cancel-loop", Title: "Cancel in loop"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in retry loop, got %v", err)
	}
}

func TestFactoryCoordinator_RealAgentWiring(t *testing.T) {
	// Verify that real *SpecWriter, *Builder, *Approver satisfy the interfaces
	var sw *SpecWriter = NewSpecWriter(SpecWriterConfig{})
	var b *Builder = NewBuilder(BuilderConfig{WorkspaceDir: "."})
	var app *Approver = NewApprover(nil, ApproverConfig{})

	cfg := CoordinatorConfig{
		SpecWriter: sw,
		Builder:    b,
		Approver:   app,
	}

	coord := NewFactoryCoordinator(cfg)
	if coord == nil {
		t.Fatal("expected non-nil coordinator from real agents wiring")
	}
}
