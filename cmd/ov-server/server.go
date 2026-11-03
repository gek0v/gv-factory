// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/registry"
	"github.com/gv-factory/gv-factory/internal/sandbox/merger"
	"github.com/gv-factory/gv-factory/internal/store"
	"github.com/gv-factory/gv-factory/internal/telemetry"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// PipelineOrchestrator represents the subset of dag.MultiFactoryOrchestrator methods required by ov-server.
type PipelineOrchestrator interface {
	SubmitRun(ctx context.Context, req dag.RunRequest) (*dag.FactoryRun, error)
	ListRuns() []*dag.FactoryRun
	GetRun(runID string) (*dag.FactoryRun, error)
	CancelRun(runID string) error
}

func generateRandomRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b[:])
}

// corsMiddleware adds CORS headers permitting frontend development and preflight OPTIONS.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://localhost:5173" || origin == "http://127.0.0.1:3000" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, traceparent, baggage")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, traceparent")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// APIValidateRequest represents the payload for POST /api/v1/pipelines/validate.
type APIValidateRequest struct {
	Template    *schema.FactoryTemplate `json:"template,omitempty"`
	CanvasGraph *dag.CanvasGraph        `json:"canvas_graph,omitempty"`
}

// APIRunRequest represents the payload accepted by POST /api/v1/runs.
type APIRunRequest struct {
	RunID       string                  `json:"run_id,omitempty"`
	Template    *schema.FactoryTemplate `json:"template,omitempty"`
	CanvasGraph *dag.CanvasGraph        `json:"canvas_graph,omitempty"`
	RepoPath    string                  `json:"repo_path,omitempty"`
	Branch      string                  `json:"branch,omitempty"`
	BaseBranch  string                  `json:"base_branch,omitempty"`
	Priority    dag.Priority            `json:"priority,omitempty"`
	MaxMemoryMB int                     `json:"max_memory_mb,omitempty"`
	Labels      map[string]string       `json:"labels,omitempty"`
}

// getClientIP extracts the client IP address from standard proxy headers or RemoteAddr.
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// InboundConfig configures inbound webhook reporters and autonomous factory integration for ov-server.
type InboundConfig struct {
	GitHubToken   string
	GitHubAPIURL  string
	GitLabToken   string
	GitLabAPIURL  string
	GitHubSecret  string
	GitLabSecret  string
	SentrySecret  string
	TriggerConfig *inbound.TriggerManagerConfig
	BridgeConfig  *inbound.BridgeConfig
	IssueReporter inbound.IssueFeedbackReporter
	PRCreator     inbound.PullRequestCreator
}

// BuildInboundWebhookHandler creates a fully wired inbound webhook handler with reporters and bridge.
func BuildInboundWebhookHandler(
	cfg InboundConfig,
	gw *gateway.Gateway,
	orchestrator PipelineOrchestrator,
	runner FactoryPipelineRunner,
) (http.Handler, *inbound.PipelineTriggerManager) {
	reporter := cfg.IssueReporter
	prCreator := cfg.PRCreator

	if reporter == nil {
		var ghReporter *inbound.GitHubIssueReporter
		if cfg.GitHubToken != "" || cfg.GitHubAPIURL != "" {
			ghReporter = inbound.NewGitHubIssueReporter(inbound.GitHubReporterConfig{
				Token:   cfg.GitHubToken,
				BaseURL: cfg.GitHubAPIURL,
			})
		}
		var glReporter *inbound.GitLabIssueReporter
		if cfg.GitLabToken != "" || cfg.GitLabAPIURL != "" {
			glReporter = inbound.NewGitLabIssueReporter(inbound.GitLabReporterConfig{
				Token:   cfg.GitLabToken,
				BaseURL: cfg.GitLabAPIURL,
			})
		}
		if ghReporter != nil || glReporter != nil {
			comp := inbound.NewCompositeIssueReporter(ghReporter, glReporter)
			reporter = comp
			if prCreator == nil {
				prCreator = comp
			}
		}
	}

	var bridge *inbound.AutonomousFactoryBridge
	if runner != nil {
		bridgeCfg := inbound.DefaultBridgeConfig()
		if cfg.BridgeConfig != nil {
			bridgeCfg = *cfg.BridgeConfig
		}
		if prCreator != nil {
			bridgeCfg.PRCreator = prCreator
		}
		bridge = inbound.NewAutonomousFactoryBridge(bridgeCfg, runner, gw, reporter)
	}

	triggerCfg := inbound.DefaultTriggerManagerConfig()
	if cfg.TriggerConfig != nil {
		triggerCfg = *cfg.TriggerConfig
	}
	if bridge != nil {
		triggerCfg.AutonomousBridge = bridge
	} else if runner != nil {
		triggerCfg.AutonomousRunner = runner
		triggerCfg.IssueReporter = reporter
	}

	var submitter inbound.PipelineSubmitter
	if orchestrator != nil {
		submitter = orchestrator
	}

	triggerMgr := inbound.NewPipelineTriggerManager(
		triggerCfg,
		submitter,
		gw,
		nil,
		nil,
	)

	webhookIngestor := inbound.NewIngestor(inbound.Config{
		GitHubSecret: cfg.GitHubSecret,
		GitLabSecret: cfg.GitLabSecret,
		SentrySecret: cfg.SentrySecret,
	}, inbound.NewMultiDispatcher(
		inbound.NewGatewayDispatcher(gw),
		triggerMgr,
	))

	return webhookIngestor, triggerMgr
}

