// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

type orchestratorMockCaller struct {
	mu           sync.Mutex
	delay        time.Duration
	tokensPerCall int
	callCount    map[string]int
}

func newOrchestratorMockCaller(delay time.Duration, tokens int) *orchestratorMockCaller {
	return &orchestratorMockCaller{
		delay:         delay,
		tokensPerCall: tokens,
		callCount:     make(map[string]int),
	}
}

func (m *orchestratorMockCaller) CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	m.mu.Lock()
	m.callCount[req.TaskID]++
	tokens := m.tokensPerCall
	m.mu.Unlock()

	return &pdk.Result{
		Success: true,
		Output:  "completed " + req.TaskID,
		Data: map[string]any{
			"tokens": tokens,
			"task":   req.TaskID,
		},
	}, nil
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()

	execGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v, out: %s", strings.Join(args, " "), err, string(out))
		}
	}

	execGit("init")
	execGit("checkout", "-B", "main")
	execGit("config", "user.email", "agent@gv-factory.local")
	execGit("config", "user.name", "Agent")

	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Factory Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	execGit("add", "README.md")
	execGit("commit", "-m", "init")

	return repoDir
}

func createSampleTemplate(name string) schema.FactoryTemplate {
	return schema.FactoryTemplate{
		Metadata: schema.Metadata{
			Name: name,
		},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{
					ID:     "step-1",
					Name:   "Ingestion",
					Plugin: "extism://plugins/ingestion.wasm",
				},
				{
					ID:        "step-2",
					Name:      "Build",
					Plugin:    "extism://plugins/builder.wasm",
					DependsOn: []string{"step-1"},
				},
			},
		},
	}
}

