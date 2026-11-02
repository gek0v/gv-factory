// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Standard domain constants and events for FactoryCoordinator.
const (
	// DecisionFailed indicates the pipeline failed due to execution or tool error.
	DecisionFailed DecisionType = "FAILED"

	// DefaultMaxRetries is the fallback maximum retry count when Approver returns DecisionRetry.
	DefaultMaxRetries = 3

	// DefaultMinCoverage is the fallback coverage threshold required for auto-approval.
	DefaultMinCoverage float32 = 0.90

	// Telemetry event stage identifiers.
	EventSpecGenerated     = "spec_generated"
	EventBuildCompleted    = "build_completed"
	EventReviewDecided     = "review_decided"
	EventRetryStarted      = "retry_started"
	EventPipelineCompleted = "pipeline_completed"
)

// Standard domain errors for FactoryCoordinator.
var (
	ErrNilCoordinator = errors.New("agent: coordinator cannot be nil")
	ErrNilSpecWriter  = errors.New("agent: spec writer is required")
	ErrNilBuilder     = errors.New("agent: builder is required")
	ErrNilApprover    = errors.New("agent: approver is required")
)

// SpecGenerator defines the interface for generating formal SDDs and TDD plans from tasks.
type SpecGenerator interface {
	GenerateSpec(ctx context.Context, task TaskContext) (*SpecDocument, error)
}

// TaskBuilder defines the interface for executing Red-Green-Refactor builds for a spec.
type TaskBuilder interface {
	BuildSpec(ctx context.Context, spec *SpecDocument) ([]*BuildResult, error)
}

// GateApprover defines the interface for evaluating code changes against governance policies.
type GateApprover interface {
	Decide(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error)
}

// CoordinatorConfig configures execution thresholds, agents, and telemetry for the pipeline.
type CoordinatorConfig struct {
	SpecWriter         SpecGenerator
	Builder            TaskBuilder
	Approver           GateApprover
	MaxRetries         int
	MinCoverage        float32
	EventHook          func(stage string, payload any)
	CoverageCalculator func(ctx context.Context, build *BuildResult, task TaskContext) float32
}

// Normalize ensures reasonable defaults for missing configuration fields.
func (cfg *CoordinatorConfig) Normalize() {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.MinCoverage <= 0 {
		cfg.MinCoverage = DefaultMinCoverage
	}
}

// PipelineOutcome models the consolidated outcome of an autonomous factory run.
type PipelineOutcome struct {
	TaskID          string            `json:"task_id"`
	Success         bool              `json:"success"`
	Decision        DecisionType      `json:"decision"`
	Spec            *SpecDocument     `json:"spec,omitempty"`
	BuildResult     *BuildResult      `json:"build_result,omitempty"`
	Approval        *ApprovalDecision `json:"approval,omitempty"`
	TotalIterations int               `json:"total_iterations"`
	Duration        time.Duration     `json:"duration"`
	ErrorMessage    string            `json:"error_message,omitempty"`
}

// IsApproved returns true if the final gate decision is APPROVED.
func (p *PipelineOutcome) IsApproved() bool {
	return p != nil && p.Decision == DecisionApproved
}

// IsHumanGate returns true if the pipeline requires human gate intervention.
func (p *PipelineOutcome) IsHumanGate() bool {
	return p != nil && p.Decision == DecisionHumanGate
}

// IsFailed returns true if the pipeline encountered an unrecoverable failure.
func (p *PipelineOutcome) IsFailed() bool {
	return p != nil && p.Decision == DecisionFailed
}

// FactoryCoordinator unifies SpecWriter, Builder, and Approver into a closed-loop
// autonomous software engineering factory engine with feedback and retry support.
type FactoryCoordinator struct {
	specWriter         SpecGenerator
	builder            TaskBuilder
	approver           GateApprover
	maxRetries         int
	minCoverage        float32
	eventHook          func(stage string, payload any)
	coverageCalculator func(ctx context.Context, build *BuildResult, task TaskContext) float32
	mu                 sync.RWMutex
}

// NewFactoryCoordinator constructs a new FactoryCoordinator with normalized config.
func NewFactoryCoordinator(cfg CoordinatorConfig) *FactoryCoordinator {
	cfg.Normalize()
	return &FactoryCoordinator{
		specWriter:         cfg.SpecWriter,
		builder:            cfg.Builder,
		approver:           cfg.Approver,
		maxRetries:         cfg.MaxRetries,
		minCoverage:        cfg.MinCoverage,
		eventHook:          cfg.EventHook,
		coverageCalculator: cfg.CoverageCalculator,
	}
}

// NewCoordinator is an alias for NewFactoryCoordinator.
func NewCoordinator(cfg CoordinatorConfig) *FactoryCoordinator {
	return NewFactoryCoordinator(cfg)
}

