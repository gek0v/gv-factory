// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox/merger"
)

// ============================================================================
// SIMULATED EXTERNAL SERVICES (GitHub, Slack, Discord)
// ============================================================================

type githubPRRecord struct {
	Repo  string
	Head  string
	Base  string
	Title string
	Body  string
}

type githubCommentRecord struct {
	Repo        string
	IssueNumber int
	Body        string
}

type fakeGitHubServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	prs      []githubPRRecord
	comments []githubCommentRecord
}

func newFakeGitHubServer(t *testing.T) *fakeGitHubServer {
	t.Helper()
	fake := &fakeGitHubServer{}

	mux := http.NewServeMux()

	// POST /repos/{owner}/{repo}/pulls
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/pulls") && r.Method == http.MethodPost {
			parts := strings.Split(strings.Trim(path, "/"), "/")
			repo := ""
			if len(parts) >= 3 {
				repo = parts[1] + "/" + parts[2]
			}

			var payload struct {
				Title string `json:"title"`
				Head  string `json:"head"`
				Base  string `json:"base"`
				Body  string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			fake.mu.Lock()
			fake.prs = append(fake.prs, githubPRRecord{
				Repo:  repo,
				Head:  payload.Head,
				Base:  payload.Base,
				Title: payload.Title,
				Body:  payload.Body,
			})
			prNum := len(fake.prs)
			fake.mu.Unlock()

			resp := inbound.PullRequestResponse{
				ID:      int64(100 + prNum),
				Number:  prNum,
				HTMLURL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, prNum),
				URL:     fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNum),
				State:   "open",
				Title:   payload.Title,
				Body:    payload.Body,
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// POST /repos/{owner}/{repo}/issues/{issue_number}/comments
		if strings.Contains(path, "/issues/") && strings.HasSuffix(path, "/comments") && r.Method == http.MethodPost {
			parts := strings.Split(strings.Trim(path, "/"), "/")
			repo := ""
			var issueNum int
			if len(parts) >= 5 {
				repo = parts[1] + "/" + parts[2]
				_, _ = fmt.Sscanf(parts[4], "%d", &issueNum)
			}

			var payload struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			fake.mu.Lock()
			fake.comments = append(fake.comments, githubCommentRecord{
				Repo:        repo,
				IssueNumber: issueNum,
				Body:        payload.Body,
			})
			commentID := len(fake.comments)
			fake.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   200 + commentID,
				"body": payload.Body,
			})
			return
		}

		http.NotFound(w, r)
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		fake.server.Close()
	})
	return fake
}

func (f *fakeGitHubServer) PRs() []githubPRRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := make([]githubPRRecord, len(f.prs))
	copy(res, f.prs)
	return res
}

func (f *fakeGitHubServer) Comments() []githubCommentRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := make([]githubCommentRecord, len(f.comments))
	copy(res, f.comments)
	return res
}

type fakeSlackServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	messages []map[string]any
}

func newFakeSlackServer(t *testing.T) *fakeSlackServer {
	t.Helper()
	fake := &fakeSlackServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		fake.mu.Lock()
		fake.messages = append(fake.messages, payload)
		ts := fmt.Sprintf("1712345678.%06d", len(fake.messages))
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"ts": ts,
		})
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		fake.server.Close()
	})
	return fake
}

func (s *fakeSlackServer) Messages() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]map[string]any, len(s.messages))
	copy(res, s.messages)
	return res
}

type fakeDiscordServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	messages []map[string]any
}

func newFakeDiscordServer(t *testing.T) *fakeDiscordServer {
	t.Helper()
	fake := &fakeDiscordServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/channels/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodPost {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			fake.mu.Lock()
			fake.messages = append(fake.messages, payload)
			msgID := fmt.Sprintf("discord-msg-%d", len(fake.messages))
			fake.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         msgID,
				"channel_id": "alerts",
			})
			return
		}
		http.NotFound(w, r)
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		fake.server.Close()
	})
	return fake
}

func (d *fakeDiscordServer) Messages() []map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	res := make([]map[string]any, len(d.messages))
	copy(res, d.messages)
	return res
}

// ============================================================================
// IN-MEMORY BRAIN EPISODE RECORDER
// ============================================================================

type memoryEpisodeRecorder struct {
	mu       sync.Mutex
	episodes []brain.TaskEpisode
}

func (m *memoryEpisodeRecorder) RecordEpisode(ctx context.Context, ep brain.TaskEpisode) (*brain.TaskEpisode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.episodes = append(m.episodes, ep)
	return &ep, nil
}

func (m *memoryEpisodeRecorder) Episodes() []brain.TaskEpisode {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]brain.TaskEpisode, len(m.episodes))
	copy(res, m.episodes)
	return res
}

