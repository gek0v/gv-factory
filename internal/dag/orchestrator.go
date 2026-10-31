// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

var (
	// ErrBranchConflict is returned when attempting to run on an already active branch.
	ErrBranchConflict = errors.New("branch is already in use by another active run")
	// ErrMaxRunsExceeded is returned when orchestrator limits are exceeded and queue is full.
	ErrMaxRunsExceeded = errors.New("maximum concurrent runs limit exceeded")
)

// FactoryRunState represents the lifecycle status of a factory run.
type FactoryRunState string

const (
	FactoryRunStateQueued    FactoryRunState = "QUEUED"
	FactoryRunStateRunning   FactoryRunState = "RUNNING"
	FactoryRunStateCompleted FactoryRunState = "COMPLETED"
	FactoryRunStateFailed    FactoryRunState = "FAILED"
	FactoryRunStateCancelled FactoryRunState = "CANCELLED"
)

// RunRequest defines parameters for initiating a factory pipeline run.
type RunRequest struct {
	RunID       string                 `json:"run_id"`
	Template    schema.FactoryTemplate `json:"template"`
	RepoPath    string                 `json:"repo_path"`
	Branch      string                 `json:"branch"`
	BaseBranch  string                 `json:"base_branch,omitempty"`
	Priority    Priority               `json:"priority,omitempty"`
	MaxMemoryMB int                    `json:"max_memory_mb,omitempty"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

// FactoryRun holds runtime state, metrics, progress and outcomes for a single pipeline run.
type FactoryRun struct {
	ID                string            `json:"id"`
	TemplateName      string            `json:"template_name"`
	Branch            string            `json:"branch"`
	BaseBranch        string            `json:"base_branch"`
	RepoPath          string            `json:"repo_path"`
	Priority          Priority          `json:"priority"`
	EffectivePriority int               `json:"effective_priority"`
	State             FactoryRunState   `json:"state"`
	ProgressPercent   float64           `json:"progress_percent"`
	TokensConsumed    int64             `json:"tokens_consumed"`
	CurrentStep       string            `json:"current_step,omitempty"`
	CompletedSteps    []string          `json:"completed_steps,omitempty"`
	FailedSteps       []string          `json:"failed_steps,omitempty"`
	TotalSteps        int               `json:"total_steps"`
	WorktreePath      string            `json:"worktree_path,omitempty"`
	QueuedAt          time.Time         `json:"queued_at"`
	StartedAt         *time.Time        `json:"started_at,omitempty"`
	CompletedAt       *time.Time        `json:"completed_at,omitempty"`
	Error             string            `json:"error,omitempty"`
	Report            *WorkflowReport   `json:"report,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`

	cancel      context.CancelFunc
	isCancelled bool
}

// GlobalMetrics captures real-time aggregate health and execution telemetry.
type GlobalMetrics struct {
	ActiveFactories        int                `json:"active_factories"`
	QueuedFactories        int                `json:"queued_factories"`
	ActiveSandboxes        int                `json:"active_sandboxes"`
	CompletedFactories     int                `json:"completed_factories"`
	FailedFactories        int                `json:"failed_factories"`
	CancelledFactories     int                `json:"cancelled_factories"`
	TotalTokensConsumed    int64              `json:"total_tokens_consumed"`
	AverageProgressPercent float64            `json:"average_progress_percent"`
	RunProgressPercentages map[string]float64 `json:"run_progress_percentages"`
	Timestamp              int64              `json:"timestamp"`
}

// OrchestratorConfig configures limits, scheduling, and telemetry broadcast.
type OrchestratorConfig struct {
	MaxConcurrentRuns    int                 `json:"max_concurrent_runs"`
	MaxConcurrentPerRepo int                 `json:"max_concurrent_per_repo"`
	DefaultTimeout       time.Duration       `json:"default_timeout"`
	TelemetryInterval    time.Duration       `json:"telemetry_interval"`
	PriorityQueueConfig  PriorityQueueConfig `json:"priority_queue_config"`
}

// MultiFactoryOrchestrator coordinates simultaneous pipelines with dedicated worktrees and priority queues.
type MultiFactoryOrchestrator struct {
	mu              sync.RWMutex
	cfg             OrchestratorConfig
	sandboxMgr      *sandbox.Manager
	engine          *Engine
	caller          PluginCaller
	gateway         EventPublisher
	dispatcherCfg   DispatcherConfig
	queue           *PriorityQueue
	registry        map[string]*FactoryRun
	requests        map[string]RunRequest
	activeBranches  map[string]string // branch -> runID
	repoActiveCount map[string]int    // repoPath -> active runs
	totalTokens     int64
	completedCount  int
	failedCount     int
	cancelledCount  int
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	scheduleNotify  chan struct{}
}

