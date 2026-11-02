// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestGitHubIssueReporter_PostIssueComment(t *testing.T) {
	t.Run("success with required headers and payload", func(t *testing.T) {
		var receivedReq *http.Request
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedReq = r
			body, _ := io.ReadAll(r.Body)
			receivedBody = body

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"id": 12345,
				"body": "Autonomous run completed successfully.",
				"html_url": "https://github.com/gv-factory/gv-factory/issues/42#issuecomment-12345"
			}`))
		}))
		defer ts.Close()

		reporter := NewGitHubIssueReporter(GitHubReporterConfig{
			Token:        "ghp_testToken123",
			BaseURL:      ts.URL,
			UserAgent:    "gv-factory-test/1.0",
			MaxRetries:   2,
			RetryBackoff: 10 * time.Millisecond,
		})

		ctx := context.Background()
		err := reporter.PostIssueComment(ctx, "gv-factory/gv-factory", 42, "Autonomous run completed successfully.")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if receivedReq == nil {
			t.Fatal("expected request to be received by test server")
		}

		if receivedReq.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", receivedReq.Method)
		}

		expectedPath := "/repos/gv-factory/gv-factory/issues/42/comments"
		if receivedReq.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, receivedReq.URL.Path)
		}

		authHeader := receivedReq.Header.Get("Authorization")
		if authHeader != "Bearer ghp_testToken123" {
			t.Errorf("expected 'Bearer ghp_testToken123', got %q", authHeader)
		}

		uaHeader := receivedReq.Header.Get("User-Agent")
		if uaHeader != "gv-factory-test/1.0" {
			t.Errorf("expected 'gv-factory-test/1.0', got %q", uaHeader)
		}

		acceptHeader := receivedReq.Header.Get("Accept")
		if !strings.Contains(acceptHeader, "application/vnd.github") && !strings.Contains(acceptHeader, "application/json") {
			t.Errorf("expected GitHub Accept header, got %q", acceptHeader)
		}

		var payload map[string]string
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}
		if payload["body"] != "Autonomous run completed successfully." {
			t.Errorf("expected payload body 'Autonomous run completed successfully.', got %q", payload["body"])
		}
	})

	t.Run("retry resilience on transient 500 error", func(t *testing.T) {
		var attempts int32

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&attempts, 1)
			if count == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message": "internal server error"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 999, "body": "recovered"}`))
		}))
		defer ts.Close()

		reporter := NewGitHubIssueReporter(GitHubReporterConfig{
			Token:        "tok",
			BaseURL:      ts.URL,
			MaxRetries:   2,
			RetryBackoff: 5 * time.Millisecond,
		})

		err := reporter.PostIssueComment(context.Background(), "owner/repo", 10, "recovered")
		if err != nil {
			t.Fatalf("expected retry to succeed, got %v", err)
		}
		if atomic.LoadInt32(&attempts) != 2 {
			t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
		}
	})

	t.Run("returns error on client 404 error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message": "Not Found"}`))
		}))
		defer ts.Close()

		reporter := NewGitHubIssueReporter(GitHubReporterConfig{
			Token:   "tok",
			BaseURL: ts.URL,
		})

		err := reporter.PostIssueComment(context.Background(), "owner/missing", 99, "comment")
		if err == nil {
			t.Fatal("expected error on 404, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected error to mention 404, got %v", err)
		}
	})

	t.Run("input validation", func(t *testing.T) {
		reporter := NewGitHubIssueReporter(GitHubReporterConfig{Token: "tok"})
		ctx := context.Background()

		if err := reporter.PostIssueComment(ctx, "", 1, "body"); err == nil {
			t.Error("expected error for empty repo")
		}
		if err := reporter.PostIssueComment(ctx, "invalid-repo", 1, "body"); err == nil {
			t.Error("expected error for repo without slash")
		}
		if err := reporter.PostIssueComment(ctx, "owner/repo", 0, "body"); err == nil {
			t.Error("expected error for non-positive issueNumber")
		}
		if err := reporter.PostIssueComment(ctx, "owner/repo", 1, ""); err == nil {
			t.Error("expected error for empty comment")
		}
	})
}

func TestGitHubIssueReporter_CreatePullRequest(t *testing.T) {
	t.Run("success creating PR", func(t *testing.T) {
		var receivedReq *http.Request
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedReq = r
			body, _ := io.ReadAll(r.Body)
			receivedBody = body

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"id": 987654,
				"number": 105,
				"state": "open",
				"title": "feat: autonomous fix for issue #42",
				"body": "Automated pull request from gv-factory",
				"html_url": "https://github.com/gv-factory/gv-factory/pull/105",
				"url": "https://api.github.com/repos/gv-factory/gv-factory/pulls/105"
			}`))
		}))
		defer ts.Close()

		reporter := NewGitHubIssueReporter(GitHubReporterConfig{
			Token:   "ghp_testToken123",
			BaseURL: ts.URL,
		})

		ctx := context.Background()
		pr, err := reporter.CreatePullRequest(
			ctx,
			"gv-factory/gv-factory",
			"ov-run/issue-42",
			"main",
			"feat: autonomous fix for issue #42",
			"Automated pull request from gv-factory",
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if pr == nil {
			t.Fatal("expected non-nil PullRequestResponse")
		}
		if pr.Number != 105 {
			t.Errorf("expected PR number 105, got %d", pr.Number)
		}
		if pr.HTMLURL != "https://github.com/gv-factory/gv-factory/pull/105" {
			t.Errorf("expected html_url 'https://github.com/gv-factory/gv-factory/pull/105', got %q", pr.HTMLURL)
		}
		if pr.State != "open" {
			t.Errorf("expected state 'open', got %q", pr.State)
		}

		if receivedReq.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", receivedReq.Method)
		}
		if receivedReq.URL.Path != "/repos/gv-factory/gv-factory/pulls" {
			t.Errorf("expected /repos/gv-factory/gv-factory/pulls, got %s", receivedReq.URL.Path)
		}

		var payload map[string]string
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}
		if payload["head"] != "ov-run/issue-42" {
			t.Errorf("expected head ov-run/issue-42, got %q", payload["head"])
		}
		if payload["base"] != "main" {
			t.Errorf("expected base main, got %q", payload["base"])
		}
		if payload["title"] != "feat: autonomous fix for issue #42" {
			t.Errorf("expected title 'feat: autonomous fix for issue #42', got %q", payload["title"])
		}
	})

	t.Run("error response on conflict / unprocessable entity", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{
				"message": "Validation Failed",
				"errors": [{"message": "A pull request already exists for branch ov-run/issue-42."}]
			}`))
		}))
		defer ts.Close()

		reporter := NewGitHubIssueReporter(GitHubReporterConfig{
			Token:   "tok",
			BaseURL: ts.URL,
		})

		_, err := reporter.CreatePullRequest(
			context.Background(),
			"gv-factory/gv-factory",
			"ov-run/issue-42",
			"main",
			"title",
			"body",
		)
		if err == nil {
			t.Fatal("expected error on 422, got nil")
		}
		if !strings.Contains(err.Error(), "422") {
			t.Errorf("expected error to mention 422, got %v", err)
		}
	})

	t.Run("input validation", func(t *testing.T) {
		reporter := NewGitHubIssueReporter(GitHubReporterConfig{Token: "tok"})
		ctx := context.Background()

		if _, err := reporter.CreatePullRequest(ctx, "", "head", "base", "t", "b"); err == nil {
			t.Error("expected error for empty repo")
		}
		if _, err := reporter.CreatePullRequest(ctx, "repo", "", "base", "t", "b"); err == nil {
			t.Error("expected error for empty head")
		}
		if _, err := reporter.CreatePullRequest(ctx, "repo", "head", "", "t", "b"); err == nil {
			t.Error("expected error for empty base")
		}
		if _, err := reporter.CreatePullRequest(ctx, "repo", "head", "base", "", "b"); err == nil {
			t.Error("expected error for empty title")
		}
	})
}

func TestGitLabIssueReporter_PostIssueComment(t *testing.T) {
	t.Run("success with url-encoded project path and headers", func(t *testing.T) {
		var receivedReq *http.Request
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedReq = r
			body, _ := io.ReadAll(r.Body)
			receivedBody = body

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"id": 54321,
				"body": "GitLab note created.",
				"system": false
			}`))
		}))
		defer ts.Close()

		reporter := NewGitLabIssueReporter(GitLabReporterConfig{
			Token:        "glpat-xyz987",
			BaseURL:      ts.URL,
			UserAgent:    "gv-factory-gitlab/1.0",
			MaxRetries:   2,
			RetryBackoff: 10 * time.Millisecond,
		})

		ctx := context.Background()
		err := reporter.PostIssueComment(ctx, "group/subgroup/project", 15, "GitLab note created.")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if receivedReq.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", receivedReq.Method)
		}

		// Verify project path is URL-encoded (%2F)
		expectedEscaped := "group%2Fsubgroup%2Fproject"
		expectedURI := fmt.Sprintf("/projects/%s/issues/15/notes", expectedEscaped)
		if receivedReq.URL.RequestURI() != expectedURI {
			t.Errorf("expected RequestURI %s, got %s", expectedURI, receivedReq.URL.RequestURI())
		}

		tokenHeader := receivedReq.Header.Get("PRIVATE-TOKEN")
		authHeader := receivedReq.Header.Get("Authorization")
		if tokenHeader != "glpat-xyz987" && authHeader != "Bearer glpat-xyz987" {
			t.Errorf("expected token header, got PRIVATE-TOKEN=%q, Authorization=%q", tokenHeader, authHeader)
		}

		var payload map[string]string
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}
		if payload["body"] != "GitLab note created." {
			t.Errorf("expected body 'GitLab note created.', got %q", payload["body"])
		}
	})

	t.Run("retry on 502 Bad Gateway", func(t *testing.T) {
		var attempts int32

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&attempts, 1)
			if count == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"message": "bad gateway"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 111, "body": "ok"}`))
		}))
		defer ts.Close()

		reporter := NewGitLabIssueReporter(GitLabReporterConfig{
			Token:        "tok",
			BaseURL:      ts.URL,
			MaxRetries:   2,
			RetryBackoff: 5 * time.Millisecond,
		})

		err := reporter.PostIssueComment(context.Background(), "mygroup/proj", 7, "ok")
		if err != nil {
			t.Fatalf("expected retry to succeed, got %v", err)
		}
		if atomic.LoadInt32(&attempts) != 2 {
			t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
		}
	})

	t.Run("input validation", func(t *testing.T) {
		reporter := NewGitLabIssueReporter(GitLabReporterConfig{Token: "tok"})
		ctx := context.Background()

		if err := reporter.PostIssueComment(ctx, "", 1, "b"); err == nil {
			t.Error("expected error for empty repo")
		}
		if err := reporter.PostIssueComment(ctx, "p", 0, "b"); err == nil {
			t.Error("expected error for non-positive issueNumber")
		}
		if err := reporter.PostIssueComment(ctx, "p", 1, ""); err == nil {
			t.Error("expected error for empty comment")
		}
	})
}

func TestGitLabIssueReporter_CreateMergeRequest(t *testing.T) {
	t.Run("success creating Merge Request", func(t *testing.T) {
		var receivedReq *http.Request
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedReq = r
			body, _ := io.ReadAll(r.Body)
			receivedBody = body

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"id": 888,
				"iid": 42,
				"title": "Resolve issue #15",
				"description": "Closes #15",
				"state": "opened",
				"web_url": "https://gitlab.com/group/proj/-/merge_requests/42"
			}`))
		}))
		defer ts.Close()

		reporter := NewGitLabIssueReporter(GitLabReporterConfig{
			Token:   "glpat-token",
			BaseURL: ts.URL,
		})

		mr, err := reporter.CreateMergeRequest(
			context.Background(),
			"group/proj",
			"ov-run/issue-15",
			"main",
			"Resolve issue #15",
			"Closes #15",
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if mr.IID != 42 {
			t.Errorf("expected IID 42, got %d", mr.IID)
		}
		if mr.WebURL != "https://gitlab.com/group/proj/-/merge_requests/42" {
			t.Errorf("expected WebURL 'https://gitlab.com/group/proj/-/merge_requests/42', got %q", mr.WebURL)
		}

		// Also check CreatePullRequest wrapper
		pr, err := reporter.CreatePullRequest(
			context.Background(),
			"group/proj",
			"ov-run/issue-15",
			"main",
			"Resolve issue #15",
			"Closes #15",
		)
		if err != nil {
			t.Fatalf("expected CreatePullRequest wrapper to succeed: %v", err)
		}
		if pr.Number != 42 {
			t.Errorf("expected PR Number 42, got %d", pr.Number)
		}
		if pr.HTMLURL != "https://gitlab.com/group/proj/-/merge_requests/42" {
			t.Errorf("expected HTMLURL to match, got %q", pr.HTMLURL)
		}

		if receivedReq.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", receivedReq.Method)
		}
		expectedURI := "/projects/group%2Fproj/merge_requests"
		if receivedReq.URL.RequestURI() != expectedURI {
			t.Errorf("expected %s, got %s", expectedURI, receivedReq.URL.RequestURI())
		}

		var payload map[string]string
		_ = json.Unmarshal(receivedBody, &payload)
		if payload["source_branch"] != "ov-run/issue-15" {
			t.Errorf("expected source_branch ov-run/issue-15, got %q", payload["source_branch"])
		}
		if payload["target_branch"] != "main" {
			t.Errorf("expected target_branch main, got %q", payload["target_branch"])
		}
	})
}

