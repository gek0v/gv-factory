// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/llm"
	"github.com/gv-factory/gv-factory/internal/gateway"
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
	Labels      []string       `json:"labels,omitempty"`
	Constraints []string       `json:"constraints,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// FactoryRunResponse represents the status and outcome returned by factory endpoints.
type FactoryRunResponse struct {
	RunID     string                 `json:"run_id"`
	Status    string                 `json:"status"` // "running", "completed", "failed"
	Outcome   *agent.PipelineOutcome `json:"outcome,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	UpdatedAt time.Time              `json:"updated_at,omitempty"`
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

// defaultFactoryRunner wires default agent components with Brain, Gateway telemetry, and sequential Merge Queue.
type defaultFactoryRunner struct {
	brainSvc    *brain.Service
	gw          *gateway.Gateway
	mergeQueue  agent.MergeQueue
	costTracker *telemetry.CostTracker
}

func (d *defaultFactoryRunner) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	hook := func(stage string, payload any) {
		broadcastStageEvent(d.gw, task.TaskID, stage, payload)
	}

	llmClient, _ := llm.NewClientFromEnv()

	cfg := agent.CoordinatorConfig{
		SpecWriter:  agent.NewSpecWriter(agent.SpecWriterConfig{Brain: d.brainSvc, LLM: llmClient}),
		Builder:     agent.NewBuilder(agent.BuilderConfig{WorkspaceDir: ".", LLMClient: llmClient}),
		Approver:    agent.NewApprover(nil, agent.ApproverConfig{}),
		Brain:       d.brainSvc,
		MergeQueue:  d.mergeQueue,
		CostTracker: d.costTracker,
		EventHook:   hook,
	}

	coord := agent.NewFactoryCoordinator(cfg)
	return coord.RunPipeline(ctx, task)
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
	tracker := NewFactoryRunTracker()
	registerFactoryEndpointsWithTracker(mux, gw, runner, tracker)
	return tracker
}

func registerFactoryEndpointsWithTracker(mux *http.ServeMux, gw *gateway.Gateway, runner FactoryPipelineRunner, tracker *FactoryRunTracker) {
	if mux == nil {
		return
	}
	if runner == nil {
		runner = &defaultFactoryRunner{gw: gw}
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
			if outcome != nil && !outcome.Success {
				status = "failed"
			}

			tracker.Set(&FactoryRunResponse{
				RunID:     req.TaskID,
				Status:    status,
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

	// GET /api/v1/factory/runs/:run_id
	mux.HandleFunc("/api/v1/factory/runs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		runID := strings.TrimPrefix(r.URL.Path, "/api/v1/factory/runs/")
		runID = strings.Trim(runID, "/")
		if runID == "" {
			http.NotFound(w, r)
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
	})
}
