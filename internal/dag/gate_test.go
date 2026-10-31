// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

type mockMergeQueue struct {
	mu       sync.Mutex
	enqueued []string
	metadata map[string]map[string]any
}

func newMockMergeQueue() *mockMergeQueue {
	return &mockMergeQueue{
		metadata: make(map[string]map[string]any),
	}
}

func (m *mockMergeQueue) EnqueueApproval(ctx context.Context, runID string, meta map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enqueued = append(m.enqueued, runID)
	m.metadata[runID] = meta
	return nil
}

type mockPRCreator struct {
	mu       sync.Mutex
	created  []PRDetails
	prNumber int
}

func newMockPRCreator() *mockPRCreator {
	return &mockPRCreator{}
}

func (m *mockPRCreator) CreatePullRequest(ctx context.Context, details PRDetails) (*PRResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created = append(m.created, details)
	m.prNumber++
	return &PRResult{
		PRNumber: m.prNumber,
		PRURL:    "https://github.com/gv-factory/gv-factory/pull/" + details.RunID,
	}, nil
}

func TestDecisionGate_FastTrackSuccess(t *testing.T) {
	authSvc := auth.NewService("gate-test-secret-001")
	mq := newMockMergeQueue()
	pr := newMockPRCreator()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	gate := NewDecisionGate(authSvc, gw, pr, mq, DecisionGateConfig{})

	report := &ApprovalReport{
		RunID:               "run-ft-1",
		Passed:              true,
		AggregateCoverage:   95.5,
		CriticalAlertsCount: 0,
		HighAlertsCount:     0,
		TotalSecurityAlerts: 0,
		Classification: FileClassification{
			TouchedFiles: []string{"internal/dag/dag.go"},
		},
	}

	gates := schema.GatesConfig{
		MinCoveragePercent: 90.0,
		MaxSecurityAlerts:  0,
		RequireApproval:    false,
	}

	state, token, err := gate.Evaluate(context.Background(), "run-ft-1", report, gates, "ov-run/run-ft-1")
	if err != nil {
		t.Fatalf("unexpected evaluate error: %v", err)
	}

	if state != RunStateApproved {
		t.Errorf("expected RunStateApproved for low-risk run, got %s", state)
	}
	if token != "" {
		t.Errorf("expected no token for fast-tracked run, got %s", token)
	}

	mq.mu.Lock()
	defer mq.mu.Unlock()
	if len(mq.enqueued) != 1 || mq.enqueued[0] != "run-ft-1" {
		t.Errorf("expected run-ft-1 enqueued in merge queue, got %v", mq.enqueued)
	}
}

