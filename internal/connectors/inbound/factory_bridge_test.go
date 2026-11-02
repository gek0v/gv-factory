// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// mockFactoryRunner implements FactoryPipelineRunner for testing.
type mockFactoryRunner struct {
	mu           sync.Mutex
	calls        []agent.TaskContext
	runOutcome   *agent.PipelineOutcome
	runErr       error
	delay        time.Duration
	blockCh      chan struct{}
	calledNotify chan struct{}
}

func newMockFactoryRunner() *mockFactoryRunner {
	return &mockFactoryRunner{
		calledNotify: make(chan struct{}, 10),
	}
}

func (m *mockFactoryRunner) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	m.mu.Lock()
	m.calls = append(m.calls, task)
	m.mu.Unlock()

	select {
	case m.calledNotify <- struct{}{}:
	default:
	}

	if m.blockCh != nil {
		select {
		case <-m.blockCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.runErr != nil {
		return nil, m.runErr
	}

	if m.runOutcome != nil {
		return m.runOutcome, nil
	}

	return &agent.PipelineOutcome{
		TaskID:          task.TaskID,
		Success:         true,
		Decision:        agent.DecisionApproved,
		TotalIterations: 1,
		Duration:        500 * time.Millisecond,
		BuildResult: &agent.BuildResult{
			TaskID:        task.TaskID,
			Success:       true,
			FilesModified: []string{"pkg/auth/token.go", "pkg/auth/token_test.go"},
			GitDiff:       "+func ValidateToken() bool { return true }",
		},
		Approval: &agent.ApprovalDecision{
			DecisionType:      agent.DecisionApproved,
			ConsolidatedScore: 95.0,
			FeedbackSummary:   "All unit tests pass and security review clear.",
		},
	}, nil
}

func (m *mockFactoryRunner) getCalls() []agent.TaskContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]agent.TaskContext, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// mockIssueReporter records comments posted to issues or pull requests.
type mockIssueReporter struct {
	mu       sync.Mutex
	comments []reportedComment
	postErr  error
}

type reportedComment struct {
	Repo        string
	IssueNumber int
	Comment     string
}

func (r *mockIssueReporter) PostIssueComment(ctx context.Context, repo string, issueNumber int, comment string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.postErr != nil {
		return r.postErr
	}
	r.comments = append(r.comments, reportedComment{
		Repo:        repo,
		IssueNumber: issueNumber,
		Comment:     comment,
	})
	return nil
}

func (r *mockIssueReporter) getComments() []reportedComment {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]reportedComment, len(r.comments))
	copy(cp, r.comments)
	return cp
}

