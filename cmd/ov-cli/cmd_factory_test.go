// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
)

func TestFactoryCLI_UsageAndHelp(t *testing.T) {
	ctx := context.Background()

	t.Run("factory without args shows usage", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory"})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		outStr := out.String()
		if !strings.Contains(outStr, "run") || !strings.Contains(outStr, "status") || !strings.Contains(outStr, "list") {
			t.Errorf("expected usage with run, status, and list subcommands, got: %s", outStr)
		}
	})

	t.Run("factory --help shows usage", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "--help"})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("expected usage manual, got: %s", out.String())
		}
	})

	t.Run("factory -h shows usage", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "-h"})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("expected usage manual, got: %s", out.String())
		}
	})

	t.Run("factory help shows usage", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "help"})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("expected usage manual, got: %s", out.String())
		}
	})

	t.Run("factory unknown subcommand returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "unknown-cmd"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		comb := out.String() + errBuf.String()
		if !strings.Contains(comb, "unknown factory subcommand") {
			t.Errorf("expected unknown factory subcommand error, got: %s", comb)
		}
	})
}

func TestFactoryCLI_Run_Validation(t *testing.T) {
	ctx := context.Background()

	t.Run("run without title returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "run"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		if !strings.Contains(out.String(), "title is required") && !strings.Contains(errBuf.String(), "title is required") {
			t.Errorf("expected title is required error, got out=%q, err=%q", out.String(), errBuf.String())
		}
	})

	t.Run("run with empty string title returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "run", ""})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		if !strings.Contains(out.String(), "title is required") && !strings.Contains(errBuf.String(), "title is required") {
			t.Errorf("expected title is required error, got out=%q, err=%q", out.String(), errBuf.String())
		}
	})

	t.Run("run with whitespace only title returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "run", "   \t  "})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		if !strings.Contains(out.String(), "title is required") && !strings.Contains(errBuf.String(), "title is required") {
			t.Errorf("expected title is required error, got out=%q, err=%q", out.String(), errBuf.String())
		}
	})

	t.Run("run with unknown flag returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "run", "Fix auth issue", "--non-existent-flag"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
	})
}

func TestFactoryCLI_Run_Async(t *testing.T) {
	ctx := context.Background()

	t.Run("run default async output format and badges", func(t *testing.T) {
		var receivedReq FactoryRunRequest
		var receivedAsync string

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/run" {
				http.NotFound(w, r)
				return
			}
			receivedAsync = r.URL.Query().Get("async")
			if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": receivedReq.TaskID,
				"status": "running",
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Implement Token Auth",
			"--description", "JWT token implementation",
			"--labels", "security,auth",
			"--constraints", "no external deps,coverage >= 90",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		if receivedAsync != "true" && receivedAsync != "1" && receivedAsync != "" {
			t.Errorf("expected async query param to be true or default, got %q", receivedAsync)
		}
		if receivedReq.Title != "Implement Token Auth" {
			t.Errorf("expected title 'Implement Token Auth', got %q", receivedReq.Title)
		}
		if receivedReq.Description != "JWT token implementation" {
			t.Errorf("expected description 'JWT token implementation', got %q", receivedReq.Description)
		}
		if len(receivedReq.Labels) != 2 || receivedReq.Labels[0] != "security" || receivedReq.Labels[1] != "auth" {
			t.Errorf("unexpected labels: %v", receivedReq.Labels)
		}
		if len(receivedReq.Constraints) != 2 || receivedReq.Constraints[0] != "no external deps" {
			t.Errorf("unexpected constraints: %v", receivedReq.Constraints)
		}
		if receivedReq.TaskID == "" {
			t.Errorf("expected auto-generated task_id, got empty")
		}

		outStr := out.String()
		if !strings.Contains(outStr, receivedReq.TaskID) {
			t.Errorf("expected output to contain Run ID %q, got: %s", receivedReq.TaskID, outStr)
		}
		if !strings.Contains(outStr, "RUNNING") {
			t.Errorf("expected RUNNING badge in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "ov-cli factory status") {
			t.Errorf("expected tip command in output, got: %s", outStr)
		}
	})

	t.Run("run explicit --async with --json flag", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req FactoryRunRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": req.TaskID,
				"status": "running",
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Async Task Test",
			"--async",
			"--json",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid JSON response, got error: %v, out: %s", err, out.String())
		}
		if resp["status"] != "running" {
			t.Errorf("expected status 'running', got %v", resp["status"])
		}
		if _, ok := resp["run_id"]; !ok {
			t.Errorf("expected run_id in JSON, got %v", resp)
		}
	})
}

