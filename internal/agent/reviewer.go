// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gv-factory/gv-factory/internal/brain"
)

// Standard severity levels for reviewer findings and results.
const (
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

// DiffReviewer defines the contract for code diff analysis agents.
type DiffReviewer interface {
	Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error)
}

// ReviewRequest encapsulates all context and code diff required for an audit.
type ReviewRequest struct {
	RunID        string        `json:"run_id"`
	Diff         string        `json:"diff"`
	Spec         *SpecDocument `json:"spec,omitempty"`
	FilesChanged []string      `json:"files_changed,omitempty"`
}

// ReviewResult aggregates the findings, score, and overall verdict of a review pass.
type ReviewResult struct {
	ReviewerName string    `json:"reviewer_name"`
	Passed       bool      `json:"passed"`
	Score        float32   `json:"score"` // 0.0 - 1.0
	Severity     string    `json:"severity"`
	Findings     []Finding `json:"findings"`
	Summary      string    `json:"summary"`
}

// Finding represents an individual detected vulnerability, policy violation, or defect.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	RuleCode string `json:"rule_code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// parsedDiffLine captures line-level metadata from unified diffs.
type parsedDiffLine struct {
	File       string
	LineNumber int
	Content    string
	IsAdded    bool
}

// parseUnifiedDiff splits a unified diff into added lines with accurate file and line tracking.
func parseUnifiedDiff(diff string, defaultFiles []string) ([]parsedDiffLine, map[string]bool) {
	var lines []parsedDiffLine
	newFiles := make(map[string]bool)

	currentFile := ""
	if len(defaultFiles) > 0 {
		currentFile = defaultFiles[0]
	}
	currentLine := 1

	hunkRegex := regexp.MustCompile(`^@@\s+-\d+(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s+@@`)
	diffFileRegex := regexp.MustCompile(`^(?:diff --git a/.* b/(.*)|\+\+\+ (?:b/)?(.*))`)

	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		text := scanner.Text()

		if strings.HasPrefix(text, "new file mode") {
			if currentFile != "" {
				newFiles[currentFile] = true
			}
			continue
		}

		if match := diffFileRegex.FindStringSubmatch(text); match != nil {
			target := match[1]
			if target == "" {
				target = match[2]
			}
			target = strings.TrimSpace(target)
			if target != "/dev/null" && target != "" {
				currentFile = target
			}
			continue
		}

		if match := hunkRegex.FindStringSubmatch(text); match != nil {
			if startLine, err := strconv.Atoi(match[1]); err == nil {
				currentLine = startLine
			}
			continue
		}

		if strings.HasPrefix(text, "+") && !strings.HasPrefix(text, "+++") {
			content := strings.TrimPrefix(text, "+")
			lines = append(lines, parsedDiffLine{
				File:       currentFile,
				LineNumber: currentLine,
				Content:    content,
				IsAdded:    true,
			})
			currentLine++
		} else if strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "---") {
			// Deletion does not advance new file line counter
			continue
		} else if !strings.HasPrefix(text, "---") {
			// Unchanged context line advances counter
			currentLine++
		}
	}

	return lines, newFiles
}

// SecurityReviewer inspects diffs for secrets, injection vectors, and dangerous patterns.
type SecurityReviewer struct{}

// NewSecurityReviewer constructs an initialized SecurityReviewer.
func NewSecurityReviewer() *SecurityReviewer {
	return &SecurityReviewer{}
}