// ============================================================================
// FACTORY RUN TRACKER & RESPONSE TYPES (Matching ov-server & ov-cli contracts)
// ============================================================================

type factoryRunResponse struct {
	RunID     string                 `json:"run_id"`
	Status    string                 `json:"status"` // "running", "completed", "failed", "gate_waiting"
	Outcome   *agent.PipelineOutcome `json:"outcome,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	UpdatedAt time.Time              `json:"updated_at,omitempty"`
}

type factoryRunRequest struct {
	TaskID      string         `json:"task_id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Constraints []string       `json:"constraints,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type factoryRunTracker struct {
	mu   sync.RWMutex
	runs map[string]*factoryRunResponse
}

func newFactoryRunTracker() *factoryRunTracker {
	return &factoryRunTracker{
		runs: make(map[string]*factoryRunResponse),
	}
}

func (t *factoryRunTracker) Set(resp *factoryRunResponse) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs[resp.RunID] = resp
}

func (t *factoryRunTracker) Get(runID string) (*factoryRunResponse, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	r, ok := t.runs[runID]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

func (t *factoryRunTracker) List() []*factoryRunResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()
	list := make([]*factoryRunResponse, 0, len(t.runs))
	for _, r := range t.runs {
		cp := *r
		list = append(list, &cp)
	}
	return list
}

// ============================================================================
// GIT FIXTURE HELPER
// ============================================================================

func setupGitRepoFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
		return string(out)
	}

	runGit("init")
	runGit("config", "user.name", "gv-factory-e2e")
	runGit("config", "user.email", "e2e@gv-factory.local")
	runGit("config", "commit.gpgsign", "false")

	// Create baseline file
	baseFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(baseFile, []byte("# Autonomous Software Factory\nBaseline\n"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "chore: initial baseline commit")
	runGit("branch", "-M", "main")

	return dir
}

// ============================================================================
// INTEGRATED TEST SERVER FIXTURE (ov-server httptest harness)
// ============================================================================

type factoryServerFixture struct {
	server        *httptest.Server
	serverURL     string
	gateway       *gateway.Gateway
	authSvc       *auth.Service
	tracker       *factoryRunTracker
	mergeQueue    *merger.Queue
	brainRecorder *memoryEpisodeRecorder
	fakeGitHub    *fakeGitHubServer
	fakeSlack     *fakeSlackServer
	fakeDiscord   *fakeDiscordServer
	repoDir       string
	coordinator   *agent.FactoryCoordinator
	bridge        *inbound.AutonomousFactoryBridge
	chatMgr       *chat.Manager
}

type serverOptions struct {
	customBuilder   agent.TaskBuilder
	customApprover  agent.GateApprover
	sensitivePaths  []string
	githubSecret    string
	pendingGateChan chan string
}