func TestFactoryCLI_Run_Sync(t *testing.T) {
	ctx := context.Background()

	outcome := agent.PipelineOutcome{
		TaskID:          "task-sync-101",
		Success:         true,
		Decision:        agent.DecisionApproved,
		TotalIterations: 2,
		Duration:        3500 * time.Millisecond,
		BuildResult: &agent.BuildResult{
			TaskID:        "task-sync-101",
			Success:       true,
			FilesModified: []string{"pkg/auth/token.go", "pkg/auth/token_test.go"},
			GitDiff:       "diff --git a/pkg/auth/token.go b/pkg/auth/token.go\n+ func Generate() string",
			TotalDuration: 2800 * time.Millisecond,
		},
	}

	t.Run("run --sync displays status table with required fields", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("async") != "false" {
				http.Error(w, "expected async=false", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Synchronous Pipeline Test",
			"--sync",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		outStr := out.String()
		// Must display: Task ID, Decision, Total Iterations, Duration, Diff summary
		if !strings.Contains(outStr, "task-sync-101") {
			t.Errorf("expected output to contain Task ID 'task-sync-101', got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "APPROVED") {
			t.Errorf("expected output to contain Decision 'APPROVED', got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "2") {
			t.Errorf("expected output to contain Total Iterations '2', got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "3.5") && !strings.Contains(outStr, "3500ms") && !strings.Contains(outStr, "3s") {
			t.Errorf("expected output to contain Duration, got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "pkg/auth/token.go") && !strings.Contains(outStr, "files modified") && !strings.Contains(outStr, "Diff") {
			t.Errorf("expected output to contain Diff summary, got:\n%s", outStr)
		}
	})

	t.Run("run --sync with --json outputs outcome JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Sync JSON Test",
			"--sync",
			"--json",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var received agent.PipelineOutcome
		if err := json.Unmarshal(out.Bytes(), &received); err != nil {
			t.Fatalf("expected valid JSON outcome, got error: %v, out: %s", err, out.String())
		}
		if received.TaskID != "task-sync-101" {
			t.Errorf("expected TaskID 'task-sync-101', got %q", received.TaskID)
		}
		if received.Decision != agent.DecisionApproved {
			t.Errorf("expected Decision 'APPROVED', got %q", received.Decision)
		}
	})

	t.Run("run --sync with failed outcome returns non-zero code", func(t *testing.T) {
		failedOutcome := agent.PipelineOutcome{
			TaskID:          "task-failed-001",
			Success:         false,
			Decision:        "FAILED",
			ErrorMessage:    "tests failed with 2 errors",
			TotalIterations: 3,
			Duration:        5 * time.Second,
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(failedOutcome)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Failing Run",
			"--sync",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected exit code 1 on pipeline failure, got %d", code)
		}
		if !strings.Contains(out.String(), "FAILED") {
			t.Errorf("expected FAILED badge in output, got: %s", out.String())
		}
		if !strings.Contains(out.String(), "tests failed with 2 errors") {
			t.Errorf("expected error message in output, got: %s", out.String())
		}
	})
}

func TestFactoryCLI_Run_ServerErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("server returns 500 error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "database connection lost"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Failing Server Run",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected exit code 1 on HTTP 500, got %d", code)
		}
		if !strings.Contains(errBuf.String(), "database connection lost") && !strings.Contains(out.String(), "database connection lost") {
			t.Errorf("expected error output to mention server error, got out=%q, err=%q", out.String(), errBuf.String())
		}
	})

	t.Run("server unreachable", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Network Unreachable Run",
			"--server", "http://127.0.0.1:59999",
		})

		if code != 1 {
			t.Fatalf("expected exit code 1 on network error, got %d", code)
		}
		if !strings.Contains(errBuf.String(), "connection refused") && !strings.Contains(errBuf.String(), "failed to connect") && !strings.Contains(errBuf.String(), "dial") {
			t.Logf("network error message: %s", errBuf.String())
		}
	})
}