// Review audits the provided diff against security rules and policies.
func (s *SecurityReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parsedLines, _ := parseUnifiedDiff(req.Diff, req.FilesChanged)
	var findings []Finding

	// Regular expressions for security patterns
	awsKeyRegex := regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	privKeyRegex := regexp.MustCompile(`-----BEGIN (?:[A-Z0-9_-]+ )?PRIVATE KEY-----`)
	gitTokenRegex := regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr|glpat)-[a-zA-Z0-9_]{20,}\b`)
	genericSecretRegex := regexp.MustCompile(`(?i)\b(?:secret|token|api_key|apikey|access_token|private_key|auth_token)\s*(?::=|=|:)\s*["'][a-zA-Z0-9_\-\.]{12,}["']`)
	passwordRegex := regexp.MustCompile(`(?i)\b(?:password|passwd|pwd)\s*(?::=|=|:)\s*["'][^"'\s]{4,}["']`)

	sqlSprintfRegex := regexp.MustCompile(`(?i)fmt\.Sprintf\(["'].*\b(SELECT|INSERT|UPDATE|DELETE)\b.*%s.*["']`)
	sqlConcatRegex := regexp.MustCompile(`(?i)(?:db|tx|conn)\.(?:Query|QueryRow|Exec)(?:Context)?\(.*(?:\+|fmt\.Sprintf)`)
	cmdInjectRegex := regexp.MustCompile(`(?i)exec\.Command(?:Context)?\(\s*["'](?:sh|bash|zsh)["']\s*,\s*["']-c["']\s*,\s*.*(?:\+|fmt\.Sprintf)`)

	insecurePermRegex := regexp.MustCompile(`\b0[67]77\b`)
	insecureTLSRegex := regexp.MustCompile(`InsecureSkipVerify:\s*true`)
	weakCryptoRegex := regexp.MustCompile(`\b(?:md5|sha1)\.New\(\)`)

	for _, dl := range parsedLines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line := dl.Content

		// 1. Secrets detection
		if awsKeyRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-SECRET-001",
				Message:  "AWS Access Key detected in code",
				Severity: SeverityCritical,
			})
		}
		if privKeyRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-SECRET-002",
				Message:  "Private key block detected in source diff",
				Severity: SeverityCritical,
			})
		}
		if gitTokenRegex.MatchString(line) || genericSecretRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-SECRET-003",
				Message:  "Hardcoded secret or token assignment detected",
				Severity: SeverityCritical,
			})
		}
		if passwordRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-SECRET-004",
				Message:  "Hardcoded password assignment detected",
				Severity: SeverityCritical,
			})
		}

		// 2. Injections detection
		if sqlSprintfRegex.MatchString(line) || sqlConcatRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-INJECT-SQL",
				Message:  "Potential SQL injection via raw string concatenation or Sprintf formatting",
				Severity: SeverityCritical,
			})
		}
		if cmdInjectRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-INJECT-CMD",
				Message:  "Potential Command injection via shell execution with dynamic arguments",
				Severity: SeverityCritical,
			})
		}

		// 3. Insecure Permissions and Weak Patterns
		if insecurePermRegex.MatchString(line) && strings.Contains(line, "os.") {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-PERM-0777",
				Message:  "Insecure file or directory permissions (0777 / write-all)",
				Severity: SeverityError,
			})
		}
		if insecureTLSRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-TLS-INSECURE",
				Message:  "InsecureSkipVerify disabled TLS server certificate validation",
				Severity: SeverityError,
			})
		}
		if weakCryptoRegex.MatchString(line) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "SEC-CRYPTO-WEAK",
				Message:  "Deprecated or cryptographically weak hash algorithm (MD5/SHA1)",
				Severity: SeverityWarn,
			})
		}
	}

	score, maxSev, passed := evaluateFindings(findings)
	summary := "Security review passed cleanly."
	if !passed {
		summary = fmt.Sprintf("Security review failed with %d findings (highest severity: %s).", len(findings), maxSev)
	}

	return &ReviewResult{
		ReviewerName: "SecurityReviewer",
		Passed:       passed,
		Score:        score,
		Severity:     maxSev,
		Findings:     findings,
		Summary:      summary,
	}, nil
}

// ArchitectureReviewer audits diffs against ADR rules, package boundaries, and project invariants.
type ArchitectureReviewer struct {
	brain BrainKnowledgeSource
	rules []brain.ArchitecturalRule
}

// NewArchitectureReviewer constructs an ArchitectureReviewer backed by optional Brain knowledge and explicit rules.
func NewArchitectureReviewer(knowledgeSource BrainKnowledgeSource, rules ...brain.ArchitecturalRule) *ArchitectureReviewer {
	return &ArchitectureReviewer{
		brain: knowledgeSource,
		rules: rules,
	}
}

