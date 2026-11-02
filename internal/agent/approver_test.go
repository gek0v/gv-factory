// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockDiffReviewer implements DiffReviewer for testing Approver.
type mockDiffReviewer struct {
	name      string
	result    *ReviewResult
	err       error
	delay     time.Duration
	callCount int32
}

func (m *mockDiffReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		res := *m.result
		if res.ReviewerName == "" {
			res.ReviewerName = m.name
		}
		return &res, nil
	}
	return &ReviewResult{
		ReviewerName: m.name,
		Passed:       true,
		Score:        1.0,
		Severity:     SeverityInfo,
		Summary:      "All checks passed",
	}, nil
}

func TestApprover_DecisionApproved(t *testing.T) {
	r1 := &mockDiffReviewer{
		name: "SecurityReviewer",
		result: &ReviewResult{
			ReviewerName: "SecurityReviewer",
			Passed:       true,
			Score:        0.95,
			Severity:     SeverityInfo,
			Summary:      "No security issues detected",
		},
	}
	r2 := &mockDiffReviewer{
		name: "ArchitectureReviewer",
		result: &ReviewResult{
			ReviewerName: "ArchitectureReviewer",
			Passed:       true,
			Score:        0.90,
			Severity:     SeverityInfo,
			Summary:      "No architectural violations",
		},
	}

	approver := NewApprover([]DiffReviewer{r1, r2}, ApproverConfig{
		MinApprovalScore: 0.85,
		MinCoverage:      0.90,
		MaxRetries:       3,
	})

	req := ApprovalRequest{
		RunID:        "run-approved-1",
		Diff:         "+line",
		FilesChanged: []string{"pkg/math/calc.go"},
		Coverage:     0.92,
		RetryCount:   0,
	}

	decision, err := approver.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.DecisionType != DecisionApproved {
		t.Errorf("expected DecisionApproved, got %s (reason: %s)", decision.DecisionType, decision.GateReason)
	}
	if decision.RequiresHumanSign {
		t.Errorf("expected RequiresHumanSign=false for approved run")
	}
	if decision.ConsolidatedScore < 0.90 {
		t.Errorf("expected ConsolidatedScore >= 0.90, got %f", decision.ConsolidatedScore)
	}
	if len(decision.ReviewerResults) != 2 {
		t.Errorf("expected 2 reviewer results, got %d", len(decision.ReviewerResults))
	}
}

func TestApprover_DecisionRetry_FixableErrors(t *testing.T) {
	r1 := &mockDiffReviewer{
		name: "SecurityReviewer",
		result: &ReviewResult{
			ReviewerName: "SecurityReviewer",
			Passed:       true,
			Score:        0.95,
			Severity:     SeverityInfo,
		},
	}
	r2 := &mockDiffReviewer{
		name: "ArchitectureReviewer",
		result: &ReviewResult{
			ReviewerName: "ArchitectureReviewer",
			Passed:       false,
			Score:        0.60,
			Severity:     SeverityError,
			Findings: []Finding{
				{
					File:     "pkg/calc.go",
					Line:     12,
					RuleCode: "ARCH-SPDX-HEADER",
					Message:  "Missing SPDX header",
					Severity: SeverityError,
				},
			},
			Summary: "Architectural violations found",
		},
	}

	approver := NewApprover([]DiffReviewer{r1, r2}, ApproverConfig{
		MinApprovalScore: 0.85,
		MinCoverage:      0.90,
		MaxRetries:       3,
	})

	req := ApprovalRequest{
		RunID:        "run-retry-1",
		Diff:         "+line",
		FilesChanged: []string{"pkg/calc.go"},
		Coverage:     0.95,
		RetryCount:   1, // < MaxRetries
	}

	decision, err := approver.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.DecisionType != DecisionRetry {
		t.Errorf("expected DecisionRetry, got %s", decision.DecisionType)
	}
	if decision.RequiresHumanSign {
		t.Errorf("expected RequiresHumanSign=false for retry")
	}
	if !strings.Contains(decision.FeedbackSummary, "ARCH-SPDX-HEADER") {
		t.Errorf("expected actionable feedback mentioning ARCH-SPDX-HEADER, got: %s", decision.FeedbackSummary)
	}
}

func TestApprover_DecisionRetry_CoverageBelowThreshold(t *testing.T) {
	r1 := &mockDiffReviewer{name: "SecurityReviewer"}
	r2 := &mockDiffReviewer{name: "ArchitectureReviewer"}

	approver := NewApprover([]DiffReviewer{r1, r2}, ApproverConfig{
		MinApprovalScore: 0.85,
		MinCoverage:      0.90,
		MaxRetries:       3,
	})

	req := ApprovalRequest{
		RunID:        "run-low-cov",
		Diff:         "+line",
		FilesChanged: []string{"pkg/calc.go"},
		Coverage:     0.78, // Below 0.90
		RetryCount:   0,
	}

	decision, err := approver.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.DecisionType != DecisionRetry {
		t.Errorf("expected DecisionRetry due to low coverage, got %s", decision.DecisionType)
	}
	if !strings.Contains(decision.FeedbackSummary, "coverage") {
		t.Errorf("expected feedback mentioning coverage, got: %s", decision.FeedbackSummary)
	}
}

