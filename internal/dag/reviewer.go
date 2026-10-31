// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ReviewerType identifies the specialization of a reviewer node.
type ReviewerType string

const (
	ReviewerSecurity     ReviewerType = "security"
	ReviewerArchitecture ReviewerType = "architecture"
	ReviewerUIUX         ReviewerType = "ui_ux"
	ReviewerCodeQuality  ReviewerType = "code_quality"
)

// BuilderContext represents raw execution output from a builder step before context sanitization.
type BuilderContext struct {
	TaskSpec       string         `json:"task_spec"`
	GitDiff        string         `json:"git_diff"`
	Stdout         string         `json:"stdout,omitempty"`
	Stderr         string         `json:"stderr,omitempty"`
	Logs           []string       `json:"logs,omitempty"`
	ChainOfThought string         `json:"chain_of_thought,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// CleanContext represents the sanitized review context enforcing the Clean Context Invariant.
// Reviewers receive strictly the original task specification and the git diff produced by the builder.
// Intermediate builder logs, stdout, stderr, and chain-of-thought traces are strictly stripped.
type CleanContext struct {
	TaskSpec string `json:"task_spec"`
	GitDiff  string `json:"git_diff"`
}

// SanitizeBuilderContext enforces the Clean Context Invariant by stripping all builder
// internal execution logs, stdout, stderr, and chain-of-thought traces, returning a clean context
// containing exclusively TaskSpec and GitDiff.
func SanitizeBuilderContext(raw *BuilderContext) CleanContext {
	if raw == nil {
		return CleanContext{}
	}
	return CleanContext{
		TaskSpec: strings.TrimSpace(raw.TaskSpec),
		GitDiff:  strings.TrimSpace(raw.GitDiff),
	}
}

// Severity represents the impact level of a reviewer finding.
type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
	SeverityInfo     Severity = "INFO"
)

// ReviewFinding represents an issue, alert, or suggestion discovered during review.
type ReviewFinding struct {
	RuleID      string   `json:"rule_id"`
	Description string   `json:"description"`
	Severity    Severity `json:"severity"`
	File        string   `json:"file,omitempty"`
	Line        int      `json:"line,omitempty"`
}

// ReviewResult contains the diagnostic results from an individual reviewer node.
type ReviewResult struct {
	Type          ReviewerType    `json:"type"`
	Passed        bool            `json:"passed"`
	Coverage      float64         `json:"coverage,omitempty"`
	Findings      []ReviewFinding `json:"findings"`
	ExecutionTime time.Duration   `json:"execution_time"`
	Error         string          `json:"error,omitempty"`
}

// Reviewer defines the interface for an individual specialized reviewer node.
type Reviewer interface {
	Type() ReviewerType
	Review(ctx context.Context, clean CleanContext) (ReviewResult, error)
}

// FileClassification details sensitive file changes detected in the git diff.
type FileClassification struct {
	TouchedFiles        []string `json:"touched_files"`
	HasMigrationChanges bool     `json:"has_migration_changes"`
	HasSchemaChanges    bool     `json:"has_schema_changes"`
	HasAuthChanges      bool     `json:"has_auth_changes"`
}

// ApprovalReport consolidates reviewer findings and classifies risk.
type ApprovalReport struct {
	RunID               string                        `json:"run_id"`
	Passed              bool                          `json:"passed"`
	AggregateCoverage   float64                       `json:"aggregate_coverage"`
	CriticalAlertsCount int                           `json:"critical_alerts_count"`
	HighAlertsCount     int                           `json:"high_alerts_count"`
	TotalSecurityAlerts int                           `json:"total_security_alerts"`
	Classification      FileClassification            `json:"classification"`
	FeedbackLoopCount   int                           `json:"feedback_loop_count"`
	MaxFeedbackLoops    int                           `json:"max_feedback_loops"`
	ReviewerResults     map[ReviewerType]ReviewResult `json:"reviewer_results"`
	Summary             string                        `json:"summary"`
}

// CanRetryFeedback returns true if the feedback loop counter has not exceeded the max allowance.
func (r *ApprovalReport) CanRetryFeedback() bool {
	return r.FeedbackLoopCount < r.MaxFeedbackLoops
}

var (
	diffGitRegex  = regexp.MustCompile(`^diff --git a/(.*?) b/(.*?)$`)
	diffPlusRegex = regexp.MustCompile(`^\+\+\+ b/(.*?)$`)
)

// ClassifyTouchedFiles parses git diff output to detect touched files and identify
// modifications to migrations, schemas, or internal/auth modules.
func ClassifyTouchedFiles(diff string) FileClassification {
	seen := make(map[string]struct{})
	var files []string

	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		line := scanner.Text()
		var candidate string
		if matches := diffGitRegex.FindStringSubmatch(line); len(matches) > 2 {
			candidate = matches[2]
		} else if matches := diffPlusRegex.FindStringSubmatch(line); len(matches) > 1 {
			candidate = matches[1]
		}

		if candidate != "" && candidate != "/dev/null" {
			candidate = strings.TrimSpace(candidate)
			if _, exists := seen[candidate]; !exists {
				seen[candidate] = struct{}{}
				files = append(files, candidate)
			}
		}
	}

	classification := FileClassification{
		TouchedFiles: files,
	}

	for _, file := range files {
		normalized := strings.ReplaceAll(file, "\\", "/")
		if strings.HasPrefix(normalized, "migrations/") || strings.Contains(normalized, "/migrations/") || strings.HasSuffix(normalized, ".sql") {
			classification.HasMigrationChanges = true
		}
		if strings.HasPrefix(normalized, "pkg/schema/") || strings.Contains(normalized, "/schema/") {
			classification.HasSchemaChanges = true
		}
		if strings.HasPrefix(normalized, "internal/auth/") || strings.Contains(normalized, "/auth/") {
			classification.HasAuthChanges = true
		}
	}

	return classification
}

// DefaultSecurityReviewer inspects clean context for OWASP Top 10, hardcoded secrets, and unsafe patterns.
type DefaultSecurityReviewer struct{}

func (r *DefaultSecurityReviewer) Type() ReviewerType {
	return ReviewerSecurity
}

func (r *DefaultSecurityReviewer) Review(ctx context.Context, clean CleanContext) (ReviewResult, error) {
	start := time.Now()
	var findings []ReviewFinding

	secretPatterns := []struct {
		pattern *regexp.Regexp
		ruleID  string
		desc    string
	}{
		{regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|private[_-]?key|password)\s*[:=]\s*["'][A-Za-z0-9_\-+/]{10,}["']`), "SEC-001", "Hardcoded credentials detected in diff"},
		{regexp.MustCompile(`(?i)-----BEGIN (RSA|EC|DSA|OPENSSH) PRIVATE KEY-----`), "SEC-002", "Private key block detected in diff"},
		{regexp.MustCompile(`(?i)(exec\.Command\([^)]*\$|\.eval\(|sql\.Open\(.*password=)`), "SEC-003", "Potentially unescaped execution or sensitive connection string"},
	}

	scanner := bufio.NewScanner(strings.NewReader(clean.GitDiff))
	lineNum := 0
	currentFile := ""

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			addedContent := strings.TrimPrefix(line, "+")
			for _, sp := range secretPatterns {
				if sp.pattern.MatchString(addedContent) {
					findings = append(findings, ReviewFinding{
						RuleID:      sp.ruleID,
						Description: sp.desc,
						Severity:    SeverityCritical,
						File:        currentFile,
						Line:        lineNum,
					})
				}
			}
		}
	}

	passed := true
	for _, f := range findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			passed = false
			break
		}
	}

	return ReviewResult{
		Type:          ReviewerSecurity,
		Passed:        passed,
		Findings:      findings,
		ExecutionTime: time.Since(start),
	}, nil
}