func TestCompositeIssueReporter(t *testing.T) {
	var githubCalled, gitlabCalled bool

	ghTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubCalled = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 1}`))
	}))
	defer ghTS.Close()

	glTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gitlabCalled = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 2}`))
	}))
	defer glTS.Close()

	ghReporter := NewGitHubIssueReporter(GitHubReporterConfig{BaseURL: ghTS.URL, Token: "tok"})
	glReporter := NewGitLabIssueReporter(GitLabReporterConfig{BaseURL: glTS.URL, Token: "tok"})

	router := NewCompositeIssueReporter(ghReporter, glReporter)

	ctx := context.Background()

	// GitHub routing
	err := router.PostIssueCommentWithSource(ctx, schema.SourceGitHub, "owner/repo", 1, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !githubCalled {
		t.Error("expected github reporter to be called")
	}

	// GitLab routing
	err = router.PostIssueCommentWithSource(ctx, schema.SourceGitLab, "group/repo", 2, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !gitlabCalled {
		t.Error("expected gitlab reporter to be called")
	}

	// Default fallback (GitHub) when source is empty
	githubCalled = false
	err = router.PostIssueComment(ctx, "owner/repo", 3, "test")
	if err != nil {
		t.Fatalf("expected no error on default PostIssueComment, got %v", err)
	}
	if !githubCalled {
		t.Error("expected default to use github reporter")
	}

	t.Run("composite CreatePullRequest", func(t *testing.T) {
		pr, err := router.CreatePullRequest(ctx, "owner/repo", "head", "base", "t", "b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pr == nil {
			t.Fatal("expected non-nil PR response")
		}

		// GitLab only composite
		glOnly := NewCompositeIssueReporter(nil, glReporter)
		pr, err = glOnly.CreatePullRequest(ctx, "group/repo", "head", "base", "t", "b")
		if err != nil {
			t.Fatalf("unexpected error from glOnly: %v", err)
		}
		if pr == nil {
			t.Fatal("expected non-nil PR response from glOnly")
		}

		// Empty composite
		emptyComp := NewCompositeIssueReporter(nil, nil)
		if _, err := emptyComp.CreatePullRequest(ctx, "repo", "head", "base", "t", "b"); err == nil {
			t.Error("expected error for empty composite")
		}
		if err := emptyComp.PostIssueComment(ctx, "repo", 1, "c"); err == nil {
			t.Error("expected error for empty composite PostIssueComment")
		}
		if err := emptyComp.PostIssueCommentWithSource(ctx, "unknown", "repo", 1, "c"); err == nil {
			t.Error("expected error for unconfigured source")
		}

		// GitLab fallback when GitHub is nil in PostIssueComment
		glFallback := NewCompositeIssueReporter(nil, glReporter)
		gitlabCalled = false
		if err := glFallback.PostIssueComment(ctx, "group/repo", 1, "c"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !gitlabCalled {
			t.Error("expected gitlab to be called when github is nil")
		}

		// Unknown source fallback to gitlab when github is nil
		gitlabCalled = false
		if err := glFallback.PostIssueCommentWithSource(ctx, "other", "group/repo", 1, "c"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !gitlabCalled {
			t.Error("expected fallback to gitlab when github is nil")
		}
	})
}

func TestIssueReporter_EdgeCasesAndCoverage(t *testing.T) {
	t.Run("default configurations and negative retries", func(t *testing.T) {
		gh := NewGitHubIssueReporter(GitHubReporterConfig{
			MaxRetries: -1,
		})
		if gh.cfg.BaseURL != "https://api.github.com" {
			t.Errorf("expected default BaseURL, got %q", gh.cfg.BaseURL)
		}
		if gh.cfg.UserAgent != "gv-factory/1.0" {
			t.Errorf("expected default UserAgent, got %q", gh.cfg.UserAgent)
		}

		gl := NewGitLabIssueReporter(GitLabReporterConfig{
			MaxRetries: -1,
		})
		if gl.cfg.BaseURL != "https://gitlab.com/api/v4" {
			t.Errorf("expected default BaseURL, got %q", gl.cfg.BaseURL)
		}
		if gl.cfg.UserAgent != "gv-factory-gitlab/1.0" {
			t.Errorf("expected default UserAgent, got %q", gl.cfg.UserAgent)
		}
	})

	t.Run("context cancellation during retry", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		ctx, cancel := context.WithCancel(context.Background())
		gh := NewGitHubIssueReporter(GitHubReporterConfig{
			BaseURL:      ts.URL,
			Token:        "Bearer already-bearer",
			MaxRetries:   5,
			RetryBackoff: 50 * time.Millisecond,
		})

		go func() {
			time.Sleep(15 * time.Millisecond)
			cancel()
		}()

		err := gh.PostIssueComment(ctx, "owner/repo", 1, "test")
		if err == nil {
			t.Fatal("expected context error, got nil")
		}
	})

	t.Run("GitLab CreateMergeRequest validation and errors", func(t *testing.T) {
		gl := NewGitLabIssueReporter(GitLabReporterConfig{Token: "Bearer glpat-xyz"})
		ctx := context.Background()

		if _, err := gl.CreateMergeRequest(ctx, "", "src", "tgt", "t", "d"); err == nil {
			t.Error("expected error for empty repo")
		}
		if _, err := gl.CreateMergeRequest(ctx, "repo", "", "tgt", "t", "d"); err == nil {
			t.Error("expected error for empty sourceBranch")
		}
		if _, err := gl.CreateMergeRequest(ctx, "repo", "src", "", "t", "d"); err == nil {
			t.Error("expected error for empty targetBranch")
		}
		if _, err := gl.CreateMergeRequest(ctx, "repo", "src", "tgt", "", "d"); err == nil {
			t.Error("expected error for empty title")
		}

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "Branch already exists"}`))
		}))
		defer ts.Close()

		gl.cfg.BaseURL = ts.URL
		if _, err := gl.CreateMergeRequest(ctx, "repo", "src", "tgt", "t", "d"); err == nil {
			t.Error("expected error from 400 response")
		}
	})

	t.Run("GitLab max retries exhausted", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`Service Unavailable`))
		}))
		defer ts.Close()

		gl := NewGitLabIssueReporter(GitLabReporterConfig{
			BaseURL:      ts.URL,
			MaxRetries:   1,
			RetryBackoff: time.Millisecond,
		})

		err := gl.PostIssueComment(context.Background(), "repo", 1, "comment")
		if err == nil {
			t.Fatal("expected max retries error")
		}
	})

	t.Run("invalid JSON response for GitHub CreatePullRequest", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`invalid-json-body`))
		}))
		defer ts.Close()

		gh := NewGitHubIssueReporter(GitHubReporterConfig{BaseURL: ts.URL, Token: "t"})
		_, err := gh.CreatePullRequest(context.Background(), "owner/repo", "head", "base", "t", "b")
		if err == nil {
			t.Fatal("expected error on invalid JSON")
		}
	})

	t.Run("invalid JSON response for GitLab CreateMergeRequest", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`invalid-json-body`))
		}))
		defer ts.Close()

		gl := NewGitLabIssueReporter(GitLabReporterConfig{BaseURL: ts.URL, Token: "t"})
		_, err := gl.CreateMergeRequest(context.Background(), "owner/repo", "head", "base", "t", "b")
		if err == nil {
			t.Fatal("expected error on invalid JSON")
		}
		// Also test CreatePullRequest wrapper error propagation
		_, err = gl.CreatePullRequest(context.Background(), "owner/repo", "head", "base", "t", "b")
		if err == nil {
			t.Fatal("expected wrapper error on invalid JSON")
		}
	})

	t.Run("network error on client Do", func(t *testing.T) {
		gh := NewGitHubIssueReporter(GitHubReporterConfig{
			BaseURL:    "http://127.0.0.1:59999",
			MaxRetries: 1,
			RetryBackoff: time.Millisecond,
		})
		err := gh.PostIssueComment(context.Background(), "owner/repo", 1, "body")
		if err == nil {
			t.Fatal("expected network connection error")
		}

		gl := NewGitLabIssueReporter(GitLabReporterConfig{
			BaseURL:    "http://127.0.0.1:59999",
			MaxRetries: 1,
			RetryBackoff: time.Millisecond,
		})
		err = gl.PostIssueComment(context.Background(), "owner/repo", 1, "body")
		if err == nil {
			t.Fatal("expected network connection error on gitlab")
		}
	})

	t.Run("AutonomousFactoryBridge PRCreator accessors", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		if bridge.PRCreator() != nil {
			t.Error("expected initial PRCreator to be nil")
		}
		mockPR := &testPRCreatorMock{}
		bridge.SetPRCreator(mockPR)
		if bridge.PRCreator() != mockPR {
			t.Error("expected SetPRCreator to set creator")
		}
	})
}