func TestFactoryCLI_Status(t *testing.T) {
	ctx := context.Background()

	t.Run("status without run_id returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "status"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		if !strings.Contains(out.String(), "run-id argument is required") && !strings.Contains(errBuf.String(), "run-id argument is required") {
			t.Errorf("expected run-id required error, got out=%q, err=%q", out.String(), errBuf.String())
		}
	})

	t.Run("status COMPLETED displays formatted status table", func(t *testing.T) {
		now := time.Now().UTC()
		record := FactoryRunResponse{
			RunID:  "run-completed-123",
			Status: "completed",
			Outcome: &agent.PipelineOutcome{
				TaskID:          "run-completed-123",
				Success:         true,
				Decision:        agent.DecisionApproved,
				TotalIterations: 1,
				Duration:        4200 * time.Millisecond,
				BuildResult: &agent.BuildResult{
					FilesModified: []string{"main.go"},
					GitDiff:       "+ fmt.Println()",
				},
			},
			CreatedAt: now.Add(-5 * time.Second),
			UpdatedAt: now,
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/runs/run-completed-123" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(record)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-completed-123",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		outStr := out.String()
		if !strings.Contains(outStr, "run-completed-123") {
			t.Errorf("expected run-id in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "COMPLETED") {
			t.Errorf("expected COMPLETED status in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "APPROVED") {
			t.Errorf("expected APPROVED decision in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "4.2") && !strings.Contains(outStr, "4200ms") && !strings.Contains(outStr, "4s") && !strings.Contains(outStr, "5s") {
			t.Errorf("expected duration in output, got: %s", outStr)
		}
	})

	t.Run("status RUNNING displays badge", func(t *testing.T) {
		record := FactoryRunResponse{
			RunID:     "run-in-progress-456",
			Status:    "running",
			CreatedAt: time.Now().UTC().Add(-10 * time.Second),
			UpdatedAt: time.Now().UTC(),
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(record)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-in-progress-456",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "RUNNING") {
			t.Errorf("expected RUNNING badge in output, got: %s", out.String())
		}
	})

	t.Run("status GATE_WAITING displays badge", func(t *testing.T) {
		record := FactoryRunResponse{
			RunID:     "run-waiting-789",
			Status:    "gate_waiting",
			CreatedAt: time.Now().UTC().Add(-30 * time.Second),
			UpdatedAt: time.Now().UTC(),
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(record)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-waiting-789",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "GATE_WAITING") {
			t.Errorf("expected GATE_WAITING badge in output, got: %s", out.String())
		}
	})

	t.Run("status FAILED displays badge and error details", func(t *testing.T) {
		record := FactoryRunResponse{
			RunID:     "run-failed-000",
			Status:    "failed",
			Error:     "build failed: syntax error in main.go:24",
			CreatedAt: time.Now().UTC().Add(-15 * time.Second),
			UpdatedAt: time.Now().UTC(),
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(record)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-failed-000",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		outStr := out.String()
		if !strings.Contains(outStr, "FAILED") {
			t.Errorf("expected FAILED badge in output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "syntax error in main.go:24") {
			t.Errorf("expected error details in output, got: %s", outStr)
		}
	})

	t.Run("status with --json flag", func(t *testing.T) {
		record := FactoryRunResponse{
			RunID:  "run-json-111",
			Status: "completed",
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(record)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-json-111",
			"--json",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}

		var parsed FactoryRunResponse
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("expected valid JSON, got: %v", err)
		}
		if parsed.RunID != "run-json-111" {
			t.Errorf("expected RunID 'run-json-111', got %q", parsed.RunID)
		}
	})

	t.Run("status 404 not found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "run not found"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "non-existent-run",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected exit code 1 for not found, got %d", code)
		}
	})
}

func TestFactoryCLI_List(t *testing.T) {
	ctx := context.Background()

	now := time.Now().UTC()
	sampleRuns := []*FactoryRunResponse{
		{
			RunID:     "run-001",
			Status:    "completed",
			CreatedAt: now.Add(-10 * time.Minute),
			UpdatedAt: now.Add(-8 * time.Minute),
			Outcome: &agent.PipelineOutcome{
				TaskID:   "run-001",
				Success:  true,
				Decision: agent.DecisionApproved,
				Duration: 120 * time.Second,
			},
		},
		{
			RunID:     "run-002",
			Status:    "running",
			CreatedAt: now.Add(-2 * time.Minute),
			UpdatedAt: now,
		},
		{
			RunID:     "run-003",
			Status:    "failed",
			Error:     "compile failure",
			CreatedAt: now.Add(-30 * time.Minute),
			UpdatedAt: now.Add(-29 * time.Minute),
		},
	}

	t.Run("list displays formatted table", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/runs" && r.URL.Path != "/api/v1/runs" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(sampleRuns)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		outStr := out.String()
		if !strings.Contains(outStr, "run-001") || !strings.Contains(outStr, "run-002") || !strings.Contains(outStr, "run-003") {
			t.Errorf("expected all runs in list output, got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "COMPLETED") || !strings.Contains(outStr, "RUNNING") || !strings.Contains(outStr, "FAILED") {
			t.Errorf("expected status badges in list output, got:\n%s", outStr)
		}
	})

	t.Run("list with --limit 2", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(sampleRuns)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--limit", "2",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}

		outStr := out.String()
		if !strings.Contains(outStr, "run-001") || !strings.Contains(outStr, "run-002") {
			t.Errorf("expected run-001 and run-002 in list output, got:\n%s", outStr)
		}
		if strings.Contains(outStr, "run-003") {
			t.Errorf("expected run-003 to be excluded by --limit 2, got:\n%s", outStr)
		}
	})

	t.Run("list with --json outputs valid array", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(sampleRuns)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--json",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}

		var received []*FactoryRunResponse
		if err := json.Unmarshal(out.Bytes(), &received); err != nil {
			t.Fatalf("expected valid JSON array, got: %v, out: %s", err, out.String())
		}
		if len(received) != 3 {
			t.Errorf("expected 3 runs, got %d", len(received))
		}
	})

	t.Run("list empty displays empty notice", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*FactoryRunResponse{})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "No factory runs found") {
			t.Errorf("expected empty notice, got: %s", out.String())
		}
	})

	t.Run("list server error returns non-zero", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected exit code 1 on server error, got %d", code)
		}
	})
}