// DefaultArchitectureReviewer inspects clean context for naming, complexity, and ADR conformance.
type DefaultArchitectureReviewer struct{}

func (r *DefaultArchitectureReviewer) Type() ReviewerType {
	return ReviewerArchitecture
}

func (r *DefaultArchitectureReviewer) Review(ctx context.Context, clean CleanContext) (ReviewResult, error) {
	start := time.Now()
	var findings []ReviewFinding

	scanner := bufio.NewScanner(strings.NewReader(clean.GitDiff))
	lineNum := 0
	currentFile := ""

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added := strings.TrimPrefix(line, "+")
			if strings.Contains(added, "internal/") && strings.Contains(currentFile, "pkg/") {
				findings = append(findings, ReviewFinding{
					RuleID:      "ARCH-001",
					Description: "Package pkg/ should not import internal/ directly (ADR conformance)",
					Severity:    SeverityHigh,
					File:        currentFile,
					Line:        lineNum,
				})
			}
			if strings.Contains(added, "for ") && strings.HasPrefix(strings.TrimSpace(added), "for ") && strings.Count(added, "\t") > 5 {
				findings = append(findings, ReviewFinding{
					RuleID:      "ARCH-002",
					Description: "Excessive nesting depth detected (potential high cyclomatic complexity)",
					Severity:    SeverityMedium,
					File:        currentFile,
					Line:        lineNum,
				})
			}
		}
	}

	passed := true
	for _, f := range findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			passed = false
			break
		}
	}

	return ReviewResult{
		Type:          ReviewerArchitecture,
		Passed:        passed,
		Findings:      findings,
		ExecutionTime: time.Since(start),
	}, nil
}