// NewMultiFactoryOrchestrator creates and initializes a multi-factory orchestrator instance.
func NewMultiFactoryOrchestrator(
	cfg OrchestratorConfig,
	sbMgr *sandbox.Manager,
	engine *Engine,
	caller PluginCaller,
	gw EventPublisher,
	dCfg DispatcherConfig,
) *MultiFactoryOrchestrator {
	if cfg.MaxConcurrentRuns <= 0 {
		cfg.MaxConcurrentRuns = 4
	}
	if cfg.MaxConcurrentPerRepo <= 0 {
		cfg.MaxConcurrentPerRepo = 2
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Minute
	}
	if cfg.TelemetryInterval <= 0 {
		cfg.TelemetryInterval = 500 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())

	pq := NewPriorityQueue(cfg.PriorityQueueConfig)

	o := &MultiFactoryOrchestrator{
		cfg:             cfg,
		sandboxMgr:      sbMgr,
		engine:          engine,
		caller:          caller,
		gateway:         gw,
		dispatcherCfg:   dCfg,
		queue:           pq,
		registry:        make(map[string]*FactoryRun),
		requests:        make(map[string]RunRequest),
		activeBranches:  make(map[string]string),
		repoActiveCount: make(map[string]int),
		ctx:             ctx,
		cancel:          cancel,
		scheduleNotify:  make(chan struct{}, 100),
	}

	o.wg.Add(1)
	go o.schedulerLoop()

	return o
}

// Stop gracefully stops the orchestrator and waits for active routines to finish.
func (o *MultiFactoryOrchestrator) Stop() {
	o.cancel()
	o.mu.Lock()
	for _, run := range o.registry {
		if run.State == FactoryRunStateRunning && run.cancel != nil {
			run.cancel()
		}
	}
	o.mu.Unlock()
	o.wg.Wait()
}

// SubmitRun enqueues a new factory pipeline execution.
func (o *MultiFactoryOrchestrator) SubmitRun(ctx context.Context, req RunRequest) (*FactoryRun, error) {
	if req.RunID == "" {
		return nil, fmt.Errorf("run ID cannot be empty")
	}
	if req.Template.Metadata.Name == "" {
		return nil, fmt.Errorf("template name cannot be empty")
	}
	if req.RepoPath == "" {
		req.RepoPath = "."
	}
	if req.Branch == "" {
		req.Branch = fmt.Sprintf("ov-run/%s", req.RunID)
	}
	if req.Priority == 0 {
		req.Priority = TemplateDefaultPriority(req.Template.Metadata.Name)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.registry[req.RunID]; exists {
		return nil, fmt.Errorf("run %q already exists", req.RunID)
	}

	for _, r := range o.registry {
		if (r.State == FactoryRunStateQueued || r.State == FactoryRunStateRunning) && r.Branch == req.Branch {
			return nil, fmt.Errorf("%w: branch %q is already in use by run %q (state: %s)", ErrBranchConflict, req.Branch, r.ID, r.State)
		}
	}

	now := time.Now()
	run := &FactoryRun{
		ID:                req.RunID,
		TemplateName:      req.Template.Metadata.Name,
		Branch:            req.Branch,
		BaseBranch:        req.BaseBranch,
		RepoPath:          req.RepoPath,
		Priority:          req.Priority,
		EffectivePriority: int(req.Priority),
		State:             FactoryRunStateQueued,
		TotalSteps:        len(req.Template.Spec.Steps),
		QueuedAt:          now,
		Labels:            req.Labels,
	}

	o.registry[req.RunID] = run
	o.requests[req.RunID] = req

	o.queue.Push(&QueueItem{
		RunID:        req.RunID,
		TemplateName: req.Template.Metadata.Name,
		Branch:       req.Branch,
		RepoPath:     req.RepoPath,
		BasePriority: req.Priority,
		QueuedAt:     now,
	})

	_ = o.publishEvent(ctx, gateway.TopicRunQueued, req.RunID, map[string]any{
		"template": req.Template.Metadata.Name,
		"branch":   req.Branch,
		"priority": req.Priority.String(),
	})

	select {
	case o.scheduleNotify <- struct{}{}:
	default:
	}

	return run, nil
}

func (o *MultiFactoryOrchestrator) schedulerLoop() {
	defer o.wg.Done()
	ticker := time.NewTicker(o.cfg.TelemetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-o.scheduleNotify:
			o.dispatchPendingRuns()
		case <-ticker.C:
			o.dispatchPendingRuns()
			_ = o.BroadcastTelemetry(o.ctx)
		}
	}
}