func TestFactoryCLI_EdgeCasesAndHelpers(t *testing.T) {
	t.Run("resolveServerURL fallback to env and default", func(t *testing.T) {
		orig := os.Getenv("OV_SERVER_URL")
		defer os.Setenv("OV_SERVER_URL", orig)

		// Explicit flag
		if u := resolveServerURL("http://explicit.com/"); u != "http://explicit.com" {
			t.Errorf("expected trimmed explicit url, got %q", u)
		}

		// Env var
		os.Setenv("OV_SERVER_URL", "http://env-server.com/")
		if u := resolveServerURL(""); u != "http://env-server.com" {
			t.Errorf("expected trimmed env url, got %q", u)
		}

		// Default
		os.Unsetenv("OV_SERVER_URL")
		if u := resolveServerURL(""); u != "http://localhost:8080" {
			t.Errorf("expected default url, got %q", u)
		}
	})

	t.Run("parseCommaSeparated variants", func(t *testing.T) {
		if res := parseCommaSeparated(""); res != nil {
			t.Errorf("expected nil for empty, got %v", res)
		}
		if res := parseCommaSeparated("  "); res != nil {
			t.Errorf("expected nil for spaces, got %v", res)
		}
		res := parseCommaSeparated("  a , , b, c  ")
		if len(res) != 3 || res[0] != "a" || res[1] != "b" || res[2] != "c" {
			t.Errorf("expected [a, b, c], got %v", res)
		}
	})

	t.Run("formatDuration branches", func(t *testing.T) {
		if s := formatDuration(0); s != "0s" {
			t.Errorf("expected '0s', got %q", s)
		}
		if s := formatDuration(-5 * time.Second); s != "0s" {
			t.Errorf("expected '0s', got %q", s)
		}
		if s := formatDuration(450 * time.Millisecond); s != "450ms" {
			t.Errorf("expected '450ms', got %q", s)
		}
		if s := formatDuration(1500 * time.Millisecond); s != "1.50s" {
			t.Errorf("expected '1.50s', got %q", s)
		}
		if s := formatDuration(90 * time.Second); s != "1m30s" {
			t.Errorf("expected '1m30s', got %q", s)
		}
	})

	t.Run("formatDecisionColor branches", func(t *testing.T) {
		c := NewColorizer()
		if s := formatDecisionColor(c, "APPROVED"); !strings.Contains(s, "APPROVED") {
			t.Errorf("expected APPROVED, got %q", s)
		}
		if s := formatDecisionColor(c, "HUMAN_GATE"); !strings.Contains(s, "HUMAN_GATE") {
			t.Errorf("expected HUMAN_GATE, got %q", s)
		}
		if s := formatDecisionColor(c, "GATE_WAITING"); !strings.Contains(s, "GATE_WAITING") {
			t.Errorf("expected GATE_WAITING, got %q", s)
		}
		if s := formatDecisionColor(c, "RETRY"); !strings.Contains(s, "RETRY") {
			t.Errorf("expected RETRY, got %q", s)
		}
		if s := formatDecisionColor(c, "FAILED"); !strings.Contains(s, "FAILED") {
			t.Errorf("expected FAILED, got %q", s)
		}
		if s := formatDecisionColor(c, "CUSTOM_VERDICT"); !strings.Contains(s, "CUSTOM_VERDICT") {
			t.Errorf("expected CUSTOM_VERDICT, got %q", s)
		}
	})

	t.Run("formatDiffSummary branches", func(t *testing.T) {
		if s := formatDiffSummary(nil); s != "none" {
			t.Errorf("expected 'none', got %q", s)
		}
		brEmpty := &agent.BuildResult{}
		if s := formatDiffSummary(brEmpty); s != "no diff detected" {
			t.Errorf("expected 'no diff detected', got %q", s)
		}
		brWithDeletions := &agent.BuildResult{
			GitDiff: "--- a/file.go\n+++ b/file.go\n- deleted line\n+ added line",
		}
		if s := formatDiffSummary(brWithDeletions); !strings.Contains(s, "+1 -1 diff lines") {
			t.Errorf("expected additions and deletions summary, got %q", s)
		}
	})

	t.Run("computeDurationStr branches", func(t *testing.T) {
		now := time.Now().UTC()
		// Case 1: Outcome duration set
		r1 := &FactoryRunResponse{
			Outcome: &agent.PipelineOutcome{Duration: 2 * time.Second},
		}
		if s := computeDurationStr(r1); s != "2.00s" {
			t.Errorf("expected '2.00s', got %q", s)
		}

		// Case 2: Outcome nil, CreatedAt and UpdatedAt set
		r2 := &FactoryRunResponse{
			CreatedAt: now.Add(-3 * time.Second),
			UpdatedAt: now,
		}
		if s := computeDurationStr(r2); s != "3.00s" {
			t.Errorf("expected '3.00s', got %q", s)
		}

		// Case 3: Zero times
		r3 := &FactoryRunResponse{}
		if s := computeDurationStr(r3); s != "n/a" {
			t.Errorf("expected 'n/a', got %q", s)
		}
	})
}

