// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

var (
	// ErrInvalidToken is returned when an approval token is tampered, malformed, or expired.
	ErrInvalidToken = errors.New("invalid or expired approval token")

	// ErrRunNotFound is returned when no approval record matches the requested run.
	ErrRunNotFound = errors.New("approval request not found for run")

	// ErrAlreadyResolved is returned when an approval decision was already submitted.
	ErrAlreadyResolved = errors.New("approval request already resolved")

	// ErrInvalidDecision is returned when an unrecognized decision string is provided.
	ErrInvalidDecision = errors.New("invalid approval decision: must be APPROVE or REJECT")
)

// RunState represents the lifecycle status of a run through the decision gate.
type RunState string

const (
	RunStatePending          RunState = "PENDING"
	RunStateAwaitingApproval RunState = "AWAITING_APPROVAL"
	RunStateApproved         RunState = "APPROVED"
	RunStateRejected         RunState = "REJECTED"
	RunStateFailed           RunState = "FAILED"
)

// GateDecision represents the human reviewer's final verdict.
type GateDecision string

const (
	DecisionApprove GateDecision = "APPROVE"
	DecisionReject  GateDecision = "REJECT"
)

// PRDetails provides metadata needed to open a GitHub Pull Request upon rejection/review diversion.
type PRDetails struct {
	RunID       string `json:"run_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	HeadBranch  string `json:"head_branch"`
	BaseBranch  string `json:"base_branch"`
}

// PRResult holds details of the created Pull Request.
type PRResult struct {
	PRNumber int    `json:"pr_number"`
	PRURL    string `json:"pr_url"`
}

// PRCreator abstracts the creation of GitHub Pull Requests.
type PRCreator interface {
	CreatePullRequest(ctx context.Context, details PRDetails) (*PRResult, error)
}

// MergeQueueReceiver abstracts enqueuing approved runs into the sequential merge queue.
type MergeQueueReceiver interface {
	EnqueueApproval(ctx context.Context, runID string, metadata map[string]any) error
}

// ActiveApproval tracks the state of a run undergoing or awaiting approval.
type ActiveApproval struct {
	RunID       string             `json:"run_id"`
	Token       string             `json:"token,omitempty"`
	ExpiresAt   time.Time          `json:"expires_at"`
	Report      *ApprovalReport    `json:"report"`
	GatesConfig schema.GatesConfig `json:"gates_config"`
	State       RunState           `json:"state"`
	Decision    GateDecision       `json:"decision,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	ResolvedAt  *time.Time         `json:"resolved_at,omitempty"`
	PRURL       string             `json:"pr_url,omitempty"`
	HeadBranch  string             `json:"head_branch,omitempty"`
	BaseBranch  string             `json:"base_branch,omitempty"`
}

// DecisionGateConfig contains configuration options for the decision gate engine.
type DecisionGateConfig struct {
	TokenTTL          time.Duration
	DefaultBaseBranch string
}

// DecisionGate manages deterministic gate evaluation, HMAC approval token lifecycles, and routing.
type DecisionGate struct {
	mu          sync.RWMutex
	authService *auth.Service
	publisher   EventPublisher
	prCreator   PRCreator
	mergeQueue  MergeQueueReceiver
	tokenTTL    time.Duration
	baseBranch  string
	approvals   map[string]*ActiveApproval
}

// NewDecisionGate constructs a new DecisionGate engine.
func NewDecisionGate(
	authService *auth.Service,
	publisher EventPublisher,
	prCreator PRCreator,
	mergeQueue MergeQueueReceiver,
	cfg DecisionGateConfig,
) *DecisionGate {
	ttl := cfg.TokenTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	baseBranch := cfg.DefaultBaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	return &DecisionGate{
		authService: authService,
		publisher:   publisher,
		prCreator:   prCreator,
		mergeQueue:  mergeQueue,
		tokenTTL:    ttl,
		baseBranch:  baseBranch,
		approvals:   make(map[string]*ActiveApproval),
	}
}