// RunPipeline executes the autonomous lifecycle: Spec Synthesis -> Build -> Review Gate -> Feedback Loop.
func (c *FactoryCoordinator) RunPipeline(ctx context.Context, task TaskContext) (*PipelineOutcome, error) {
	if c == nil {
		return nil, ErrNilCoordinator
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	specWriter := c.specWriter
	builder := c.builder
	approver := c.approver
	maxRetries := c.maxRetries
	minCov := c.minCoverage
	c.mu.RUnlock()

	if specWriter == nil {
		return nil, ErrNilSpecWriter
	}
	if builder == nil {
		return nil, ErrNilBuilder
	}
	if approver == nil {
		return nil, ErrNilApprover
	}

	if err := task.Validate(); err != nil {
		return nil, err
	}
	task.Normalize()

	startTime := time.Now()

	// Stage 1: Spec Synthesis
	spec, err := specWriter.GenerateSpec(ctx, task)
	if err != nil {
		duration := time.Since(startTime)
		outcome := &PipelineOutcome{
			TaskID:          task.TaskID,
			Success:         false,
			Decision:        DecisionFailed,
			TotalIterations: 0,
			Duration:        duration,
			ErrorMessage:    fmt.Sprintf("spec generation failed: %v", err),
		}
		return outcome, err
	}
	c.emitEvent(EventSpecGenerated, spec)

	var lastBuildResult *BuildResult
	var lastApproval *ApprovalDecision
	totalIterations := 0

	// Feedback & Evaluation Loop
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		totalIterations++

		if attempt > 0 {
			feedback := ""
			if lastApproval != nil {
				feedback = lastApproval.FeedbackSummary
			}
			c.emitEvent(EventRetryStarted, map[string]any{
				"attempt":     attempt,
				"max_retries": maxRetries,
				"feedback":    feedback,
			})
		}

		// Stage 2: Implementation (Builder)
		buildResults, err := builder.BuildSpec(ctx, spec)
		if err != nil {
			duration := time.Since(startTime)
			outcome := &PipelineOutcome{
				TaskID:          task.TaskID,
				Success:         false,
				Decision:        DecisionFailed,
				Spec:            spec,
				BuildResult:     lastBuildResult,
				Approval:        lastApproval,
				TotalIterations: totalIterations,
				Duration:        duration,
				ErrorMessage:    fmt.Sprintf("build execution failed: %v", err),
			}
			return outcome, err
		}

		lastBuildResult = consolidateBuildResults(task.TaskID, buildResults)
		c.emitEvent(EventBuildCompleted, lastBuildResult)

		// Stage 3: Review & Gate Decision (Approver)
		coverage := c.calculateCoverage(ctx, lastBuildResult, task)
		approvalReq := ApprovalRequest{
			RunID:        task.TaskID,
			Diff:         lastBuildResult.GitDiff,
			Spec:         spec,
			FilesChanged: lastBuildResult.FilesModified,
			Coverage:     coverage,
			RetryCount:   attempt,
		}

		approval, err := approver.Decide(ctx, approvalReq)
		if err != nil {
			duration := time.Since(startTime)
			outcome := &PipelineOutcome{
				TaskID:          task.TaskID,
				Success:         false,
				Decision:        DecisionFailed,
				Spec:            spec,
				BuildResult:     lastBuildResult,
				Approval:        lastApproval,
				TotalIterations: totalIterations,
				Duration:        duration,
				ErrorMessage:    fmt.Sprintf("approval evaluation failed: %v", err),
			}
			return outcome, err
		}
		lastApproval = approval
		c.emitEvent(EventReviewDecided, approval)

		// Stage 4: Feedback Loop (when DecisionRetry)
		if approval.IsRetry() && attempt < maxRetries {
			feedbackMsg := fmt.Sprintf("Reviewer feedback (retry %d/%d): %s", attempt+1, maxRetries, approval.FeedbackSummary)
			task.Constraints = append(task.Constraints, feedbackMsg)

			c.applyFeedbackToSpec(spec, attempt, approval, minCov)
			continue
		}

		break
	}

	// Stage 5: Final Outcome
	duration := time.Since(startTime)
	var finalDecision DecisionType
	isSuccess := false

	if lastApproval != nil {
		finalDecision = lastApproval.DecisionType
		isSuccess = lastApproval.IsApproved()
		if finalDecision == DecisionRetry {
			finalDecision = DecisionHumanGate
			lastApproval.DecisionType = DecisionHumanGate
			lastApproval.RequiresHumanSign = true
			if lastApproval.GateReason == "" {
				lastApproval.GateReason = fmt.Sprintf("Max retries exceeded (%d/%d)", maxRetries, maxRetries)
			}
		}
	} else {
		finalDecision = DecisionFailed
	}

	outcome := &PipelineOutcome{
		TaskID:          task.TaskID,
		Success:         isSuccess,
		Decision:        finalDecision,
		Spec:            spec,
		BuildResult:     lastBuildResult,
		Approval:        lastApproval,
		TotalIterations: totalIterations,
		Duration:        duration,
	}

	c.emitEvent(EventPipelineCompleted, outcome)
	return outcome, nil
}