func TestFactoryCLI_Run_AdditionalEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("run with explicit --task-id", func(t *testing.T) {
		var receivedReq FactoryRunRequest
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"run_id": receivedReq.TaskID, "status": "running"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Task with explicit ID",
			"--task-id", "custom-id-999",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if receivedReq.TaskID != "custom-id-999" {
			t.Errorf("expected custom-id-999, got %q", receivedReq.TaskID)
		}
	})

	t.Run("run --sync with HumanGate decision", func(t *testing.T) {
		outcome := agent.PipelineOutcome{
			TaskID:          "task-gate-1",
			Success:         true,
			Decision:        agent.DecisionHumanGate,
			TotalIterations: 1,
			Duration:        1 * time.Second,
		}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Human Gate Required",
			"--sync",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "GATE_WAITING") {
			t.Errorf("expected GATE_WAITING badge, got:\n%s", out.String())
		}
	})

	t.Run("run --sync malformed outcome JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid-json"))
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Broken JSON",
			"--sync",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected 1 on broken json, got %d", code)
		}
	})

	t.Run("run async malformed response JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("{broken"))
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Broken JSON Async",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected 1 on broken json, got %d", code)
		}
	})
}

func TestFactoryCLI_Status_AdditionalEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("status with unknown flag returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "status", "run-123", "--bad-flag"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
	})

	t.Run("status network failure", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-123",
			"--server", "http://127.0.0.1:59998",
		})
		if code != 1 {
			t.Fatalf("expected 1, got %d", code)
		}
	})

	t.Run("status malformed JSON response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid-json"))
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "run-123",
			"--server", ts.URL,
		})
		if code != 1 {
			t.Fatalf("expected 1, got %d", code)
		}
	})
}