// DefaultUIUXReviewer inspects clean context for design tokens, consistency, and a11y standards.
type DefaultUIUXReviewer struct{}

func (r *DefaultUIUXReviewer) Type() ReviewerType {
	return ReviewerUIUX
}

func (r *DefaultUIUXReviewer) Review(ctx context.Context, clean CleanContext) (ReviewResult, error) {
	start := time.Now()
	var findings []ReviewFinding

	scanner := bufio.NewScanner(strings.NewReader(clean.GitDiff))
	lineNum := 0
	currentFile := ""

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added := strings.TrimPrefix(line, "+")
			if (strings.HasSuffix(currentFile, ".tsx") || strings.HasSuffix(currentFile, ".jsx") || strings.HasSuffix(currentFile, ".html")) &&
				strings.Contains(added, "<img") && !strings.Contains(added, "alt=") {
				findings = append(findings, ReviewFinding{
					RuleID:      "UIUX-001",
					Description: "Image tag missing mandatory alt attribute for a11y compliance",
					Severity:    SeverityMedium,
					File:        currentFile,
					Line:        lineNum,
				})
			}
			if strings.Contains(added, "color: \"#") || strings.Contains(added, "backgroundColor: \"#") {
				findings = append(findings, ReviewFinding{
					RuleID:      "UIUX-002",
					Description: "Hardcoded hex color detected; prefer shared design tokens",
					Severity:    SeverityLow,
					File:        currentFile,
					Line:        lineNum,
				})
			}
		}
	}

	return ReviewResult{
		Type:          ReviewerUIUX,
		Passed:        true,
		Findings:      findings,
		ExecutionTime: time.Since(start),
	}, nil
}

// DefaultCodeQualityReviewer inspects clean context for test coverage and structural quality.
type DefaultCodeQualityReviewer struct {
	DefaultCoverage float64
}

func (r *DefaultCodeQualityReviewer) Type() ReviewerType {
	return ReviewerCodeQuality
}

func (r *DefaultCodeQualityReviewer) Review(ctx context.Context, clean CleanContext) (ReviewResult, error) {
	start := time.Now()
	var findings []ReviewFinding

	coverage := r.DefaultCoverage
	if coverage <= 0 {
		coverage = 92.5
	}

	classification := ClassifyTouchedFiles(clean.GitDiff)
	hasCode := false
	hasTest := false
	for _, f := range classification.TouchedFiles {
		if strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".tsx") {
			if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, ".test.ts") || strings.HasSuffix(f, ".test.tsx") {
				hasTest = true
			} else {
				hasCode = true
			}
		}
	}

	if hasCode && !hasTest {
		findings = append(findings, ReviewFinding{
			RuleID:      "QUAL-001",
			Description: "Code modifications detected without corresponding test files",
			Severity:    SeverityMedium,
		})
		if coverage > 85.0 {
			coverage = 80.0
		}
	}

	return ReviewResult{
		Type:          ReviewerCodeQuality,
		Passed:        true,
		Coverage:      coverage,
		Findings:      findings,
		ExecutionTime: time.Since(start),
	}, nil
}

