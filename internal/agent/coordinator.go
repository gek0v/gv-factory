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

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/sandbox/merger"
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
	EventMergeEnqueued     = "merge_enqueued"
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

// EpisodeRecorder defines the contract for persisting task execution learnings into the Brain.
type EpisodeRecorder interface {
	RecordEpisode(ctx context.Context, ep brain.TaskEpisode) (*brain.TaskEpisode, error)
}

// MergeQueue abstracts enqueuing approved factory outputs into the sequential merge queue.
type MergeQueue interface {
	Enqueue(ctx context.Context, req merger.MergeRequest) error
}

// CoordinatorConfig configures execution thresholds, agents, and telemetry for the pipeline.
type CoordinatorConfig struct {
	SpecWriter         SpecGenerator
	Builder            TaskBuilder
	Approver           GateApprover
	Brain              EpisodeRecorder
	MergeQueue         MergeQueue
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
	MergeStatus     string            `json:"merge_status,omitempty"`
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
	brain              EpisodeRecorder
	mergeQueue         MergeQueue
	maxRetries         int
	minCoverage        float32
	eventHook          func(stage string, payload any)
	coverageCalculator func(ctx context.Context, build *BuildResult, task TaskContext) float32
	mu                 sync.RWMutex
}

// PipelineOption configures runtime parameters for a pipeline execution.
type PipelineOption func(*pipelineRunOptions)

type pipelineRunOptions struct {
	mergeQueue MergeQueue
}

// WithMergeQueue provides or overrides the MergeQueue for a specific pipeline execution.
func WithMergeQueue(mq MergeQueue) PipelineOption {
	return func(opts *pipelineRunOptions) {
		opts.mergeQueue = mq
	}
}

// NewFactoryCoordinator constructs a new FactoryCoordinator with normalized config.
func NewFactoryCoordinator(cfg CoordinatorConfig) *FactoryCoordinator {
	cfg.Normalize()
	return &FactoryCoordinator{
		specWriter:         cfg.SpecWriter,
		builder:            cfg.Builder,
		approver:           cfg.Approver,
		brain:              cfg.Brain,
		mergeQueue:         cfg.MergeQueue,
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
func (c *FactoryCoordinator) RunPipeline(ctx context.Context, task TaskContext, opts ...PipelineOption) (*PipelineOutcome, error) {
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
	activeMergeQueue := c.mergeQueue
	c.mu.RUnlock()

	runOpts := pipelineRunOptions{
		mergeQueue: activeMergeQueue,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&runOpts)
		}
	}

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
	var retryFeedbackHistory []string

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
		c.recordPipelineEpisode(ctx, task, outcome, retryFeedbackHistory)
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
			c.recordPipelineEpisode(ctx, task, outcome, retryFeedbackHistory)
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
			c.recordPipelineEpisode(ctx, task, outcome, retryFeedbackHistory)
			return outcome, err
		}
		lastApproval = approval
		c.emitEvent(EventReviewDecided, approval)

		// Stage 4: Feedback Loop (when DecisionRetry)
		if approval.IsRetry() && attempt < maxRetries {
			feedbackMsg := fmt.Sprintf("Reviewer feedback (retry %d/%d): %s", attempt+1, maxRetries, approval.FeedbackSummary)
			task.Constraints = append(task.Constraints, feedbackMsg)

			findingMsgs := []string{approval.FeedbackSummary}
			for _, res := range approval.ReviewerResults {
				for _, f := range res.Findings {
					findingMsgs = append(findingMsgs, fmt.Sprintf("[%s] %s: %s", f.Severity, f.RuleCode, f.Message))
				}
			}
			retryFeedbackHistory = append(retryFeedbackHistory, strings.Join(findingMsgs, " - "))

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

	// Automatic Merge Queue Integration upon DecisionApproved
	var enqueueErr error
	if outcome.Decision == DecisionApproved && runOpts.mergeQueue != nil {
		branch := fmt.Sprintf("ov-run/%s", task.TaskID)
		if b, ok := task.Metadata["source_branch"].(string); ok && strings.TrimSpace(b) != "" {
			branch = strings.TrimSpace(b)
		} else if b, ok := task.Metadata["branch"].(string); ok && strings.TrimSpace(b) != "" {
			branch = strings.TrimSpace(b)
		}

		targetBranch := "main"
		if tb, ok := task.Metadata["target_branch"].(string); ok && strings.TrimSpace(tb) != "" {
			targetBranch = strings.TrimSpace(tb)
		}

		mReq := merger.MergeRequest{
			RunID:        task.TaskID,
			SourceBranch: branch,
			TargetBranch: targetBranch,
			Metadata:     task.Metadata,
		}

		if err := runOpts.mergeQueue.Enqueue(ctx, mReq); err != nil {
			outcome.MergeStatus = "enqueue_failed"
			outcome.ErrorMessage = fmt.Sprintf("failed to enqueue to merge queue: %v", err)
			enqueueErr = fmt.Errorf("failed to enqueue to merge queue: %w", err)
		} else {
			outcome.MergeStatus = "enqueued"
			c.emitEvent(EventMergeEnqueued, map[string]any{
				"run_id":        task.TaskID,
				"source_branch": branch,
				"target_branch": targetBranch,
			})
		}
	}

	c.recordPipelineEpisode(ctx, task, outcome, retryFeedbackHistory)
	c.emitEvent(EventPipelineCompleted, outcome)
	if enqueueErr != nil {
		return outcome, enqueueErr
	}
	return outcome, nil
}