func TestFactoryCLI_List_AdditionalEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("list with unknown flag returns error", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "list", "--bad-flag"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
	})

	t.Run("list fallback to /api/v1/runs when /api/v1/factory/runs is 404", func(t *testing.T) {
		now := time.Now().UTC()
		sampleRuns := []*FactoryRunResponse{
			{
				RunID:     "fallback-run-1",
				Status:    "completed",
				CreatedAt: now,
				UpdatedAt: now,
			},
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/factory/runs" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path == "/api/v1/runs" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(sampleRuns)
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if !strings.Contains(out.String(), "fallback-run-1") {
			t.Errorf("expected fallback run in output, got:\n%s", out.String())
		}
	})

	t.Run("list fallback failure returns error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/factory/runs" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--server", ts.URL,
		})

		if code != 1 {
			t.Fatalf("expected 1, got %d", code)
		}
	})

	t.Run("list network error returns 1", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "list",
			"--server", "http://127.0.0.1:59997",
		})

		if code != 1 {
			t.Fatalf("expected 1 on network error, got %d", code)
		}
	})
}

func TestFactoryCLI_Gates_Lifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("gates empty list", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/gates/pending" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"gates": []any{},
				"total": 0,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "gates", "--server", ts.URL})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "No pending gates") {
			t.Errorf("expected empty state message, got: %s", out.String())
		}
	})

	t.Run("gates with pending items and formatted table", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/gates/pending" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"gates": []map[string]any{
					{
						"run_id":            "task-sec-1",
						"token":             "task-sec-1:1800000000:abcdef123456",
						"token_hmac_prefix": "abcdef12...",
						"reason":            "High risk file delete requires sign-off",
						"expires_at":        time.Now().Add(1 * time.Hour),
					},
				},
				"total": 1,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "gates", "--server", ts.URL})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		outStr := out.String()
		if !strings.Contains(outStr, "task-sec-1") || !strings.Contains(outStr, "High risk file delete") {
			t.Errorf("expected gate details in output table, got: %s", outStr)
		}
	})

	t.Run("gates with --json flag", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"gates": []map[string]any{
					{
						"run_id": "task-json-2",
						"token":  "task-json-2:1800000000:99887766",
						"reason": "Test gate json",
					},
				},
				"total": 1,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "gates", "--server", ts.URL, "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}

		var parsed map[string]any
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to parse json output: %v", err)
		}
		if parsed["total"] != float64(1) {
			t.Errorf("expected total 1, got %v", parsed["total"])
		}
	})

	t.Run("gates server error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal database failure"))
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "gates", "--server", ts.URL})
		if code != 1 {
			t.Fatalf("expected 1, got %d", code)
		}
	})
}

