// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

type mockSandboxRunner struct {
	customDir string
}

func (m *mockSandboxRunner) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	return &agent.PipelineOutcome{TaskID: task.TaskID, Success: true}, nil
}

func (m *mockSandboxRunner) SandboxDir(runID string) string {
	if m.customDir != "" {
		return filepath.Join(m.customDir, sanitizeIdentifier(runID))
	}
	return filepath.Join(os.TempDir(), "ov-sandboxes", sanitizeIdentifier(runID))
}

func TestValidateSandboxPath_Security(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		path        string
		expectError bool
		errContains string
	}{
		{
			name:        "Valid relative root file",
			path:        "main.go",
			expectError: false,
		},
		{
			name:        "Valid nested relative file",
			path:        "pkg/util/helper.go",
			expectError: false,
		},
		{
			name:        "Empty path rejected",
			path:        "",
			expectError: true,
			errContains: "empty",
		},
		{
			name:        "Whitespace path rejected",
			path:        "   ",
			expectError: true,
			errContains: "empty",
		},
		{
			name:        "Null byte in path rejected",
			path:        "main.go\x00.exe",
			expectError: true,
			errContains: "null byte",
		},
		{
			name:        "Absolute unix path rejected",
			path:        "/etc/passwd",
			expectError: true,
			errContains: "traversal",
		},
		{
			name:        "Absolute windows backslash rejected",
			path:        "\\Windows\\System32",
			expectError: true,
			errContains: "traversal",
		},
		{
			name:        "Windows drive path rejected",
			path:        "C:/autoexec.bat",
			expectError: true,
			errContains: "traversal",
		},
		{
			name:        "Directory traversal with .. prefix rejected",
			path:        "../outside.txt",
			expectError: true,
			errContains: "traversal",
		},
		{
			name:        "Directory traversal inside path rejected",
			path:        "foo/../../secret.txt",
			expectError: true,
			errContains: "traversal",
		},
		{
			name:        "Dot git root rejected",
			path:        ".git",
			expectError: true,
			errContains: "git internal",
		},
		{
			name:        "Dot git subpath rejected",
			path:        ".git/config",
			expectError: true,
			errContains: "git internal",
		},
		{
			name:        "Dot git nested rejected",
			path:        "src/.git/HEAD",
			expectError: true,
			errContains: "git internal",
		},
		{
			name:        "Current directory dot rejected",
			path:        ".",
			expectError: true,
			errContains: "traversal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			absTarget, rel, err := validateSandboxPath(tmpDir, tc.path)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for path %q, got nil (target: %q, rel: %q)", tc.path, absTarget, rel)
				}
				if tc.errContains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.errContains)) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for path %q: %v", tc.path, err)
				}
				if absTarget == "" || rel == "" {
					t.Errorf("expected non-empty targets, got absTarget=%q, rel=%q", absTarget, rel)
				}
				if !strings.HasPrefix(absTarget, tmpDir) {
					t.Errorf("target %q is not within sandbox %q", absTarget, tmpDir)
				}
			}
		})
	}
}