func setupFactoryServer(t *testing.T, opts serverOptions) *factoryServerFixture {
	t.Helper()

	if opts.githubSecret == "" {
		opts.githubSecret = "e2e-gh-webhook-secret"
	}

	repoDir := setupGitRepoFixture(t)
	fakeGH := newFakeGitHubServer(t)
	fakeSlack := newFakeSlackServer(t)
	fakeDiscord := newFakeDiscordServer(t)

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	authSvc := auth.NewService("e2e-hmac-gate-secret-key-32bytes!")
	tracker := newFactoryRunTracker()
	brainRec := &memoryEpisodeRecorder{}

	// Sequential Merge Queue with Git pre-validation (git merge-tree)
	mq := merger.NewQueue(nil, gw, nil, nil, merger.Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	// Approver
	approver := opts.customApprover
	if approver == nil {
		approverCfg := agent.ApproverConfig{
			MinApprovalScore: 0.85,
			MinCoverage:      0.90,
			MaxRetries:       2,
			SensitivePaths:   opts.sensitivePaths,
		}
		approver = agent.NewApprover(nil, approverCfg)
	}

	// Builder
	builder := opts.customBuilder
	if builder == nil {
		builder = &e2eBuilder{
			repoDir: repoDir,
		}
	}

	// SpecWriter
	specWriter := agent.NewSpecWriter(agent.SpecWriterConfig{})

	// FactoryCoordinator
	coordCfg := agent.CoordinatorConfig{
		SpecWriter:  specWriter,
		Builder:     builder,
		Approver:    approver,
		Brain:       brainRec,
		MergeQueue:  mq,
		MinCoverage: 0.90,
		MaxRetries:  2,
	}
	coordinator := agent.NewFactoryCoordinator(coordCfg)

	// GitHub Issue Reporter & PR Creator
	ghReporter := inbound.NewGitHubIssueReporter(inbound.GitHubReporterConfig{
		BaseURL: fakeGH.server.URL,
		Token:   "mock-gh-token",
	})

	// AutonomousFactoryBridge
	bridgeCfg := inbound.DefaultBridgeConfig()
	bridgeCfg.Async = false // synchronous execution for deterministic assertions
	bridgeCfg.PRCreator = ghReporter
	runner := &pipelineRunnerAdapter{coord: coordinator, tracker: tracker}
	bridge := inbound.NewAutonomousFactoryBridge(bridgeCfg, runner, gw, ghReporter)

	// Pipeline Trigger Manager
	triggerCfg := inbound.DefaultTriggerManagerConfig()
	triggerCfg.AutonomousBridge = bridge
	triggerMgr := inbound.NewPipelineTriggerManager(triggerCfg, nil, gw, nil, nil)

	// Webhook Ingestor
	ingestor := inbound.NewIngestor(inbound.Config{
		GitHubSecret: opts.githubSecret,
	}, inbound.NewMultiDispatcher(
		inbound.NewGatewayDispatcher(gw),
		triggerMgr,
	))

	// Chat Manager & Interactive Decision Gate Notifiers
	chatMgr := chat.NewManager()
	slackAdapter := chat.NewSlackAdapter(chat.SlackConfig{
		BotToken:       "xoxb-mock-token",
		DefaultChannel: "alerts",
		BaseURL:        fakeSlack.server.URL,
	})
	chatMgr.Register(slackAdapter)

	discordAdapter := chat.NewDiscordAdapter(chat.DiscordConfig{
		BotToken:         "Bot mock-token",
		DefaultChannelID: "alerts",
		BaseURL:          fakeDiscord.server.URL,
	})
	chatMgr.Register(discordAdapter)

	// Attach chat manager to gateway TopicGateApprovalRequired
	detachChat, err := chatMgr.AttachToGateway(context.Background(), gw, "alerts")
	if err != nil {
		t.Fatalf("failed to attach chat manager to gateway: %v", err)
	}
	t.Cleanup(func() {
		detachChat()
	})

	// Mount HTTP endpoints mimicking ov-server architecture
	mux := http.NewServeMux()

	// Webhook endpoints
	mux.HandleFunc("/api/v1/webhooks/github", ingestor.ServeHTTP)
	mux.HandleFunc("/webhooks/github", ingestor.ServeHTTP)

	// In-memory Pending Gates tracking
	type pendingGateRecord struct {
		RunID     string    `json:"run_id"`
		Token     string    `json:"token"`
		TaskTitle string    `json:"task_title"`
		Reason    string    `json:"reason"`
		CreatedAt time.Time `json:"created_at"`
	}
	var (
		pendingGatesMu sync.Mutex
		pendingGates   = make(map[string]pendingGateRecord)
	)

	// Subscribe to TopicGateApprovalRequired to track pending gates
	gateSubCh := make(chan gateway.Event, 16)
	unsubGate, err := gw.SubscribeTaskEvents(context.Background(), gateway.TopicGateApprovalRequired, gateSubCh)
	if err != nil {
		t.Fatalf("failed to subscribe to gate approval required: %v", err)
	}
	go func() {
		for ev := range gateSubCh {
			p := ev.Payload
			if p == nil {
				continue
			}
			token, _ := p["token"].(string)
			runID := ev.RunID
			if runID == "" {
				runID, _ = p["run_id"].(string)
			}
			title, _ := p["task_title"].(string)
			reason, _ := p["gate_reason"].(string)

			pendingGatesMu.Lock()
			pendingGates[runID] = pendingGateRecord{
				RunID:     runID,
				Token:     token,
				TaskTitle: title,
				Reason:    reason,
				CreatedAt: time.Now(),
			}
			pendingGatesMu.Unlock()

			if opts.pendingGateChan != nil {
				select {
				case opts.pendingGateChan <- token:
				default:
				}
			}
		}
	}()
	t.Cleanup(func() {
		unsubGate()
	})

	// GET /api/v1/gates/pending
	mux.HandleFunc("/api/v1/gates/pending", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		pendingGatesMu.Lock()
		list := make([]pendingGateRecord, 0, len(pendingGates))
		for _, g := range pendingGates {
			list = append(list, g)
		}
		pendingGatesMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"gates": list,
			"total": len(list),
		})
	})

	// POST /api/v1/gates/resolve and /api/gate/resolve
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid payload: " + err.Error()})
			return
		}

		decision := strings.ToLower(strings.TrimSpace(payload.Decision))
		if decision != "approved" && decision != "rejected" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "decision must be 'approved' or 'rejected'"})
			return
		}

		// Cryptographic HMAC token validation
		tokenRunID, valid := authSvc.ValidateApprovalToken(payload.Token)
		if !valid || tokenRunID != payload.RunID {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid or expired approval token"})
			return
		}

		// Remove from pending
		pendingGatesMu.Lock()
		delete(pendingGates, payload.RunID)
		pendingGatesMu.Unlock()

		// Publish resolved event over gateway
		_ = gw.Publish(r.Context(), gateway.TopicGateResolved, gateway.Event{
			Type:      gateway.TopicGateResolved,
			RunID:     payload.RunID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"run_id":   payload.RunID,
				"token":    payload.Token,
				"decision": payload.Decision,
			},
		})

		// Update run status in tracker if present
		if existing, ok := tracker.Get(payload.RunID); ok {
			existing.Status = "completed"
			if existing.Outcome != nil {
				existing.Outcome.Decision = agent.DecisionApproved
				existing.Outcome.Success = (decision == "approved")
			}
			existing.UpdatedAt = time.Now().UTC()
			tracker.Set(existing)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Gate decision recorded",
		})
	}
	mux.HandleFunc("/api/v1/gates/resolve", gateResolveHandler)
	mux.HandleFunc("/api/gate/resolve", gateResolveHandler)

	// POST /api/v1/factory/run
	mux.HandleFunc("/api/v1/factory/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req factoryRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid JSON: " + err.Error()})
			return
		}

		req.TaskID = strings.TrimSpace(req.TaskID)
		req.Title = strings.TrimSpace(req.Title)
		if req.TaskID == "" || req.Title == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "task_id and title are required"})
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

		now := time.Now().UTC()
		tracker.Set(&factoryRunResponse{
			RunID:     req.TaskID,
			Status:    "running",
			CreatedAt: now,
			UpdatedAt: now,
		})

		runPipeline := func(ctx context.Context) (*agent.PipelineOutcome, error) {
			outcome, err := coordinator.RunPipeline(ctx, taskCtx)
			now := time.Now().UTC()
			status := "completed"
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
				if outcome == nil || !outcome.Success {
					status = "failed"
				}
			}
			if outcome != nil {
				if outcome.IsHumanGate() {
					status = "gate_waiting"
					// Auto-publish gate approval required with HMAC token
					token := authSvc.GenerateApprovalToken(req.TaskID, time.Now().Add(1*time.Hour))
					_ = gw.PublishTaskEvent(ctx, gateway.Event{
						Type:      gateway.TopicGateApprovalRequired,
						RunID:     req.TaskID,
						Timestamp: time.Now().UnixMilli(),
						Payload: map[string]any{
							"run_id":      req.TaskID,
							"token":       token,
							"task_title":  req.Title,
							"gate_reason": outcome.Approval.GateReason,
							"diff":        outcome.BuildResult.GitDiff,
						},
					})
				} else if !outcome.Success {
					status = "failed"
				}
			}

			tracker.Set(&factoryRunResponse{
				RunID:     req.TaskID,
				Status:    status,
				Outcome:   outcome,
				Error:     errMsg,
				UpdatedAt: now,
			})
			return outcome, err
		}

		if isAsync {
			go func() {
				_, _ = runPipeline(context.Background())
			}()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": req.TaskID,
				"status": "running",
			})
			return
		}

		outcome, err := runPipeline(r.Context())
		if outcome != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
	})

	// GET /api/v1/factory/runs
	mux.HandleFunc("/api/v1/factory/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tracker.List())
	})

	// GET /api/v1/factory/runs/{run_id}
	mux.HandleFunc("/api/v1/factory/runs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		runID := strings.TrimPrefix(r.URL.Path, "/api/v1/factory/runs/")
		runID = strings.TrimSpace(runID)
		resp, ok := tracker.Get(runID)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "factory run not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		server.Close()
		_ = gw.Close()
	})

	return &factoryServerFixture{
		server:        server,
		serverURL:     server.URL,
		gateway:       gw,
		authSvc:       authSvc,
		tracker:       tracker,
		mergeQueue:    mq,
		brainRecorder: brainRec,
		fakeGitHub:    fakeGH,
		fakeSlack:     fakeSlack,
		fakeDiscord:   fakeDiscord,
		repoDir:       repoDir,
		coordinator:   coordinator,
		bridge:        bridge,
		chatMgr:       chatMgr,
	}
}

