// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSanitizeBuilderContext_CleanContextInvariant(t *testing.T) {
	raw := &BuilderContext{
		TaskSpec:       "Implement secure authentication endpoint",
		GitDiff:        "diff --git a/internal/auth/auth.go b/internal/auth/auth.go\n+ func Authenticate() {}",
		Stdout:         "BUILD SUCCESS: compiled 14 packages in 1.2s",
		Stderr:         "warning: unused variable 'token'",
		Logs:           []string{"step 1: downloading deps", "step 2: running linter", "step 3: test passed"},
		ChainOfThought: "I need to check if the password is valid, so I will add a check...",
		Metadata: map[string]any{
			"internal_builder_id": "builder-worker-99",
			"memory_usage_mb":     128,
		},
	}

	clean := SanitizeBuilderContext(raw)

	if clean.TaskSpec != raw.TaskSpec {
		t.Errorf("expected TaskSpec %q, got %q", raw.TaskSpec, clean.TaskSpec)
	}
	if clean.GitDiff != raw.GitDiff {
		t.Errorf("expected GitDiff %q, got %q", raw.GitDiff, clean.GitDiff)
	}

	nilClean := SanitizeBuilderContext(nil)
	if nilClean.TaskSpec != "" || nilClean.GitDiff != "" {
		t.Errorf("expected empty CleanContext for nil input, got %+v", nilClean)
	}
}

func TestClassifyTouchedFiles(t *testing.T) {
	diff := `diff --git a/migrations/001_create_users.sql b/migrations/001_create_users.sql
--- a/migrations/001_create_users.sql
+++ b/migrations/001_create_users.sql
@@ -1 +1,2 @@
+CREATE TABLE users (id SERIAL PRIMARY KEY);
diff --git a/pkg/schema/schema.go b/pkg/schema/schema.go
--- a/pkg/schema/schema.go
+++ b/pkg/schema/schema.go
@@ -10 +10,2 @@
+type NewConfig struct{}
diff --git a/internal/auth/session.go b/internal/auth/session.go
--- a/internal/auth/session.go
+++ b/internal/auth/session.go
@@ -5 +5,2 @@
+func ValidateSession() bool { return true }
diff --git a/cmd/ov-server/main.go b/cmd/ov-server/main.go
--- a/cmd/ov-server/main.go
+++ b/cmd/ov-server/main.go
@@ -10 +10,2 @@
+// entrypoint
`

	classification := ClassifyTouchedFiles(diff)

	if len(classification.TouchedFiles) != 4 {
		t.Fatalf("expected 4 touched files, got %d: %v", len(classification.TouchedFiles), classification.TouchedFiles)
	}
	if !classification.HasMigrationChanges {
		t.Errorf("expected HasMigrationChanges to be true")
	}
	if !classification.HasSchemaChanges {
		t.Errorf("expected HasSchemaChanges to be true")
	}
	if !classification.HasAuthChanges {
		t.Errorf("expected HasAuthChanges to be true")
	}

	normalDiff := `diff --git a/internal/sandbox/sandbox.go b/internal/sandbox/sandbox.go
+++ b/internal/sandbox/sandbox.go
@@ -1 +1 @@
+// tweak
`
	normalClassification := ClassifyTouchedFiles(normalDiff)
	if normalClassification.HasMigrationChanges || normalClassification.HasSchemaChanges || normalClassification.HasAuthChanges {
		t.Errorf("expected no sensitive flags for normal sandbox file, got %+v", normalClassification)
	}
}

type spyReviewer struct {
	rType        ReviewerType
	delay        time.Duration
	receivedCtx  CleanContext
	calledCount  int32
	mockFindings []ReviewFinding
	mockCoverage float64
	mockPassed   bool
}

func (s *spyReviewer) Type() ReviewerType {
	return s.rType
}

func (s *spyReviewer) Review(ctx context.Context, clean CleanContext) (ReviewResult, error) {
	atomic.AddInt32(&s.calledCount, 1)
	s.receivedCtx = clean
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return ReviewResult{
		Type:     s.rType,
		Passed:   s.mockPassed,
		Coverage: s.mockCoverage,
		Findings: s.mockFindings,
	}, nil
}