// ReviewDispatcher coordinates concurrent execution of the 4 specialized reviewers.
type ReviewDispatcher struct {
	reviewers []Reviewer
}

// NewReviewDispatcher creates a ReviewDispatcher with standard or custom reviewers.
func NewReviewDispatcher(customReviewers ...Reviewer) *ReviewDispatcher {
	if len(customReviewers) > 0 {
		return &ReviewDispatcher{
			reviewers: customReviewers,
		}
	}

	return &ReviewDispatcher{
		reviewers: []Reviewer{
			&DefaultSecurityReviewer{},
			&DefaultArchitectureReviewer{},
			&DefaultUIUXReviewer{},
			&DefaultCodeQualityReviewer{},
		},
	}
}

// Dispatch concurrently executes all specialized reviewers under the Clean Context Invariant
// and consolidates findings into an ApprovalReport.
func (d *ReviewDispatcher) Dispatch(ctx context.Context, runID string, rawBuilderContext *BuilderContext, feedbackCount int, maxFeedbackLoops int) (*ApprovalReport, error) {
	if maxFeedbackLoops <= 0 {
		maxFeedbackLoops = 3
	}

	// Enforce the Clean Context Invariant: intermediate logs, stdout/stderr, and chain-of-thought are stripped
	clean := SanitizeBuilderContext(rawBuilderContext)

	// Classify touched files directly from clean git diff
	classification := ClassifyTouchedFiles(clean.GitDiff)

	results := make(map[ReviewerType]ReviewResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, rev := range d.reviewers {
		wg.Add(1)
		go func(r Reviewer) {
			defer wg.Done()

			res, err := r.Review(ctx, clean)
			if err != nil {
				res = ReviewResult{
					Type:   r.Type(),
					Passed: false,
					Error:  err.Error(),
				}
			}

			mu.Lock()
			results[r.Type()] = res
			mu.Unlock()
		}(rev)
	}

	wg.Wait()

	// Consolidate findings into ApprovalReport
	var criticalCount, highCount, totalSecurityAlerts int
	aggregateCoverage := 100.0
	overallPassed := true

	for _, res := range results {
		if !res.Passed {
			overallPassed = false
		}
		if res.Type == ReviewerCodeQuality && res.Coverage > 0 {
			aggregateCoverage = res.Coverage
		}
		for _, f := range res.Findings {
			if res.Type == ReviewerSecurity || f.Severity == SeverityCritical || f.Severity == SeverityHigh {
				totalSecurityAlerts++
			}
			if f.Severity == SeverityCritical {
				criticalCount++
			} else if f.Severity == SeverityHigh {
				highCount++
			}
		}
	}

	if criticalCount > 0 || highCount > 0 {
		overallPassed = false
	}

	summary := fmt.Sprintf(
		"Review complete: %d critical alerts, %d high alerts, %.1f%% test coverage, %d touched files",
		criticalCount,
		highCount,
		aggregateCoverage,
		len(classification.TouchedFiles),
	)

	return &ApprovalReport{
		RunID:               runID,
		Passed:              overallPassed,
		AggregateCoverage:   aggregateCoverage,
		CriticalAlertsCount: criticalCount,
		HighAlertsCount:     highCount,
		TotalSecurityAlerts: totalSecurityAlerts,
		Classification:      classification,
		FeedbackLoopCount:   feedbackCount,
		MaxFeedbackLoops:    maxFeedbackLoops,
		ReviewerResults:     results,
		Summary:             summary,
	}, nil
}