func TestApprover_DecisionHumanGate_CriticalSecurity(t *testing.T) {
	r1 := &mockDiffReviewer{
		name: "SecurityReviewer",
		result: &ReviewResult{
			ReviewerName: "SecurityReviewer",
			Passed:       false,
			Score:        0.1,
			Severity:     SeverityCritical,
			Findings: []Finding{
				{
					File:     "config.go",
					Line:     5,
					RuleCode: "SEC-SECRET-001",
					Message:  "Hardcoded AWS secret key detected",
					Severity: SeverityCritical,
				},
			},
			Summary: "Critical security vulnerabilities detected",
		},
	}

	approver := NewApprover([]DiffReviewer{r1}, ApproverConfig{
		MinApprovalScore: 0.85,
		MaxRetries:       3,
	})

	req := ApprovalRequest{
		RunID:        "run-sec-crit",
		Diff:         "+key",
		FilesChanged: []string{"config.go"},
		Coverage:     0.95,
		RetryCount:   0,
	}

	decision, err := approver.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.DecisionType != DecisionHumanGate {
		t.Errorf("expected DecisionHumanGate for critical security, got %s", decision.DecisionType)
	}
	if !decision.RequiresHumanSign {
		t.Errorf("expected RequiresHumanSign=true for critical security")
	}
	if !strings.Contains(strings.ToLower(decision.GateReason), "security") && !strings.Contains(strings.ToLower(decision.GateReason), "critical") {
		t.Errorf("expected gate reason to mention security/critical, got: %s", decision.GateReason)
	}
}

func TestApprover_DecisionHumanGate_ExceededRetries(t *testing.T) {
	r1 := &mockDiffReviewer{
		name: "ArchitectureReviewer",
		result: &ReviewResult{
			ReviewerName: "ArchitectureReviewer",
			Passed:       false,
			Score:        0.5,
			Severity:     SeverityError,
			Findings: []Finding{
				{
					File:     "pkg/a.go",
					Line:     1,
					RuleCode: "ARCH-BOUNDARY",
					Message:  "Boundary error",
					Severity: SeverityError,
				},
			},
		},
	}

	approver := NewApprover([]DiffReviewer{r1}, ApproverConfig{
		MinApprovalScore: 0.85,
		MaxRetries:       3,
	})

	req := ApprovalRequest{
		RunID:        "run-max-retries",
		Diff:         "+line",
		FilesChanged: []string{"pkg/a.go"},
		Coverage:     0.92,
		RetryCount:   3, // == MaxRetries
	}

	decision, err := approver.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.DecisionType != DecisionHumanGate {
		t.Errorf("expected DecisionHumanGate when retries exceeded, got %s", decision.DecisionType)
	}
	if !decision.RequiresHumanSign {
		t.Errorf("expected RequiresHumanSign=true when retries exceeded")
	}
	if !strings.Contains(strings.ToLower(decision.GateReason), "retry") && !strings.Contains(strings.ToLower(decision.GateReason), "retries") {
		t.Errorf("expected GateReason to mention retries, got: %s", decision.GateReason)
	}
}