// TestAutonomousFactoryBridge_TriggerRecognition verifies label and command matching across GitHub and GitLab events.
func TestAutonomousFactoryBridge_TriggerRecognition(t *testing.T) {
	runner := newMockFactoryRunner()
	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	tests := []struct {
		name         string
		event        schema.TriggerEvent
		wantHandled  bool
		wantSkipped  bool
		wantReasonSub string
	}{
		{
			name: "GitHub issue labeled ready-for-agent",
			event: schema.TriggerEvent{
				ID:          "del-1",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueLabeled,
				Repository:  "owner/repo",
				IssueNumber: 101,
				Payload: map[string]any{
					"label": map[string]any{"name": "ready-for-agent"},
					"issue": map[string]any{
						"number": 101,
						"title":  "Fix authentication race",
						"body":   "Fix token expiration under load\n- [ ] Add race test",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitHub issue labeled autofix",
			event: schema.TriggerEvent{
				ID:          "del-2",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueLabeled,
				Repository:  "owner/repo",
				IssueNumber: 102,
				Payload: map[string]any{
					"label": map[string]any{"name": "autofix"},
					"issue": map[string]any{
						"number": 102,
						"title":  "Bug in parser",
						"body":   "Fix parser null check",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitHub issue labeled factory",
			event: schema.TriggerEvent{
				ID:          "del-3",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueLabeled,
				Repository:  "owner/repo",
				IssueNumber: 103,
				Payload: map[string]any{
					"label": map[string]any{"name": "factory"},
					"issue": map[string]any{
						"number": 103,
						"title":  "Implement health endpoint",
						"body":   "Add /healthz",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitHub issue labeled non-matching",
			event: schema.TriggerEvent{
				ID:          "del-4",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueLabeled,
				Repository:  "owner/repo",
				IssueNumber: 104,
				Payload: map[string]any{
					"label": map[string]any{"name": "documentation"},
					"issue": map[string]any{
						"number": 104,
						"title":  "Docs update",
						"body":   "Update README",
					},
				},
			},
			wantHandled:   false,
			wantSkipped:   true,
			wantReasonSub: "no matching label or command",
		},
		{
			name: "GitHub issue comment with @factory build",
			event: schema.TriggerEvent{
				ID:          "del-5",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueCommented,
				Repository:  "owner/repo",
				IssueNumber: 105,
				Payload: map[string]any{
					"comment": map[string]any{
						"body": "@factory build please fix memory leak",
					},
					"issue": map[string]any{
						"number": 105,
						"title":  "Memory leak in worker",
						"body":   "Original issue body\n- [ ] Add leak test",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitHub issue comment with /factory",
			event: schema.TriggerEvent{
				ID:          "del-6",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueCommented,
				Repository:  "owner/repo",
				IssueNumber: 106,
				Payload: map[string]any{
					"comment": map[string]any{
						"body": "/factory rebuild with strict coverage",
					},
					"issue": map[string]any{
						"number": 106,
						"title":  "Add coverage",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitHub issue comment without command trigger",
			event: schema.TriggerEvent{
				ID:          "del-7",
				Source:      schema.SourceGitHub,
				EventType:   schema.EventIssueCommented,
				Repository:  "owner/repo",
				IssueNumber: 107,
				Payload: map[string]any{
					"comment": map[string]any{
						"body": "Just a regular comment from a reviewer",
					},
					"issue": map[string]any{
						"number": 107,
						"title":  "Regular issue",
					},
				},
			},
			wantHandled:   false,
			wantSkipped:   true,
			wantReasonSub: "no matching label or command",
		},
		{
			name: "GitLab issue note with @factory build",
			event: schema.TriggerEvent{
				ID:          "del-8",
				Source:      schema.SourceGitLab,
				EventType:   schema.EventIssueCommented,
				Repository:  "group/project",
				IssueNumber: 201,
				Payload: map[string]any{
					"object_attributes": map[string]any{
						"note": "@factory build test and verify",
					},
					"issue": map[string]any{
						"iid":   201,
						"title": "GitLab issue title",
						"description": "GitLab issue description\n* [ ] Constraint 1",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitLab issue with ready-for-agent in labels list",
			event: schema.TriggerEvent{
				ID:          "del-9",
				Source:      schema.SourceGitLab,
				EventType:   schema.EventIssueOpened,
				Repository:  "group/project",
				IssueNumber: 202,
				Payload: map[string]any{
					"labels": []any{
						map[string]any{"title": "bug"},
						map[string]any{"title": "ready-for-agent"},
					},
					"object_attributes": map[string]any{
						"iid":         202,
						"title":       "GitLab labeled issue",
						"description": "Body content",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "GitHub pull request labeled with factory",
			event: schema.TriggerEvent{
				ID:         "del-10",
				Source:     schema.SourceGitHub,
				EventType:  "pr.labeled",
				Repository: "owner/repo",
				PRNumber:   50,
				Payload: map[string]any{
					"label": map[string]any{"name": "factory"},
					"pull_request": map[string]any{
						"number": 50,
						"title":  "Feature PR",
						"body":   "Fixes something\n- [ ] zero CVEs",
					},
				},
			},
			wantHandled: true,
			wantSkipped: false,
		},
		{
			name: "Unsupported event type push is skipped",
			event: schema.TriggerEvent{
				ID:         "del-11",
				Source:     schema.SourceGitHub,
				EventType:  schema.EventPush,
				Repository: "owner/repo",
				Payload:    map[string]any{"ref": "refs/heads/main"},
			},
			wantHandled:   false,
			wantSkipped:   true,
			wantReasonSub: "unsupported event type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := bridge.HandleEvent(context.Background(), tc.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Handled != tc.wantHandled {
				t.Errorf("expected Handled=%v, got %v", tc.wantHandled, res.Handled)
			}
			if res.Skipped != tc.wantSkipped {
				t.Errorf("expected Skipped=%v, got %v", tc.wantSkipped, res.Skipped)
			}
			if tc.wantReasonSub != "" && !strings.Contains(strings.ToLower(res.Reason), strings.ToLower(tc.wantReasonSub)) {
				t.Errorf("expected reason to contain %q, got %q", tc.wantReasonSub, res.Reason)
			}
		})
	}
}

// TestAutonomousFactoryBridge_TaskContextHydration verifies extraction of task ID, title, branch, labels, constraints, and sanitized description.
func TestAutonomousFactoryBridge_TaskContextHydration(t *testing.T) {
	runner := newMockFactoryRunner()
	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	ev := schema.TriggerEvent{
		ID:          "delivery-xyz",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueCommented,
		Repository:  "gv-factory/gv-factory",
		IssueNumber: 42,
		Sender:      "octocat",
		Payload: map[string]any{
			"comment": map[string]any{
				"body": "@factory build Please handle edge cases cleanly.",
			},
			"issue": map[string]any{
				"number": 42,
				"title":  "Implement Inbound Factory Bridge",
				"body": `## Requirements
Implement autonomous webhook bridge.

## Constraints
- [ ] Must achieve >= 90% unit test coverage
* [ ] No external dependencies outside stdlib and internal packages
- [x] Must support GitHub and GitLab
Constraint: Zero security vulnerabilities in AST parsing
Constraints:
- Maximum execution timeout 15 minutes`,
				"labels": []any{
					map[string]any{"name": "enhancement"},
					map[string]any{"name": "ready-for-agent"},
				},
			},
		},
	}

	res, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	if !res.Handled {
		t.Fatalf("expected event to be handled, got reason: %s", res.Reason)
	}

	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(calls))
	}

	task := calls[0]
	if task.TaskID != "issue-42" {
		t.Errorf("expected TaskID 'issue-42', got %q", task.TaskID)
	}
	if task.Title != "Implement Inbound Factory Bridge" {
		t.Errorf("expected Title 'Implement Inbound Factory Bridge', got %q", task.Title)
	}
	if res.Branch != "ov-run/issue-42" {
		t.Errorf("expected branch 'ov-run/issue-42', got %q", res.Branch)
	}
	if task.Metadata["target_branch"] != "ov-run/issue-42" {
		t.Errorf("expected metadata target_branch 'ov-run/issue-42', got %v", task.Metadata["target_branch"])
	}

	// Verify sanitized description does not include command trigger string "@factory build"
	if strings.Contains(task.Description, "@factory build") {
		t.Errorf("sanitized description should not contain '@factory build', got:\n%s", task.Description)
	}

	// Verify constraints extracted
	expectedConstraints := []string{
		"Must achieve >= 90% unit test coverage",
		"No external dependencies outside stdlib and internal packages",
		"Must support GitHub and GitLab",
		"Zero security vulnerabilities in AST parsing",
		"Maximum execution timeout 15 minutes",
	}

	for _, exp := range expectedConstraints {
		found := false
		for _, c := range task.Constraints {
			if strings.Contains(c, exp) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected constraint %q was not found in task.Constraints: %v", exp, task.Constraints)
		}
	}

	// Verify labels
	hasEnhancement := false
	hasReady := false
	for _, l := range task.Labels {
		if l == "enhancement" {
			hasEnhancement = true
		}
		if l == "ready-for-agent" {
			hasReady = true
		}
	}
	if !hasEnhancement || !hasReady {
		t.Errorf("expected labels 'enhancement' and 'ready-for-agent', got %v", task.Labels)
	}
}

// TestAutonomousFactoryBridge_PipelineExecution_Approved verifies synchronous approved execution, events, and issue comment.
func TestAutonomousFactoryBridge_PipelineExecution_Approved(t *testing.T) {
	runner := newMockFactoryRunner()
	runner.runOutcome = &agent.PipelineOutcome{
		TaskID:          "issue-101",
		Success:         true,
		Decision:        agent.DecisionApproved,
		TotalIterations: 2,
		Duration:        12500 * time.Millisecond,
		BuildResult: &agent.BuildResult{
			TaskID:        "issue-101",
			Success:       true,
			FilesModified: []string{"internal/auth/signer.go", "internal/auth/signer_test.go"},
			GitDiff:       "diff --git a/internal/auth/signer.go b/internal/auth/signer.go\n+func Sign() string { return \"token\" }",
		},
		Approval: &agent.ApprovalDecision{
			DecisionType:      agent.DecisionApproved,
			ConsolidatedScore: 98.5,
			FeedbackSummary:   "Code adheres to standards and coverage is 95%.",
		},
	}

	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	cfg.Async = false
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	ev := schema.TriggerEvent{
		ID:          "del-approved-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 101,
		Payload: map[string]any{
			"label": map[string]any{"name": "ready-for-agent"},
			"issue": map[string]any{
				"number": 101,
				"title":  "Approved Issue",
				"body":   "Description here",
			},
		},
	}

	res, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Handled || res.Skipped {
		t.Fatalf("expected handled event, got res: %+v", res)
	}
	if res.Outcome == nil || !res.Outcome.IsApproved() {
		t.Fatalf("expected approved outcome, got %+v", res.Outcome)
	}
	if !res.CommentPosted {
		t.Errorf("expected CommentPosted=true")
	}

	// Verify Gateway events
	events := publisher.getEvents()
	var foundTriggered, foundCompleted bool
	for _, e := range events {
		if e.Type == gateway.TopicPipelineTriggered {
			foundTriggered = true
			if e.Payload["issue_number"] != 101 {
				t.Errorf("expected issue_number 101 in event payload, got %v", e.Payload["issue_number"])
			}
		}
		if e.Type == gateway.TopicRunCompleted || e.Type == gateway.TopicTaskCompleted {
			foundCompleted = true
		}
	}
	if !foundTriggered {
		t.Errorf("expected gateway.TopicPipelineTriggered event to be published")
	}
	if !foundCompleted {
		t.Errorf("expected completed gateway event to be published")
	}

	// Verify Issue comment
	comments := reporter.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 issue comment, got %d", len(comments))
	}
	c := comments[0]
	if c.Repo != "owner/repo" || c.IssueNumber != 101 {
		t.Errorf("expected comment for owner/repo#101, got %s#%d", c.Repo, c.IssueNumber)
	}
	// Markdown formatting checks for Approved
	if !strings.Contains(c.Comment, "Approved") {
		t.Errorf("comment should mention Approved status, got:\n%s", c.Comment)
	}
	if !strings.Contains(c.Comment, "Iterations") || !strings.Contains(c.Comment, "2") {
		t.Errorf("comment should include iterations count (2), got:\n%s", c.Comment)
	}
	if !strings.Contains(c.Comment, "ov-run/issue-101") {
		t.Errorf("comment should include branch pointer 'ov-run/issue-101', got:\n%s", c.Comment)
	}
	if !strings.Contains(c.Comment, "internal/auth/signer.go") {
		t.Errorf("comment should include diff overview / files modified, got:\n%s", c.Comment)
	}
}

// TestAutonomousFactoryBridge_PipelineExecution_HumanGate verifies human gate intervention feedback format and topic publication.
func TestAutonomousFactoryBridge_PipelineExecution_HumanGate(t *testing.T) {
	runner := newMockFactoryRunner()
	runner.runOutcome = &agent.PipelineOutcome{
		TaskID:          "issue-102",
		Success:         false,
		Decision:        agent.DecisionHumanGate,
		TotalIterations: 3,
		Duration:        20 * time.Second,
		Approval: &agent.ApprovalDecision{
			DecisionType:      agent.DecisionHumanGate,
			RequiresHumanSign: true,
			GateReason:        "Modified core crypto configuration requiring security officer approval.",
			FeedbackSummary:   "High security alert in internal/crypto/keys.go",
			ReviewerResults: []agent.ReviewResult{
				{
					ReviewerName: "security",
					Passed:       false,
					Score:        50.0,
					Summary:      "Requires manual key review",
				},
			},
		},
	}

	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	cfg.GateApprovalBaseURL = "https://factory.internal/gate/approve"
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	ev := schema.TriggerEvent{
		ID:          "del-gate-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 102,
		Payload: map[string]any{
			"label": map[string]any{"name": "factory"},
			"issue": map[string]any{
				"number": 102,
				"title":  "Rotate encryption keys",
			},
		},
	}

	res, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Handled || res.Outcome == nil || !res.Outcome.IsHumanGate() {
		t.Fatalf("expected human gate outcome, got %+v", res)
	}

	// Verify Gate approval required event
	events := publisher.getEvents()
	var foundGateEvent bool
	for _, e := range events {
		if e.Type == gateway.TopicGateApprovalRequired {
			foundGateEvent = true
			break
		}
	}
	if !foundGateEvent {
		t.Errorf("expected gateway.TopicGateApprovalRequired event")
	}

	// Verify Issue comment formatting
	comments := reporter.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 issue comment, got %d", len(comments))
	}
	c := comments[0].Comment
	if !strings.Contains(c, "Human") || !strings.Contains(c, "Approval") {
		t.Errorf("comment should mention Human Approval Required, got:\n%s", c)
	}
	if !strings.Contains(c, "Modified core crypto configuration") {
		t.Errorf("comment should contain findings summary / gate reason, got:\n%s", c)
	}
	if !strings.Contains(c, "https://factory.internal/gate/approve") && !strings.Contains(c, "ov gate approve") {
		t.Errorf("comment should contain HMAC gate URL or CLI hint, got:\n%s", c)
	}
}

// TestAutonomousFactoryBridge_PipelineExecution_Failed verifies failed/rejected reporting and event topics.
func TestAutonomousFactoryBridge_PipelineExecution_Failed(t *testing.T) {
	runner := newMockFactoryRunner()
	runner.runOutcome = &agent.PipelineOutcome{
		TaskID:          "issue-103",
		Success:         false,
		Decision:        agent.DecisionFailed,
		TotalIterations: 4,
		Duration:        35 * time.Second,
		ErrorMessage:    "Max retries exceeded: unit tests failed in package internal/parser",
		BuildResult: &agent.BuildResult{
			TaskID:       "issue-103",
			Success:      false,
			ErrorMessage: "TestParseSyntaxError: expected nil, got syntax error at line 4",
		},
	}

	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	ev := schema.TriggerEvent{
		ID:          "del-fail-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 103,
		Payload: map[string]any{
			"label": map[string]any{"name": "autofix"},
			"issue": map[string]any{
				"number": 103,
				"title":  "Parser bug",
			},
		},
	}

	res, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Handled || res.Outcome == nil || !res.Outcome.IsFailed() {
		t.Fatalf("expected failed outcome, got %+v", res)
	}

	// Verify failed event
	events := publisher.getEvents()
	var foundFailedEvent bool
	for _, e := range events {
		if e.Type == gateway.TopicRunFailed || e.Type == gateway.TopicTaskFailed {
			foundFailedEvent = true
			break
		}
	}
	if !foundFailedEvent {
		t.Errorf("expected failure event topic on gateway")
	}

	// Verify failure comment
	comments := reporter.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 issue comment, got %d", len(comments))
	}
	c := comments[0].Comment
	if !strings.Contains(c, "Failed") {
		t.Errorf("comment should indicate Failure, got:\n%s", c)
	}
	if !strings.Contains(c, "Max retries exceeded") {
		t.Errorf("comment should contain error details, got:\n%s", c)
	}
	if !strings.Contains(c, "TestParseSyntaxError") {
		t.Errorf("comment should mention failed test or phase output, got:\n%s", c)
	}
}

// TestAutonomousFactoryBridge_AsyncDispatch verifies asynchronous background execution.
func TestAutonomousFactoryBridge_AsyncDispatch(t *testing.T) {
	runner := newMockFactoryRunner()
	runner.delay = 50 * time.Millisecond

	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	cfg.Async = true
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	ev := schema.TriggerEvent{
		ID:          "del-async-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 104,
		Payload: map[string]any{
			"label": map[string]any{"name": "ready-for-agent"},
			"issue": map[string]any{
				"number": 104,
				"title":  "Async run issue",
			},
		},
	}

	start := time.Now()
	res, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	elapsed := time.Since(start)

	if !res.Handled {
		t.Fatalf("expected handled event, got reason %s", res.Reason)
	}
	if !res.Async {
		t.Errorf("expected Async=true in result")
	}
	// HandleEvent should return almost immediately (less than the runner delay)
	if elapsed >= runner.delay {
		t.Errorf("expected fast return for async dispatch, took %v", elapsed)
	}

	// Wait for runner to be called and background worker to finish
	select {
	case <-runner.calledNotify:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for runner invocation")
	}

	// Wait for bridge to complete background tasks
	bridge.Wait()

	comments := reporter.getComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment after async completion, got %d", len(comments))
	}
}

// TestAutonomousFactoryBridge_Deduplication verifies delivery ID deduplication.
func TestAutonomousFactoryBridge_Deduplication(t *testing.T) {
	runner := newMockFactoryRunner()
	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	cfg := DefaultBridgeConfig()
	bridge := NewAutonomousFactoryBridge(cfg, runner, publisher, reporter)
	defer bridge.Close()

	ev := schema.TriggerEvent{
		ID:          "unique-delivery-id-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 105,
		Payload: map[string]any{
			"label": map[string]any{"name": "ready-for-agent"},
			"issue": map[string]any{
				"number": 105,
				"title":  "Dedup issue",
			},
		},
	}

	res1, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil || !res1.Handled {
		t.Fatalf("expected first event to be handled: %v, res: %+v", err, res1)
	}

	res2, err := bridge.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected error on second event: %v", err)
	}
	if res2.Handled || !res2.Skipped {
		t.Fatalf("expected second event to be skipped as duplicate, got %+v", res2)
	}
	if !strings.Contains(strings.ToLower(res2.Reason), "duplicate") {
		t.Errorf("expected duplicate reason, got %q", res2.Reason)
	}
}

// TestAutonomousFactoryBridge_DispatcherInterface verifies Dispatch method adapter.
func TestAutonomousFactoryBridge_DispatcherInterface(t *testing.T) {
	runner := newMockFactoryRunner()
	publisher := &mockEventPublisher{}
	reporter := &mockIssueReporter{}

	bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), runner, publisher, reporter)
	defer bridge.Close()

	var d Dispatcher = bridge

	ev := schema.TriggerEvent{
		ID:          "dispatch-test-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 106,
		Payload: map[string]any{
			"label": map[string]any{"name": "ready-for-agent"},
			"issue": map[string]any{
				"number": 106,
				"title":  "Dispatcher interface test",
			},
		},
	}

	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch returned unexpected error: %v", err)
	}

	if len(runner.getCalls()) != 1 {
		t.Fatalf("expected runner to be invoked via Dispatch")
	}
}

// TestAutonomousFactoryBridge_ErrorHandling verifies nil runner, pipeline errors, and reporter errors.
func TestAutonomousFactoryBridge_ErrorHandling(t *testing.T) {
	t.Run("nil runner returns error on matching event", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		defer bridge.Close()

		ev := schema.TriggerEvent{
			ID:          "err-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "owner/repo",
			IssueNumber: 107,
			Payload: map[string]any{
				"label": map[string]any{"name": "ready-for-agent"},
				"issue": map[string]any{"number": 107, "title": "Test"},
			},
		}

		_, err := bridge.HandleEvent(context.Background(), ev)
		if err == nil {
			t.Errorf("expected error when runner is nil")
		}
	})

	t.Run("pipeline runner error propagation", func(t *testing.T) {
		runner := newMockFactoryRunner()
		runner.runErr = errors.New("underlying workspace allocation error")

		publisher := &mockEventPublisher{}
		reporter := &mockIssueReporter{}
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), runner, publisher, reporter)
		defer bridge.Close()

		ev := schema.TriggerEvent{
			ID:          "err-2",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "owner/repo",
			IssueNumber: 108,
			Payload: map[string]any{
				"label": map[string]any{"name": "ready-for-agent"},
				"issue": map[string]any{"number": 108, "title": "Test Error"},
			},
		}

		res, err := bridge.HandleEvent(context.Background(), ev)
		if err == nil {
			t.Errorf("expected error from runner")
		}
		if res == nil || res.Handled {
			t.Errorf("expected Handled=false on error, got %+v", res)
		}

		// Check failure event emitted
		events := publisher.getEvents()
		var foundFail bool
		for _, e := range events {
			if e.Type == gateway.TopicRunFailed || e.Type == gateway.TopicTaskFailed {
				foundFail = true
				break
			}
		}
		if !foundFail {
			t.Errorf("expected failure event on runner error")
		}
	})

	t.Run("reporter failure does not crash execution", func(t *testing.T) {
		runner := newMockFactoryRunner()
		reporter := &mockIssueReporter{postErr: errors.New("GitHub API 403 Forbidden")}
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), runner, nil, reporter)
		defer bridge.Close()

		ev := schema.TriggerEvent{
			ID:          "err-3",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "owner/repo",
			IssueNumber: 109,
			Payload: map[string]any{
				"label": map[string]any{"name": "ready-for-agent"},
				"issue": map[string]any{"number": 109, "title": "Test Reporter Error"},
			},
		}

		res, err := bridge.HandleEvent(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error when reporter fails: %v", err)
		}
		if !res.Handled {
			t.Errorf("expected Handled=true even if comment reporting failed")
		}
		if res.CommentPosted {
			t.Errorf("expected CommentPosted=false when reporter returns error")
		}
	})
}

// TestAutonomousFactoryBridge_EdgeCasesAndCoverage exercises constructors, fallbacks, and boundary conditions.
func TestAutonomousFactoryBridge_EdgeCasesAndCoverage(t *testing.T) {
	t.Run("empty config triggers fallback defaults", func(t *testing.T) {
		customDedup := NewMemoryDeduplicator(10 * time.Minute)
		defer customDedup.Close()

		emptyCfg := BridgeConfig{
			TargetLabels:     nil,
			CommandTriggers:  nil,
			BaseBranch:       "",
			CLISuggestionCmd: "",
			DeduplicationTTL: -1,
			Deduplicator:     customDedup,
		}

		runner := newMockFactoryRunner()
		bridge := NewAutonomousFactoryBridge(emptyCfg, runner, nil, nil)
		defer bridge.Close()

		if len(bridge.cfg.TargetLabels) == 0 {
			t.Errorf("expected default target labels")
		}
		if len(bridge.cfg.CommandTriggers) == 0 {
			t.Errorf("expected default command triggers")
		}
		if bridge.cfg.BaseBranch != "main" {
			t.Errorf("expected default base branch 'main', got %s", bridge.cfg.BaseBranch)
		}
		if bridge.cfg.CLISuggestionCmd != "ov gate approve" {
			t.Errorf("expected default CLI suggestion command, got %s", bridge.cfg.CLISuggestionCmd)
		}
	})

	t.Run("Dispatch returns error on non-handled non-skipped failure", func(t *testing.T) {
		runner := newMockFactoryRunner()
		runner.runErr = errors.New("boom")
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), runner, nil, nil)
		defer bridge.Close()

		ev := schema.TriggerEvent{
			ID:          "disp-err-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "owner/repo",
			IssueNumber: 501,
			Payload: map[string]any{
				"label": map[string]any{"name": "factory"},
				"issue": map[string]any{"number": 501, "title": "Fail Dispatch"},
			},
		}

		err := bridge.Dispatch(context.Background(), ev)
		if err == nil {
			t.Errorf("expected error from Dispatch when runner fails")
		}
	})

	t.Run("MatchesTrigger nil event", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		defer bridge.Close()

		matched, reason := bridge.MatchesTrigger(nil)
		if matched || reason != "event is nil" {
			t.Errorf("expected false, 'event is nil', got %v, %s", matched, reason)
		}
	})

	t.Run("ExtractLabels string and slice variations", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		defer bridge.Close()

		ev := &schema.TriggerEvent{
			Payload: map[string]any{
				"label": "urgent",
				"issue": map[string]any{
					"labels": []string{"bug", "critical"},
				},
				"pull_request": map[string]any{
					"labels": []any{"frontend", "v2"},
				},
				"object_attributes": map[string]any{
					"labels": []any{
						map[string]any{"title": "backend"},
					},
				},
			},
		}

		labels := bridge.ExtractLabels(ev)
		expected := map[string]bool{"urgent": true, "bug": true, "critical": true, "frontend": true, "v2": true, "backend": true}
		for _, l := range labels {
			delete(expected, l)
		}
		if len(expected) > 0 {
			t.Errorf("missing expected labels: %v (got %v)", expected, labels)
		}

		// Nil ev
		if len(bridge.ExtractLabels(nil)) != 0 {
			t.Errorf("expected empty labels for nil event")
		}
	})

	t.Run("HydrateTaskContext PR-only and fallback title", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		defer bridge.Close()

		evPR := &schema.TriggerEvent{
			Source:     schema.SourceGitHub,
			EventType:  "pr.labeled",
			Repository: "owner/repo",
			PRNumber:   77,
			Payload: map[string]any{
				"pull_request": map[string]any{
					"title": "PR Title",
					"body":  "PR Body text",
				},
			},
		}

		task, branch := bridge.HydrateTaskContext(evPR)
		if task.TaskID != "pr-77" {
			t.Errorf("expected TaskID 'pr-77', got %q", task.TaskID)
		}
		if branch != "ov-run/pr-77" {
			t.Errorf("expected branch 'ov-run/pr-77', got %q", branch)
		}
		if task.Title != "PR Title" {
			t.Errorf("expected title 'PR Title', got %q", task.Title)
		}

		// Event without issue/pr number but with ID
		evID := &schema.TriggerEvent{
			ID:         "delivery-id-999",
			Source:     schema.SourceGitHub,
			EventType:  schema.EventIssueOpened,
			Repository: "owner/repo",
			Payload: map[string]any{
				"object_attributes": map[string]any{
					"title": "GitLab Title",
				},
			},
		}
		taskID, branchID := bridge.HydrateTaskContext(evID)
		if taskID.TaskID != "task-delivery-id-999" {
			t.Errorf("expected task-delivery-id-999, got %q", taskID.TaskID)
		}
		if branchID != "ov-run/task-delivery-id-999" {
			t.Errorf("expected ov-run/task-delivery-id-999, got %q", branchID)
		}
		if taskID.Title != "GitLab Title" {
			t.Errorf("expected 'GitLab Title', got %q", taskID.Title)
		}

		// Event with no title and no ID
		evEmpty := &schema.TriggerEvent{
			Source:    schema.SourceGitHub,
			EventType: schema.EventIssueOpened,
			Payload:   map[string]any{},
		}
		taskEmpty, _ := bridge.HydrateTaskContext(evEmpty)
		if !strings.HasPrefix(taskEmpty.Title, "Autonomous task for") {
			t.Errorf("expected fallback title prefix, got %q", taskEmpty.Title)
		}
	})

	t.Run("postCommentIfAvailable edge cases", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		defer bridge.Close()

		// Nil reporter
		if bridge.postCommentIfAvailable(context.Background(), schema.TriggerEvent{Repository: "repo", IssueNumber: 1}, "msg") {
			t.Errorf("expected false when reporter is nil")
		}

		// Empty repository
		reporter := &mockIssueReporter{}
		bridgeWithRep := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, reporter)
		defer bridgeWithRep.Close()

		if bridgeWithRep.postCommentIfAvailable(context.Background(), schema.TriggerEvent{Repository: "", IssueNumber: 1}, "msg") {
			t.Errorf("expected false when repo is empty")
		}

		// Zero issue/PR number
		if bridgeWithRep.postCommentIfAvailable(context.Background(), schema.TriggerEvent{Repository: "repo", IssueNumber: 0, PRNumber: 0}, "msg") {
			t.Errorf("expected false when issue number is zero")
		}
	})

	t.Run("FormatFeedbackComment nil and human gate with passed reviewers", func(t *testing.T) {
		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), nil, nil, nil)
		defer bridge.Close()

		if bridge.FormatFeedbackComment(nil, "branch", "task") != "" {
			t.Errorf("expected empty string for nil outcome")
		}

		gateOutcome := &agent.PipelineOutcome{
			Decision: agent.DecisionHumanGate,
			Approval: &agent.ApprovalDecision{
				ReviewerResults: []agent.ReviewResult{
					{
						ReviewerName: "architecture",
						Passed:       true,
						Score:        100.0,
						Summary:      "Clean architecture",
					},
				},
			},
		}
		comment := bridge.FormatFeedbackComment(gateOutcome, "branch", "task")
		if !strings.Contains(comment, "Status PASSED") {
			t.Errorf("expected Status PASSED for passed reviewer, got:\n%s", comment)
		}
	})
}
