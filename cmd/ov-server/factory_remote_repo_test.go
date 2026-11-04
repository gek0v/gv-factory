// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/store"
)

// mockPRCreator records CreatePullRequest calls for unit testing.
type mockPRCreator struct {
	mu           sync.Mutex
	called       bool
	lastRepo     string
	lastHead     string
	lastBase     string
	lastTitle    string
	lastBody     string
	returnURL    string
	returnNumber int
	returnErr    error
}

func (m *mockPRCreator) CreatePullRequest(ctx context.Context, repo, head, base, title, body string) (*inbound.PullRequestResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.lastRepo = repo
	m.lastHead = head
	m.lastBase = base
	m.lastTitle = title
	m.lastBody = body

	if m.returnErr != nil {
		return nil, m.returnErr
	}
	url := m.returnURL
	if url == "" {
		url = fmt.Sprintf("https://github.com/%s/pull/42", repo)
	}
	num := m.returnNumber
	if num == 0 {
		num = 42
	}
	return &inbound.PullRequestResponse{
		ID:      int64(num),
		Number:  num,
		HTMLURL: url,
		URL:     url,
		State:   "open",
		Title:   title,
		Body:    body,
	}, nil
}

// helper to create a valid bare git repository with a committed 'main' branch.
func createTestBareRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	seedDir := filepath.Join(tempDir, "seed")
	bareDir := filepath.Join(tempDir, "remote.git")

	if err := os.MkdirAll(seedDir, 0755); err != nil {
		t.Fatalf("failed to create seed directory: %v", err)
	}

	runCmd := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s failed: %v, out: %s", args, dir, err, string(out))
		}
	}

	runCmd(seedDir, "init")
	runCmd(seedDir, "config", "user.name", "Test Committer")
	runCmd(seedDir, "config", "user.email", "test@gv-factory.local")
	readmePath := filepath.Join(seedDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Remote Repo Test\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runCmd(seedDir, "add", "README.md")
	runCmd(seedDir, "commit", "-m", "initial seed commit")
	runCmd(seedDir, "branch", "-M", "main")

	runCmd(tempDir, "clone", "--bare", seedDir, bareDir)
	return bareDir
}