// Evaluate evaluates the ApprovalReport against the GatesConfig:
// If conditions for Fast-Track are met, marks state as APPROVED and enqueues to merge queue.
// If any gate condition requires human review, generates an HMAC token, marks AWAITING_APPROVAL,
// and emits gate.approval_required over internal/gateway.
func (g *DecisionGate) Evaluate(
	ctx context.Context,
	runID string,
	report *ApprovalReport,
	gates schema.GatesConfig,
	headBranch string,
) (RunState, string, error) {
	if runID == "" {
		return RunStateFailed, "", fmt.Errorf("runID cannot be empty")
	}
	if report == nil {
		return RunStateFailed, "", fmt.Errorf("approval report cannot be nil")
	}

	minCoverage := gates.MinCoveragePercent
	if minCoverage <= 0 {
		minCoverage = 90.0
	}
	maxAlerts := gates.MaxSecurityAlerts

	canFastTrack := !gates.RequireApproval &&
		report.AggregateCoverage >= minCoverage &&
		report.TotalSecurityAlerts <= maxAlerts &&
		report.CriticalAlertsCount == 0 &&
		report.HighAlertsCount == 0 &&
		!report.Classification.HasMigrationChanges &&
		!report.Classification.HasSchemaChanges &&
		!report.Classification.HasAuthChanges

	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()

	if canFastTrack {
		approval := &ActiveApproval{
			RunID:       runID,
			Report:      report,
			GatesConfig: gates,
			State:       RunStateApproved,
			CreatedAt:   now,
			ResolvedAt:  &now,
			HeadBranch:  headBranch,
			BaseBranch:  g.baseBranch,
		}
		g.approvals[runID] = approval

		if g.mergeQueue != nil {
			_ = g.mergeQueue.EnqueueApproval(ctx, runID, map[string]any{
				"fast_track": true,
				"coverage":   report.AggregateCoverage,
			})
		}

		return RunStateApproved, "", nil
	}

	// Human-in-the-Loop Required: pause workflow, generate HMAC approval token, and emit gateway event
	expiresAt := now.Add(g.tokenTTL)
	token := g.authService.GenerateApprovalToken(runID, expiresAt)

	approval := &ActiveApproval{
		RunID:       runID,
		Token:       token,
		ExpiresAt:   expiresAt,
		Report:      report,
		GatesConfig: gates,
		State:       RunStateAwaitingApproval,
		CreatedAt:   now,
		HeadBranch:  headBranch,
		BaseBranch:  g.baseBranch,
	}
	g.approvals[runID] = approval

	if g.publisher != nil {
		_ = g.publisher.PublishTaskEvent(ctx, gateway.Event{
			Type:  gateway.TopicGateApprovalRequired,
			RunID: runID,
			Payload: map[string]any{
				"token":               token,
				"run_id":              runID,
				"expires_at":          expiresAt.Unix(),
				"diagnostics_summary": report.Summary,
				"report":              report,
			},
			Timestamp: now.Unix(),
		})
	}

	return RunStateAwaitingApproval, token, nil
}

// SubmitApproval handles human reviewer resolution (APPROVE or REJECT).
func (g *DecisionGate) SubmitApproval(ctx context.Context, token string, decision GateDecision) (*ActiveApproval, error) {
	if decision != DecisionApprove && decision != DecisionReject {
		return nil, ErrInvalidDecision
	}

	runID, valid := g.authService.ValidateApprovalToken(token)
	if !valid || runID == "" {
		return nil, ErrInvalidToken
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	approval, exists := g.approvals[runID]
	if !exists {
		return nil, ErrRunNotFound
	}
	if approval.State != RunStateAwaitingApproval {
		return nil, ErrAlreadyResolved
	}

	now := time.Now()
	approval.ResolvedAt = &now
	approval.Decision = decision

	switch decision {
	case DecisionApprove:
		approval.State = RunStateApproved
		if g.mergeQueue != nil {
			_ = g.mergeQueue.EnqueueApproval(ctx, runID, map[string]any{
				"human_approved": true,
				"decision":       string(decision),
			})
		}
		return approval, nil

	case DecisionReject:
		approval.State = RunStateRejected

		if g.prCreator != nil {
			desc := fmt.Sprintf("## Factory Run Rejected by Gate Reviewer\n\n"+
				"**Run ID:** `%s`\n"+
				"**Decision:** `%s`\n\n"+
				"### Reviewer Diagnostics Summary:\n%s\n\n"+
				"**Detailed Metrics:**\n"+
				"- Aggregate Test Coverage: `%.1f%%`\n"+
				"- Critical Security Alerts: `%d`\n"+
				"- High Security Alerts: `%d`\n"+
				"- Database Migrations Modified: `%t`\n"+
				"- Schemas Modified: `%t`\n"+
				"- Auth Module Modified: `%t`\n",
				runID,
				decision,
				approval.Report.Summary,
				approval.Report.AggregateCoverage,
				approval.Report.CriticalAlertsCount,
				approval.Report.HighAlertsCount,
				approval.Report.Classification.HasMigrationChanges,
				approval.Report.Classification.HasSchemaChanges,
				approval.Report.Classification.HasAuthChanges,
			)

			head := approval.HeadBranch
			if head == "" {
				head = fmt.Sprintf("ov-run/%s", runID)
			}

			res, err := g.prCreator.CreatePullRequest(ctx, PRDetails{
				RunID:       runID,
				Title:       fmt.Sprintf("ov-run/%s: Human Review Required / Rejected", runID),
				Description: desc,
				HeadBranch:  head,
				BaseBranch:  approval.BaseBranch,
			})
			if err == nil && res != nil {
				approval.PRURL = res.PRURL
			}
		}

		return approval, nil
	}

	return nil, ErrInvalidDecision
}

// GetApproval retrieves an active approval record by runID.
func (g *DecisionGate) GetApproval(runID string) (*ActiveApproval, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	approval, ok := g.approvals[runID]
	if !ok {
		return nil, false
	}
	cp := *approval
	return &cp, true
}