// NewServerMuxWithInbound configures the HTTP request multiplexer for ov-server including autonomous inbound triggering.
func NewServerMuxWithInbound(
	brainSvc *brain.Service,
	gw *gateway.Gateway,
	inboundCfg InboundConfig,
	chatHandler *chat.ChatHandler,
	orchestrator PipelineOrchestrator,
	authSvc *auth.Service,
	st store.Store,
	workerReg *WorkerRegistry,
	runner FactoryPipelineRunner,
	tel ...*telemetry.Telemetry,
) (http.Handler, *inbound.PipelineTriggerManager) {
	ingestor, triggerMgr := BuildInboundWebhookHandler(inboundCfg, gw, orchestrator, runner)
	mux := NewServerMuxWithRunner(brainSvc, gw, ingestor, chatHandler, orchestrator, authSvc, st, workerReg, runner, tel...)
	return mux, triggerMgr
}

// NewServerMux configures and returns the HTTP request multiplexer for ov-server.
func NewServerMux(
	brainSvc *brain.Service,
	gw *gateway.Gateway,
	webhookIngestor http.Handler,
	chatHandler *chat.ChatHandler,
	orchestrator PipelineOrchestrator,
	authSvc *auth.Service,
	st store.Store,
	workerReg *WorkerRegistry,
	tel ...*telemetry.Telemetry,
) http.Handler {
	return NewServerMuxWithRunner(brainSvc, gw, webhookIngestor, chatHandler, orchestrator, authSvc, st, workerReg, nil, tel...)
}

