// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/llm"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/store"
	"github.com/gv-factory/gv-factory/internal/telemetry"
)

// FactoryPipelineRunner defines the contract for executing autonomous factory pipelines.
type FactoryPipelineRunner interface {
	RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error)
}

// FactoryRunRequest represents the payload accepted by POST /api/v1/factory/run.
type FactoryRunRequest struct {
	TaskID      string         `json:"task_id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Model       string         `json:"model,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Constraints []string       `json:"constraints,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// FactoryRunResponse represents the status and outcome returned by factory endpoints.
type FactoryRunResponse struct {
	RunID     string                 `json:"run_id"`
	Status    string                 `json:"status"` // "running", "completed", "failed", "gate_waiting"
	Token     string                 `json:"token,omitempty"`
	Outcome   *agent.PipelineOutcome `json:"outcome,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	UpdatedAt time.Time              `json:"updated_at,omitempty"`
}

// FactoryEndpointsOptions configures optional dependencies for factory endpoints.
type FactoryEndpointsOptions struct {
	AuthSvc        *auth.Service
	Store          store.Store
	MergeQueue     agent.MergeQueue
	Tracker        *FactoryRunTracker
	PendingTracker *PendingGateTracker
	SandboxBaseDir string
}

// FactoryRunTracker provides thread-safe in-memory tracking of factory pipeline runs.
type FactoryRunTracker struct {
	mu   sync.RWMutex
	runs map[string]*FactoryRunResponse
}

// NewFactoryRunTracker constructs an initialized FactoryRunTracker.
func NewFactoryRunTracker() *FactoryRunTracker {
	return &FactoryRunTracker{
		runs: make(map[string]*FactoryRunResponse),
	}
}

// Set stores or updates a run record.
func (t *FactoryRunTracker) Set(resp *FactoryRunResponse) {
	if t == nil || resp == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs[resp.RunID] = resp
}

// Get retrieves a run record by run ID.
func (t *FactoryRunTracker) Get(runID string) (*FactoryRunResponse, bool) {
	if t == nil {
		return nil, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	resp, ok := t.runs[runID]
	if !ok {
		return nil, false
	}
	cp := *resp
	return &cp, true
}

// List returns all tracked run records.
func (t *FactoryRunTracker) List() []*FactoryRunResponse {
	if t == nil {
		return []*FactoryRunResponse{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	list := make([]*FactoryRunResponse, 0, len(t.runs))
	for _, r := range t.runs {
		cp := *r
		list = append(list, &cp)
	}
	return list
}

// SandboxDirProvider allows runner implementations to expose the location of a run's sandbox directory.
type SandboxDirProvider interface {
	SandboxDir(runID string) string
}

// defaultFactoryRunner wires default agent components with Brain, Gateway telemetry, and sequential Merge Queue.
type defaultFactoryRunner struct {
	brainSvc       *brain.Service
	gw             *gateway.Gateway
	mergeQueue     agent.MergeQueue
	costTracker    *telemetry.CostTracker
	sandboxBaseDir string
	specWriter     agent.SpecGenerator
	builder        agent.TaskBuilder
	approver       agent.GateApprover
}

// SandboxDir returns the absolute path to the sandbox directory for task/run ID.
func (d *defaultFactoryRunner) SandboxDir(runID string) string {
	base := d.sandboxBaseDir
	if base == "" {
		base = filepath.Join(os.TempDir(), "ov-sandboxes")
	}
	return filepath.Join(base, sanitizeIdentifier(runID))
}

func runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func initGitSandbox(ctx context.Context, dir string, taskID string, baseBranch string) error {
	if baseBranch == "" {
		baseBranch = "main"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create sandbox directory: %w", err)
	}

	gitDir := filepath.Join(dir, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		if _, err := runGitCmd(ctx, dir, "init"); err != nil {
			return fmt.Errorf("failed to git init sandbox: %w", err)
		}
	}

	if _, err := runGitCmd(ctx, dir, "config", "user.name", "GV Factory Bot"); err != nil {
		return fmt.Errorf("failed to set git user.name: %w", err)
	}
	if _, err := runGitCmd(ctx, dir, "config", "user.email", "bot@gv-factory.local"); err != nil {
		return fmt.Errorf("failed to set git user.email: %w", err)
	}

	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		modContent := fmt.Sprintf("module %s\n\ngo 1.22\n", strings.ToLower(sanitizeIdentifier(taskID)))
		if err := os.WriteFile(goModPath, []byte(modContent), 0644); err != nil {
			return fmt.Errorf("failed to write go.mod: %w", err)
		}
	}

	_, err := runGitCmd(ctx, dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		_, _ = runGitCmd(ctx, dir, "branch", "-M", baseBranch)
		if _, err := runGitCmd(ctx, dir, "add", "go.mod"); err != nil {
			return fmt.Errorf("failed to git add go.mod: %w", err)
		}
		if _, err := runGitCmd(ctx, dir, "commit", "-m", "chore: initial commit with go.mod"); err != nil {
			return fmt.Errorf("failed to create initial git commit: %w", err)
		}
		_, _ = runGitCmd(ctx, dir, "branch", "-M", baseBranch)
	}

	branchName := fmt.Sprintf("ov-run/%s", taskID)
	if _, err := runGitCmd(ctx, dir, "checkout", "-B", branchName); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	return nil
}

func extractGitDiff(ctx context.Context, dir string, baseBranch string) string {
	diff, err := runGitCmd(ctx, dir, "diff", "HEAD~1")
	if err == nil && strings.TrimSpace(diff) != "" {
		return diff
	}
	if baseBranch != "" {
		diff, err = runGitCmd(ctx, dir, "diff", baseBranch)
		if err == nil && strings.TrimSpace(diff) != "" {
			return diff
		}
	}
	diff, _ = runGitCmd(ctx, dir, "diff")
	return diff
}

func commitAndExtractDiff(ctx context.Context, dir, taskID, taskTitle, baseBranch string) string {
	status, err := runGitCmd(ctx, dir, "status", "--porcelain")
	if err == nil && strings.TrimSpace(status) != "" {
		_, _ = runGitCmd(ctx, dir, "add", "-A")
		msg := fmt.Sprintf("feat(%s): implement %s", sanitizeIdentifier(taskID), taskTitle)
		if strings.TrimSpace(taskTitle) == "" {
			msg = fmt.Sprintf("feat(%s): implement atomic tasks", sanitizeIdentifier(taskID))
		}
		_, _ = runGitCmd(ctx, dir, "commit", "-m", msg)
	}

	return extractGitDiff(ctx, dir, baseBranch)
}

type gitSandboxBuilder struct {
	inner      agent.TaskBuilder
	dir        string
	taskID     string
	taskTitle  string
	baseBranch string
}

func (g *gitSandboxBuilder) BuildSpec(ctx context.Context, spec *agent.SpecDocument) ([]*agent.BuildResult, error) {
	results, err := g.inner.BuildSpec(ctx, spec)
	if err != nil {
		return results, err
	}

	diff := commitAndExtractDiff(ctx, g.dir, g.taskID, g.taskTitle, g.baseBranch)
	if diff != "" {
		namesOut, _ := runGitCmd(ctx, g.dir, "diff", "--name-only", "HEAD~1")
		if strings.TrimSpace(namesOut) == "" && g.baseBranch != "" {
			namesOut, _ = runGitCmd(ctx, g.dir, "diff", "--name-only", g.baseBranch)
		}
		var fileNames []string
		if strings.TrimSpace(namesOut) != "" {
			for _, line := range strings.Split(strings.TrimSpace(namesOut), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					fileNames = append(fileNames, line)
				}
			}
		}

		if len(results) == 0 {
			results = append(results, &agent.BuildResult{
				TaskID:        g.taskID,
				Success:       true,
				GitDiff:       diff,
				FilesModified: fileNames,
			})
		} else {
			for _, r := range results {
				if r != nil {
					r.GitDiff = diff
					if len(r.FilesModified) == 0 && len(fileNames) > 0 {
						r.FilesModified = fileNames
					}
				}
			}
		}
	}

	return results, nil
}

func (d *defaultFactoryRunner) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	if err := task.Validate(); err != nil {
		return nil, err
	}

	hook := func(stage string, payload any) {
		broadcastStageEvent(d.gw, task.TaskID, stage, payload)
	}

	llmClient, _ := llm.NewClientFromEnv()

	model := os.Getenv("MODEL")
	if model == "" {
		model = os.Getenv("OPENAI_COMPATIBLE_MODEL")
	}
	if model == "" {
		model = os.Getenv("OPENAI_MODEL")
	}
	if m, ok := task.Metadata["model"].(string); ok && strings.TrimSpace(m) != "" {
		model = strings.TrimSpace(m)
	}

	sandboxBaseDir := d.sandboxBaseDir
	if sandboxBaseDir == "" {
		sandboxBaseDir = filepath.Join(os.TempDir(), "ov-sandboxes")
	}
	sandboxDir := filepath.Join(sandboxBaseDir, sanitizeIdentifier(task.TaskID))

	baseBranch := "main"
	if tb, ok := task.Metadata["target_branch"].(string); ok && strings.TrimSpace(tb) != "" {
		baseBranch = strings.TrimSpace(tb)
	} else if bb, ok := task.Metadata["base_branch"].(string); ok && strings.TrimSpace(bb) != "" {
		baseBranch = strings.TrimSpace(bb)
	}

	if err := initGitSandbox(ctx, sandboxDir, task.TaskID, baseBranch); err != nil {
		return nil, fmt.Errorf("failed to initialize git sandbox: %w", err)
	}

	var brainSource agent.BrainKnowledgeSource
	if d.brainSvc != nil {
		brainSource = d.brainSvc
	}

	specWriter := d.specWriter
	if specWriter == nil {
		specWriter = agent.NewSpecWriter(agent.SpecWriterConfig{Brain: brainSource, LLM: llmClient, Model: model})
	}

	rawBuilder := d.builder
	if rawBuilder == nil {
		rawBuilder = agent.NewBuilder(agent.BuilderConfig{WorkspaceDir: sandboxDir, LLMClient: llmClient, Model: model})
	}
	wrappedBuilder := &gitSandboxBuilder{
		inner:      rawBuilder,
		dir:        sandboxDir,
		taskID:     task.TaskID,
		taskTitle:  task.Title,
		baseBranch: baseBranch,
	}

	approver := d.approver
	if approver == nil {
		approver = agent.NewApprover(nil, agent.ApproverConfig{})
	}

	var brainRecorder agent.EpisodeRecorder
	if d.brainSvc != nil {
		brainRecorder = d.brainSvc
	}

	cfg := agent.CoordinatorConfig{
		SpecWriter:  specWriter,
		Builder:     wrappedBuilder,
		Approver:    approver,
		Brain:       brainRecorder,
		MergeQueue:  d.mergeQueue,
		CostTracker: d.costTracker,
		EventHook:   hook,
	}

	coord := agent.NewFactoryCoordinator(cfg)
	outcome, err := coord.RunPipeline(ctx, task)
	if outcome != nil && outcome.BuildResult != nil && outcome.BuildResult.GitDiff == "" {
		if diff := extractGitDiff(ctx, sandboxDir, baseBranch); diff != "" {
			outcome.BuildResult.GitDiff = diff
		}
	}
	return outcome, err
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	res := b.String()
	if res == "" {
		return "task"
	}
	return res
}

func broadcastStageEvent(gw *gateway.Gateway, runID string, stage string, payload any) {
	if gw == nil {
		return
	}

	payloadMap := toPayloadMap(payload)
	ev := gateway.Event{
		Type:      stage,
		RunID:     runID,
		TaskID:    runID,
		Payload:   payloadMap,
		Timestamp: time.Now().UnixMilli(),
	}

	_ = gw.Publish(context.Background(), stage, ev)
}

func toPayloadMap(payload any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	if m, ok := payload.(map[string]any); ok {
		return m
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return map[string]any{"raw": fmt.Sprintf("%v", payload)}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		return m
	}
	return map[string]any{"data": string(data)}
}

// RegisterFactoryEndpoints mounts factory execution and status endpoints onto mux.
func RegisterFactoryEndpoints(mux *http.ServeMux, gw *gateway.Gateway, runner FactoryPipelineRunner) *FactoryRunTracker {
	return RegisterFactoryEndpointsWithOptions(mux, gw, runner, FactoryEndpointsOptions{})
}

// RegisterFactoryEndpointsWithOptions mounts factory endpoints with optional auth, store, mergeQueue and trackers.
func RegisterFactoryEndpointsWithOptions(
	mux *http.ServeMux,
	gw *gateway.Gateway,
	runner FactoryPipelineRunner,
	opts FactoryEndpointsOptions,
) *FactoryRunTracker {
	tracker := opts.Tracker
	if tracker == nil {
		tracker = NewFactoryRunTracker()
	}
	registerFactoryEndpointsWithTrackerAndOptions(mux, gw, runner, tracker, opts)
	return tracker
}

func registerFactoryEndpointsWithTracker(mux *http.ServeMux, gw *gateway.Gateway, runner FactoryPipelineRunner, tracker *FactoryRunTracker) {
	registerFactoryEndpointsWithTrackerAndOptions(mux, gw, runner, tracker, FactoryEndpointsOptions{Tracker: tracker})
}

func registerFactoryEndpointsWithTrackerAndOptions(
	mux *http.ServeMux,
	gw *gateway.Gateway,
	runner FactoryPipelineRunner,
	tracker *FactoryRunTracker,
	opts FactoryEndpointsOptions,
) {
	if mux == nil {
		return
	}
	if runner == nil {
		runner = &defaultFactoryRunner{gw: gw, mergeQueue: opts.MergeQueue}
	}
	if tracker == nil {
		tracker = NewFactoryRunTracker()
	}

	// POST /api/v1/factory/run
	mux.HandleFunc("/api/v1/factory/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req FactoryRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid JSON payload: " + err.Error(),
			})
			return
		}

		req.TaskID = strings.TrimSpace(req.TaskID)
		req.Title = strings.TrimSpace(req.Title)
		if req.TaskID == "" || req.Title == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "task_id and title are required",
			})
			return
		}

		asyncParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("async")))
		isAsync := asyncParam != "false" && asyncParam != "0"

		taskCtx := agent.TaskContext{
			TaskID:      req.TaskID,
			Title:       req.Title,
			Description: req.Description,
			Labels:      req.Labels,
			Constraints: req.Constraints,
			Metadata:    req.Metadata,
		}
		if taskCtx.Metadata == nil {
			taskCtx.Metadata = make(map[string]any)
		}
		if strings.TrimSpace(req.Model) != "" {
			taskCtx.Metadata["model"] = strings.TrimSpace(req.Model)
		}

		emittedStages := make(map[string]bool)
		var emitMu sync.Mutex
		taskCtx.Metadata["event_hook"] = func(stage string, payload any) {
			emitMu.Lock()
			emittedStages[stage] = true
			emitMu.Unlock()
			broadcastStageEvent(gw, req.TaskID, stage, payload)
		}

		now := time.Now().UTC()
		tracker.Set(&FactoryRunResponse{
			RunID:     req.TaskID,
			Status:    "running",
			CreatedAt: now,
			UpdatedAt: now,
		})

		executePipeline := func(ctx context.Context) (*agent.PipelineOutcome, error) {
			outcome, err := runner.RunPipeline(ctx, taskCtx)
			now := time.Now().UTC()
			status := "completed"
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
				if outcome == nil || !outcome.Success {
					status = "failed"
				}
			}
			token := ""
			diff := ""
			reason := ""
			if outcome != nil {
				if outcome.IsHumanGate() {
					status = "gate_waiting"

					if outcome.BuildResult != nil && outcome.BuildResult.GitDiff != "" {
						diff = outcome.BuildResult.GitDiff
					}

					if outcome.Approval != nil {
						if outcome.Approval.GateReason != "" {
							reason = outcome.Approval.GateReason
						} else if outcome.Approval.FeedbackSummary != "" {
							reason = outcome.Approval.FeedbackSummary
						}
					}
					if reason == "" {
						reason = "Human approval required before merge"
					}

					expiration := now.Add(24 * time.Hour)
					authSvc := opts.AuthSvc
					if authSvc == nil {
						authSvc = auth.NewService("ov-factory-default-auth-secret-key-32b")
					}
					token = authSvc.GenerateApprovalToken(req.TaskID, expiration)

					if opts.Store != nil {
						_ = opts.Store.RecordPendingGate(context.Background(), &store.PendingGate{
							ID:       token,
							RunID:    req.TaskID,
							Token:    token,
							GateType: "HUMAN_GATE",
							ToolName: "factory_pipeline",
							Arguments: map[string]any{
								"diff":       diff,
								"reason":     reason,
								"run_id":     req.TaskID,
								"token":      token,
								"expires_at": expiration.Unix(),
							},
							Status:    store.GateStatusPending,
							CreatedAt: now,
							ExpiresAt: expiration,
						})
					}

					if opts.PendingTracker != nil {
						opts.PendingTracker.RecordGate(PendingGateInfo{
							RunID:           req.TaskID,
							Token:           token,
							TokenHMACPrefix: auth.ExtractTokenPrefix(token),
							Diff:            diff,
							Reason:          reason,
							CreatedAt:       now,
							ExpiresAt:       expiration,
							TimeoutMS:       int64(24 * time.Hour / time.Millisecond),
							ToolName:        "factory_pipeline",
						})
					}

					gateWaitingPayload := map[string]any{
						"token":      token,
						"run_id":     req.TaskID,
						"diff":       diff,
						"reason":     reason,
						"expires_at": expiration.Unix(),
						"status":     "gate_waiting",
					}
					broadcastStageEvent(gw, req.TaskID, "gate_waiting", gateWaitingPayload)

					if gw != nil {
						_ = gw.Publish(context.Background(), gateway.TopicGateApprovalRequired, gateway.Event{
							Type:      gateway.TopicGateApprovalRequired,
							RunID:     req.TaskID,
							TaskID:    req.TaskID,
							Payload:   gateWaitingPayload,
							Timestamp: now.UnixMilli(),
						})
					}
				} else if !outcome.Success {
					status = "failed"
				}
			}

			tracker.Set(&FactoryRunResponse{
				RunID:     req.TaskID,
				Status:    status,
				Token:     token,
				Outcome:   outcome,
				Error:     errMsg,
				UpdatedAt: now,
			})

			// Ensure pipeline_completed is broadcast if not emitted by runner
			emitMu.Lock()
			completedEmitted := emittedStages["pipeline_completed"]
			emitMu.Unlock()
			if !completedEmitted {
				broadcastStageEvent(gw, req.TaskID, "pipeline_completed", outcome)
			}

			return outcome, err
		}

		if isAsync {
			go func() {
				_, _ = executePipeline(context.Background())
			}()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": req.TaskID,
				"status": "running",
			})
			return
		}

		// Synchronous execution
		outcome, err := executePipeline(r.Context())
		if outcome != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		errMsg := "pipeline execution failed"
		if err != nil {
			errMsg = err.Error()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": errMsg,
		})
	})

	// GET /api/v1/factory/runs
	mux.HandleFunc("/api/v1/factory/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		runs := tracker.List()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(runs)
	})

	resolveSandboxDir := func(runID string) string {
		if opts.SandboxBaseDir != "" {
			return filepath.Join(opts.SandboxBaseDir, sanitizeIdentifier(runID))
		}
		if sdp, ok := runner.(SandboxDirProvider); ok {
			return sdp.SandboxDir(runID)
		}
		return filepath.Join(os.TempDir(), "ov-sandboxes", sanitizeIdentifier(runID))
	}

	// GET /api/v1/factory/runs/:run_id and sandbox files endpoints
	mux.HandleFunc("/api/v1/factory/runs/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/factory/runs/")
		path = strings.Trim(path, "/")
		if path == "" {
			http.NotFound(w, r)
			return
		}

		parts := strings.Split(path, "/")
		runID := parts[0]

		switch {
		case len(parts) == 1:
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			record, found := tracker.Get(runID)
			if !found {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("run %q not found", runID),
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(record)

		case len(parts) >= 2 && parts[1] == "files":
			sub := ""
			if len(parts) > 2 {
				sub = strings.Join(parts[2:], "/")
			}
			sandboxDir := resolveSandboxDir(runID)
			handleFactoryFiles(w, r, runID, sub, sandboxDir, tracker, opts, gw)

		default:
			http.NotFound(w, r)
		}
	})
}