func TestReviewDispatcher_ConcurrentExecutionAndIsolation(t *testing.T) {
	rev1 := &spyReviewer{rType: ReviewerSecurity, delay: 30 * time.Millisecond, mockPassed: true}
	rev2 := &spyReviewer{rType: ReviewerArchitecture, delay: 30 * time.Millisecond, mockPassed: true}
	rev3 := &spyReviewer{rType: ReviewerUIUX, delay: 30 * time.Millisecond, mockPassed: true}
	rev4 := &spyReviewer{rType: ReviewerCodeQuality, delay: 30 * time.Millisecond, mockPassed: true, mockCoverage: 95.0}

	dispatcher := NewReviewDispatcher(rev1, rev2, rev3, rev4)

	rawBuilder := &BuilderContext{
		TaskSpec:       "Build checkout pipeline",
		GitDiff:        "diff --git a/internal/dag/dag.go b/internal/dag/dag.go\n+++ b/internal/dag/dag.go\n+ // new logic",
		Stdout:         "STDOUT SECRET TRACE",
		Stderr:         "STDERR ERROR TRACE",
		Logs:           []string{"log 1", "log 2"},
		ChainOfThought: "Thinking about implementation details...",
	}

	start := time.Now()
	report, err := dispatcher.Dispatch(context.Background(), "run-101", rawBuilder, 1, 3)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("dispatch returned unexpected error: %v", err)
	}

	// 4 reviewers with 30ms each in parallel should take much less than sequential 120ms
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected parallel execution (<100ms), took %v", elapsed)
	}

	for _, rev := range []*spyReviewer{rev1, rev2, rev3, rev4} {
		if atomic.LoadInt32(&rev.calledCount) != 1 {
			t.Errorf("reviewer %s was not called exactly once", rev.rType)
		}
		if rev.receivedCtx.TaskSpec != rawBuilder.TaskSpec {
			t.Errorf("reviewer %s did not receive correct TaskSpec", rev.rType)
		}
		if rev.receivedCtx.GitDiff != rawBuilder.GitDiff {
			t.Errorf("reviewer %s did not receive correct GitDiff", rev.rType)
		}
		if strings.Contains(rev.receivedCtx.TaskSpec, "STDOUT") || strings.Contains(rev.receivedCtx.GitDiff, "Thinking") {
			t.Errorf("dirty traces leaked into reviewer context: %+v", rev.receivedCtx)
		}
	}

	if report.RunID != "run-101" {
		t.Errorf("expected RunID 'run-101', got %s", report.RunID)
	}
	if !report.Passed {
		t.Errorf("expected report to pass, but passed=false")
	}
	if report.AggregateCoverage != 95.0 {
		t.Errorf("expected coverage 95.0, got %f", report.AggregateCoverage)
	}
	if report.FeedbackLoopCount != 1 || report.MaxFeedbackLoops != 3 {
		t.Errorf("unexpected feedback loops: count=%d, max=%d", report.FeedbackLoopCount, report.MaxFeedbackLoops)
	}
	if !report.CanRetryFeedback() {
		t.Errorf("expected CanRetryFeedback to be true")
	}
}

func TestReviewDispatcher_SecurityAlertConsolidation(t *testing.T) {
	dispatcher := NewReviewDispatcher()

	dirtyDiff := `diff --git a/internal/auth/auth.go b/internal/auth/auth.go
--- a/internal/auth/auth.go
+++ b/internal/auth/auth.go
@@ -10 +10,2 @@
+ const apiKey = "api_key_1234567890abcdef"
+ db.Exec("SELECT * FROM users WHERE name = " + input)
`
	raw := &BuilderContext{
		TaskSpec: "Add auth keys",
		GitDiff:  dirtyDiff,
	}

	report, err := dispatcher.Dispatch(context.Background(), "run-sec-01", raw, 3, 3)
	if err != nil {
		t.Fatalf("unexpected dispatch error: %v", err)
	}

	if report.Passed {
		t.Errorf("expected report to fail due to critical security alert")
	}
	if report.CriticalAlertsCount == 0 {
		t.Errorf("expected at least 1 critical alert, got %d", report.CriticalAlertsCount)
	}
	if !report.Classification.HasAuthChanges {
		t.Errorf("expected classification to detect auth changes")
	}
	if report.CanRetryFeedback() {
		t.Errorf("expected CanRetryFeedback to be false when feedbackCount >= max")
	}
}