func (o *MultiFactoryOrchestrator) dispatchPendingRuns() {
	o.mu.Lock()
	defer o.mu.Unlock()

	var activeCount int
	for _, r := range o.registry {
		if r.State == FactoryRunStateRunning {
			activeCount++
		}
	}

	availableSlots := o.cfg.MaxConcurrentRuns - activeCount
	if availableSlots <= 0 || o.queue.Len() == 0 {
		return
	}

	var deferred []*QueueItem

	for availableSlots > 0 && o.queue.Len() > 0 {
		item := o.queue.Pop()
		if item == nil {
			break
		}

		run, exists := o.registry[item.RunID]
		if !exists || run.State != FactoryRunStateQueued {
			continue
		}

		req := o.requests[item.RunID]

		// Check max concurrent per repository limit
		if o.cfg.MaxConcurrentPerRepo > 0 && o.repoActiveCount[req.RepoPath] >= o.cfg.MaxConcurrentPerRepo {
			deferred = append(deferred, item)
			continue
		}

		// Check branch collision
		if _, active := o.activeBranches[req.Branch]; active {
			deferred = append(deferred, item)
			continue
		}

		// Eligible to run
		run.State = FactoryRunStateRunning
		now := time.Now()
		run.StartedAt = &now
		o.activeBranches[req.Branch] = run.ID
		o.repoActiveCount[req.RepoPath]++
		availableSlots--

		// Starvation prevention: notify queue of dispatch
		o.queue.NotifyDispatch(item.RunID)

		o.wg.Add(1)
		go o.executeRun(run.ID, req)
	}

	for _, it := range deferred {
		o.queue.Push(it)
	}
}

func (o *MultiFactoryOrchestrator) executeRun(runID string, req RunRequest) {
	defer o.wg.Done()

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = o.cfg.DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(o.ctx, timeout)

	o.mu.Lock()
	run, ok := o.registry[runID]
	if !ok {
		o.mu.Unlock()
		cancel()
		return
	}
	run.cancel = cancel
	o.mu.Unlock()

	var cleanedUp bool
	cleanupOnce := func() {
		if !cleanedUp {
			cleanedUp = true
			cancel()
			o.cleanupRun(runID, req)
		}
	}
	defer cleanupOnce()

	var worktreePath string
	if o.sandboxMgr != nil {
		env, err := o.sandboxMgr.ProvisionWorktreeWithOptions(runCtx, runID, sandbox.WorktreeOptions{
			Branch:      req.Branch,
			BaseBranch:  req.BaseBranch,
			MultiTenant: true,
		})
		if err != nil {
			o.mu.Lock()
			now := time.Now()
			run.CompletedAt = &now
			if run.isCancelled || errors.Is(err, context.Canceled) || runCtx.Err() == context.Canceled {
				run.State = FactoryRunStateCancelled
				o.cancelledCount++
				o.mu.Unlock()
				_ = o.publishEvent(context.Background(), gateway.TopicRunCancelled, runID, map[string]any{"reason": "cancelled during provisioning"})
			} else {
				run.State = FactoryRunStateFailed
				run.Error = fmt.Sprintf("worktree provisioning failed: %v", err)
				o.failedCount++
				o.mu.Unlock()
				_ = o.publishEvent(context.Background(), gateway.TopicRunFailed, runID, map[string]any{"error": run.Error})
			}
			return
		}
		worktreePath = env.WorktreePath
		o.mu.Lock()
		run.WorktreePath = worktreePath
		o.mu.Unlock()
	}

	_ = o.publishEvent(runCtx, gateway.TopicRunStarted, runID, map[string]any{
		"template":      req.Template.Metadata.Name,
		"branch":        req.Branch,
		"worktree_path": worktreePath,
		"priority":      run.Priority.String(),
	})

	report, execErr := o.runPipelineSteps(runCtx, runID, req, worktreePath)

	o.mu.Lock()
	now := time.Now()
	run.CompletedAt = &now
	run.Report = report
	if run.isCancelled || errors.Is(execErr, context.Canceled) || errors.Is(o.ctx.Err(), context.Canceled) {
		run.State = FactoryRunStateCancelled
		o.cancelledCount++
	} else if execErr != nil {
		run.State = FactoryRunStateFailed
		run.Error = execErr.Error()
		o.failedCount++
	} else if report != nil && report.Status == StatusCompleted {
		run.State = FactoryRunStateCompleted
		run.ProgressPercent = 100.0
		o.completedCount++
	} else {
		run.State = FactoryRunStateFailed
		if report != nil && len(report.FailedSteps) > 0 {
			run.Error = fmt.Sprintf("pipeline steps failed: %v", report.FailedSteps)
		}
		o.failedCount++
	}
	finalState := run.State
	tokensConsumed := run.TokensConsumed
	finalError := run.Error
	o.mu.Unlock()

	cleanupOnce()

	if finalState == FactoryRunStateCompleted {
		_ = o.publishEvent(context.Background(), gateway.TopicRunCompleted, runID, map[string]any{
			"report":          report,
			"tokens_consumed": tokensConsumed,
		})
	} else if finalState == FactoryRunStateCancelled {
		_ = o.publishEvent(context.Background(), gateway.TopicRunCancelled, runID, map[string]any{"reason": "cancelled"})
	} else {
		_ = o.publishEvent(context.Background(), gateway.TopicRunFailed, runID, map[string]any{"error": finalError})
	}
}