func TestRemoteRepo_InitGitSandbox_ClonesBareRepository(t *testing.T) {
	ctx := context.Background()
	bareRepo := createTestBareRepo(t)
	sandboxDir := filepath.Join(t.TempDir(), "sandbox")
	taskID := "task-remote-100"

	opts := GitSandboxOptions{
		RepositoryURL: bareRepo,
	}

	err := initGitSandbox(ctx, sandboxDir, taskID, "main", opts)
	if err != nil {
		t.Fatalf("initGitSandbox failed: %v", err)
	}

	// Verify README was cloned
	readmeContent, err := os.ReadFile(filepath.Join(sandboxDir, "README.md"))
	if err != nil {
		t.Fatalf("expected README.md in sandbox: %v", err)
	}
	if !strings.Contains(string(readmeContent), "Remote Repo Test") {
		t.Errorf("unexpected README content: %s", string(readmeContent))
	}

	// Verify current branch is ov-run/task-remote-100
	branchOut, err := runGitCmd(ctx, sandboxDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if branchOut != fmt.Sprintf("ov-run/%s", taskID) {
		t.Errorf("expected branch ov-run/%s, got %s", taskID, branchOut)
	}

	// Verify user.name and user.email
	userName, _ := runGitCmd(ctx, sandboxDir, "config", "user.name")
	if userName != "GV Factory Bot" {
		t.Errorf("expected user.name 'GV Factory Bot', got %q", userName)
	}
	userEmail, _ := runGitCmd(ctx, sandboxDir, "config", "user.email")
	if userEmail != "bot@gv-factory.local" {
		t.Errorf("expected user.email 'bot@gv-factory.local', got %q", userEmail)
	}

	// Second invocation should be idempotent
	if err := initGitSandbox(ctx, sandboxDir, taskID, "main", opts); err != nil {
		t.Fatalf("second call to initGitSandbox failed: %v", err)
	}
}

func TestRemoteRepo_PushBranchToOrigin(t *testing.T) {
	ctx := context.Background()
	bareRepo := createTestBareRepo(t)
	sandboxDir := filepath.Join(t.TempDir(), "sandbox-push")
	taskID := "task-push-200"

	err := initGitSandbox(ctx, sandboxDir, taskID, "main", GitSandboxOptions{
		RepositoryURL: bareRepo,
	})
	if err != nil {
		t.Fatalf("initGitSandbox failed: %v", err)
	}

	// Create and commit a new file in sandbox
	newFilePath := filepath.Join(sandboxDir, "feature.go")
	if err := os.WriteFile(newFilePath, []byte("// New feature\npackage main\n"), 0644); err != nil {
		t.Fatalf("failed to write feature.go: %v", err)
	}
	_, _ = runGitCmd(ctx, sandboxDir, "add", "feature.go")
	_, err = runGitCmd(ctx, sandboxDir, "commit", "-m", "feat: add feature")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	mockPR := &mockPRCreator{returnURL: "https://example.com/pr/1"}
	prResp, err := handleRemoteRepoPushAndPR(
		ctx,
		sandboxDir,
		taskID,
		"Add feature",
		"Detailed description of feature",
		bareRepo,
		"main",
		"+package main",
		"",
		mockPR,
		"Approved",
	)
	if err != nil {
		t.Fatalf("handleRemoteRepoPushAndPR failed: %v", err)
	}

	// Verify branch was pushed to bare repo
	branchRef := fmt.Sprintf("ov-run/%s", taskID)
	out, err := runGitCmd(ctx, bareRepo, "rev-parse", "--verify", branchRef)
	if err != nil {
		t.Fatalf("branch %s not found in remote bare repo: %v, out: %s", branchRef, err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected commit SHA for branch %s in bare repo", branchRef)
	}

	// For local bare repo, mockPR is still called when provided
	if prResp == nil {
		t.Errorf("expected prResp from handleRemoteRepoPushAndPR")
	}
}

func TestRemoteRepo_AuthInjectionAndMasking(t *testing.T) {
	// Test injectAuthToURL
	ghURL := "https://github.com/gv-factory/gv-factory.git"
	injectedGH := injectAuthToURL(ghURL, "ghp_secretToken123")
	if !strings.Contains(injectedGH, "x-access-token:ghp_secretToken123@") {
		t.Errorf("expected GitHub token injected, got: %s", injectedGH)
	}

	glURL := "https://gitlab.com/group/repo.git"
	injectedGL := injectAuthToURL(glURL, "glpat_gitlabToken456")
	if !strings.Contains(injectedGL, "oauth2:glpat_gitlabToken456@") {
		t.Errorf("expected GitLab token injected, got: %s", injectedGL)
	}

	localURL := "/tmp/repo.git"
	if res := injectAuthToURL(localURL, "secret"); res != localURL {
		t.Errorf("expected local path unchanged, got: %s", res)
	}

	if res := injectAuthToURL(ghURL, ""); res != ghURL {
		t.Errorf("expected empty token unchanged, got: %s", res)
	}

	// Test maskCredentials
	masked := maskCredentials("Failed to clone https://x-access-token:ghp_secretToken123@github.com/repo.git")
	if strings.Contains(masked, "ghp_secretToken123") {
		t.Errorf("maskCredentials failed to mask token in URL: %s", masked)
	}
	if !strings.Contains(masked, "***") {
		t.Errorf("expected *** in masked URL: %s", masked)
	}

	maskedExtra := maskCredentials("Error with sensitive secretToken456 occurred", "secretToken456")
	if strings.Contains(maskedExtra, "secretToken456") {
		t.Errorf("maskCredentials failed to mask extra sensitive token: %s", maskedExtra)
	}

	// Test extractAuthToken
	meta := map[string]any{"github_token": "token_from_meta"}
	if tok := extractAuthToken(meta, "https://github.com/foo/bar"); tok != "token_from_meta" {
		t.Errorf("expected 'token_from_meta', got %q", tok)
	}

	metaGL := map[string]any{"gitlab_token": "gl_token_from_meta"}
	if tok := extractAuthToken(metaGL, "https://gitlab.com/foo/bar"); tok != "gl_token_from_meta" {
		t.Errorf("expected 'gl_token_from_meta', got %q", tok)
	}

	t.Setenv("GITHUB_TOKEN", "env_github_token_xyz")
	if tok := extractAuthToken(nil, "https://github.com/foo/bar"); tok != "env_github_token_xyz" {
		t.Errorf("expected 'env_github_token_xyz', got %q", tok)
	}

	t.Setenv("GITLAB_TOKEN", "env_gitlab_token_abc")
	if tok := extractAuthToken(nil, "https://gitlab.com/foo/bar"); tok != "env_gitlab_token_abc" {
		t.Errorf("expected 'env_gitlab_token_abc', got %q", tok)
	}
}

func TestRemoteRepo_ParseRepoOwnerAndName(t *testing.T) {
	cases := []struct {
		url          string
		wantProvider string
		wantPath     string
		wantErr      bool
	}{
		{
			url:          "https://github.com/gv-factory/gv-factory.git",
			wantProvider: "github",
			wantPath:     "gv-factory/gv-factory",
		},
		{
			url:          "https://github.com/gv-factory/gv-factory",
			wantProvider: "github",
			wantPath:     "gv-factory/gv-factory",
		},
		{
			url:          "git@github.com:gv-factory/gv-factory.git",
			wantProvider: "github",
			wantPath:     "gv-factory/gv-factory",
		},
		{
			url:          "https://gitlab.com/group/subgroup/project.git",
			wantProvider: "gitlab",
			wantPath:     "group/subgroup/project",
		},
		{
			url:          "git@gitlab.com:mygroup/myproject.git",
			wantProvider: "gitlab",
			wantPath:     "mygroup/myproject",
		},
		{
			url:          "/var/git/local-repo.git",
			wantProvider: "local",
			wantPath:     "",
		},
		{
			url:     "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		prov, p, err := parseRepoOwnerAndName(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseRepoOwnerAndName(%q) expected error, got nil", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseRepoOwnerAndName(%q) unexpected error: %v", tc.url, err)
			continue
		}
		if prov != tc.wantProvider {
			t.Errorf("parseRepoOwnerAndName(%q) provider = %q, want %q", tc.url, prov, tc.wantProvider)
		}
		if p != tc.wantPath {
			t.Errorf("parseRepoOwnerAndName(%q) path = %q, want %q", tc.url, p, tc.wantPath)
		}
	}
}

func TestRemoteRepo_POSTRun_EndToEndWithPR(t *testing.T) {
	gw := gateway.NewGateway()
	bareRepo := createTestBareRepo(t)
	mockPR := &mockPRCreator{
		returnURL:    "https://github.com/gv-factory/test-repo/pull/77",
		returnNumber: 77,
	}

	runner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			// Simulate real builder writing a file
			sandboxBaseDir := filepath.Join(os.TempDir(), "ov-sandboxes")
			sandboxDir := filepath.Join(sandboxBaseDir, sanitizeIdentifier(task.TaskID))
			_ = os.WriteFile(filepath.Join(sandboxDir, "added.txt"), []byte("automated build"), 0644)
			_, _ = runGitCmd(ctx, sandboxDir, "add", "added.txt")
			_, _ = runGitCmd(ctx, sandboxDir, "commit", "-m", "feat: automated build")

			return &agent.PipelineOutcome{
				TaskID:          task.TaskID,
				Success:         true,
				Decision:        agent.DecisionApproved,
				TotalIterations: 1,
				Duration:        100 * time.Millisecond,
				BuildResult: &agent.BuildResult{
					TaskID:        task.TaskID,
					Success:       true,
					FilesModified: []string{"added.txt"},
					GitDiff:       "+automated build",
				},
			}, nil
		},
	}

	mux := http.NewServeMux()
	tracker := RegisterFactoryEndpointsWithOptions(mux, gw, runner, FactoryEndpointsOptions{
		PRCreator: mockPR,
	})

	// Pre-init sandbox to clone bareRepo
	taskID := "task-sync-run-300"
	sandboxDir := filepath.Join(os.TempDir(), "ov-sandboxes", taskID)
	_ = os.RemoveAll(sandboxDir)
	if err := initGitSandbox(context.Background(), sandboxDir, taskID, "main", GitSandboxOptions{
		RepositoryURL: bareRepo,
	}); err != nil {
		t.Fatalf("failed initGitSandbox: %v", err)
	}

	reqBody := FactoryRunRequest{
		TaskID:        taskID,
		Title:         "Add new automated capability",
		Description:   "Adds added.txt to the repository",
		RepositoryURL: "https://github.com/gv-factory/test-repo.git",
		BaseBranch:    "main",
		Metadata: map[string]any{
			"github_token": "test-gh-token",
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify PR was created
	mockPR.mu.Lock()
	called := mockPR.called
	lastRepo := mockPR.lastRepo
	lastHead := mockPR.lastHead
	lastBase := mockPR.lastBase
	mockPR.mu.Unlock()

	if !called {
		t.Errorf("expected PRCreator.CreatePullRequest to be called")
	}
	if lastRepo != "gv-factory/test-repo" {
		t.Errorf("expected repo 'gv-factory/test-repo', got %q", lastRepo)
	}
	if lastHead != fmt.Sprintf("ov-run/%s", taskID) {
		t.Errorf("expected head ov-run/%s, got %q", taskID, lastHead)
	}
	if lastBase != "main" {
		t.Errorf("expected base 'main', got %q", lastBase)
	}

	// Verify tracker record
	rec, found := tracker.Get(taskID)
	if !found {
		t.Fatalf("run %s not found in tracker", taskID)
	}
	if rec.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", rec.Status)
	}
	if rec.PullRequestURL != "https://github.com/gv-factory/test-repo/pull/77" {
		t.Errorf("unexpected PullRequestURL: %s", rec.PullRequestURL)
	}
	if rec.RepositoryURL != "https://github.com/gv-factory/test-repo.git" {
		t.Errorf("unexpected RepositoryURL: %s", rec.RepositoryURL)
	}
}

// mockFactoryRunnerWithPR implements FactoryPipelineRunner and PRCreatorProvider.
type mockFactoryRunnerWithPR struct {
	runFn     func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error)
	prCreator inbound.PullRequestCreator
}

