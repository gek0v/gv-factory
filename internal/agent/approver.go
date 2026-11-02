// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// DecisionType represents the verdict produced by the Approver decision agent.
type DecisionType string

const (
	// DecisionApproved indicates all checks passed and changes are qualified for fast-track merge.
	DecisionApproved DecisionType = "APPROVED"
	// DecisionRetry indicates fixable defects exist and retry quota is available for the Builder.
	DecisionRetry DecisionType = "RETRY"
	// DecisionHumanGate indicates human intervention is required via PR or cryptographic HMAC approval.
	DecisionHumanGate DecisionType = "HUMAN_GATE"
)

// ApproverConfig configures evaluation thresholds and sensitive boundaries for the Approver agent.
type ApproverConfig struct {
	MinApprovalScore  float32  `json:"min_approval_score"`
	MinCoverage       float32  `json:"min_coverage"`
	MaxRetries        int      `json:"max_retries"`
	SensitivePaths    []string `json:"sensitive_paths"`
	RequiredReviewers []string `json:"required_reviewers,omitempty"`
}

// Normalize ensures reasonable defaults for unconfigured ApproverConfig fields.
func (cfg *ApproverConfig) Normalize() {
	if cfg.MinApprovalScore <= 0 {
		cfg.MinApprovalScore = 0.80
	}
	if cfg.MinCoverage <= 0 {
		cfg.MinCoverage = 0.90
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if len(cfg.SensitivePaths) == 0 {
		cfg.SensitivePaths = []string{
			"migrations/",
			"internal/auth/",
			"deployments/",
			".github/",
		}
	}
}

// ApprovalRequest encapsulates diff, spec, coverage, and retry state for evaluation.
type ApprovalRequest struct {
	RunID        string        `json:"run_id"`
	Diff         string        `json:"diff"`
	Spec         *SpecDocument `json:"spec,omitempty"`
	FilesChanged []string      `json:"files_changed,omitempty"`
	Coverage     float32       `json:"coverage"`
	RetryCount   int           `json:"retry_count"`
}

// ApprovalDecision models the consolidated outcome from all parallel reviewers and policy gates.
type ApprovalDecision struct {
	DecisionType      DecisionType   `json:"decision_type"`
	ConsolidatedScore float32        `json:"consolidated_score"`
	FeedbackSummary   string         `json:"feedback_summary"`
	ReviewerResults   []ReviewResult `json:"reviewer_results"`
	RequiresHumanSign bool           `json:"requires_human_sign"`
	GateReason        string         `json:"gate_reason,omitempty"`
}

// IsApproved returns true if the decision is ready for automated fast-track.
func (d *ApprovalDecision) IsApproved() bool {
	return d != nil && d.DecisionType == DecisionApproved
}

// IsRetry returns true if the decision demands Builder retry.
func (d *ApprovalDecision) IsRetry() bool {
	return d != nil && d.DecisionType == DecisionRetry
}

// IsHumanGate returns true if human approval sign-off is mandatory.
func (d *ApprovalDecision) IsHumanGate() bool {
	return d != nil && d.DecisionType == DecisionHumanGate
}

// Approver orchestrates parallel diff reviewers and applies automated merge / retry / human-gate policies.
type Approver struct {
	reviewers []DiffReviewer
	config    ApproverConfig
}

// NewApprover creates an Approver with registered reviewers and normalized configuration.
func NewApprover(reviewers []DiffReviewer, cfg ApproverConfig) *Approver {
	cfg.Normalize()
	if len(reviewers) == 0 {
		reviewers = []DiffReviewer{
			NewSecurityReviewer(),
			NewArchitectureReviewer(nil),
		}
	}
	return &Approver{
		reviewers: reviewers,
		config:    cfg,
	}
}

// Decide runs reviewers in parallel, consolidates verdicts, and determines the gating decision.
func (a *Approver) Decide(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reviewReq := ReviewRequest{
		RunID:        req.RunID,
		Diff:         req.Diff,
		Spec:         req.Spec,
		FilesChanged: req.FilesChanged,
	}

	// 1. Execute reviewers in parallel
	type revOutcome struct {
		result *ReviewResult
		err    error
	}

	outcomeChan := make(chan revOutcome, len(a.reviewers))
	var wg sync.WaitGroup

	for _, rev := range a.reviewers {
		wg.Add(1)
		go func(r DiffReviewer) {
			defer wg.Done()
			res, err := r.Review(ctx, reviewReq)
			outcomeChan <- revOutcome{result: res, err: err}
		}(rev)
	}

	// Wait in separate goroutine and close channel
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-doneChan:
	}
	close(outcomeChan)

	var reviewerResults []ReviewResult
	var allErrors []error

	for out := range outcomeChan {
		if out.err != nil {
			allErrors = append(allErrors, out.err)
			reviewerResults = append(reviewerResults, ReviewResult{
				ReviewerName: "DiffReviewer",
				Passed:       false,
				Score:        0.0,
				Severity:     SeverityError,
				Summary:      fmt.Sprintf("Reviewer execution error: %v", out.err),
				Findings: []Finding{
					{
						RuleCode: "ERR-REVIEWER-EXEC",
						Message:  out.err.Error(),
						Severity: SeverityError,
					},
				},
			})
		} else if out.result != nil {
			reviewerResults = append(reviewerResults, *out.result)
		}
	}

	// 2. Identify sensitive path modifications
	var sensitiveFileMatched string
	filesToCheck := append([]string{}, req.FilesChanged...)
	// Also inspect files from diff lines if FilesChanged was empty
	if len(filesToCheck) == 0 {
		parsed, _ := parseUnifiedDiff(req.Diff, nil)
		seen := make(map[string]bool)
		for _, pl := range parsed {
			if pl.File != "" && !seen[pl.File] {
				seen[pl.File] = true
				filesToCheck = append(filesToCheck, pl.File)
			}
		}
	}

	for _, file := range filesToCheck {
		for _, sens := range a.config.SensitivePaths {
			if strings.HasPrefix(file, sens) || strings.Contains(file, sens) {
				sensitiveFileMatched = file
				break
			}
		}
		if sensitiveFileMatched != "" {
			break
		}
	}

	// 3. Consolidate scores and evaluate findings
	var totalScore float32
	var hasCritical bool
	var hasError bool
	var allPassed = true
	var actionableFindings []string

	for _, res := range reviewerResults {
		totalScore += res.Score
		if !res.Passed {
			allPassed = false
		}
		if res.Severity == SeverityCritical {
			hasCritical = true
		}
		if res.Severity == SeverityError {
			hasError = true
		}
		for _, f := range res.Findings {
			if f.Severity == SeverityCritical {
				hasCritical = true
			}
			if f.Severity == SeverityError {
				hasError = true
			}
			actionableFindings = append(actionableFindings, fmt.Sprintf("- [%s] %s:%d: %s (%s)", res.ReviewerName, f.File, f.Line, f.Message, f.RuleCode))
		}
	}

	consolidatedScore := float32(1.0)
	if len(reviewerResults) > 0 {
		consolidatedScore = totalScore / float32(len(reviewerResults))
	}

	coverageSufficient := req.Coverage >= a.config.MinCoverage

	// 4. Decision Gate Resolution
	// Gate A: Sensitive path touched -> HUMAN_GATE
	if sensitiveFileMatched != "" {
		return &ApprovalDecision{
			DecisionType:      DecisionHumanGate,
			ConsolidatedScore: consolidatedScore,
			ReviewerResults:   reviewerResults,
			RequiresHumanSign: true,
			GateReason:        fmt.Sprintf("Sensitive path modified (%s); requires human sign-off via PR or HMAC gate", sensitiveFileMatched),
			FeedbackSummary:   "Changes modify sensitive paths and cannot be automatically merged without human approval.",
		}, nil
	}

	// Gate B: Critical security findings -> HUMAN_GATE
	if hasCritical {
		return &ApprovalDecision{
			DecisionType:      DecisionHumanGate,
			ConsolidatedScore: consolidatedScore,
			ReviewerResults:   reviewerResults,
			RequiresHumanSign: true,
			GateReason:        "Critical security vulnerability detected; auto-merge blocked and human sign-off required",
			FeedbackSummary: fmt.Sprintf("Critical security issues identified:\n%s",
				strings.Join(actionableFindings, "\n")),
		}, nil
	}

	// Gate C: Fixable errors, failed reviewers, or low coverage
	needsFix := !allPassed || hasError || !coverageSufficient || consolidatedScore < a.config.MinApprovalScore || len(allErrors) > 0

	if needsFix {
		// If retries exceeded -> HUMAN_GATE
		if req.RetryCount >= a.config.MaxRetries {
			reason := fmt.Sprintf("Max retries exceeded (%d/%d) with unresolved reviewer failures", req.RetryCount, a.config.MaxRetries)
			if !coverageSufficient {
				reason = fmt.Sprintf("Max retries exceeded (%d/%d) with insufficient test coverage (%.1f%% < %.1f%%)",
					req.RetryCount, a.config.MaxRetries, req.Coverage*100, a.config.MinCoverage*100)
			}
			return &ApprovalDecision{
				DecisionType:      DecisionHumanGate,
				ConsolidatedScore: consolidatedScore,
				ReviewerResults:   reviewerResults,
				RequiresHumanSign: true,
				GateReason:        reason,
				FeedbackSummary: fmt.Sprintf("Execution exhausted %d retries without reaching passing criteria:\n%s",
					a.config.MaxRetries, strings.Join(actionableFindings, "\n")),
			}, nil
		}

		// Retries remaining -> RETRY
		var feedbackItems []string
		if !coverageSufficient {
			feedbackItems = append(feedbackItems, fmt.Sprintf("- Test coverage is %.1f%%, which is below the required %.1f%% threshold.", req.Coverage*100, a.config.MinCoverage*100))
		}
		feedbackItems = append(feedbackItems, actionableFindings...)

		return &ApprovalDecision{
			DecisionType:      DecisionRetry,
			ConsolidatedScore: consolidatedScore,
			ReviewerResults:   reviewerResults,
			RequiresHumanSign: false,
			FeedbackSummary: fmt.Sprintf("Reviewers identified fixable defects (retry %d/%d):\n%s",
				req.RetryCount+1, a.config.MaxRetries, strings.Join(feedbackItems, "\n")),
		}, nil
	}

	// Gate D: All clean -> APPROVED
	return &ApprovalDecision{
		DecisionType:      DecisionApproved,
		ConsolidatedScore: consolidatedScore,
		ReviewerResults:   reviewerResults,
		RequiresHumanSign: false,
		FeedbackSummary:   "All reviewers passed, 0 critical/error findings, coverage threshold satisfied. Approved for fast-track merge.",
	}, nil
}

// Evaluate is an alias for Decide to fulfill standard agent contracts.
func (a *Approver) Evaluate(ctx context.Context, req ApprovalRequest) (*ApprovalDecision, error) {
	return a.Decide(ctx, req)
}
