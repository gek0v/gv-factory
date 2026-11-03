// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

// Standard severity levels for reviewer findings and results.
const (
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"

	// DefaultReviewerModel is the default LLM model for semantic diff review.
	DefaultReviewerModel = "claude-3-5-sonnet"
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

// reviewerConfig stores optional configuration for diff reviewers.
type reviewerConfig struct {
	llmClient llm.LLMClient
	model     string
	brain     BrainKnowledgeSource
	rules     []brain.ArchitecturalRule
}

// ReviewerOption configures optional behavior for diff reviewers.
type ReviewerOption func(*reviewerConfig)

// WithLLM configures an LLMClient and optional model for semantic diff audits.
func WithLLM(client llm.LLMClient, model ...string) ReviewerOption {
	return func(cfg *reviewerConfig) {
		cfg.llmClient = client
		if len(model) > 0 && model[0] != "" {
			cfg.model = model[0]
		}
	}
}

// WithReviewerModel configures the model identifier for reviewer LLM prompts.
func WithReviewerModel(model string) ReviewerOption {
	return func(cfg *reviewerConfig) {
		if model != "" {
			cfg.model = model
		}
	}
}

// WithReviewerBrain configures a BrainKnowledgeSource for architectural knowledge.
func WithReviewerBrain(source BrainKnowledgeSource) ReviewerOption {
	return func(cfg *reviewerConfig) {
		cfg.brain = source
	}
}

// WithReviewerRules sets explicit architectural rules.
func WithReviewerRules(rules ...brain.ArchitecturalRule) ReviewerOption {
	return func(cfg *reviewerConfig) {
		cfg.rules = append(cfg.rules, rules...)
	}
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

// reviewerLLMResponse models the structured JSON returned by semantic reviewer LLM audits.
type reviewerLLMResponse struct {
	Findings []Finding `json:"findings"`
	Summary  string    `json:"summary"`
}

// SecurityReviewer inspects diffs for secrets, injection vectors, dangerous patterns,
// and concurrency data races using deterministic regex checks and optional semantic LLM audits.
type SecurityReviewer struct {
	llmClient llm.LLMClient
	model     string
}

// NewSecurityReviewer constructs an initialized SecurityReviewer with optional functional options.
func NewSecurityReviewer(opts ...ReviewerOption) *SecurityReviewer {
	cfg := reviewerConfig{
		model: DefaultReviewerModel,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &SecurityReviewer{
		llmClient: cfg.llmClient,
		model:     cfg.model,
	}
}

// NewSecurityReviewerWithLLM constructs a SecurityReviewer equipped with an LLM client.
func NewSecurityReviewerWithLLM(client llm.LLMClient, model ...string) *SecurityReviewer {
	m := DefaultReviewerModel
	if len(model) > 0 && model[0] != "" {
		m = model[0]
	}
	return &SecurityReviewer{
		llmClient: client,
		model:     m,
	}
}

// WithLLM sets or updates the LLMClient for semantic security auditing.
func (s *SecurityReviewer) WithLLM(client llm.LLMClient, model ...string) *SecurityReviewer {
	s.llmClient = client
	if len(model) > 0 && model[0] != "" {
		s.model = model[0]
	}
	return s
}

// Review audits the provided diff against security rules, secrets, injection vectors, and data races.
func (s *SecurityReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1. Run deterministic regex analysis (baseline & robust offline fallback)
	deterministicFindings, err := s.runDeterministicAudit(ctx, req)
	if err != nil {
		return nil, err
	}

	findings := deterministicFindings
	var semanticSummary string

	// 2. If LLM client is configured, evaluate diff semantically for vulnerabilities and data races
	if s.llmClient != nil {
		llmFindings, summary, llmErr := s.auditWithLLM(ctx, req)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if llmErr == nil {
			if len(llmFindings) > 0 {
				findings = mergeFindings(deterministicFindings, llmFindings)
			}
			semanticSummary = summary
		}
		// If llmErr != nil, fallback gracefully to deterministic findings (offline resilience)
	}

	score, maxSev, passed := evaluateFindings(findings)
	summary := "Security review passed cleanly."
	if !passed {
		summary = fmt.Sprintf("Security review failed with %d findings (highest severity: %s).", len(findings), maxSev)
		if semanticSummary != "" {
			summary += " " + semanticSummary
		}
	} else if semanticSummary != "" {
		summary = semanticSummary
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

// runDeterministicAudit executes offline regex-based vulnerability and secrets detection.
func (s *SecurityReviewer) runDeterministicAudit(ctx context.Context, req ReviewRequest) ([]Finding, error) {
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

		// Secrets detection
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

		// Injections detection
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

		// Insecure Permissions and Weak Patterns
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

	return findings, nil
}

// auditWithLLM executes a semantic security and concurrency audit against the LLM.
func (s *SecurityReviewer) auditWithLLM(ctx context.Context, req ReviewRequest) ([]Finding, string, error) {
	if s.llmClient == nil {
		return nil, "", nil
	}

	model := s.model
	if model == "" {
		model = DefaultReviewerModel
	}

	systemPrompt := "You are an expert security auditor and concurrency analyzer for Go codebases. " +
		"Analyze the provided git diff for security vulnerabilities (e.g., injection vectors, hardcoded credentials/secrets, weak crypto, insecure permissions) " +
		"and concurrency hazards (e.g., data races, unprotected shared state, goroutine leaks, improper mutex usage). " +
		"Output ONLY a valid JSON object matching the requested schema without markdown code blocks or conversational commentary."

	userPrompt := fmt.Sprintf(`Perform a security and concurrency audit on the following git diff:

Diff:
%s

Files changed: %s

Return a JSON object in this format:
{
  "findings": [
    {
      "file": "path/to/file.go",
      "line": 10,
      "rule_code": "SEC-RACE-001",
      "message": "Potential data race on shared state access without mutex synchronization",
      "severity": "CRITICAL"
    }
  ],
  "summary": "Summary of audit results"
}
Allowed severity values: "INFO", "WARN", "ERROR", "CRITICAL".
If clean, return an empty "findings" array.`, req.Diff, strings.Join(req.FilesChanged, ", "))

	chatReq := &llm.ChatRequest{
		Model: model,
		Messages: []llm.ChatMessage{
			llm.NewSystemMessage(systemPrompt),
			llm.NewUserMessage(userPrompt),
		},
	}

	resp, err := s.llmClient.ChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, "", err
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return nil, "", nil
	}

	cleanJSON := extractReviewerJSON(resp.Message.Content)
	var parsed reviewerLLMResponse
	if err := json.Unmarshal([]byte(cleanJSON), &parsed); err != nil {
		return nil, "", err
	}

	return parsed.Findings, parsed.Summary, nil
}

// ArchitectureReviewer audits diffs against ADR rules, package boundaries, and project invariants,
// supporting both deterministic regex checks and semantic LLM audits.
type ArchitectureReviewer struct {
	brain     BrainKnowledgeSource
	rules     []brain.ArchitecturalRule
	llmClient llm.LLMClient
	model     string
}

// NewArchitectureReviewer constructs an ArchitectureReviewer backed by optional Brain knowledge and explicit rules.
func NewArchitectureReviewer(knowledgeSource BrainKnowledgeSource, rules ...brain.ArchitecturalRule) *ArchitectureReviewer {
	return &ArchitectureReviewer{
		brain: knowledgeSource,
		rules: rules,
		model: DefaultReviewerModel,
	}
}

// NewArchitectureReviewerWithLLM constructs an ArchitectureReviewer equipped with an LLM client.
func NewArchitectureReviewerWithLLM(knowledgeSource BrainKnowledgeSource, client llm.LLMClient, rules ...brain.ArchitecturalRule) *ArchitectureReviewer {
	return &ArchitectureReviewer{
		brain:     knowledgeSource,
		rules:     rules,
		llmClient: client,
		model:     DefaultReviewerModel,
	}
}

// WithLLM sets or updates the LLMClient for semantic architecture auditing.
func (a *ArchitectureReviewer) WithLLM(client llm.LLMClient, model ...string) *ArchitectureReviewer {
	a.llmClient = client
	if len(model) > 0 && model[0] != "" {
		a.model = model[0]
	}
	return a
}

// Review audits the provided diff against active architectural rules, constraints, and concurrency design.
func (a *ArchitectureReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1. Gather active architectural rules
	activeRules := append([]brain.ArchitecturalRule{}, a.rules...)
	if a.brain != nil {
		if brainRules, err := a.brain.ListRules(ctx, 100); err == nil {
			activeRules = append(activeRules, brainRules...)
		}
	}

	// 2. Run deterministic checks (offline baseline)
	deterministicFindings, err := a.runDeterministicAudit(ctx, req, activeRules)
	if err != nil {
		return nil, err
	}

	findings := deterministicFindings
	var semanticSummary string

	// 3. Run semantic LLM audit if LLM client is available
	if a.llmClient != nil {
		llmFindings, summary, llmErr := a.auditWithLLM(ctx, req, activeRules)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if llmErr == nil {
			if len(llmFindings) > 0 {
				findings = mergeFindings(deterministicFindings, llmFindings)
			}
			semanticSummary = summary
		}
		// Graceful fallback to deterministic findings on LLM error
	}

	score, maxSev, passed := evaluateFindings(findings)
	summary := "Architecture review passed cleanly."
	if !passed {
		summary = fmt.Sprintf("Architecture review identified %d rule violations (highest severity: %s).", len(findings), maxSev)
		if semanticSummary != "" {
			summary += " " + semanticSummary
		}
	} else if semanticSummary != "" {
		summary = semanticSummary
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

// runDeterministicAudit executes offline regex checks for package boundaries, worktrees, and SPDX headers.
func (a *ArchitectureReviewer) runDeterministicAudit(ctx context.Context, req ReviewRequest, activeRules []brain.ArchitecturalRule) ([]Finding, error) {
	parsedLines, newFiles := parseUnifiedDiff(req.Diff, req.FilesChanged)
	var findings []Finding

	// Package boundary: pkg/ cannot import internal/
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

	// Sandbox invariant: no git checkout / switch inside sandboxes or direct checkout main
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

	// Mandatory SPDX Header in new Go files
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

	// Custom / brain architectural rules
	for _, rule := range activeRules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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

	return findings, nil
}

// auditWithLLM executes a semantic architectural evaluation using an LLM.
func (a *ArchitectureReviewer) auditWithLLM(ctx context.Context, req ReviewRequest, activeRules []brain.ArchitecturalRule) ([]Finding, string, error) {
	if a.llmClient == nil {
		return nil, "", nil
	}

	model := a.model
	if model == "" {
		model = DefaultReviewerModel
	}

	rulesDesc := ""
	for _, r := range activeRules {
		rulesDesc += fmt.Sprintf("- [%s] %s: %s\n", r.RuleCode, r.Title, r.Description)
	}

	systemPrompt := "You are an expert software architect reviewing Go code diffs for architectural integrity. " +
		"Audit the diff for package boundary violations (e.g. pkg/ importing internal/), sandbox isolation invariants " +
		"(e.g. direct git checkout or switch instead of git worktrees), licensing compliance (mandatory SPDX Apache-2.0 headers in new files), " +
		"architectural data races or concurrency violations, and adherence to specific project rules. " +
		"Output ONLY a valid JSON object matching the requested schema without markdown code blocks or surrounding text."

	userPrompt := fmt.Sprintf(`Audit the following diff against architectural rules:
Architectural Rules:
%s

Diff:
%s

Files changed: %s

Return a JSON object in this format:
{
  "findings": [
    {
      "file": "path/to/file.go",
      "line": 15,
      "rule_code": "ARCH-BOUNDARY-PKG-INTERNAL",
      "message": "Package boundary violation: pkg/ code cannot import internal/ packages",
      "severity": "ERROR"
    }
  ],
  "summary": "Summary of architectural review findings"
}
Allowed severity values: "INFO", "WARN", "ERROR", "CRITICAL".
If clean, return an empty "findings" array.`, rulesDesc, req.Diff, strings.Join(req.FilesChanged, ", "))

	chatReq := &llm.ChatRequest{
		Model: model,
		Messages: []llm.ChatMessage{
			llm.NewSystemMessage(systemPrompt),
			llm.NewUserMessage(userPrompt),
		},
	}

	resp, err := a.llmClient.ChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, "", err
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return nil, "", nil
	}

	cleanJSON := extractReviewerJSON(resp.Message.Content)
	var parsed reviewerLLMResponse
	if err := json.Unmarshal([]byte(cleanJSON), &parsed); err != nil {
		return nil, "", err
	}

	return parsed.Findings, parsed.Summary, nil
}

// SemanticReviewer performs a unified semantic audit covering security vulnerabilities,
// data races, and architectural constraints, with automatic deterministic fallback.
type SemanticReviewer struct {
	llmClient    llm.LLMClient
	model        string
	brain        BrainKnowledgeSource
	rules        []brain.ArchitecturalRule
	secReviewer  *SecurityReviewer
	archReviewer *ArchitectureReviewer
}

// NewSemanticReviewer constructs a unified SemanticReviewer.
func NewSemanticReviewer(client llm.LLMClient, opts ...ReviewerOption) *SemanticReviewer {
	cfg := reviewerConfig{
		llmClient: client,
		model:     DefaultReviewerModel,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &SemanticReviewer{
		llmClient:    cfg.llmClient,
		model:        cfg.model,
		brain:        cfg.brain,
		rules:        cfg.rules,
		secReviewer:  NewSecurityReviewer(),
		archReviewer: NewArchitectureReviewer(cfg.brain, cfg.rules...),
	}
}

// WithLLM sets or updates the LLMClient for semantic reviewing.
func (r *SemanticReviewer) WithLLM(client llm.LLMClient, model ...string) *SemanticReviewer {
	r.llmClient = client
	if len(model) > 0 && model[0] != "" {
		r.model = model[0]
	}
	return r
}

// Review conducts a comprehensive audit combining security vulnerabilities, data races, and architectural invariants.
func (r *SemanticReviewer) Review(ctx context.Context, req ReviewRequest) (*ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Always compute deterministic fallback findings
	secDet, err := r.secReviewer.runDeterministicAudit(ctx, req)
	if err != nil {
		return nil, err
	}
	activeRules := append([]brain.ArchitecturalRule{}, r.rules...)
	if r.brain != nil {
		if brainRules, err := r.brain.ListRules(ctx, 100); err == nil {
			activeRules = append(activeRules, brainRules...)
		}
	}
	archDet, err := r.archReviewer.runDeterministicAudit(ctx, req, activeRules)
	if err != nil {
		return nil, err
	}

	deterministicFindings := mergeFindings(secDet, archDet)
	findings := deterministicFindings
	var semanticSummary string

	if r.llmClient != nil {
		llmFindings, summary, llmErr := r.auditWithLLM(ctx, req, activeRules)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if llmErr == nil {
			if len(llmFindings) > 0 {
				findings = mergeFindings(deterministicFindings, llmFindings)
			}
			semanticSummary = summary
		}
		// On LLM failure, deterministicFindings remain as fallback
	}

	score, maxSev, passed := evaluateFindings(findings)
	summary := "Semantic review passed cleanly."
	if !passed {
		summary = fmt.Sprintf("Semantic review failed with %d findings (highest severity: %s).", len(findings), maxSev)
		if semanticSummary != "" {
			summary += " " + semanticSummary
		}
	} else if semanticSummary != "" {
		summary = semanticSummary
	}

	return &ReviewResult{
		ReviewerName: "SemanticReviewer",
		Passed:       passed,
		Score:        score,
		Severity:     maxSev,
		Findings:     findings,
		Summary:      summary,
	}, nil
}

// auditWithLLM executes the unified prompt auditing vulnerabilities, data races, and architectural rules.
func (r *SemanticReviewer) auditWithLLM(ctx context.Context, req ReviewRequest, activeRules []brain.ArchitecturalRule) ([]Finding, string, error) {
	model := r.model
	if model == "" {
		model = DefaultReviewerModel
	}

	rulesDesc := ""
	for _, rule := range activeRules {
		rulesDesc += fmt.Sprintf("- [%s] %s: %s\n", rule.RuleCode, rule.Title, rule.Description)
	}

	systemPrompt := "You are a principal engineer conducting an automated code review on Go diffs. " +
		"Audit the diff for three critical areas:\n" +
		"1. Security vulnerabilities (secrets, credential leaks, SQL/command injection, weak cryptography, insecure permissions).\n" +
		"2. Data races and concurrency issues (unprotected shared variables/maps/slices, missing mutexes, goroutine leaks, deadlocks).\n" +
		"3. Architectural and governance violations (pkg/ importing internal/, git checkout/switch in sandboxes, missing SPDX Apache-2.0 headers in new files, direct main mutations).\n" +
		"Output ONLY a valid JSON object matching the requested schema without markdown formatting or surrounding explanation."

	userPrompt := fmt.Sprintf(`Perform a semantic code review on the following git diff:

Active Architectural Rules:
%s

Diff:
%s

Files changed: %s

Return a JSON object in this exact format:
{
  "findings": [
    {
      "file": "path/to/file.go",
      "line": 10,
      "rule_code": "SEC-RACE-001",
      "message": "Data race on concurrent access to shared map",
      "severity": "CRITICAL"
    }
  ],
  "summary": "Overall summary of semantic audit"
}
Allowed severity values: "INFO", "WARN", "ERROR", "CRITICAL".
If clean, return an empty "findings" array.`, rulesDesc, req.Diff, strings.Join(req.FilesChanged, ", "))

	chatReq := &llm.ChatRequest{
		Model: model,
		Messages: []llm.ChatMessage{
			llm.NewSystemMessage(systemPrompt),
			llm.NewUserMessage(userPrompt),
		},
	}

	resp, err := r.llmClient.ChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, "", err
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return nil, "", nil
	}

	cleanJSON := extractReviewerJSON(resp.Message.Content)
	var parsed reviewerLLMResponse
	if err := json.Unmarshal([]byte(cleanJSON), &parsed); err != nil {
		return nil, "", err
	}

	return parsed.Findings, parsed.Summary, nil
}

// extractReviewerJSON extracts JSON content stripped of Markdown code fences.
var reviewerCodeFenceRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

func extractReviewerJSON(s string) string {
	s = strings.TrimSpace(s)
	if matches := reviewerCodeFenceRegex.FindStringSubmatch(s); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

// mergeFindings combines two slices of findings and deduplicates by File, Line, and RuleCode.
func mergeFindings(base []Finding, additions []Finding) []Finding {
	seen := make(map[string]bool)
	var merged []Finding

	for _, f := range base {
		f.Severity = normalizeSeverity(f.Severity)
		key := fmt.Sprintf("%s:%d:%s", f.File, f.Line, f.RuleCode)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, f)
		}
	}

	for _, f := range additions {
		f.Severity = normalizeSeverity(f.Severity)
		key := fmt.Sprintf("%s:%d:%s", f.File, f.Line, f.RuleCode)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, f)
		}
	}

	return merged
}

// normalizeSeverity ensures severity matches standard capitalized severity constants.
func normalizeSeverity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case SeverityCritical:
		return SeverityCritical
	case SeverityError:
		return SeverityError
	case SeverityWarn, "WARNING":
		return SeverityWarn
	case SeverityInfo, "INFORMATIONAL":
		return SeverityInfo
	default:
		return SeverityWarn
	}
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