func TestFactoryCLI_Approve_Lifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("approve missing run_id returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "approve"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		if !strings.Contains(out.String(), "run-id argument is required") {
			t.Errorf("expected run-id argument is required error, got: %s", out.String())
		}
	})

	t.Run("approve with auto-resolved token and success output", func(t *testing.T) {
		var resolvedPayload map[string]any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/gates/pending" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"gates": []map[string]any{
						{
							"run_id": "run-auto-1",
							"token":  "run-auto-1:1800000000:validhmactoken",
							"reason": "Approval test",
						},
					},
					"total": 1,
				})
				return
			}
			if r.URL.Path == "/api/v1/gates/resolve" {
				_ = json.NewDecoder(r.Body).Decode(&resolvedPayload)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "approve", "run-auto-1", "--server", ts.URL})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Output: %s", code, out.String())
		}
		if !strings.Contains(out.String(), "approved") || !strings.Contains(out.String(), "merge queue") {
			t.Errorf("expected approved message in output, got: %s", out.String())
		}
		if resolvedPayload["run_id"] != "run-auto-1" || resolvedPayload["token"] != "run-auto-1:1800000000:validhmactoken" {
			t.Errorf("unexpected payload sent to /api/v1/gates/resolve: %+v", resolvedPayload)
		}
	})

	t.Run("approve with explicit --token and --json flag", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/gates/resolve" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "approve", "run-explicit-2",
			"--token", "explicit-token-123",
			"--server", ts.URL,
			"--json",
		})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Output: %s", code, out.String())
		}

		var parsed map[string]any
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to parse json response: %v", err)
		}
		if parsed["decision"] != "approved" || parsed["run_id"] != "run-explicit-2" {
			t.Errorf("unexpected json result: %+v", parsed)
		}
	})

	t.Run("approve gate not found returns 1", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"gates": []any{},
				"total": 0,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "approve", "nonexistent-run", "--server", ts.URL})
		if code != 1 {
			t.Fatalf("expected 1, got %d", code)
		}
		if !strings.Contains(out.String(), "could not locate approval token") {
			t.Errorf("expected error locating token, got: %s", out.String())
		}
	})

	t.Run("approve server rejects with 401 returns 1", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "approve", "run-rejected-auth",
			"--token", "bad-token",
			"--server", ts.URL,
		})
		if code != 1 {
			t.Fatalf("expected 1, got %d", code)
		}
		if !strings.Contains(out.String(), "server rejected approval") {
			t.Errorf("expected rejection error, got: %s", out.String())
		}
	})
}

func TestFactoryCLI_Reject_Lifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("reject missing run_id returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"factory", "reject"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
		if !strings.Contains(out.String(), "run-id argument is required") {
			t.Errorf("expected run-id argument is required, got: %s", out.String())
		}
	})

	t.Run("reject with reason and auto-resolved token", func(t *testing.T) {
		var resolvedPayload map[string]any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/gates/pending" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"gates": []map[string]any{
						{
							"run_id": "run-reject-1",
							"token":  "run-reject-1:1800000000:rejecttoken",
						},
					},
					"total": 1,
				})
				return
			}
			if r.URL.Path == "/api/v1/gates/resolve" {
				_ = json.NewDecoder(r.Body).Decode(&resolvedPayload)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "reject", "run-reject-1",
			"--reason", "Security vulnerability in patch",
			"--server", ts.URL,
		})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Output: %s", code, out.String())
		}
		if !strings.Contains(out.String(), "rejected") || !strings.Contains(out.String(), "Security vulnerability in patch") {
			t.Errorf("expected reject confirmation with reason, got: %s", out.String())
		}
		if resolvedPayload["decision"] != "rejected" || resolvedPayload["reason"] != "Security vulnerability in patch" {
			t.Errorf("unexpected payload: %+v", resolvedPayload)
		}
	})

	t.Run("reject with --json flag", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/gates/resolve" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "reject", "run-rej-json",
			"--token", "tok-123",
			"--reason", "Architectural constraint violated",
			"--server", ts.URL,
			"--json",
		})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}

		var parsed map[string]any
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if parsed["decision"] != "rejected" || parsed["reason"] != "Architectural constraint violated" {
			t.Errorf("unexpected json result: %+v", parsed)
		}
	})
}