// NewServerMuxWithRunner configures and returns the HTTP request multiplexer for ov-server with an explicit factory runner.
func NewServerMuxWithRunner(
	brainSvc *brain.Service,
	gw *gateway.Gateway,
	webhookIngestor http.Handler,
	chatHandler *chat.ChatHandler,
	orchestrator PipelineOrchestrator,
	authSvc *auth.Service,
	st store.Store,
	workerReg *WorkerRegistry,
	runner FactoryPipelineRunner,
	tel ...*telemetry.Telemetry,
) http.Handler {
	if st == nil {
		st = store.NewMemoryStore()
	}
	var t *telemetry.Telemetry
	if len(tel) > 0 && tel[0] != nil {
		t = tel[0]
	} else {
		t, _ = telemetry.New(context.Background(), telemetry.DefaultConfig())
	}

	mux := http.NewServeMux()

	// Metrics endpoint for Prometheus scraping
	if t != nil && t.Metrics != nil {
		mux.Handle("/metrics", t.Metrics.Handler())
	}

	// Health check endpoints for orchestration & load balancers
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		brainStatus := "connected"
		if brainSvc == nil || brainSvc.Ping(r.Context()) != nil {
			brainStatus = "degraded"
		}

		gwStatus := "nats"
		if gw == nil || gw.IsInMemory() {
			gwStatus = "in-memory"
		}

		telStatus := "disabled"
		if t != nil {
			telStatus = t.Status()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"service":   "ov-server",
			"brain":     brainStatus,
			"gateway":   gwStatus,
			"telemetry": telStatus,
		})
	}

	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/v1/health", healthHandler)

	// Readiness check endpoint
	readyHandler := func(w http.ResponseWriter, r *http.Request) {
		isReady := true
		brainStatus := "connected"
		if brainSvc == nil || brainSvc.Ping(r.Context()) != nil {
			brainStatus = "degraded"
		}

		gwStatus := "nats"
		if gw == nil || gw.IsInMemory() {
			gwStatus = "in-memory"
		}

		telStatus := "disabled"
		if t != nil {
			telStatus = t.Status()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"ready":     isReady,
			"service":   "ov-server",
			"brain":     brainStatus,
			"gateway":   gwStatus,
			"telemetry": telStatus,
		})
	}
	mux.HandleFunc("/readyz", readyHandler)

	// Inbound webhook endpoints (/api/v1/webhooks/{provider})
	if webhookIngestor != nil {
		mux.Handle("/api/v1/webhooks/", webhookIngestor)
	}

	// Interactive Chat endpoints (Slack & Discord)
	if chatHandler != nil {
		mux.HandleFunc("/api/v1/chat/slack/interactions", chatHandler.HandleSlackInteractions)
		mux.HandleFunc("/api/v1/chat/slack/commands", chatHandler.HandleSlackCommands)
		mux.HandleFunc("/api/v1/chat/discord/interactions", chatHandler.HandleDiscordInteractions)
	}

	// Real-time SSE event stream endpoints
	if gw != nil && gw.Hub() != nil {
		mux.Handle("/events", gw.Hub())
		mux.Handle("/api/v1/events", gw.Hub())
	}

	// Trackers and sequential merge queue shared between gates and factory pipelines
	pendingTracker := NewPendingGateTracker(context.Background(), gw, st)
	factoryTracker := NewFactoryRunTracker()

	var factoryMergeQueue agent.MergeQueue
	if gw != nil {
		factoryMergeQueue = merger.NewQueue(nil, gw, nil, nil, merger.Config{RepoPath: ".", BaseBranch: "main"})
	}
	if runner != nil {
		if defRunner, ok := runner.(*defaultFactoryRunner); ok && defRunner.mergeQueue != nil {
			factoryMergeQueue = defRunner.mergeQueue
		}
	}

	// Gate approval resolution endpoint from frontend or integrations
	gateResolveHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			RunID    string `json:"run_id"`
			Token    string `json:"token"`
			Decision string `json:"decision"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		// Fallback token lookup by run_id if client omitted token but gate is tracked
		if payload.Token == "" && payload.RunID != "" && pendingTracker != nil {
			for _, g := range pendingTracker.ListPending() {
				if g.RunID == payload.RunID && g.Token != "" {
					payload.Token = g.Token
					break
				}
			}
		}

		now := time.Now().UTC()
		clientIP := getClientIP(r)
		userAgent := r.UserAgent()
		tokenHMACPrefix := auth.ExtractTokenPrefix(payload.Token)

		auditRec := auth.AuditRecord{
			Timestamp:       now,
			RunID:           payload.RunID,
			Decision:        payload.Decision,
			ClientIP:        clientIP,
			UserAgent:       userAgent,
			TokenHMACPrefix: tokenHMACPrefix,
		}

		var auditLogger auth.AuditLogger
		if authSvc != nil {
			auditLogger = authSvc.AuditLogger()
		}

		recordAuditAndPublish := func(status auth.AuditStatus, details string) {
			auditRec.Status = status
			auditRec.Details = details
			if auditLogger != nil {
				auditLogger.Record(auditRec)
			}
			if gw != nil {
				_ = gw.Publish(r.Context(), gateway.TopicGateAudit, gateway.Event{
					Type:      gateway.TopicGateAudit,
					RunID:     payload.RunID,
					Timestamp: now.UnixMilli(),
					Payload: map[string]any{
						"timestamp":         now.Format(time.RFC3339Nano),
						"run_id":            payload.RunID,
						"decision":          payload.Decision,
						"client_ip":         clientIP,
						"user_agent":        userAgent,
						"token_hmac_prefix": tokenHMACPrefix,
						"status":            string(status),
						"details":           details,
					},
				})
			}
		}

		// Decision validation (must be 'approved' or 'rejected')
		normalizedDecision := strings.ToLower(strings.TrimSpace(payload.Decision))
		if normalizedDecision != "approved" && normalizedDecision != "rejected" {
			recordAuditAndPublish(auth.AuditStatusInvalidDecision, "Decision must be 'approved' or 'rejected'")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "decision must be 'approved' or 'rejected'",
			})
			return
		}

		// Cryptographic HMAC token validation
		if authSvc == nil {
			recordAuditAndPublish(auth.AuditStatusInvalidSignature, "Auth service not configured on server")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
			return
		}

		tokenRunID, valid := authSvc.ValidateApprovalToken(payload.Token)
		if !valid {
			recordAuditAndPublish(auth.AuditStatusInvalidSignature, "HMAC signature verification failed or token expired")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
			return
		}

		if tokenRunID != payload.RunID {
			recordAuditAndPublish(auth.AuditStatusRunIDMismatch, "Token run ID does not match payload run ID")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
			return
		}

		recordAuditAndPublish(auth.AuditStatusSuccess, "Gate resolution approved cryptographically")
		log.Printf("ov-server: gate resolved for run %q with decision %q", payload.RunID, payload.Decision)

		if st != nil {
			_, _ = st.ResolveGate(r.Context(), payload.Token, store.GateResolution{
				Decision: payload.Decision,
				Actor:    clientIP,
				Reason:   "Resolved via REST API",
				Metadata: map[string]any{
					"user_agent": userAgent,
				},
			})
		}

		if gw != nil {
			_ = gw.Publish(r.Context(), gateway.TopicGateResolved, gateway.Event{
				Type:      gateway.TopicGateResolved,
				RunID:     payload.RunID,
				Timestamp: now.UnixMilli(),
				Payload: map[string]any{
					"run_id":   payload.RunID,
					"token":    payload.Token,
					"decision": payload.Decision,
				},
			})
		}

		// If gate belongs to a factory run, update status and enqueue to MergeQueue on approval
		if factoryTracker != nil {
			if runRecord, isFactoryRun := factoryTracker.Get(payload.RunID); isFactoryRun {
				if normalizedDecision == "approved" {
					runRecord.Status = "completed"
					runRecord.UpdatedAt = now
					if runRecord.Outcome != nil {
						runRecord.Outcome.Decision = agent.DecisionApproved
						runRecord.Outcome.Success = true
						runRecord.Outcome.MergeStatus = "enqueued"
					}
					factoryTracker.Set(runRecord)

					if factoryMergeQueue != nil {
						branchName := fmt.Sprintf("ov-run/%s", payload.RunID)
						_ = factoryMergeQueue.Enqueue(r.Context(), merger.MergeRequest{
							RunID:        payload.RunID,
							SourceBranch: branchName,
							TargetBranch: "main",
							Metadata: map[string]any{
								"resolved_by": clientIP,
								"token":       payload.Token,
							},
						})
					}

					if gw != nil {
						_ = gw.Publish(r.Context(), "pipeline_completed", gateway.Event{
							Type:      "pipeline_completed",
							RunID:     payload.RunID,
							TaskID:    payload.RunID,
							Timestamp: now.UnixMilli(),
							Payload: map[string]any{
								"run_id":   payload.RunID,
								"status":   "completed",
								"decision": "APPROVED",
								"outcome":  runRecord.Outcome,
							},
						})
					}
				} else if normalizedDecision == "rejected" {
					runRecord.Status = "failed"
					runRecord.UpdatedAt = now
					if runRecord.Outcome != nil {
						runRecord.Outcome.Decision = agent.DecisionType("REJECTED")
						runRecord.Outcome.Success = false
					}
					factoryTracker.Set(runRecord)

					if gw != nil {
						_ = gw.Publish(r.Context(), "pipeline_completed", gateway.Event{
							Type:      "pipeline_completed",
							RunID:     payload.RunID,
							TaskID:    payload.RunID,
							Timestamp: now.UnixMilli(),
							Payload: map[string]any{
								"run_id":   payload.RunID,
								"status":   "failed",
								"decision": "REJECTED",
								"outcome":  runRecord.Outcome,
							},
						})
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Gate decision recorded",
		})
	}

	mux.HandleFunc("/api/gate/resolve", gateResolveHandler)
	mux.HandleFunc("/api/v1/gates/resolve", gateResolveHandler)

	// Step lifecycle tracker
	if gw != nil && st != nil {
		_ = NewStepLifecycleTracker(context.Background(), gw, st)
	}

	// Pending decision gates endpoint
	mux.HandleFunc("/api/v1/gates/pending", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		gates := pendingTracker.ListPending()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"gates": gates,
			"total": len(gates),
		})
	})

	// Worker pool monitoring registry and endpoints
	if workerReg == nil {
		workerReg = NewWorkerRegistry(context.Background(), gw)
	}
	mux.HandleFunc("/api/v1/workers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		workers := workerReg.ListWorkers()
		summary := workerReg.Summary()

		resp := WorkersResponse{
			Workers:          workers,
			TotalWorkers:     summary.TotalWorkers,
			ActiveWorkers:    summary.ActiveWorkers,
			TotalCapacity:    summary.TotalCapacity,
			TotalActiveTasks: summary.TotalActiveTasks,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/v1/workers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		workerID := strings.TrimPrefix(r.URL.Path, "/api/v1/workers/")
		workerID = strings.Trim(workerID, "/")
		if workerID == "" {
			http.NotFound(w, r)
			return
		}

		workerInfo, found := workerReg.GetWorker(workerID)
		if !found {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": fmt.Sprintf("worker %q not found", workerID),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(workerInfo)
	})

	// Pipeline validation endpoint (/api/v1/pipelines/validate)
	mux.HandleFunc("/api/v1/pipelines/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req APIValidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"valid":  false,
				"errors": []string{fmt.Sprintf("invalid JSON payload: %v", err)},
			})
			return
		}

		var targetTpl *schema.FactoryTemplate
		if req.Template != nil {
			targetTpl = req.Template
		} else if req.CanvasGraph != nil {
			var err error
			targetTpl, err = dag.CanvasToTemplate(req.CanvasGraph)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"valid":  false,
					"errors": []string{fmt.Sprintf("failed to convert canvas to template: %v", err)},
				})
				return
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"valid":  false,
				"errors": []string{"either 'template' or 'canvas_graph' must be provided"},
			})
			return
		}

		issues, err := dag.ValidateTemplate(targetTpl)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err != nil || len(issues) > 0 {
			if len(issues) == 0 && err != nil {
				issues = []string{err.Error()}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"valid":  false,
				"errors": issues,
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid": true,
		})
	})

	// Runs endpoints (/api/v1/runs and /api/v1/runs/)
	mux.HandleFunc("/api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		if orchestrator == nil {
			http.Error(w, "Orchestrator not initialized", http.StatusServiceUnavailable)
			return
		}

		switch r.Method {
		case http.MethodGet:
			runs := orchestrator.ListRuns()
			if runs == nil {
				runs = []*dag.FactoryRun{}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(runs)

		case http.MethodPost:
			var req APIRunRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("invalid JSON payload: %v", err),
				})
				return
			}

			var targetTpl *schema.FactoryTemplate
			if req.Template != nil {
				targetTpl = req.Template
			} else if req.CanvasGraph != nil {
				var err error
				targetTpl, err = dag.CanvasToTemplate(req.CanvasGraph)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": fmt.Sprintf("failed to convert canvas to template: %v", err),
					})
					return
				}
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "either 'template' or 'canvas_graph' must be provided",
				})
				return
			}

			// Validate acyclicity and integrity
			if issues, err := dag.ValidateTemplate(targetTpl); err != nil || len(issues) > 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				errMsg := "template validation failed"
				if len(issues) > 0 {
					errMsg = strings.Join(issues, "; ")
				} else if err != nil {
					errMsg = err.Error()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":  errMsg,
					"issues": issues,
				})
				return
			}

			runID := req.RunID
			if runID == "" {
				runID = generateRandomRunID()
			}

			runReq := dag.RunRequest{
				RunID:       runID,
				Template:    *targetTpl,
				RepoPath:    req.RepoPath,
				Branch:      req.Branch,
				BaseBranch:  req.BaseBranch,
				Priority:    req.Priority,
				MaxMemoryMB: req.MaxMemoryMB,
				Labels:      req.Labels,
			}

			run, err := orchestrator.SubmitRun(r.Context(), runReq)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

			if st != nil {
				_ = st.CreateRun(r.Context(), &store.Run{
					ID:           run.ID,
					TemplateName: run.TemplateName,
					Status:       store.RunStatus(run.State),
					InitiatedBy:  getClientIP(r),
					Metadata: map[string]any{
						"repo_path":   req.RepoPath,
						"branch":      req.Branch,
						"base_branch": req.BaseBranch,
						"priority":    int(req.Priority),
					},
					CreatedAt: run.QueuedAt,
				})
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": run.ID,
				"state":  run.State,
				"run":    run,
			})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	// Sandbox Interactive Terminal WebSocket handler
	terminalHandler := NewTerminalHandler(DefaultTerminalOptions())
	RegisterTerminalRoutes(mux, terminalHandler)

	mux.HandleFunc("/api/v1/runs/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
		parts := strings.Split(strings.Trim(path, "/"), "/")

		if len(parts) == 0 || parts[0] == "" {
			http.NotFound(w, r)
			return
		}

		runID := parts[0]

		if len(parts) == 1 {
			// GET /api/v1/runs/{id}
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			if orchestrator != nil {
				run, err := orchestrator.GetRun(runID)
				if err == nil && run != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(run)
					return
				}
			}

			if st != nil {
				storedRun, stErr := st.GetRun(r.Context(), runID)
				if stErr == nil && storedRun != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(storedRun)
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": fmt.Sprintf("run %q not found", runID),
			})
			return
		}

		if len(parts) == 2 && parts[1] == "steps" {
			// GET /api/v1/runs/{id}/steps
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			// Validate run existence in orchestrator or store
			runExists := false
			if orchestrator != nil {
				if _, err := orchestrator.GetRun(runID); err == nil {
					runExists = true
				}
			}
			if !runExists && st != nil {
				if _, err := st.GetRun(r.Context(), runID); err == nil {
					runExists = true
				}
			}

			if !runExists {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("run %q not found", runID),
				})
				return
			}

			if st == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode([]*store.Step{})
				return
			}

			steps, err := st.ListStepsByRun(r.Context(), runID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

			if steps == nil {
				steps = []*store.Step{}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(steps)
			return
		}

		if len(parts) == 2 && parts[1] == "events" {
			// GET /api/v1/runs/{id}/events
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			if _, err := orchestrator.GetRun(runID); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("run %q not found", runID),
				})
				return
			}

			if gw == nil || gw.Hub() == nil {
				http.Error(w, "Gateway hub not initialized", http.StatusServiceUnavailable)
				return
			}

			gw.Hub().ServeHTTPRun(w, r, runID)
			return
		}

		if len(parts) == 2 && parts[1] == "terminal" {
			// GET /api/v1/runs/{id}/terminal (WebSocket)
			terminalHandler.ServeTerminalHTTP(w, r, runID)
			return
		}

		if len(parts) == 2 && parts[1] == "cancel" {
			// POST /api/v1/runs/{id}/cancel
			if r.Method != http.MethodPost {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			if err := orchestrator.CancelRun(runID); err != nil {
				w.Header().Set("Content-Type", "application/json")
				if errors.Is(err, dag.ErrRunNotFound) {
					w.WriteHeader(http.StatusNotFound)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": fmt.Sprintf("run %q cancelled", runID),
			})
			return
		}

		http.NotFound(w, r)
	})

	// Codebase Brain endpoints (search, sync ADRs, repository indexer, stats)
	registerBrainEndpoints(mux, brainSvc)

	// Global Pi Skills, Tools, Plugins and Agent Templates registry endpoints
	regManager := registry.NewManager(registry.ManagerOptions{
		WorkspaceRoot:   ".",
		IncludeUserHome: true,
	})
	registerRegistryEndpoints(mux, regManager)

	// Cost and Token Telemetry endpoints
	var costTracker *telemetry.CostTracker
	if t != nil && t.CostTracker != nil {
		costTracker = t.CostTracker
	} else {
		costTracker = telemetry.NewCostTracker()
	}
	registerCostEndpoints(mux, costTracker)

	// Autonomous Factory execution and status endpoints
	if runner == nil {
		runner = &defaultFactoryRunner{brainSvc: brainSvc, gw: gw, mergeQueue: factoryMergeQueue, costTracker: costTracker}
	}
	RegisterFactoryEndpointsWithOptions(mux, gw, runner, FactoryEndpointsOptions{
		AuthSvc:        authSvc,
		Store:          st,
		MergeQueue:     factoryMergeQueue,
		Tracker:        factoryTracker,
		PendingTracker: pendingTracker,
	})

	var handler http.Handler = corsMiddleware(mux)
	if t != nil && t.Metrics != nil {
		handler = corsMiddleware(telemetry.NewHTTPMiddleware(t.Metrics, t.Tracer)(mux))
	}

	return handler
}