func (m *mockFactoryRunnerWithPR) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	if m.runFn != nil {
		return m.runFn(ctx, task)
	}
	return &agent.PipelineOutcome{TaskID: task.TaskID, Success: true}, nil
}

func (m *mockFactoryRunnerWithPR) PRCreator() inbound.PullRequestCreator {
	return m.prCreator
}

func TestRemoteRepo_GateResolve_Approved_PushesAndCreatesPR(t *testing.T) {
	gw := gateway.NewGateway()
	st := store.NewMemoryStore()
	authSvc := auth.NewService("ov-test-secret-32-bytes-long-abc")
	mockPR := &mockPRCreator{
		returnURL: "https://github.com/gv-factory/gate-repo/pull/99",
	}

	bareRepo := createTestBareRepo(t)
	runID := "task-gate-resolve-400"
	sandboxDir := filepath.Join(os.TempDir(), "ov-sandboxes", runID)
	_ = os.RemoveAll(sandboxDir)
	_ = initGitSandbox(context.Background(), sandboxDir, runID, "main", GitSandboxOptions{
		RepositoryURL: bareRepo,
	})

	// Make a commit in sandbox
	_ = os.WriteFile(filepath.Join(sandboxDir, "patch.txt"), []byte("gate fix\n"), 0644)
	_, _ = runGitCmd(context.Background(), sandboxDir, "add", "patch.txt")
	_, _ = runGitCmd(context.Background(), sandboxDir, "commit", "-m", "fix: patch for gate")

	runner := &mockFactoryRunnerWithPR{
		prCreator: mockPR,
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			return &agent.PipelineOutcome{
				TaskID:   task.TaskID,
				Success:  false,
				Decision: agent.DecisionHumanGate,
				BuildResult: &agent.BuildResult{
					TaskID:  task.TaskID,
					GitDiff: "+patch for gate",
				},
				Approval: &agent.ApprovalDecision{
					DecisionType: agent.DecisionHumanGate,
					GateReason:   "Needs approval",
				},
			}, nil
		},
	}

	serverHandler := NewServerMuxWithRunner(nil, gw, nil, nil, nil, authSvc, st, nil, runner)

	// Step 1: POST /api/v1/factory/run to initiate run and track gate_waiting
	runPayload := FactoryRunRequest{
		TaskID:        runID,
		Title:         "Task requiring gate approval",
		RepositoryURL: bareRepo,
		BaseBranch:    "main",
	}
	runBody, _ := json.Marshal(runPayload)
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewReader(runBody))
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	serverHandler.ServeHTTP(postW, postReq)

	// Retrieve approval token from GET /api/v1/factory/runs/:id
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/"+runID, nil)
	getW := httptest.NewRecorder()
	serverHandler.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on GET run, got %d: %s", getW.Code, getW.Body.String())
	}

	var runResp FactoryRunResponse
	if err := json.Unmarshal(getW.Body.Bytes(), &runResp); err != nil {
		t.Fatalf("failed to parse run response: %v", err)
	}
	if runResp.Status != "gate_waiting" || runResp.Token == "" {
		t.Fatalf("expected gate_waiting with token, got status=%s token=%s", runResp.Status, runResp.Token)
	}

	// Step 2: Resolve the gate with approval
	resolvePayload := map[string]any{
		"run_id":   runID,
		"token":    runResp.Token,
		"decision": "approved",
	}
	resolveBody, _ := json.Marshal(resolvePayload)

	req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(resolveBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serverHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on /api/gate/resolve, got %d: %s", w.Code, w.Body.String())
	}

	// Verify branch was pushed to bare repo
	branchRef := fmt.Sprintf("ov-run/%s", runID)
	out, err := runGitCmd(context.Background(), bareRepo, "rev-parse", "--verify", branchRef)
	if err != nil {
		t.Fatalf("branch %s not pushed to remote bare repo: %v, out: %s", branchRef, err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected branch SHA in bare repo")
	}

	// Verify status updated to completed
	getFinalReq := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/"+runID, nil)
	getFinalW := httptest.NewRecorder()
	serverHandler.ServeHTTP(getFinalW, getFinalReq)
	var finalResp FactoryRunResponse
	_ = json.Unmarshal(getFinalW.Body.Bytes(), &finalResp)
	if finalResp.Status != "completed" {
		t.Errorf("expected final status 'completed', got %q", finalResp.Status)
	}
}