func TestDecisionGate_PauseOnRiskAndEmitEvent(t *testing.T) {
	authSvc := auth.NewService("gate-test-secret-001")
	mq := newMockMergeQueue()
	pr := newMockPRCreator()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	eventsCh := make(chan gateway.Event, 10)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), gateway.TopicGateApprovalRequired, eventsCh)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer unsub()

	gate := NewDecisionGate(authSvc, gw, pr, mq, DecisionGateConfig{
		TokenTTL: 1 * time.Hour,
	})

	testCases := []struct {
		name   string
		runID  string
		report *ApprovalReport
	}{
		{
			name:  "low coverage",
			runID: "run-low-cov",
			report: &ApprovalReport{
				RunID:             "run-low-cov",
				Passed:            true,
				AggregateCoverage: 82.0, // below 90%
				Summary:           "Coverage 82% below threshold",
			},
		},
		{
			name:  "critical security alert",
			runID: "run-sec-risk",
			report: &ApprovalReport{
				RunID:               "run-sec-risk",
				Passed:              false,
				AggregateCoverage:   95.0,
				CriticalAlertsCount: 1,
				Summary:             "Hardcoded secret detected",
			},
		},
		{
			name:  "database migration changed",
			runID: "run-migration",
			report: &ApprovalReport{
				RunID:             "run-migration",
				Passed:            true,
				AggregateCoverage: 95.0,
				Classification: FileClassification{
					HasMigrationChanges: true,
					TouchedFiles:        []string{"migrations/002_add_table.sql"},
				},
				Summary: "Migration file altered",
			},
		},
		{
			name:  "auth module changed",
			runID: "run-auth-mod",
			report: &ApprovalReport{
				RunID:             "run-auth-mod",
				Passed:            true,
				AggregateCoverage: 95.0,
				Classification: FileClassification{
					HasAuthChanges: true,
					TouchedFiles:   []string{"internal/auth/auth.go"},
				},
				Summary: "Internal auth file touched",
			},
		},
	}

	gates := schema.GatesConfig{
		MinCoveragePercent: 90.0,
		MaxSecurityAlerts:  0,
		RequireApproval:    false,
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state, token, err := gate.Evaluate(context.Background(), tc.runID, tc.report, gates, "ov-run/"+tc.runID)
			if err != nil {
				t.Fatalf("unexpected evaluate error: %v", err)
			}

			if state != RunStateAwaitingApproval {
				t.Errorf("expected RunStateAwaitingApproval, got %s", state)
			}
			if token == "" {
				t.Errorf("expected non-empty HMAC approval token")
			}

			// Validate token authenticity with auth service
			extractedID, valid := authSvc.ValidateApprovalToken(token)
			if !valid || extractedID != tc.runID {
				t.Errorf("expected token to validate to %s, got id=%s, valid=%t", tc.runID, extractedID, valid)
			}

			// Receive event
			select {
			case ev := <-eventsCh:
				if ev.Type != gateway.TopicGateApprovalRequired {
					t.Errorf("expected event type %s, got %s", gateway.TopicGateApprovalRequired, ev.Type)
				}
				if ev.RunID != tc.runID {
					t.Errorf("expected event RunID %s, got %s", tc.runID, ev.RunID)
				}
				if ev.Payload["token"] != token {
					t.Errorf("expected event to include token %s", token)
				}
			case <-time.After(500 * time.Millisecond):
				t.Errorf("timed out waiting for gateway event for %s", tc.runID)
			}
		})
	}
}

func TestDecisionGate_SubmitApproval_Approve(t *testing.T) {
	authSvc := auth.NewService("gate-test-secret-002")
	mq := newMockMergeQueue()
	pr := newMockPRCreator()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	gate := NewDecisionGate(authSvc, gw, pr, mq, DecisionGateConfig{})

	report := &ApprovalReport{
		RunID:             "run-submit-app",
		Passed:            true,
		AggregateCoverage: 85.0,
	}
	gates := schema.GatesConfig{MinCoveragePercent: 90.0}

	state, token, err := gate.Evaluate(context.Background(), "run-submit-app", report, gates, "ov-run/run-submit-app")
	if err != nil || state != RunStateAwaitingApproval {
		t.Fatalf("setup failed: state=%s, err=%v", state, err)
	}

	approval, err := gate.SubmitApproval(context.Background(), token, DecisionApprove)
	if err != nil {
		t.Fatalf("unexpected error submitting approval: %v", err)
	}

	if approval.State != RunStateApproved {
		t.Errorf("expected state RunStateApproved, got %s", approval.State)
	}
	if approval.Decision != DecisionApprove {
		t.Errorf("expected decision DecisionApprove, got %s", approval.Decision)
	}
	if approval.ResolvedAt == nil {
		t.Errorf("expected ResolvedAt timestamp to be set")
	}

	mq.mu.Lock()
	defer mq.mu.Unlock()
	if len(mq.enqueued) != 1 || mq.enqueued[0] != "run-submit-app" {
		t.Errorf("expected run-submit-app in merge queue, got %v", mq.enqueued)
	}

	if _, err := gate.SubmitApproval(context.Background(), token, DecisionApprove); err != ErrAlreadyResolved {
		t.Errorf("expected ErrAlreadyResolved, got %v", err)
	}
}