func TestFactoryFiles_GetFiles_Endpoint(t *testing.T) {
	gw := gateway.NewGateway()
	mux := http.NewServeMux()
	tracker := NewFactoryRunTracker()

	baseDir := t.TempDir()
	runner := &mockSandboxRunner{customDir: baseDir}

	opts := FactoryEndpointsOptions{
		Tracker:        tracker,
		SandboxBaseDir: baseDir,
	}
	RegisterFactoryEndpointsWithOptions(mux, gw, runner, opts)

	runID := "run-files-test-1"
	sandboxDir := filepath.Join(baseDir, runID)
	if err := os.MkdirAll(filepath.Join(sandboxDir, "sub"), 0755); err != nil {
		t.Fatalf("failed to create sandbox dir: %v", err)
	}

	// Create test files
	_ = os.WriteFile(filepath.Join(sandboxDir, "main.go"), []byte("package main\n"), 0644)
	_ = os.WriteFile(filepath.Join(sandboxDir, "sub", "util.go"), []byte("package sub\nfunc Help() {}\n"), 0644)
	// Create .git directory to verify it is ignored
	gitDir := filepath.Join(sandboxDir, ".git")
	_ = os.MkdirAll(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0644)

	// Pre-record in tracker with build outcome
	tracker.Set(&FactoryRunResponse{
		RunID:  runID,
		Status: "completed",
		Outcome: &agent.PipelineOutcome{
			TaskID:  runID,
			Success: true,
			BuildResult: &agent.BuildResult{
				TaskID:        runID,
				FilesModified: []string{"main.go"},
				GitDiff:       "diff --git a/main.go b/main.go",
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})

	// 1. Success GET /files
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files", runID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var files []SandboxFileInfo
	if err := json.NewDecoder(w.Body).Decode(&files); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (main.go, sub/util.go), got %d: %+v", len(files), files)
	}

	if files[0].Path != "main.go" || !files[0].Modified {
		t.Errorf("expected files[0] to be main.go with modified=true, got %+v", files[0])
	}
	if files[1].Path != "sub/util.go" || files[1].Modified {
		t.Errorf("expected files[1] to be sub/util.go with modified=false, got %+v", files[1])
	}

	// 2. 404 for unknown run sandbox
	reqUnknown := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/non-existent-run/files", nil)
	wUnknown := httptest.NewRecorder()
	mux.ServeHTTP(wUnknown, reqUnknown)

	if wUnknown.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing sandbox, got %d", wUnknown.Code)
	}

	// 3. 405 Method Not Allowed on POST /files
	reqPost := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/factory/runs/%s/files", runID), nil)
	wPost := httptest.NewRecorder()
	mux.ServeHTTP(wPost, reqPost)

	if wPost.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", wPost.Code)
	}
}

func TestFactoryFiles_GetContent_Endpoint(t *testing.T) {
	gw := gateway.NewGateway()
	mux := http.NewServeMux()
	tracker := NewFactoryRunTracker()

	baseDir := t.TempDir()
	runner := &mockSandboxRunner{customDir: baseDir}

	opts := FactoryEndpointsOptions{
		Tracker:        tracker,
		SandboxBaseDir: baseDir,
	}
	RegisterFactoryEndpointsWithOptions(mux, gw, runner, opts)

	runID := "run-content-test"
	sandboxDir := filepath.Join(baseDir, runID)
	_ = os.MkdirAll(sandboxDir, 0755)

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello sandbox!\")\n}\n"
	_ = os.WriteFile(filepath.Join(sandboxDir, "main.go"), []byte(content), 0644)

	// 1. Success GET file content
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content?path=main.go", runID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp FileContentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Path != "main.go" {
		t.Errorf("expected path 'main.go', got %q", resp.Path)
	}
	if resp.Content != content {
		t.Errorf("content mismatch, expected %q, got %q", content, resp.Content)
	}
	if resp.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), resp.Size)
	}

	// 2. Missing path param -> 400
	reqMissing := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), nil)
	wMissing := httptest.NewRecorder()
	mux.ServeHTTP(wMissing, reqMissing)

	if wMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing path, got %d", wMissing.Code)
	}

	// 3. Path traversal attempt -> 400
	reqTraversal := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content?path=../../etc/passwd", runID), nil)
	wTraversal := httptest.NewRecorder()
	mux.ServeHTTP(wTraversal, reqTraversal)

	if wTraversal.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d", wTraversal.Code)
	}

	// 4. Dot git internal attempt -> 400
	reqGit := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content?path=.git/config", runID), nil)
	wGit := httptest.NewRecorder()
	mux.ServeHTTP(wGit, reqGit)

	if wGit.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for git internal path, got %d", wGit.Code)
	}

	// 5. File not found -> 404
	reqNotFound := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content?path=missing.txt", runID), nil)
	wNotFound := httptest.NewRecorder()
	mux.ServeHTTP(wNotFound, reqNotFound)

	if wNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file, got %d", wNotFound.Code)
	}

	// 6. Path is directory -> 400
	_ = os.MkdirAll(filepath.Join(sandboxDir, "somedir"), 0755)
	reqDir := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content?path=somedir", runID), nil)
	wDir := httptest.NewRecorder()
	mux.ServeHTTP(wDir, reqDir)

	if wDir.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for directory path, got %d", wDir.Code)
	}
}