func TestRemoteRepo_EdgeCasesAndErrors(t *testing.T) {
	ctx := context.Background()

	// Empty repository_url in handleRemoteRepoPushAndPR returns nil
	res, err := handleRemoteRepoPushAndPR(ctx, "/tmp", "t1", "title", "desc", "", "main", "", "", nil, "")
	if res != nil || err != nil {
		t.Errorf("expected nil, nil for empty repo URL, got %v, %v", res, err)
	}

	// Non-git directory fails push
	invalidDir := t.TempDir()
	_, err = handleRemoteRepoPushAndPR(ctx, invalidDir, "t2", "title", "desc", "https://github.com/foo/bar.git", "main", "", "tok", nil, "")
	if err == nil {
		t.Errorf("expected error pushing from non-git directory, got nil")
	}

	// PR creator returns error
	failingPR := &mockPRCreator{returnErr: errors.New("github api rate limit")}
	bareRepo := createTestBareRepo(t)
	sandboxDir := filepath.Join(t.TempDir(), "sandbox-err")
	_ = initGitSandbox(ctx, sandboxDir, "t3", "main", GitSandboxOptions{RepositoryURL: bareRepo})
	_, _ = runGitCmd(ctx, sandboxDir, "commit", "--allow-empty", "-m", "empty")

	_, err = handleRemoteRepoPushAndPR(ctx, sandboxDir, "t3", "title", "desc", bareRepo, "main", "", "tok", failingPR, "")
	if err == nil {
		t.Errorf("expected error when PR creator fails, got nil")
	}
}