func TestDecisionGate_SubmitApproval_Reject_PRDiversion(t *testing.T) {
	authSvc := auth.NewService("gate-test-secret-003")
	mq := newMockMergeQueue()
	pr := newMockPRCreator()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	gate := NewDecisionGate(authSvc, gw, pr, mq, DecisionGateConfig{})

	report := &ApprovalReport{
		RunID:               "run-submit-rej",
		Passed:              false,
		AggregateCoverage:   88.0,
		CriticalAlertsCount: 1,
		HighAlertsCount:     2,
		Classification: FileClassification{
			HasMigrationChanges: true,
			TouchedFiles:        []string{"migrations/003.sql"},
		},
		Summary: "Security alerts and migration changes detected",
	}
	gates := schema.GatesConfig{MinCoveragePercent: 90.0}

	state, token, err := gate.Evaluate(context.Background(), "run-submit-rej", report, gates, "ov-run/run-submit-rej")
	if err != nil || state != RunStateAwaitingApproval {
		t.Fatalf("setup failed: state=%s, err=%v", state, err)
	}

	approval, err := gate.SubmitApproval(context.Background(), token, DecisionReject)
	if err != nil {
		t.Fatalf("unexpected error submitting rejection: %v", err)
	}

	if approval.State != RunStateRejected {
		t.Errorf("expected state RunStateRejected, got %s", approval.State)
	}
	if approval.Decision != DecisionReject {
		t.Errorf("expected decision DecisionReject, got %s", approval.Decision)
	}

	mq.mu.Lock()
	if len(mq.enqueued) != 0 {
		t.Errorf("expected merge queue to be empty on rejection, got %v", mq.enqueued)
	}
	mq.mu.Unlock()

	pr.mu.Lock()
	defer pr.mu.Unlock()
	if len(pr.created) != 1 {
		t.Fatalf("expected exactly 1 PR created, got %d", len(pr.created))
	}
	createdPR := pr.created[0]
	if createdPR.RunID != "run-submit-rej" {
		t.Errorf("expected PR runID run-submit-rej, got %s", createdPR.RunID)
	}
	if !strings.Contains(createdPR.Description, "Security alerts and migration changes detected") {
		t.Errorf("expected diagnostics summary in PR description, got: %s", createdPR.Description)
	}
	if !strings.Contains(createdPR.Description, "Critical Security Alerts: `1`") {
		t.Errorf("expected alert details in PR description, got: %s", createdPR.Description)
	}
	if approval.PRURL == "" {
		t.Errorf("expected PRURL to be recorded on approval, got empty string")
	}
}

func TestDecisionGate_InvalidAndExpiredTokens(t *testing.T) {
	authSvc := auth.NewService("gate-test-secret-004")
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	gate := NewDecisionGate(authSvc, gw, nil, nil, DecisionGateConfig{})

	// 1. Invalid format
	if _, err := gate.SubmitApproval(context.Background(), "invalid-token", DecisionApprove); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken for garbage token, got %v", err)
	}

	// 2. Tampered token
	validToken := authSvc.GenerateApprovalToken("run-xyz", time.Now().Add(1*time.Hour))
	tamperedToken := validToken + "bad"
	if _, err := gate.SubmitApproval(context.Background(), tamperedToken, DecisionApprove); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken for tampered token, got %v", err)
	}

	// 3. Expired token
	expiredToken := authSvc.GenerateApprovalToken("run-xyz", time.Now().Add(-1*time.Minute))
	if _, err := gate.SubmitApproval(context.Background(), expiredToken, DecisionApprove); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken for expired token, got %v", err)
	}
}

func TestDecisionGate_ConcurrentEvaluationsAndApprovalsRace(t *testing.T) {
	authSvc := auth.NewService("gate-test-secret-race")
	mq := newMockMergeQueue()
	pr := newMockPRCreator()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	gate := NewDecisionGate(authSvc, gw, pr, mq, DecisionGateConfig{})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runID := "concurrent-run-" + string(rune('a'+id))
			report := &ApprovalReport{
				RunID:             runID,
				AggregateCoverage: 80.0,
			}
			gates := schema.GatesConfig{MinCoveragePercent: 90.0}

			state, token, err := gate.Evaluate(context.Background(), runID, report, gates, "head")
			if err != nil || state != RunStateAwaitingApproval {
				t.Errorf("concurrent evaluate failed for %s: %v", runID, err)
				return
			}

			decision := DecisionApprove
			if id%2 == 0 {
				decision = DecisionReject
			}

			_, subErr := gate.SubmitApproval(context.Background(), token, decision)
			if subErr != nil {
				t.Errorf("concurrent submit failed for %s: %v", runID, subErr)
			}
		}(i)
	}

	wg.Wait()
}