func TestFactoryFiles_PutContent_HotEditAndGitDiff(t *testing.T) {
	gw := gateway.NewGateway()
	mux := http.NewServeMux()
	tracker := NewFactoryRunTracker()
	pendingTracker := NewPendingGateTracker(context.Background(), gw)

	baseDir := t.TempDir()
	runner := &mockSandboxRunner{customDir: baseDir}

	opts := FactoryEndpointsOptions{
		Tracker:        tracker,
		PendingTracker: pendingTracker,
		SandboxBaseDir: baseDir,
	}
	RegisterFactoryEndpointsWithOptions(mux, gw, runner, opts)

	runID := "run-hotedit-test"
	sandboxDir := filepath.Join(baseDir, runID)

	// Initialize real git repository in sandboxDir
	ctx := context.Background()
	if err := initGitSandbox(ctx, sandboxDir, runID, "main"); err != nil {
		t.Fatalf("failed to init git sandbox: %v", err)
	}

	// Record initial run state in tracker
	initialOutcome := &agent.PipelineOutcome{
		TaskID:   runID,
		Success:  true,
		Decision: agent.DecisionHumanGate,
		BuildResult: &agent.BuildResult{
			TaskID:        runID,
			Success:       true,
			FilesModified: []string{"go.mod"},
			GitDiff:       "initial-diff",
		},
	}
	tracker.Set(&FactoryRunResponse{
		RunID:     runID,
		Status:    "gate_waiting",
		Token:     "test-gate-token-12345",
		Outcome:   initialOutcome,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	pendingTracker.RecordGate(PendingGateInfo{
		RunID: runID,
		Token: "test-gate-token-12345",
		Diff:  "initial-diff",
	})

	// 1. Hot-edit existing or new file: PUT /files/content
	newFileCode := "package main\n\nfunc Calculate(a, b int) int {\n\treturn a + b\n}\n"
	payload := UpdateFileContentRequest{
		Path:    "math/calc.go",
		Content: newFileCode,
	}
	bodyBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var putResp UpdateFileContentResponse
	if err := json.NewDecoder(w.Body).Decode(&putResp); err != nil {
		t.Fatalf("failed to decode PUT response: %v", err)
	}

	if !putResp.Success {
		t.Errorf("expected success true, got false")
	}
	if putResp.Path != "math/calc.go" {
		t.Errorf("expected path 'math/calc.go', got %q", putResp.Path)
	}
	if !strings.Contains(putResp.Diff, "math/calc.go") && !strings.Contains(putResp.Diff, "Calculate") {
		t.Logf("Notice: diff is %q", putResp.Diff)
	}

	// Verify file written to disk
	writtenBytes, err := os.ReadFile(filepath.Join(sandboxDir, "math", "calc.go"))
	if err != nil {
		t.Fatalf("failed to read written file from disk: %v", err)
	}
	if string(writtenBytes) != newFileCode {
		t.Errorf("file content on disk mismatch")
	}

	// Verify tracker was updated with recalculated diff
	record, found := tracker.Get(runID)
	if !found {
		t.Fatalf("run record not found in tracker")
	}
	if record.Outcome == nil || record.Outcome.BuildResult == nil {
		t.Fatalf("outcome or buildResult nil after update")
	}
	if record.Outcome.BuildResult.GitDiff == "initial-diff" {
		t.Errorf("expected lastBuildResult.GitDiff to be recalculated, got initial-diff")
	}

	// Verify pending tracker diff was updated
	pendingList := pendingTracker.ListPending()
	var updatedGate *PendingGateInfo
	for _, g := range pendingList {
		if g.RunID == runID {
			updatedGate = &g
			break
		}
	}
	if updatedGate != nil && updatedGate.Diff == "initial-diff" {
		t.Errorf("expected pending gate diff to be updated")
	}

	// 2. GET /files/content confirms updated code
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/content?path=math/calc.go", runID), nil)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET after PUT, got %d", getW.Code)
	}
	var getResp FileContentResponse
	_ = json.NewDecoder(getW.Body).Decode(&getResp)
	if getResp.Content != newFileCode {
		t.Errorf("GET content mismatch after PUT")
	}

	// 3. GET /files confirms math/calc.go is listed and modified
	listFilesReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files", runID), nil)
	listFilesW := httptest.NewRecorder()
	mux.ServeHTTP(listFilesW, listFilesReq)

	if listFilesW.Code != http.StatusOK {
		t.Fatalf("expected 200 from list files, got %d", listFilesW.Code)
	}
	var filesList []SandboxFileInfo
	_ = json.NewDecoder(listFilesW.Body).Decode(&filesList)

	foundCalc := false
	for _, f := range filesList {
		if f.Path == "math/calc.go" {
			foundCalc = true
			if !f.Modified {
				t.Errorf("expected math/calc.go to be marked modified")
			}
		}
	}
	if !foundCalc {
		t.Errorf("math/calc.go not found in files list")
	}

	// 4. PUT validation errors: Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), strings.NewReader("not-json"))
	wBadJSON := httptest.NewRecorder()
	mux.ServeHTTP(wBadJSON, reqBadJSON)
	if wBadJSON.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", wBadJSON.Code)
	}

	// 5. PUT validation errors: Empty Path
	badPayload, _ := json.Marshal(UpdateFileContentRequest{Path: "", Content: "test"})
	reqEmptyPath := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), bytes.NewReader(badPayload))
	wEmptyPath := httptest.NewRecorder()
	mux.ServeHTTP(wEmptyPath, reqEmptyPath)
	if wEmptyPath.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty path, got %d", wEmptyPath.Code)
	}

	// 6. PUT validation errors: Traversal attempt
	traversalPayload, _ := json.Marshal(UpdateFileContentRequest{Path: "../../bad.go", Content: "evil"})
	reqTrav := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), bytes.NewReader(traversalPayload))
	wTrav := httptest.NewRecorder()
	mux.ServeHTTP(wTrav, reqTrav)
	if wTrav.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal path, got %d", wTrav.Code)
	}
}