func (o *MultiFactoryOrchestrator) cleanupRun(runID string, req RunRequest) {
	if o.sandboxMgr != nil {
		_ = o.sandboxMgr.Destroy(context.Background(), runID)
	}

	o.mu.Lock()
	delete(o.activeBranches, req.Branch)
	o.repoActiveCount[req.RepoPath]--
	if o.repoActiveCount[req.RepoPath] <= 0 {
		delete(o.repoActiveCount, req.RepoPath)
	}
	o.mu.Unlock()

	select {
	case o.scheduleNotify <- struct{}{}:
	default:
	}

	_ = o.BroadcastTelemetry(context.Background())
}

type stepTelemetryPublisher struct {
	orchestrator *MultiFactoryOrchestrator
	runID        string
	underlying   EventPublisher
}

func (p *stepTelemetryPublisher) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	p.orchestrator.onTaskEvent(ev)
	if p.underlying != nil {
		return p.underlying.PublishTaskEvent(ctx, ev)
	}
	return nil
}

func (o *MultiFactoryOrchestrator) runPipelineSteps(
	ctx context.Context,
	runID string,
	req RunRequest,
	worktreePath string,
) (*WorkflowReport, error) {
	pub := &stepTelemetryPublisher{
		orchestrator: o,
		runID:        runID,
		underlying:   o.gateway,
	}

	dispatcher := NewDispatcher(o.engine, o.caller, pub, o.dispatcherCfg)
	return dispatcher.ExecuteWorkflow(ctx, runID, req.Template)
}

func (o *MultiFactoryOrchestrator) onTaskEvent(ev gateway.Event) {
	o.mu.Lock()
	defer o.mu.Unlock()

	run, exists := o.registry[ev.RunID]
	if !exists {
		return
	}

	switch ev.Type {
	case gateway.TopicTaskStarted:
		run.CurrentStep = ev.TaskID
	case gateway.TopicTaskCompleted:
		run.CompletedSteps = append(run.CompletedSteps, ev.TaskID)
		if run.TotalSteps > 0 {
			run.ProgressPercent = (float64(len(run.CompletedSteps)) / float64(run.TotalSteps)) * 100.0
		}
		if data, ok := ev.Payload["data"].(map[string]any); ok {
			var tokens int64
			if t, ok := data["tokens"].(int); ok {
				tokens = int64(t)
			} else if t, ok := data["tokens"].(float64); ok {
				tokens = int64(t)
			} else if t, ok := data["tokens"].(int64); ok {
				tokens = t
			} else if t, ok := data["token_count"].(int); ok {
				tokens = int64(t)
			} else if t, ok := data["token_count"].(float64); ok {
				tokens = int64(t)
			} else if t, ok := data["token_count"].(int64); ok {
				tokens = t
			}
			if tokens > 0 {
				run.TokensConsumed += tokens
				o.totalTokens += tokens
			}
		}
	case gateway.TopicTaskFailed:
		run.FailedSteps = append(run.FailedSteps, ev.TaskID)
	}
}


func (o *MultiFactoryOrchestrator) publishEvent(ctx context.Context, topic string, runID string, payload map[string]any) error {
	if o.gateway == nil {
		return nil
	}
	return o.gateway.PublishTaskEvent(ctx, gateway.Event{
		Type:      topic,
		RunID:     runID,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
	})
}