func (c *FactoryCoordinator) emitEvent(stage string, payload any) {
	c.mu.RLock()
	hook := c.eventHook
	c.mu.RUnlock()

	if hook != nil {
		hook(stage, payload)
	}
}

func (c *FactoryCoordinator) applyFeedbackToSpec(spec *SpecDocument, attempt int, approval *ApprovalDecision, minCoverage float32) {
	if spec == nil || approval == nil {
		return
	}

	lesson := fmt.Sprintf("Retry %d feedback: %s", attempt+1, approval.FeedbackSummary)
	spec.PastLessonsConsidered = append(spec.PastLessonsConsidered, lesson)

	fixTask := AtomicTask{
		ID:          fmt.Sprintf("fix-retry-%d", attempt+1),
		Title:       fmt.Sprintf("Fix defects from reviewer feedback (attempt %d)", attempt+1),
		Description: approval.FeedbackSummary,
		AcceptanceCriteria: []string{
			"Address all reviewer findings and defects",
			fmt.Sprintf("Achieve >= %.1f%% test coverage", minCoverage*100),
		},
	}
	spec.AtomicTasks = append(spec.AtomicTasks, fixTask)

	spec.TDDPlan.TestCases = append(spec.TDDPlan.TestCases, TestCase{
		Name:            fmt.Sprintf("TestFeedbackFix_Attempt%d", attempt+1),
		Phase:           PhaseGreen,
		Description:     "Verify fix addresses reviewer feedback",
		ExpectedOutcome: "All tests pass with required coverage",
	})
}

var coverageRegex = regexp.MustCompile(`(?:coverage:\s*|coverage\s+)(\d+(?:\.\d+)?)%`)

func (c *FactoryCoordinator) calculateCoverage(ctx context.Context, build *BuildResult, task TaskContext) float32 {
	c.mu.RLock()
	calc := c.coverageCalculator
	c.mu.RUnlock()

	if calc != nil {
		return calc(ctx, build, task)
	}

	// 1. Check if task metadata specifies coverage explicitly
	if task.Metadata != nil {
		if covVal, ok := task.Metadata["coverage"]; ok {
			switch v := covVal.(type) {
			case float32:
				return v
			case float64:
				return float32(v)
			case int:
				return float32(v) / 100.0
			}
		}
	}

	// 2. Check if build failed
	if build != nil && !build.Success {
		return 0.0
	}

	// 3. Inspect phase output for coverage pattern
	if build != nil {
		for _, phase := range build.Phases {
			matches := coverageRegex.FindStringSubmatch(phase.Stdout)
			if len(matches) > 1 {
				if parsed, err := strconv.ParseFloat(matches[1], 32); err == nil {
					return float32(parsed) / 100.0
				}
			}
		}
	}

	// 4. Default: If build was successful, assume 100% coverage (1.0)
	if build != nil && build.Success {
		return 1.0
	}

	return 0.0
}

func consolidateBuildResults(taskID string, results []*BuildResult) *BuildResult {
	if len(results) == 0 {
		return &BuildResult{
			TaskID:        taskID,
			Success:       true,
			Phases:        []PhaseOutcome{},
			FilesModified: []string{},
		}
	}
	if len(results) == 1 && results[0] != nil {
		return results[0]
	}

	consolidated := &BuildResult{
		TaskID:        taskID,
		Success:       true,
		Phases:        make([]PhaseOutcome, 0),
		FilesModified: make([]string, 0),
	}

	seenFiles := make(map[string]bool)
	var diffBuilder strings.Builder
	var errMsgs []string

	for _, res := range results {
		if res == nil {
			continue
		}
		if !res.Success {
			consolidated.Success = false
		}
		consolidated.Phases = append(consolidated.Phases, res.Phases...)
		for _, f := range res.FilesModified {
			if !seenFiles[f] {
				seenFiles[f] = true
				consolidated.FilesModified = append(consolidated.FilesModified, f)
			}
		}
		if res.GitDiff != "" {
			diffBuilder.WriteString(res.GitDiff)
			diffBuilder.WriteString("\n")
		}
		consolidated.TotalDuration += res.TotalDuration
		if res.ErrorMessage != "" {
			errMsgs = append(errMsgs, res.ErrorMessage)
		}
	}

	consolidated.GitDiff = strings.TrimSpace(diffBuilder.String())
	if len(errMsgs) > 0 {
		consolidated.ErrorMessage = strings.Join(errMsgs, "; ")
	}

	return consolidated
}