func TestApprover_DecisionHumanGate_SensitivePaths(t *testing.T) {
	r1 := &mockDiffReviewer{name: "SecurityReviewer"}
	r2 := &mockDiffReviewer{name: "ArchitectureReviewer"}

	sensitiveCases := []struct {
		name string
		file string
	}{
		{"migration file", "migrations/brain/002_vector_index.sql"},
		{"auth file", "internal/auth/tokens.go"},
		{"deployments file", "deployments/docker-compose.yml"},
	}

	for _, sc := range sensitiveCases {
		t.Run(sc.name, func(t *testing.T) {
			approver := NewApprover([]DiffReviewer{r1, r2}, ApproverConfig{
				MinApprovalScore: 0.85,
				MinCoverage:      0.90,
				MaxRetries:       3,
				SensitivePaths:   []string{"migrations/", "internal/auth/", "deployments/"},
			})

			req := ApprovalRequest{
				RunID:        "run-sens-" + sc.name,
				Diff:         "+line",
				FilesChanged: []string{sc.file},
				Coverage:     0.95,
				RetryCount:   0,
			}

			decision, err := approver.Decide(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if decision.DecisionType != DecisionHumanGate {
				t.Errorf("expected DecisionHumanGate for sensitive file %s, got %s", sc.file, decision.DecisionType)
			}
			if !decision.RequiresHumanSign {
				t.Errorf("expected RequiresHumanSign=true for sensitive path")
			}
			if !strings.Contains(strings.ToLower(decision.GateReason), "sensitive") {
				t.Errorf("expected GateReason to mention sensitive path, got: %s", decision.GateReason)
			}
		})
	}
}

func TestApprover_ParallelExecution(t *testing.T) {
	var activeGoroutines int32
	var maxConcurrent int32
	var mu sync.Mutex

	reviewers := make([]DiffReviewer, 4)
	for i := 0; i < 4; i++ {
		revName := "reviewer"
		reviewers[i] = &mockDiffReviewer{
			name:  revName,
			delay: 20 * time.Millisecond,
		}
	}

	// Wrap to track concurrency
	wrappedReviewers := make([]DiffReviewer, len(reviewers))
	for i, r := range reviewers {
		curr := r
		wrappedReviewers[i] = &customFnReviewer{
			fn: func(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
				cur := atomic.AddInt32(&activeGoroutines, 1)
				mu.Lock()
				if cur > maxConcurrent {
					maxConcurrent = cur
				}
				mu.Unlock()
				defer atomic.AddInt32(&activeGoroutines, -1)

				return curr.Review(ctx, req)
			},
		}
	}

	approver := NewApprover(wrappedReviewers, ApproverConfig{
		MinApprovalScore: 0.80,
		MinCoverage:      0.80,
	})

	start := time.Now()
	decision, err := approver.Decide(context.Background(), ApprovalRequest{
		RunID:    "run-par",
		Coverage: 0.90,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.DecisionType != DecisionApproved {
		t.Errorf("expected DecisionApproved, got %s", decision.DecisionType)
	}

	// If ran in parallel, maxConcurrent should be > 1 and elapsed should be well below 4 * 20ms = 80ms
	if maxConcurrent < 2 {
		t.Errorf("expected at least 2 concurrent reviewers, got maxConcurrent=%d", maxConcurrent)
	}
	if elapsed > 70*time.Millisecond {
		t.Logf("warning: elapsed %v indicates slower execution than expected for 20ms parallel tasks", elapsed)
	}
}

func TestApprover_ReviewerErrorResilience(t *testing.T) {
	r1 := &mockDiffReviewer{
		name: "FailingReviewer",
		err:  errors.New("network failure during AST parse"),
	}
	r2 := &mockDiffReviewer{
		name: "PassingReviewer",
		result: &ReviewResult{
			ReviewerName: "PassingReviewer",
			Passed:       true,
			Score:        1.0,
			Severity:     SeverityInfo,
		},
	}

	approver := NewApprover([]DiffReviewer{r1, r2}, ApproverConfig{
		MinApprovalScore: 0.80,
		MaxRetries:       2,
	})

	decision, err := approver.Decide(context.Background(), ApprovalRequest{
		RunID:      "run-err-res",
		Coverage:   0.90,
		RetryCount: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error from Approver: %v", err)
	}

	// Should not panic, and failing reviewer error should result in retry or gate
	if decision.DecisionType != DecisionRetry {
		t.Errorf("expected DecisionRetry when reviewer encounters error, got %s", decision.DecisionType)
	}
}

func TestApprover_ContextCanceled(t *testing.T) {
	r1 := &mockDiffReviewer{
		name:  "SlowReviewer",
		delay: 500 * time.Millisecond,
	}

	approver := NewApprover([]DiffReviewer{r1}, ApproverConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := approver.Decide(ctx, ApprovalRequest{RunID: "run-timeout"})
	if err == nil {
		t.Fatalf("expected context error on timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected DeadlineExceeded or Canceled, got %v", err)
	}
}

// customFnReviewer allows custom inline logic for DiffReviewer
type customFnReviewer struct {
	fn func(ctx context.Context, req ReviewRequest) (*ReviewResult, error)
}

func (c *customFnReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	return c.fn(ctx, req)
}

func TestApprover_HelperMethodsAndDefaults(t *testing.T) {
	// Test NewApprover with nil reviewers triggers default reviewers
	approver := NewApprover(nil, ApproverConfig{})
	if len(approver.reviewers) != 2 {
		t.Errorf("expected 2 default reviewers, got %d", len(approver.reviewers))
	}

	// Test Evaluate alias
	dec, err := approver.Evaluate(context.Background(), ApprovalRequest{
		RunID:    "run-eval-test",
		Diff:     "+func Hello() {}",
		Coverage: 0.95,
	})
	if err != nil {
		t.Fatalf("unexpected error from Evaluate: %v", err)
	}
	if dec == nil {
		t.Fatalf("expected non-nil decision")
	}

	// Test decision helper methods
	apprDec := &ApprovalDecision{DecisionType: DecisionApproved}
	if !apprDec.IsApproved() || apprDec.IsRetry() || apprDec.IsHumanGate() {
		t.Errorf("helpers mismatch for DecisionApproved")
	}

	retryDec := &ApprovalDecision{DecisionType: DecisionRetry}
	if retryDec.IsApproved() || !retryDec.IsRetry() || retryDec.IsHumanGate() {
		t.Errorf("helpers mismatch for DecisionRetry")
	}

	gateDec := &ApprovalDecision{DecisionType: DecisionHumanGate}
	if gateDec.IsApproved() || gateDec.IsRetry() || !gateDec.IsHumanGate() {
		t.Errorf("helpers mismatch for DecisionHumanGate")
	}

	var nilDec *ApprovalDecision
	if nilDec.IsApproved() || nilDec.IsRetry() || nilDec.IsHumanGate() {
		t.Errorf("nil decision should return false for all helpers")
	}
}