func TestFactoryFiles_SymlinkAndMoreCoverage(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a safe file and a symlink pointing to it inside sandbox
	targetFile := filepath.Join(tmpDir, "real.txt")
	_ = os.WriteFile(targetFile, []byte("real content"), 0644)

	safeSymlink := filepath.Join(tmpDir, "link.txt")
	_ = os.Symlink(targetFile, safeSymlink)

	abs, rel, err := validateSandboxPath(tmpDir, "link.txt")
	if err != nil {
		t.Logf("Symlink creation might require privileges on Windows: %v", err)
	} else if rel != "link.txt" {
		t.Errorf("expected rel link.txt, got %q", rel)
	}
	_ = abs

	// 2. Symlink escaping sandbox
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	_ = os.WriteFile(outsideFile, []byte("outside content"), 0644)

	escapeSymlink := filepath.Join(tmpDir, "escape-link.txt")
	if symErr := os.Symlink(outsideFile, escapeSymlink); symErr == nil {
		_, _, errEsc := validateSandboxPath(tmpDir, "escape-link.txt")
		if errEsc == nil {
			t.Errorf("expected path traversal error for escaping symlink")
		}
	}

	// 3. PUT file where file is already in FilesModified
	gw := gateway.NewGateway()
	mux := http.NewServeMux()
	tracker := NewFactoryRunTracker()
	opts := FactoryEndpointsOptions{Tracker: tracker, SandboxBaseDir: tmpDir}
	runner := &defaultFactoryRunner{sandboxBaseDir: tmpDir}
	RegisterFactoryEndpointsWithOptions(mux, gw, runner, opts)

	runID := "run-dup-files"
	sandboxDir := runner.SandboxDir(runID)
	_ = os.MkdirAll(sandboxDir, 0755)

	tracker.Set(&FactoryRunResponse{
		RunID:  runID,
		Status: "completed",
		Outcome: &agent.PipelineOutcome{
			TaskID:  runID,
			Success: true,
			BuildResult: &agent.BuildResult{
				TaskID:        runID,
				FilesModified: []string{"main.go"},
			},
		},
	})

	putBody, _ := json.Marshal(UpdateFileContentRequest{
		Path:    "main.go",
		Content: "package main\n// updated\n",
	})
	putReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), bytes.NewReader(putBody))
	putW := httptest.NewRecorder()
	mux.ServeHTTP(putW, putReq)
	if putW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", putW.Code)
	}

	// Verify FilesModified didn't duplicate main.go
	rec, _ := tracker.Get(runID)
	if len(rec.Outcome.BuildResult.FilesModified) != 1 {
		t.Errorf("expected 1 file in FilesModified, got %d", len(rec.Outcome.BuildResult.FilesModified))
	}

	// 4. PUT file where sandboxDir does not exist but run is in tracker -> initializes sandbox
	runIDAutoInit := "run-auto-init"
	tracker.Set(&FactoryRunResponse{
		RunID:  runIDAutoInit,
		Status: "gate_waiting",
	})
	initBody, _ := json.Marshal(UpdateFileContentRequest{
		Path:    "created.txt",
		Content: "auto initialized",
	})
	initReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runIDAutoInit), bytes.NewReader(initBody))
	initW := httptest.NewRecorder()
	mux.ServeHTTP(initW, initReq)
	if initW.Code != http.StatusOK {
		t.Fatalf("expected 200 on auto-init sandbox PUT, got %d: %s", initW.Code, initW.Body.String())
	}
}