func TestFactoryCLI_Run_ModelFlag(t *testing.T) {
	ctx := context.Background()

	t.Run("passes --model flag in request payload", func(t *testing.T) {
		var receivedReq FactoryRunRequest

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/run" {
				http.NotFound(w, r)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": receivedReq.TaskID,
				"status": "running",
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Implement DeepSeek Pipeline",
			"--model", "deepseek-chat",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		if receivedReq.Model != "deepseek-chat" {
			t.Errorf("expected model 'deepseek-chat', got %q", receivedReq.Model)
		}
		if receivedReq.Title != "Implement DeepSeek Pipeline" {
			t.Errorf("expected title 'Implement DeepSeek Pipeline', got %q", receivedReq.Title)
		}
	})

	t.Run("passes --model flag with quick presets like ollama/llama3.3 in sync mode", func(t *testing.T) {
		var receivedReq FactoryRunRequest

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/run" {
				http.NotFound(w, r)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			outcome := agent.PipelineOutcome{
				TaskID:   receivedReq.TaskID,
				Success:  true,
				Decision: agent.DecisionApproved,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Local Ollama Run",
			"--model", "ollama/llama3.3",
			"--sync",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		if receivedReq.Model != "ollama/llama3.3" {
			t.Errorf("expected model 'ollama/llama3.3', got %q", receivedReq.Model)
		}
	})
}

func TestFactoryCLI_Run_RemoteRepoFlags(t *testing.T) {
	ctx := context.Background()

	t.Run("passes --repo and --base-branch flags in request payload", func(t *testing.T) {
		var receivedReq FactoryRunRequest

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/run" {
				http.NotFound(w, r)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"run_id": receivedReq.TaskID,
				"status": "running",
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Remote Clone Task",
			"--repo", "https://github.com/org/repo.git",
			"--base-branch", "develop",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		if receivedReq.RepositoryURL != "https://github.com/org/repo.git" {
			t.Errorf("expected repository_url 'https://github.com/org/repo.git', got %q", receivedReq.RepositoryURL)
		}
		if receivedReq.BaseBranch != "develop" {
			t.Errorf("expected base_branch 'develop', got %q", receivedReq.BaseBranch)
		}
		if !strings.Contains(out.String(), "https://github.com/org/repo.git") {
			t.Errorf("expected output to mention repo URL, got: %s", out.String())
		}
		if !strings.Contains(out.String(), "develop") {
			t.Errorf("expected output to mention develop branch, got: %s", out.String())
		}
	})

	t.Run("displays repository and pull_request_url in status table", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/factory/runs/task-status-repo-1" {
				http.NotFound(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(&FactoryRunResponse{
				RunID:          "task-status-repo-1",
				Status:         "completed",
				RepositoryURL:  "https://github.com/gv-factory/gv-factory.git",
				BaseBranch:     "main",
				PullRequestURL: "https://github.com/gv-factory/gv-factory/pull/155",
				Outcome: &agent.PipelineOutcome{
					TaskID:   "task-status-repo-1",
					Success:  true,
					Decision: agent.DecisionApproved,
				},
				CreatedAt: time.Now().Add(-10 * time.Minute),
				UpdatedAt: time.Now(),
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "status", "task-status-repo-1",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		outStr := out.String()
		if !strings.Contains(outStr, "Repository:") || !strings.Contains(outStr, "https://github.com/gv-factory/gv-factory.git (main)") {
			t.Errorf("expected repository and branch in status output, got: %s", outStr)
		}
		if !strings.Contains(outStr, "Pull Request:") || !strings.Contains(outStr, "https://github.com/gv-factory/gv-factory/pull/155") {
			t.Errorf("expected pull request URL in status output, got: %s", outStr)
		}
	})

	t.Run("run sync mode with repo and json flag", func(t *testing.T) {
		var receivedReq FactoryRunRequest

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)
			outcome := agent.PipelineOutcome{
				TaskID:   receivedReq.TaskID,
				Success:  true,
				Decision: agent.DecisionApproved,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(outcome)
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{
			"factory", "run", "Sync Repo Run",
			"--repo", "https://github.com/org/sync-repo.git",
			"--base-branch", "main",
			"--sync",
			"--json",
			"--server", ts.URL,
		})

		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if receivedReq.RepositoryURL != "https://github.com/org/sync-repo.git" {
			t.Errorf("expected repo in sync request, got: %s", receivedReq.RepositoryURL)
		}
	})
}