func TestOrchestrator_ConcurrentMultiFactoryExecution(t *testing.T) {
	repoDir := initTestGitRepo(t)
	worktreeBase := t.TempDir()

	sbMgr := sandbox.NewManager(sandbox.Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
		MaxConcurrent:   5,
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	caller := newOrchestratorMockCaller(20*time.Millisecond, 100)
	engine := NewEngine()

	cfg := OrchestratorConfig{
		MaxConcurrentRuns:    4,
		MaxConcurrentPerRepo: 4,
		DefaultTimeout:       10 * time.Second,
		TelemetryInterval:    50 * time.Millisecond,
	}

	dCfg := DispatcherConfig{
		DefaultStepPolicy: StepPolicy{
			MaxRetries:     1,
			InitialBackoff: 10 * time.Millisecond,
			Timeout:        3 * time.Second,
		},
	}

	orchestrator := NewMultiFactoryOrchestrator(cfg, sbMgr, engine, caller, gw, dCfg)
	defer orchestrator.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const runCount = 3
	runs := make([]*FactoryRun, runCount)
	var err error

	for i := 0; i < runCount; i++ {
		runID := fmt.Sprintf("concurrent-run-%d", i+1)
		branch := fmt.Sprintf("ov-run/feature-%d", i+1)
		tpl := createSampleTemplate(fmt.Sprintf("factory-tpl-%d", i+1))

		runs[i], err = orchestrator.SubmitRun(ctx, RunRequest{
			RunID:    runID,
			Template: tpl,
			RepoPath: repoDir,
			Branch:   branch,
		})
		if err != nil {
			t.Fatalf("failed to submit run %d: %v", i, err)
		}
	}

	// Wait for all runs to finish
	var allDone bool
	for start := time.Now(); time.Since(start) < 15*time.Second; {
		allDone = true
		for _, r := range runs {
			snapshot, err := orchestrator.GetRun(r.ID)
			if err != nil {
				t.Fatalf("failed to get run %s: %v", r.ID, err)
			}
			if snapshot.State != FactoryRunStateCompleted && snapshot.State != FactoryRunStateFailed {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	for _, r := range runs {
		snap, _ := orchestrator.GetRun(r.ID)
		t.Logf("RUN %s: state=%s, progress=%.1f, err=%s", r.ID, snap.State, snap.ProgressPercent, snap.Error)
	}

	if !allDone {
		t.Fatalf("timed out waiting for concurrent runs to finish")
	}

	// Validate results
	for i, r := range runs {
		snapshot, err := orchestrator.GetRun(r.ID)
		if err != nil {
			t.Fatalf("failed to fetch run %s: %v", r.ID, err)
		}
		if snapshot.State != FactoryRunStateCompleted {
			t.Fatalf("expected run %d to complete successfully, got state %s, error: %s", i, snapshot.State, snapshot.Error)
		}
		if snapshot.ProgressPercent != 100.0 {
			t.Fatalf("expected 100%% progress, got %.1f", snapshot.ProgressPercent)
		}
		if snapshot.TokensConsumed != 200 { // 2 steps * 100 tokens
			t.Fatalf("expected 200 tokens consumed, got %d", snapshot.TokensConsumed)
		}
	}

	// Verify deterministic worktree cleanup across all completed runs
	if sbMgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sandboxes after all runs finished, got %d", sbMgr.ActiveCount())
	}
}

func TestOrchestrator_WorktreeIsolationAndDeterministicCleanup(t *testing.T) {
	repoDir := initTestGitRepo(t)
	worktreeBase := t.TempDir()

	sbMgr := sandbox.NewManager(sandbox.Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	// Slow caller to inspect worktree on disk during execution
	caller := newOrchestratorMockCaller(200*time.Millisecond, 50)
	engine := NewEngine()

	cfg := OrchestratorConfig{
		MaxConcurrentRuns: 2,
		DefaultTimeout:    5 * time.Second,
	}

	orchestrator := NewMultiFactoryOrchestrator(cfg, sbMgr, engine, caller, gw, DispatcherConfig{})
	defer orchestrator.Stop()

	runID := "run-isolation-check"
	branch := "ov-run/isolation-branch"

	run, err := orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    runID,
		Template: createSampleTemplate("isolation-tpl"),
		RepoPath: repoDir,
		Branch:   branch,
	})
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait until running and inspect worktree path
	expectedSubpath := filepath.Join("ov-factory", "runs", runID, filepath.FromSlash(branch))
	var capturedWorktree string

	for start := time.Now(); time.Since(start) < 3*time.Second; {
		snap, _ := orchestrator.GetRun(run.ID)
		if snap.State == FactoryRunStateRunning && snap.WorktreePath != "" {
			capturedWorktree = snap.WorktreePath
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if capturedWorktree == "" {
		t.Fatalf("expected running state with allocated worktree path")
	}

	if !strings.Contains(capturedWorktree, expectedSubpath) {
		t.Fatalf("expected worktree path containing %q, got %q", expectedSubpath, capturedWorktree)
	}

	// Verify directory exists on disk during run
	if _, err := os.Stat(capturedWorktree); err != nil {
		t.Fatalf("expected worktree to exist on disk during run: %v", err)
	}

	// Wait for completion
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		snap, _ := orchestrator.GetRun(run.ID)
		if snap.State == FactoryRunStateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Verify directory is deterministically destroyed and cleaned up
	for start := time.Now(); time.Since(start) < 3*time.Second; {
		if _, err := os.Stat(capturedWorktree); os.IsNotExist(err) && sbMgr.ActiveCount() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := os.Stat(capturedWorktree); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be cleaned up after run finished, but it still exists")
	}
	if sbMgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sandboxes, got %d", sbMgr.ActiveCount())
	}
}

func TestOrchestrator_BranchCollisionGuard(t *testing.T) {
	repoDir := initTestGitRepo(t)
	worktreeBase := t.TempDir()

	sbMgr := sandbox.NewManager(sandbox.Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	caller := newOrchestratorMockCaller(300*time.Millisecond, 10)
	engine := NewEngine()

	orchestrator := NewMultiFactoryOrchestrator(OrchestratorConfig{MaxConcurrentRuns: 4}, sbMgr, engine, caller, gw, DispatcherConfig{})
	defer orchestrator.Stop()

	branch := "ov-run/shared-branch"

	// First submission
	_, err := orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "run-col-1",
		Template: createSampleTemplate("tpl-1"),
		RepoPath: repoDir,
		Branch:   branch,
	})
	if err != nil {
		t.Fatalf("unexpected error on first submit: %v", err)
	}

	// Second submission on same branch while first is active
	_, err = orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "run-col-2",
		Template: createSampleTemplate("tpl-2"),
		RepoPath: repoDir,
		Branch:   branch,
	})
	if err == nil {
		t.Fatalf("expected branch conflict error for duplicate branch, got nil")
	}
}