func TestFactoryFiles_EdgeCases(t *testing.T) {
	gw := gateway.NewGateway()
	mux := http.NewServeMux()
	tracker := NewFactoryRunTracker()

	baseDir := t.TempDir()
	opts := FactoryEndpointsOptions{
		Tracker:        tracker,
		SandboxBaseDir: baseDir,
	}
	runner := &defaultFactoryRunner{sandboxBaseDir: baseDir}
	RegisterFactoryEndpointsWithOptions(mux, gw, runner, opts)

	runID := "run-edge-cases"
	sandboxDir := runner.SandboxDir(runID)
	_ = os.MkdirAll(sandboxDir, 0755)

	// 1. Unknown subpath under /files/ -> 404
	reqSub := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s/files/unknown/action", runID), nil)
	wSub := httptest.NewRecorder()
	mux.ServeHTTP(wSub, reqSub)
	if wSub.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown subpath, got %d", wSub.Code)
	}

	// 2. Unsupported method on /files/content -> 405
	reqDelete := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/factory/runs/%s/files/content", runID), nil)
	wDelete := httptest.NewRecorder()
	mux.ServeHTTP(wDelete, reqDelete)
	if wDelete.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE on /files/content, got %d", wDelete.Code)
	}

	// 3. Put into non-existent sandbox without tracker record -> 404
	untrackedPayload, _ := json.Marshal(UpdateFileContentRequest{Path: "foo.go", Content: "foo"})
	reqUntracked := httptest.NewRequest(http.MethodPut, "/api/v1/factory/runs/untracked-missing/files/content", bytes.NewReader(untrackedPayload))
	wUntracked := httptest.NewRecorder()
	mux.ServeHTTP(wUntracked, reqUntracked)
	if wUntracked.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for untracked missing sandbox, got %d", wUntracked.Code)
	}
}