func (c *FactoryCoordinator) recordPipelineEpisode(ctx context.Context, task TaskContext, outcome *PipelineOutcome, retryFeedback []string) {
	if c == nil || outcome == nil {
		return
	}
	c.mu.RLock()
	brainRec := c.brain
	c.mu.RUnlock()
	if brainRec == nil {
		return
	}

	var epOutcome string
	var lessons string

	switch outcome.Decision {
	case DecisionApproved:
		if len(retryFeedback) > 0 || outcome.TotalIterations > 1 {
			epOutcome = "retry_resolved"
			if len(retryFeedback) > 0 {
				lessons = fmt.Sprintf("Reviewer findings resolved during retries (%d iterations): %s", outcome.TotalIterations, strings.Join(retryFeedback, "; "))
			} else {
				lessons = fmt.Sprintf("Reviewer findings resolved during retries (%d iterations)", outcome.TotalIterations)
			}
		} else {
			epOutcome = "success"
			lessons = fmt.Sprintf("Pipeline completed successfully on iteration %d with all quality checks passing", outcome.TotalIterations)
		}

	case DecisionRetry:
		epOutcome = "retry_resolved"
		if len(retryFeedback) > 0 {
			lessons = fmt.Sprintf("Reviewer findings: %s", strings.Join(retryFeedback, "; "))
		} else {
			lessons = "Pipeline in retry state"
		}

	case DecisionHumanGate:
		epOutcome = "gate_required"
		var findings []string
		if outcome.Approval != nil {
			if outcome.Approval.GateReason != "" {
				findings = append(findings, "Gate reason: "+outcome.Approval.GateReason)
			}
			if outcome.Approval.FeedbackSummary != "" {
				findings = append(findings, "Feedback: "+outcome.Approval.FeedbackSummary)
			}
			for _, res := range outcome.Approval.ReviewerResults {
				for _, f := range res.Findings {
					findings = append(findings, fmt.Sprintf("[%s] %s: %s", f.Severity, f.RuleCode, f.Message))
				}
			}
		}
		if len(findings) == 0 {
			findings = append(findings, "Human gate intervention required")
		}
		lessons = strings.Join(findings, "; ")

	case DecisionFailed:
		fallthrough
	default:
		epOutcome = "failed"
		var failureCauses []string
		if outcome.ErrorMessage != "" {
			failureCauses = append(failureCauses, outcome.ErrorMessage)
		}
		if outcome.Approval != nil {
			if outcome.Approval.FeedbackSummary != "" {
				failureCauses = append(failureCauses, outcome.Approval.FeedbackSummary)
			}
			for _, res := range outcome.Approval.ReviewerResults {
				for _, f := range res.Findings {
					failureCauses = append(failureCauses, fmt.Sprintf("[%s] %s: %s", f.Severity, f.RuleCode, f.Message))
				}
			}
		}
		if len(failureCauses) == 0 {
			failureCauses = append(failureCauses, "Pipeline execution failed")
		}
		lessons = strings.Join(failureCauses, "; ")
	}

	summary := fmt.Sprintf("Task %s (%s): outcome=%s, decision=%s, iterations=%d, duration=%v",
		task.TaskID, task.Title, epOutcome, outcome.Decision, outcome.TotalIterations, outcome.Duration.Round(time.Millisecond))

	episode := brain.TaskEpisode{
		RunID:          task.TaskID,
		TaskName:       task.Title,
		Outcome:        epOutcome,
		Summary:        summary,
		LessonsLearned: lessons,
	}

	recordCtx := ctx
	if recordCtx == nil || recordCtx.Err() != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	_, _ = brainRec.RecordEpisode(recordCtx, episode)
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