func TestOrchestrator_CancelRun(t *testing.T) {
	repoDir := initTestGitRepo(t)
	worktreeBase := t.TempDir()

	sbMgr := sandbox.NewManager(sandbox.Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	// Very long running caller so we can cancel it mid-run
	caller := newOrchestratorMockCaller(5*time.Second, 10)
	engine := NewEngine()

	orchestrator := NewMultiFactoryOrchestrator(OrchestratorConfig{
		MaxConcurrentRuns: 1, // Serial execution to test queued cancel too
	}, sbMgr, engine, caller, gw, DispatcherConfig{})
	defer orchestrator.Stop()

	// 1. Submit running job
	run1, err := orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "cancel-run-1",
		Template: createSampleTemplate("tpl-long"),
		RepoPath: repoDir,
		Branch:   "ov-run/cancel-b1",
	})
	if err != nil {
		t.Fatalf("submit run1 error: %v", err)
	}

	// 2. Submit queued job
	run2, err := orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "cancel-run-2",
		Template: createSampleTemplate("tpl-queued"),
		RepoPath: repoDir,
		Branch:   "ov-run/cancel-b2",
	})
	if err != nil {
		t.Fatalf("submit run2 error: %v", err)
	}

	// Cancel queued job immediately
	if err := orchestrator.CancelRun(run2.ID); err != nil {
		t.Fatalf("failed to cancel queued run: %v", err)
	}

	snap2, _ := orchestrator.GetRun(run2.ID)
	if snap2.State != FactoryRunStateCancelled {
		t.Fatalf("expected run2 to be CANCELLED, got %s", snap2.State)
	}

	// Wait until run1 starts running
	for start := time.Now(); time.Since(start) < 3*time.Second; {
		snap1, _ := orchestrator.GetRun(run1.ID)
		if snap1.State == FactoryRunStateRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel running job
	if err := orchestrator.CancelRun(run1.ID); err != nil {
		t.Fatalf("failed to cancel running run: %v", err)
	}

	// Wait for cleanup
	for start := time.Now(); time.Since(start) < 3*time.Second; {
		snap1, _ := orchestrator.GetRun(run1.ID)
		if snap1.State == FactoryRunStateCancelled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	snap1, _ := orchestrator.GetRun(run1.ID)
	if snap1.State != FactoryRunStateCancelled {
		t.Fatalf("expected run1 to be CANCELLED, got %s", snap1.State)
	}

	// Sandbox must be cleaned up
	if sbMgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 active sandboxes after cancel, got %d", sbMgr.ActiveCount())
	}
}

func TestOrchestrator_PrioritySchedulingDispatchesHighFirst(t *testing.T) {
	repoDir := initTestGitRepo(t)
	worktreeBase := t.TempDir()

	sbMgr := sandbox.NewManager(sandbox.Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	caller := newOrchestratorMockCaller(100*time.Millisecond, 20)
	engine := NewEngine()

	// MaxConcurrentRuns = 1 to enforce queueing
	cfg := OrchestratorConfig{
		MaxConcurrentRuns: 1,
		DefaultTimeout:    10 * time.Second,
	}

	orchestrator := NewMultiFactoryOrchestrator(cfg, sbMgr, engine, caller, gw, DispatcherConfig{})
	defer orchestrator.Stop()

	// Track start times of each run
	var dispatchOrderMu sync.Mutex
	var dispatchOrder []string

	eventsCh := make(chan gateway.Event, 50)
	unsub, _ := gw.SubscribeTaskEvents(context.Background(), gateway.TopicRunStarted, eventsCh)
	defer unsub()

	go func() {
		for ev := range eventsCh {
			dispatchOrderMu.Lock()
			dispatchOrder = append(dispatchOrder, ev.RunID)
			dispatchOrderMu.Unlock()
		}
	}()

	// 1. Submit initial run that occupies the only available slot
	_, err := orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "run-initial",
		Template: createSampleTemplate("initial"),
		RepoPath: repoDir,
		Branch:   "ov-run/init-b",
		Priority: PriorityNormal,
	})
	if err != nil {
		t.Fatalf("submit initial error: %v", err)
	}

	// Small pause so initial starts
	time.Sleep(30 * time.Millisecond)

	// 2. Submit low priority run (e.g. refactor-doc) -> queued
	_, err = orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "run-low",
		Template: createSampleTemplate("refactor-doc"),
		RepoPath: repoDir,
		Branch:   "ov-run/low-b",
		Priority: PriorityLow,
	})
	if err != nil {
		t.Fatalf("submit low error: %v", err)
	}

	// 3. Submit high priority run (e.g. bugfix-express) -> queued
	_, err = orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    "run-high",
		Template: createSampleTemplate("bugfix-express"),
		RepoPath: repoDir,
		Branch:   "ov-run/high-b",
		Priority: PriorityHigh,
	})
	if err != nil {
		t.Fatalf("submit high error: %v", err)
	}

	// Wait for all 3 to finish
	for start := time.Now(); time.Since(start) < 10*time.Second; {
		snapLow, _ := orchestrator.GetRun("run-low")
		snapHigh, _ := orchestrator.GetRun("run-high")
		if snapLow != nil && snapHigh != nil &&
			snapLow.State == FactoryRunStateCompleted &&
			snapHigh.State == FactoryRunStateCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	dispatchOrderMu.Lock()
	order := make([]string, len(dispatchOrder))
	copy(order, dispatchOrder)
	dispatchOrderMu.Unlock()

	if len(order) < 3 {
		t.Fatalf("expected at least 3 dispatched runs, got %d (%v)", len(order), order)
	}

	// First dispatched is run-initial
	if order[0] != "run-initial" {
		t.Errorf("expected run-initial first, got %s", order[0])
	}
	// Second dispatched MUST be run-high ahead of run-low, despite run-low being enqueued earlier!
	if order[1] != "run-high" {
		t.Errorf("expected high-priority run-high dispatched before run-low, got %s", order[1])
	}
	// Third dispatched is run-low
	if order[2] != "run-low" {
		t.Errorf("expected run-low dispatched third, got %s", order[2])
	}
}