// Review audits the provided diff against active architectural rules and constraints.
func (a *ArchitectureReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parsedLines, newFiles := parseUnifiedDiff(req.Diff, req.FilesChanged)
	var findings []Finding

	// 1. Gather active architectural rules
	activeRules := append([]brain.ArchitecturalRule{}, a.rules...)
	if a.brain != nil {
		if brainRules, err := a.brain.ListRules(ctx, 100); err == nil {
			activeRules = append(activeRules, brainRules...)
		}
	}

	// 2. Check Package boundary: pkg/ cannot import internal/
	importInternalRegex := regexp.MustCompile(`["']github\.com/gv-factory/gv-factory/internal/`)
	for _, dl := range parsedLines {
		if strings.HasPrefix(dl.File, "pkg/") && importInternalRegex.MatchString(dl.Content) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "ARCH-BOUNDARY-PKG-INTERNAL",
				Message:  "Package boundary violation: pkg/ code cannot import internal/ packages",
				Severity: SeverityError,
			})
		}
	}

	// 3. Check Sandbox invariant: no git checkout / switch inside sandboxes or direct checkout main
	gitCheckoutRegex := regexp.MustCompile(`(?i)(?:git\s+(?:checkout|switch)|["']git["']\s*,\s*["'](?:checkout|switch)["'])`)
	for _, dl := range parsedLines {
		if gitCheckoutRegex.MatchString(dl.Content) {
			findings = append(findings, Finding{
				File:     dl.File,
				Line:     dl.LineNumber,
				RuleCode: "ARCH-NO-GIT-CHECKOUT",
				Message:  "Git checkout/switch is forbidden; worktrees must be used for isolation",
				Severity: SeverityError,
			})
		}
	}

	// 4. Check Mandatory SPDX Header in new Go files
	// REUSE-IgnoreStart
	spdxHeaderMarker := "// SPDX-License-Identifier: Apache-2.0"
	// REUSE-IgnoreEnd
	diffByFile := groupDiffLinesByFile(parsedLines)

	for file := range newFiles {
		if strings.HasSuffix(file, ".go") {
			lines := diffByFile[file]
			hasSPDX := false
			for _, l := range lines {
				if strings.Contains(l.Content, spdxHeaderMarker) {
					hasSPDX = true
					break
				}
			}
			if !hasSPDX {
				lineNum := 1
				if len(lines) > 0 {
					lineNum = lines[0].LineNumber
				}
				findings = append(findings, Finding{
					File:     file,
					Line:     lineNum,
					RuleCode: "ARCH-SPDX-HEADER",
					Message:  "Mandatory SPDX license header missing from new Go source file",
					Severity: SeverityError,
				})
			}
		}
	}

	// 5. Evaluate custom / brain architectural rules against diff
	for _, rule := range activeRules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Match rule patterns (e.g. main branch mutation)
		if strings.Contains(strings.ToLower(rule.Title), "main") || strings.Contains(strings.ToLower(rule.Description), "main") {
			mainPushRegex := regexp.MustCompile(`\bgit\s+push\s+(?:origin\s+)?main\b`)
			for _, dl := range parsedLines {
				if mainPushRegex.MatchString(dl.Content) {
					findings = append(findings, Finding{
						File:     dl.File,
						Line:     dl.LineNumber,
						RuleCode: rule.RuleCode,
						Message:  fmt.Sprintf("Violation of %s: %s", rule.Title, rule.Description),
						Severity: SeverityError,
					})
				}
			}
		}
	}

	score, maxSev, passed := evaluateFindings(findings)
	summary := "Architecture review passed cleanly."
	if !passed {
		summary = fmt.Sprintf("Architecture review identified %d rule violations (highest severity: %s).", len(findings), maxSev)
	}

	return &ReviewResult{
		ReviewerName: "ArchitectureReviewer",
		Passed:       passed,
		Score:        score,
		Severity:     maxSev,
		Findings:     findings,
		Summary:      summary,
	}, nil
}

func groupDiffLinesByFile(lines []parsedDiffLine) map[string][]parsedDiffLine {
	result := make(map[string][]parsedDiffLine)
	for _, l := range lines {
		result[l.File] = append(result[l.File], l)
	}
	return result
}

// evaluateFindings computes overall score, maximum severity, and pass/fail boolean.
func evaluateFindings(findings []Finding) (float32, string, bool) {
	if len(findings) == 0 {
		return 1.0, SeverityInfo, true
	}

	score := float32(1.0)
	hasCritical := false
	hasError := false
	hasWarn := false

	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			hasCritical = true
			score -= 0.40
		case SeverityError:
			hasError = true
			score -= 0.25
		case SeverityWarn:
			hasWarn = true
			score -= 0.10
		case SeverityInfo:
			score -= 0.02
		}
	}

	if score < 0.0 {
		score = 0.0
	}

	maxSev := SeverityInfo
	if hasWarn {
		maxSev = SeverityWarn
	}
	if hasError {
		maxSev = SeverityError
	}
	if hasCritical {
		maxSev = SeverityCritical
	}

	passed := !hasCritical && !hasError && score >= 0.70
	return score, maxSev, passed
}