// GetRun returns a snapshot of the requested run by ID.
func (o *MultiFactoryOrchestrator) GetRun(runID string) (*FactoryRun, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	run, ok := o.registry[runID]
	if !ok {
		return nil, ErrRunNotFound
	}

	copied := *run
	return &copied, nil
}

// ListRuns returns all runs tracked by the orchestrator.
func (o *MultiFactoryOrchestrator) ListRuns() []*FactoryRun {
	o.mu.RLock()
	defer o.mu.RUnlock()

	out := make([]*FactoryRun, 0, len(o.registry))
	for _, run := range o.registry {
		copied := *run
		out = append(out, &copied)
	}
	return out
}

// CancelRun aborts a queued or running pipeline.
func (o *MultiFactoryOrchestrator) CancelRun(runID string) error {
	o.mu.Lock()
	run, exists := o.registry[runID]
	if !exists {
		o.mu.Unlock()
		return ErrRunNotFound
	}

	if run.State == FactoryRunStateQueued {
		_ = o.queue.Remove(runID)
		run.State = FactoryRunStateCancelled
		now := time.Now()
		run.CompletedAt = &now
		o.cancelledCount++
		o.mu.Unlock()
		_ = o.publishEvent(context.Background(), gateway.TopicRunCancelled, runID, map[string]any{"reason": "cancelled in queue"})
		_ = o.BroadcastTelemetry(context.Background())
		return nil
	}

	if run.State == FactoryRunStateRunning {
		run.isCancelled = true
		cancel := run.cancel
		o.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return nil
	}

	o.mu.Unlock()
	return fmt.Errorf("run %q cannot be cancelled in state %s", runID, run.State)
}

// GetMetrics aggregates and returns current multi-factory telemetry metrics.
func (o *MultiFactoryOrchestrator) GetMetrics() GlobalMetrics {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var activeCount, queuedCount int
	var totalPct float64
	runPct := make(map[string]float64)

	for id, r := range o.registry {
		switch r.State {
		case FactoryRunStateRunning:
			activeCount++
			totalPct += r.ProgressPercent
			runPct[id] = r.ProgressPercent
		case FactoryRunStateQueued:
			queuedCount++
		}
	}

	var avgPct float64
	if activeCount > 0 {
		avgPct = totalPct / float64(activeCount)
	}

	activeSandboxes := 0
	if o.sandboxMgr != nil {
		activeSandboxes = o.sandboxMgr.ActiveCount()
	}

	return GlobalMetrics{
		ActiveFactories:        activeCount,
		QueuedFactories:        queuedCount,
		ActiveSandboxes:        activeSandboxes,
		CompletedFactories:     o.completedCount,
		FailedFactories:        o.failedCount,
		CancelledFactories:     o.cancelledCount,
		TotalTokensConsumed:    o.totalTokens,
		AverageProgressPercent: avgPct,
		RunProgressPercentages: runPct,
		Timestamp:              time.Now().Unix(),
	}
}

// BroadcastTelemetry publishes aggregate metrics to the gateway topic.
func (o *MultiFactoryOrchestrator) BroadcastTelemetry(ctx context.Context) error {
	if o.gateway == nil {
		return nil
	}
	metrics := o.GetMetrics()
	payload := map[string]any{
		"active_factories":         metrics.ActiveFactories,
		"queued_factories":         metrics.QueuedFactories,
		"active_sandboxes":         metrics.ActiveSandboxes,
		"completed_factories":      metrics.CompletedFactories,
		"failed_factories":         metrics.FailedFactories,
		"cancelled_factories":      metrics.CancelledFactories,
		"total_tokens_consumed":    metrics.TotalTokensConsumed,
		"average_progress_percent": metrics.AverageProgressPercent,
		"run_progress":             metrics.RunProgressPercentages,
	}
	return o.gateway.PublishTaskEvent(ctx, gateway.Event{
		Type:      gateway.TopicFactoryMetrics,
		Payload:   payload,
		Timestamp: metrics.Timestamp,
	})
}

// ActiveCount returns count of active running factories.
func (o *MultiFactoryOrchestrator) ActiveCount() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	var count int
	for _, r := range o.registry {
		if r.State == FactoryRunStateRunning {
			count++
		}
	}
	return count
}

// QueuedCount returns count of queued factories.
func (o *MultiFactoryOrchestrator) QueuedCount() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.queue.Len()
}