func TestOrchestrator_TelemetryStream(t *testing.T) {
	repoDir := initTestGitRepo(t)
	worktreeBase := t.TempDir()

	sbMgr := sandbox.NewManager(sandbox.Config{
		RepoPath:        repoDir,
		BaseWorktreeDir: worktreeBase,
		BaseBranch:      "main",
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	caller := newOrchestratorMockCaller(50*time.Millisecond, 75)
	engine := NewEngine()

	cfg := OrchestratorConfig{
		MaxConcurrentRuns: 2,
		TelemetryInterval: 30 * time.Millisecond,
		DefaultTimeout:    5 * time.Second,
	}

	orchestrator := NewMultiFactoryOrchestrator(cfg, sbMgr, engine, caller, gw, DispatcherConfig{})
	defer orchestrator.Stop()

	telemetryCh := make(chan gateway.Event, 50)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), gateway.TopicFactoryMetrics, telemetryCh)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer unsub()

	// Submit a run
	runID := "telemetry-run"
	_, err = orchestrator.SubmitRun(context.Background(), RunRequest{
		RunID:    runID,
		Template: createSampleTemplate("tpl-telem"),
		RepoPath: repoDir,
		Branch:   "ov-run/telem-b",
	})
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for run completion
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		snap, _ := orchestrator.GetRun(runID)
		if snap != nil && snap.State == FactoryRunStateCompleted {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Verify telemetry events received
	var receivedMetrics bool
	for {
		select {
		case ev := <-telemetryCh:
			receivedMetrics = true
			if ev.Type != gateway.TopicFactoryMetrics {
				t.Errorf("expected topic %s, got %s", gateway.TopicFactoryMetrics, ev.Type)
			}
			payload := ev.Payload
			if payload == nil {
				t.Fatalf("expected non-nil telemetry payload")
			}
			if _, ok := payload["completed_factories"]; !ok {
				t.Errorf("payload missing completed_factories: %v", payload)
			}
		default:
			goto CheckReceived
		}
	}

CheckReceived:
	if !receivedMetrics {
		t.Fatalf("expected to receive telemetry metrics events over gateway")
	}

	// Check final aggregate metrics from orchestrator
	metrics := orchestrator.GetMetrics()
	if metrics.CompletedFactories < 1 {
		t.Errorf("expected at least 1 completed factory, got %d", metrics.CompletedFactories)
	}
	if metrics.TotalTokensConsumed != 150 { // 2 steps * 75 tokens
		t.Errorf("expected 150 total tokens consumed, got %d", metrics.TotalTokensConsumed)
	}
}