func TestRemoteRepo_CoverageAndHelpers(t *testing.T) {
	ctx := context.Background()

	// 1. PRCreator on defaultFactoryRunner
	var nilRunner *defaultFactoryRunner
	if nilRunner.PRCreator() != nil {
		t.Errorf("expected nil PRCreator on nil runner")
	}
	mockPR := &mockPRCreator{}
	dRunner := &defaultFactoryRunner{prCreator: mockPR}
	if dRunner.PRCreator() != mockPR {
		t.Errorf("expected mockPR on runner")
	}

	// 2. extractAuthToken coverage
	if tok := extractAuthToken(map[string]any{"git_token": "gtok"}, "http://example.com"); tok != "gtok" {
		t.Errorf("expected 'gtok', got %q", tok)
	}
	if tok := extractAuthToken(map[string]any{"token": "tok1"}, "http://example.com"); tok != "tok1" {
		t.Errorf("expected 'tok1', got %q", tok)
	}
	if tok := extractAuthToken(map[string]any{"auth_token": "atok"}, "http://example.com"); tok != "atok" {
		t.Errorf("expected 'atok', got %q", tok)
	}
	t.Setenv("GH_TOKEN", "ghtok")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	if tok := extractAuthToken(nil, "https://github.com/a/b"); tok != "ghtok" {
		t.Errorf("expected 'ghtok', got %q", tok)
	}

	// 3. handleRemoteRepoPushAndPR dynamic creator instantiation
	bareRepo := createTestBareRepo(t)
	sandboxDir := filepath.Join(t.TempDir(), "sandbox-dyn")
	_ = initGitSandbox(ctx, sandboxDir, "t-dyn", "main", GitSandboxOptions{RepositoryURL: bareRepo})
	_, _ = runGitCmd(ctx, sandboxDir, "commit", "--allow-empty", "-m", "empty")

	// Dynamic GitHub reporter instantiation branch
	t.Setenv("GITHUB_API_URL", "http://127.0.0.1:54321")
	_, _ = handleRemoteRepoPushAndPR(ctx, sandboxDir, "t-dyn", "Title", "Desc", "https://github.com/foo/bar.git", "main", "+diff", "ghp_fake", nil, "")

	// Dynamic GitLab reporter instantiation branch
	t.Setenv("GITLAB_API_URL", "http://127.0.0.1:54322")
	_, _ = handleRemoteRepoPushAndPR(ctx, sandboxDir, "t-dyn", "Title", "Desc", "https://gitlab.com/foo/bar.git", "main", "+diff", "glpat_fake", nil, "")

	// 4. DefaultRunner SandboxDir fallback
	defR := &defaultFactoryRunner{}
	sd := defR.SandboxDir("test-run")
	if !strings.Contains(sd, "test-run") {
		t.Errorf("expected test-run in sandbox dir, got: %s", sd)
	}
}