// pipelineRunnerAdapter adapts *agent.FactoryCoordinator to inbound.FactoryPipelineRunner and updates tracker.
type pipelineRunnerAdapter struct {
	coord   *agent.FactoryCoordinator
	tracker *factoryRunTracker
}

func (a *pipelineRunnerAdapter) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	now := time.Now().UTC()
	if a.tracker != nil {
		a.tracker.Set(&factoryRunResponse{
			RunID:     task.TaskID,
			Status:    "running",
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	outcome, err := a.coord.RunPipeline(ctx, task)
	now = time.Now().UTC()
	status := "completed"
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
		if outcome == nil || !outcome.Success {
			status = "failed"
		}
	}
	if outcome != nil {
		if outcome.IsHumanGate() {
			status = "gate_waiting"
		} else if !outcome.Success {
			status = "failed"
		}
	}

	if a.tracker != nil {
		a.tracker.Set(&factoryRunResponse{
			RunID:     task.TaskID,
			Status:    status,
			Outcome:   outcome,
			Error:     errMsg,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	return outcome, err
}

// ============================================================================
// REALISTIC TDD BUILDER FOR E2E PIPELINE (Red-Green-Refactor)
// ============================================================================

type e2eBuilder struct {
	repoDir       string
	buildFn       func(ctx context.Context, spec *agent.SpecDocument) ([]*agent.BuildResult, error)
	filesModified []string
	callCount     int32
}

func (b *e2eBuilder) BuildSpec(ctx context.Context, spec *agent.SpecDocument) ([]*agent.BuildResult, error) {
	atomic.AddInt32(&b.callCount, 1)
	if b.buildFn != nil {
		return b.buildFn(ctx, spec)
	}

	// Default realistic build implementation
	files := b.filesModified
	if len(files) == 0 {
		files = []string{"pkg/jwt/rotation.go", "pkg/jwt/rotation_test.go"}
	}

	diffBuilder := strings.Builder{}
	for _, f := range files {
		diffBuilder.WriteString(fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,15 @@\n+// SPDX-License-Identifier: Apache-2.0\n+// SPDX-FileCopyrightText: 2026 gv-factory contributors\n+\n+package jwt\n+\n+func Placeholder() {}\n", f, f, f))
	}

	res := &agent.BuildResult{
		TaskID:        spec.Title,
		Success:       true,
		FilesModified: files,
		GitDiff:       diffBuilder.String(),
		TotalDuration: 15 * time.Millisecond,
	}
	return []*agent.BuildResult{res}, nil
}

// ============================================================================
// CLI RUNNER HELPER
// ============================================================================

func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdArgs := append([]string{"run", "github.com/gv-factory/gv-factory/cmd/ov-cli"}, args...)
	cmd := exec.CommandContext(ctx, "go", cmdArgs...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to invoke ov-cli: %v\nstderr: %s", err, stderrBuf.String())
		}
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode
}

func computeGitHubHMAC(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// ============================================================================
// E2E TEST SCENARIO 1:
// GitHub Webhook (`ready-for-agent`) -> AutonomousFactoryBridge ->
// FactoryCoordinator -> MergeQueue -> GitHub PR & Feedback Comment ->
// Brain Episode -> Verified via `ov-cli factory status`
// ============================================================================

func TestE2E_FactoryPipeline_GitHubWebhookToMergeQueueAndStatus(t *testing.T) {
	secret := "e2e-github-webhook-secret-xyz"
	fix := setupFactoryServer(t, serverOptions{
		githubSecret: secret,
	})

	// 1. Prepare target branch in git repo fixture so git merge-tree can validate cleanly
	issueNum := 42
	branchName := fmt.Sprintf("ov-run/issue-%d", issueNum)
	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = fix.repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
		return string(out)
	}

	// Create feature branch with new non-conflicting file
	runGit("checkout", "-b", branchName, "main")
	_ = os.WriteFile(filepath.Join(fix.repoDir, "jwt_rotation.go"), []byte("package jwt\n\nfunc Rotate() bool { return true }\n"), 0644)
	runGit("add", "jwt_rotation.go")
	runGit("commit", "-m", "feat: implement JWT rotation mechanism")
	runGit("checkout", "main")

	// 2. Prepare GitHub Inbound Webhook Payload (issues.labeled with 'ready-for-agent')
	webhookPayload := map[string]any{
		"action": "labeled",
		"label": map[string]any{
			"name": "ready-for-agent",
		},
		"issue": map[string]any{
			"number": issueNum,
			"title":  "Implement JWT rotation mechanism",
			"body":   "We need JWT refresh tokens rotated every 24h.\n\n<!-- CONSTRAINTS: no external deps, coverage >= 90 -->",
			"labels": []map[string]any{
				{"name": "ready-for-agent"},
			},
		},
		"repository": map[string]any{
			"full_name":      "gv-factory/gv-factory",
			"default_branch": "main",
		},
		"sender": map[string]any{
			"login": "octocat",
		},
	}

	bodyBytes, err := json.Marshal(webhookPayload)
	if err != nil {
		t.Fatalf("failed to marshal webhook payload: %v", err)
	}
	signature := computeGitHubHMAC([]byte(secret), bodyBytes)

	// 3. Dispatch Webhook to ov-server
	req, err := http.NewRequest(http.MethodPost, fix.serverURL+"/api/v1/webhooks/github", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("failed to build webhook HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-e2e-issue-42")
	req.Header.Set("X-Hub-Signature-256", signature)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("webhook request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected HTTP 200 or 202 from webhook, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	// Wait briefly for synchronous pipeline hooks to complete
	expectedTaskID := fmt.Sprintf("issue-%d", issueNum)

	// 4. Verify Merge Queue received the approved merge request
	if fix.mergeQueue.Len() != 1 {
		t.Fatalf("expected 1 item in MergeQueue, got %d", fix.mergeQueue.Len())
	}

	// Process item in MergeQueue and verify git merge-tree in-memory pre-validation
	processRes, err := fix.mergeQueue.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("failed to process MergeQueue: %v", err)
	}
	if processRes.Status != merger.StatusMerged {
		t.Errorf("expected MergeQueue status %s, got %s", merger.StatusMerged, processRes.Status)
	}
	if !processRes.Simulation.ConflictFree {
		t.Errorf("expected conflict-free merge tree simulation, got conflicts: %v", processRes.Simulation.ConflictFiles)
	}

	// 5. Verify GitHub PR Creation on Simulated GitHub API
	prs := fix.fakeGitHub.PRs()
	if len(prs) != 1 {
		t.Fatalf("expected 1 GitHub Pull Request to be opened, got %d", len(prs))
	}
	pr := prs[0]
	if !strings.Contains(pr.Title, "Implement JWT rotation mechanism") {
		t.Errorf("expected PR title to contain issue title, got %q", pr.Title)
	}
	if pr.Head != branchName {
		t.Errorf("expected PR head branch %q, got %q", branchName, pr.Head)
	}
	if pr.Base != "main" {
		t.Errorf("expected PR base branch 'main', got %q", pr.Base)
	}

	// 6. Verify GitHub Feedback Comment on Issue #42
	comments := fix.fakeGitHub.Comments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 GitHub Feedback Comment, got %d", len(comments))
	}
	comment := comments[0]
	if comment.IssueNumber != issueNum {
		t.Errorf("expected comment on issue #%d, got #%d", issueNum, comment.IssueNumber)
	}
	if !strings.Contains(comment.Body, "APPROVED") && !strings.Contains(comment.Body, "Autonomous Implementation") {
		t.Errorf("expected feedback comment to mention approved implementation, got: %s", comment.Body)
	}

	// 7. Verify Brain Task Episode Persistence
	episodes := fix.brainRecorder.Episodes()
	if len(episodes) != 1 {
		t.Fatalf("expected 1 Brain Task Episode recorded, got %d", len(episodes))
	}
	ep := episodes[0]
	if ep.RunID != expectedTaskID {
		t.Errorf("expected Brain Episode RunID %q, got %q", expectedTaskID, ep.RunID)
	}
	if ep.Outcome != "success" {
		t.Errorf("expected Brain Episode Outcome 'success', got %q", ep.Outcome)
	}

	// 8. Verify via `ov-cli factory status <task_id>`
	stdout, stderr, exitCode := runCLI(t, "factory", "status", expectedTaskID, "--server", fix.serverURL)
	if exitCode != 0 {
		t.Fatalf("ov-cli factory status failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	if !strings.Contains(stdout, expectedTaskID) {
		t.Errorf("expected status output to contain run ID %q, got: %s", expectedTaskID, stdout)
	}
	if !strings.Contains(stdout, "COMPLETED") && !strings.Contains(stdout, "APPROVED") {
		t.Errorf("expected status output to show completed or approved, got: %s", stdout)
	}

	// Also verify JSON output format
	jsonOut, _, jsonCode := runCLI(t, "factory", "status", expectedTaskID, "--server", fix.serverURL, "--json")
	if jsonCode != 0 {
		t.Fatalf("ov-cli factory status --json failed with code %d: %s", jsonCode, jsonOut)
	}
	var parsedStatus factoryRunResponse
	if err := json.Unmarshal([]byte(jsonOut), &parsedStatus); err != nil {
		t.Fatalf("failed to parse JSON from ov-cli factory status: %v\nOutput: %s", err, jsonOut)
	}
	if parsedStatus.RunID != expectedTaskID {
		t.Errorf("expected JSON RunID %q, got %q", expectedTaskID, parsedStatus.RunID)
	}
	if parsedStatus.Status != "completed" {
		t.Errorf("expected JSON status 'completed', got %q", parsedStatus.Status)
	}
}

// ============================================================================
// E2E TEST SCENARIO 2:
// Sensitive File Trigger (`internal/auth/`) -> DecisionHumanGate ->
// Slack Block Kit card & Discord embed -> HMAC gate resolution via
// `/api/v1/gates/resolve` -> Status update to approved
// ============================================================================

func TestE2E_FactoryPipeline_SensitiveFileHumanGateSlackDiscordHMAC(t *testing.T) {
	gateTokenChan := make(chan string, 1)

	// Builder modifying sensitive path `internal/auth/tokens.go`
	builder := &e2eBuilder{
		filesModified: []string{"internal/auth/tokens.go"},
	}

	fix := setupFactoryServer(t, serverOptions{
		customBuilder:   builder,
		sensitivePaths:  []string{"internal/auth/", "migrations/"},
		pendingGateChan: gateTokenChan,
	})

	taskID := "run-auth-sensitive-99"

	// 1. Dispatch Factory Run via API touching sensitive file
	runPayload := factoryRunRequest{
		TaskID:      taskID,
		Title:       "Rotate JWT Secret Key and Tokens",
		Description: "Security patch modifying authentication tokens",
		Labels:      []string{"security", "auth"},
	}
	body, _ := json.Marshal(runPayload)

	req, err := http.NewRequest(http.MethodPost, fix.serverURL+"/api/v1/factory/run?async=false", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create run request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to dispatch pipeline: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected HTTP 200 from synchronous run, got %d. Body: %s", resp.StatusCode, string(respBytes))
	}

	var outcome agent.PipelineOutcome
	if err := json.NewDecoder(resp.Body).Decode(&outcome); err != nil {
		t.Fatalf("failed to decode pipeline outcome: %v", err)
	}

	// 2. Verify Pipeline resulted in DecisionHumanGate
	if outcome.Decision != agent.DecisionHumanGate {
		t.Fatalf("expected DecisionHumanGate for sensitive file, got %s", outcome.Decision)
	}
	if outcome.Approval == nil || !outcome.Approval.RequiresHumanSign {
		t.Fatalf("expected Approval.RequiresHumanSign=true, got: %+v", outcome.Approval)
	}

	// Retrieve HMAC token generated for the gate
	var token string
	select {
	case token = <-gateTokenChan:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gate token event")
	}

	if token == "" {
		t.Fatal("expected non-empty HMAC approval token")
	}

	// 3. Verify Slack Block Kit Card Dispatch & 4. Verify Discord Card Dispatch
	var slackMessages []map[string]any
	var discordMessages []map[string]any
	for i := 0; i < 50; i++ {
		slackMessages = fix.fakeSlack.Messages()
		discordMessages = fix.fakeDiscord.Messages()
		if len(slackMessages) > 0 && len(discordMessages) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(slackMessages) == 0 {
		t.Fatal("expected Slack Block Kit message to be dispatched")
	}
	slackMsg := slackMessages[0]
	blocks, ok := slackMsg["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("expected Slack message to contain Block Kit 'blocks', got: %+v", slackMsg)
	}

	// Validate action block contains interactive buttons
	hasActionsBlock := false
	for _, blk := range blocks {
		if bMap, ok := blk.(map[string]any); ok {
			if bMap["type"] == "actions" {
				hasActionsBlock = true
				break
			}
		}
	}
	if !hasActionsBlock {
		t.Errorf("expected Slack Block Kit to contain an 'actions' block with interactive buttons")
	}

	if len(discordMessages) == 0 {
		t.Fatal("expected Discord message to be dispatched")
	}

	// 5. Query GET /api/v1/gates/pending to verify active gate tracking
	reqPending, _ := http.NewRequest(http.MethodGet, fix.serverURL+"/api/v1/gates/pending", nil)
	respPending, err := client.Do(reqPending)
	if err != nil {
		t.Fatalf("failed to query pending gates: %v", err)
	}
	defer respPending.Body.Close()

	var pendingResp struct {
		Gates []struct {
			RunID string `json:"run_id"`
			Token string `json:"token"`
		} `json:"gates"`
		Total int `json:"total"`
	}
	_ = json.NewDecoder(respPending.Body).Decode(&pendingResp)
	if pendingResp.Total != 1 || len(pendingResp.Gates) != 1 {
		t.Fatalf("expected 1 pending gate, got %d", pendingResp.Total)
	}
	if pendingResp.Gates[0].RunID != taskID {
		t.Errorf("expected pending gate RunID %q, got %q", taskID, pendingResp.Gates[0].RunID)
	}

	// 6. Test Gate Resolution with Invalid HMAC Token -> Must Return 401 Unauthorized
	invalidPayload := map[string]string{
		"run_id":   taskID,
		"token":    "tampered-invalid-hmac-token",
		"decision": "approved",
	}
	invBytes, _ := json.Marshal(invalidPayload)
	reqInv, _ := http.NewRequest(http.MethodPost, fix.serverURL+"/api/v1/gates/resolve", bytes.NewReader(invBytes))
	reqInv.Header.Set("Content-Type", "application/json")
	respInv, err := client.Do(reqInv)
	if err != nil {
		t.Fatalf("failed to post invalid gate resolution: %v", err)
	}
	defer respInv.Body.Close()
	if respInv.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401 for invalid token, got %d", respInv.StatusCode)
	}

	// 7. Resolve Gate via Valid HMAC Token on POST /api/v1/gates/resolve
	validPayload := map[string]string{
		"run_id":   taskID,
		"token":    token,
		"decision": "approved",
	}
	valBytes, _ := json.Marshal(validPayload)
	reqVal, _ := http.NewRequest(http.MethodPost, fix.serverURL+"/api/v1/gates/resolve", bytes.NewReader(valBytes))
	reqVal.Header.Set("Content-Type", "application/json")
	respVal, err := client.Do(reqVal)
	if err != nil {
		t.Fatalf("failed to post valid gate resolution: %v", err)
	}
	defer respVal.Body.Close()

	if respVal.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respVal.Body)
		t.Fatalf("expected HTTP 200 from valid gate resolution, got %d. Body: %s", respVal.StatusCode, string(body))
	}

	// 8. Verify Status Update to Approved via `ov-cli factory status`
	stdout, stderr, exitCode := runCLI(t, "factory", "status", taskID, "--server", fix.serverURL)
	if exitCode != 0 {
		t.Fatalf("ov-cli factory status failed with exit code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	if !strings.Contains(stdout, "COMPLETED") && !strings.Contains(stdout, "APPROVED") {
		t.Errorf("expected status output to show resolved completed/approved, got: %s", stdout)
	}

	// 9. Verify GET /api/v1/gates/pending is now empty
	reqEmpty, _ := http.NewRequest(http.MethodGet, fix.serverURL+"/api/v1/gates/pending", nil)
	respEmpty, _ := client.Do(reqEmpty)
	defer respEmpty.Body.Close()
	var emptyResp struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(respEmpty.Body).Decode(&emptyResp)
	if emptyResp.Total != 0 {
		t.Errorf("expected 0 pending gates after resolution, got %d", emptyResp.Total)
	}
}

// ============================================================================
// E2E TEST SCENARIO 3:
// `ov-cli factory run "<title>" --sync` against test server verifying full
// outcome table, iterations, and duration.
// ============================================================================

func TestE2E_FactoryPipeline_CLIRunSyncOutcomeTable(t *testing.T) {
	fix := setupFactoryServer(t, serverOptions{})

	title := "Implement Parallel Multi-Agent Task Dispatcher"

	// Dispatch run via CLI with --sync
	stdout, stderr, exitCode := runCLI(t,
		"factory", "run", title,
		"--sync",
		"--server", fix.serverURL,
		"--description", "Build concurrent executor for autonomous agent pipeline",
		"--labels", "core,orchestrator,concurrency",
		"--constraints", "zero-alloc,no-locks",
	)

	if exitCode != 0 {
		t.Fatalf("ov-cli factory run --sync returned non-zero code %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	}

	// Assertions verifying the formatted outcome table
	// 1. Table header banner
	if !strings.Contains(stdout, "AUTONOMOUS FACTORY PIPELINE EXECUTION") {
		t.Errorf("expected outcome table banner 'AUTONOMOUS FACTORY PIPELINE EXECUTION' in output:\n%s", stdout)
	}

	// 2. Status badge
	if !strings.Contains(stdout, "COMPLETED") {
		t.Errorf("expected status badge 'COMPLETED' in output:\n%s", stdout)
	}

	// 3. Decision
	if !strings.Contains(stdout, "APPROVED") {
		t.Errorf("expected decision 'APPROVED' in output:\n%s", stdout)
	}

	// 4. Iterations field
	if !strings.Contains(stdout, "Total Iterations:") {
		t.Errorf("expected 'Total Iterations:' field in output:\n%s", stdout)
	}

	// 5. Duration field
	if !strings.Contains(stdout, "Duration:") {
		t.Errorf("expected 'Duration:' field in output:\n%s", stdout)
	}

	// 6. Diff summary
	if !strings.Contains(stdout, "Diff Summary:") {
		t.Errorf("expected 'Diff Summary:' field in output:\n%s", stdout)
	}
}
