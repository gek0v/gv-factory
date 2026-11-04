// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

var (
	ErrNilRunner = errors.New("runtime: runner cannot be nil")
	ErrNilSpec   = errors.New("runtime: spec document cannot be nil")
)

// AgentTemplate defines a parameterized configuration template for autonomous agents.
type AgentTemplate struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Model        string   `json:"model,omitempty"`
	Thinking     string   `json:"thinking,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Extensions   []string `json:"extensions,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
}

// DefaultSpecWriterTemplate returns the standard agent configuration for software spec synthesis.
func DefaultSpecWriterTemplate() AgentTemplate {
	return AgentTemplate{
		Name:     "SpecWriter",
		Role:     "Architectural Spec Synthesizer",
		Model:    "",
		Thinking: "medium",
		Skills:   []string{"codebase-design", "domain-modeling"},
		Tools:    []string{"read", "grep", "find", "ls"},
		SystemPrompt: `You are the SpecWriter autonomous agent in gv-factory.
Your task is to analyze the assigned task and repository architecture, and synthesize a formal Software Design Document (SDD) with a complete Test-Driven Development (TDD) plan.
You MUST output your final answer as a single valid JSON object matching the SpecDocument schema:
{
  "title": "Task title",
  "architecture_overview": "Summary of architectural changes",
  "impacted_files": ["file1.go"],
  "architectural_rules_applied": ["Rule 1"],
  "past_lessons_considered": [],
  "tdd_plan": {
    "summary": "TDD plan summary",
    "test_cases": [
      {
        "name": "TestCaseName",
        "description": "What it tests",
        "type": "unit",
        "target_file": "file1_test.go",
        "expected": "Expected behavior"
      }
    ]
  },
  "atomic_tasks": [
    {
      "id": "task-1",
      "title": "Task 1",
      "description": "Implement feature X",
      "dependencies": [],
      "acceptance_criteria": ["Tests pass"]
    }
  ]
}`,
	}
}

// DefaultBuilderTemplate returns the standard agent configuration for Red-Green-Refactor builds.
func DefaultBuilderTemplate() AgentTemplate {
	return BuilderTemplateForStack(sandbox.DefaultGoStack())
}

// BuilderTemplateForStack returns an AgentTemplate configured for the detected language and stack.
func BuilderTemplateForStack(stack sandbox.StackInfo) AgentTemplate {
	testCmd := strings.Join(stack.TestCommand, " ")
	if testCmd == "" {
		testCmd = "go test -v -cover ./..."
	}
	lang := stack.Language
	if lang == "" {
		lang = "go"
	}
	srcExt := stack.SourceExtension
	if srcExt == "" {
		srcExt = ".go"
	}
	testExt := stack.TestExtension
	if testExt == "" {
		testExt = "_test.go"
	}

	systemPrompt := fmt.Sprintf(`You are the Builder autonomous engineer agent in gv-factory specialized in %s.
Your task is to execute the Red-Green-Refactor TDD cycle inside the isolated git worktree:
1. Write unit tests first asserting expected behavior in *%s files.
2. Run '%s' to confirm failure (Red).
3. Implement the minimum clean code in *%s files to make tests pass (Green).
4. Refactor and ensure all linters and tests pass with >= 90%% coverage.
5. All code must strictly include the SPDX header:
   // SPDX-License-Identifier: Apache-2.0
   // SPDX-FileCopyrightText: 2026 gv-factory contributors
Report your progress and output a final summary of modified files and verification results.`,
		lang, testExt, testCmd, srcExt,
	)

	name := "Builder"
	if lang != "go" {
		name = fmt.Sprintf("Builder-%s", strings.Title(lang))
	}

	return AgentTemplate{
		Name:         name,
		Role:         fmt.Sprintf("TDD Autonomous Engineer (%s)", strings.Title(lang)),
		Model:        "",
		Thinking:     "high",
		Skills:       []string{"tdd"},
		Tools:        []string{"read", "bash", "edit", "write"},
		SystemPrompt: systemPrompt,
	}
}

// DefaultSecurityReviewerTemplate returns the standard configuration for OWASP/secrets audit.
func DefaultSecurityReviewerTemplate() AgentTemplate {
	return AgentTemplate{
		Name:     "Security Reviewer",
		Role:     "Security & Vulnerability Auditor",
		Model:    "",
		Thinking: "medium",
		Skills:   []string{"security-audit"},
		Tools:    []string{"read", "grep", "find"},
		SystemPrompt: `You are the Security Reviewer autonomous agent in gv-factory.
Your task is to audit code diffs for security vulnerabilities, injection flaws, OWASP Top 10 risks, hardcoded credentials, and unauthorized access.
You MUST output your final verdict as a single valid JSON object matching the ReviewResult schema:
{
  "reviewer_name": "Security Reviewer",
  "passed": true,
  "score": 1.0,
  "severity": "LOW",
  "findings": [],
  "summary": "Clean security audit, no vulnerabilities detected."
}`,
	}
}

// DefaultArchitectureReviewerTemplate returns the standard configuration for architectural review.
func DefaultArchitectureReviewerTemplate() AgentTemplate {
	return AgentTemplate{
		Name:     "Architecture Reviewer",
		Role:     "Architecture & Standards Auditor",
		Model:    "",
		Thinking: "medium",
		Skills:   []string{"codebase-design", "code-quality-audit"},
		Tools:    []string{"read", "grep", "find"},
		SystemPrompt: `You are the Architecture Reviewer autonomous agent in gv-factory.
Your task is to audit code diffs against project architecture, package boundaries, REUSE SPDX licensing, and Go idioms.
You MUST output your final verdict as a single valid JSON object matching the ReviewResult schema:
{
  "reviewer_name": "Architecture Reviewer",
  "passed": true,
  "score": 1.0,
  "severity": "LOW",
  "findings": [],
  "summary": "Architectural guidelines and SPDX compliance verified."
}`,
	}
}

// DefaultCodeReviewerTemplate returns the standard configuration for code quality and test review.
func DefaultCodeReviewerTemplate() AgentTemplate {
	return AgentTemplate{
		Name:     "Code Reviewer",
		Role:     "Code Quality & QA Auditor",
		Model:    "",
		Thinking: "medium",
		Skills:   []string{"code-review", "qa-audit"},
		Tools:    []string{"read", "grep", "find"},
		SystemPrompt: `You are the Code Reviewer autonomous agent in gv-factory.
Your task is to inspect code quality, idiomatic patterns, test completeness, and error handling.
You MUST output your final verdict as a single valid JSON object matching the ReviewResult schema:
{
  "reviewer_name": "Code Reviewer",
  "passed": true,
  "score": 1.0,
  "severity": "LOW",
  "findings": [],
  "summary": "High code quality and adequate test assertions."
}`,
	}
}

// PiSpecWriter adapts PiRunner to the agent.SpecGenerator interface.
type PiSpecWriter struct {
	runner       *PiRunner
	template     AgentTemplate
	workspaceDir string
}

// NewPiSpecWriter constructs a PiSpecWriter.
func NewPiSpecWriter(runner *PiRunner, template AgentTemplate, workspaceDir string) (*PiSpecWriter, error) {
	if runner == nil {
		return nil, ErrNilRunner
	}
	if template.Name == "" {
		template = DefaultSpecWriterTemplate()
	}
	return &PiSpecWriter{
		runner:       runner,
		template:     template,
		workspaceDir: workspaceDir,
	}, nil
}

// GenerateSpec synthesizes a formal SpecDocument by executing an agent run via Pi-Bolt.
func (w *PiSpecWriter) GenerateSpec(ctx context.Context, task agent.TaskContext) (*agent.SpecDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := task.Validate(); err != nil {
		return nil, err
	}
	task.Normalize()

	userPrompt := fmt.Sprintf(`Synthesize a formal Software Design Document for task:
ID: %s
Title: %s
Description: %s
Constraints: %s
Repository Root: %s`,
		task.TaskID,
		task.Title,
		task.Description,
		strings.Join(task.Constraints, "; "),
		task.RepositoryRoot,
	)

	ws := w.workspaceDir
	if ws == "" {
		ws = task.RepositoryRoot
	}

	model := w.template.Model
	if model == "" {
		model = os.Getenv("MODEL")
	}
	if model == "" {
		model = os.Getenv("OPENAI_COMPATIBLE_MODEL")
	}
	if model == "" {
		model = os.Getenv("OPENAI_MODEL")
	}

	cfg := PiRunConfig{
		TaskID:       task.TaskID,
		WorkspaceDir: ws,
		Model:        model,
		Thinking:     w.template.Thinking,
		Skills:       w.template.Skills,
		Tools:        w.template.Tools,
		Extensions:   w.template.Extensions,
		SystemPrompt: w.template.SystemPrompt,
		UserPrompt:   userPrompt,
		Mode:         "json",
		NoSession:    true,
	}

	res, err := w.runner.Run(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("runtime: spec synthesis failed: %w", err)
	}

	var spec agent.SpecDocument
	if err := extractJSON(res.Output, &spec); err != nil {
		// Fallback: construct minimally valid spec if LLM responded with freeform text
		spec = agent.SpecDocument{
			Title:                task.Title,
			ArchitectureOverview: res.Output,
			ImpactedFiles:        []string{},
			AtomicTasks: []agent.AtomicTask{
				{
					ID:                 task.TaskID + "-01",
					Title:              task.Title,
					Description:        task.Description,
					AcceptanceCriteria: []string{"go test ./... passes"},
				},
			},
		}
	}

	if spec.Title == "" {
		spec.Title = task.Title
	}

	return &spec, nil
}

// PiBuilder adapts PiRunner to the agent.TaskBuilder interface.
type PiBuilder struct {
	runner       *PiRunner
	template     AgentTemplate
	workspaceDir string
	stack        sandbox.StackInfo
}

// NewPiBuilder constructs a PiBuilder.
func NewPiBuilder(runner *PiRunner, template AgentTemplate, workspaceDir string) (*PiBuilder, error) {
	if runner == nil {
		return nil, ErrNilRunner
	}
	stack := sandbox.DetectStack(workspaceDir)
	if template.Name == "" {
		template = BuilderTemplateForStack(stack)
	}
	return &PiBuilder{
		runner:       runner,
		template:     template,
		workspaceDir: workspaceDir,
		stack:        stack,
	}, nil
}

// NewPiBuilderForStack constructs a PiBuilder configured explicitly for a target stack.
func NewPiBuilderForStack(runner *PiRunner, template AgentTemplate, workspaceDir string, stack sandbox.StackInfo) (*PiBuilder, error) {
	if runner == nil {
		return nil, ErrNilRunner
	}
	if stack.Language == "" {
		stack = sandbox.DetectStack(workspaceDir)
	}
	if template.Name == "" {
		template = BuilderTemplateForStack(stack)
	}
	return &PiBuilder{
		runner:       runner,
		template:     template,
		workspaceDir: workspaceDir,
		stack:        stack,
	}, nil
}

// BuildSpec executes autonomous TDD builds for the given spec using Pi-Bolt.
func (b *PiBuilder) BuildSpec(ctx context.Context, spec *agent.SpecDocument) ([]*agent.BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, ErrNilSpec
	}

	tasks := spec.AtomicTasks
	if len(tasks) == 0 {
		tasks = []agent.AtomicTask{
			{
				ID:          "build-spec",
				Description: spec.ArchitectureOverview,
			},
		}
	}

	results := make([]*agent.BuildResult, 0, len(tasks))

	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		userPrompt := fmt.Sprintf(`Execute TDD implementation for atomic task:
Task ID: %s
Title: %s
Description: %s
Language: %s (Test Command: %s, Source: *%s, Tests: *%s)
Dependencies: %s
Acceptance Criteria: %s

Architecture Overview: %s`,
			task.ID,
			task.Title,
			task.Description,
			b.stack.Language,
			strings.Join(b.stack.TestCommand, " "),
			b.stack.SourceExtension,
			b.stack.TestExtension,
			strings.Join(task.Dependencies, ", "),
			strings.Join(task.AcceptanceCriteria, "; "),
			spec.ArchitectureOverview,
		)

		model := b.template.Model
		if model == "" {
			model = os.Getenv("MODEL")
		}
		if model == "" {
			model = os.Getenv("OPENAI_COMPATIBLE_MODEL")
		}
		if model == "" {
			model = os.Getenv("OPENAI_MODEL")
		}

		cfg := PiRunConfig{
			TaskID:       task.ID,
			WorkspaceDir: b.workspaceDir,
			Model:        model,
			Thinking:     b.template.Thinking,
			Skills:       b.template.Skills,
			Tools:        b.template.Tools,
			Extensions:   b.template.Extensions,
			SystemPrompt: b.template.SystemPrompt,
			UserPrompt:   userPrompt,
			Mode:         "json",
			NoSession:    true,
		}

		start := time.Now()
		res, err := b.runner.Run(ctx, cfg)

		buildRes := &agent.BuildResult{
			TaskID:        task.ID,
			TotalDuration: time.Since(start),
			Phases: []agent.PhaseOutcome{
				{
					Phase:    "pi-runner-tdd",
					ExitCode: 0,
					Stdout:   "",
					Duration: time.Since(start),
				},
			},
		}

		if res != nil {
			buildRes.Phases[0].ExitCode = res.ExitCode
			buildRes.Phases[0].Stdout = res.Output
			buildRes.Phases[0].Stderr = res.Stderr
			buildRes.Success = res.ExitCode == 0
		}

		if err != nil {
			buildRes.Success = false
			buildRes.ErrorMessage = err.Error()
		}

		results = append(results, buildRes)
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// PiReviewer adapts PiRunner to the agent.DiffReviewer interface.
type PiReviewer struct {
	name         string
	runner       *PiRunner
	template     AgentTemplate
	workspaceDir string
}

// NewPiReviewer constructs a PiReviewer for a specific audit dimension.
func NewPiReviewer(runner *PiRunner, template AgentTemplate, workspaceDir string) (*PiReviewer, error) {
	if runner == nil {
		return nil, ErrNilRunner
	}
	if template.Name == "" {
		template = DefaultSecurityReviewerTemplate()
	}
	return &PiReviewer{
		name:         template.Name,
		runner:       runner,
		template:     template,
		workspaceDir: workspaceDir,
	}, nil
}

// Name returns the identifier of the reviewer pass.
func (r *PiReviewer) Name() string {
	return r.name
}

// Review audits the code changes against the reviewer's specialty.
func (r *PiReviewer) Review(ctx context.Context, req agent.ReviewRequest) (*agent.ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	userPrompt := fmt.Sprintf(`Review the following code diff for %s:
Run ID: %s
Files Changed: %s

=== GIT DIFF ===
%s
=== END DIFF ===`,
		r.name,
		req.RunID,
		strings.Join(req.FilesChanged, ", "),
		req.Diff,
	)

	model := r.template.Model
	if model == "" {
		model = os.Getenv("MODEL")
	}
	if model == "" {
		model = os.Getenv("OPENAI_COMPATIBLE_MODEL")
	}
	if model == "" {
		model = os.Getenv("OPENAI_MODEL")
	}

	cfg := PiRunConfig{
		RunID:        req.RunID,
		WorkspaceDir: r.workspaceDir,
		Model:        model,
		Thinking:     r.template.Thinking,
		Skills:       r.template.Skills,
		Tools:        r.template.Tools,
		Extensions:   r.template.Extensions,
		SystemPrompt: r.template.SystemPrompt,
		UserPrompt:   userPrompt,
		Mode:         "json",
		NoSession:    true,
	}

	res, err := r.runner.Run(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("runtime: %s review failed: %w", r.name, err)
	}

	var reviewResult agent.ReviewResult
	if err := extractJSON(res.Output, &reviewResult); err != nil {
		// Fallback when review output is unstructured text
		passed := !strings.Contains(strings.ToLower(res.Output), "vulnerability") &&
			!strings.Contains(strings.ToLower(res.Output), "violation") &&
			!strings.Contains(strings.ToLower(res.Output), "defect")

		score := float32(0.85)
		if !passed {
			score = 0.50
		}

		reviewResult = agent.ReviewResult{
			ReviewerName: r.name,
			Passed:       passed,
			Score:        score,
			Severity:     "LOW",
			Summary:      res.Output,
			Findings:     []agent.Finding{},
		}
	}

	reviewResult.ReviewerName = r.name
	return &reviewResult, nil
}

// AdapterOption configures PiCoordinatorAdapter.
type AdapterOption func(*PiCoordinatorAdapter)

// WithSpecTemplate overrides the SpecWriter template.
func WithSpecTemplate(tpl AgentTemplate) AdapterOption {
	return func(a *PiCoordinatorAdapter) {
		a.specTemplate = tpl
	}
}

// WithBuilderTemplate overrides the Builder template.
func WithBuilderTemplate(tpl AgentTemplate) AdapterOption {
	return func(a *PiCoordinatorAdapter) {
		a.builderTemplate = tpl
	}
}

// WithReviewerTemplates sets custom reviewer templates.
func WithReviewerTemplates(tpls ...AgentTemplate) AdapterOption {
	return func(a *PiCoordinatorAdapter) {
		a.reviewerTemplates = tpls
	}
}

// PiCoordinatorAdapter bundles SpecWriter, Builder, and parallel Reviewers for FactoryCoordinator.
type PiCoordinatorAdapter struct {
	runner            *PiRunner
	workspaceDir      string
	specTemplate      AgentTemplate
	builderTemplate   AgentTemplate
	reviewerTemplates []AgentTemplate
}

// NewPiCoordinatorAdapter creates a fully configured adapter connecting PiRunner with FactoryCoordinator.
func NewPiCoordinatorAdapter(runner *PiRunner, workspaceDir string, opts ...AdapterOption) (*PiCoordinatorAdapter, error) {
	if runner == nil {
		return nil, ErrNilRunner
	}

	adapter := &PiCoordinatorAdapter{
		runner:          runner,
		workspaceDir:    workspaceDir,
		specTemplate:    DefaultSpecWriterTemplate(),
		builderTemplate: DefaultBuilderTemplate(),
		reviewerTemplates: []AgentTemplate{
			DefaultSecurityReviewerTemplate(),
			DefaultArchitectureReviewerTemplate(),
			DefaultCodeReviewerTemplate(),
		},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(adapter)
		}
	}

	return adapter, nil
}

// SpecWriter constructs a SpecGenerator backed by PiRunner.
func (a *PiCoordinatorAdapter) SpecWriter() agent.SpecGenerator {
	w, _ := NewPiSpecWriter(a.runner, a.specTemplate, a.workspaceDir)
	return w
}

// Builder constructs a TaskBuilder backed by PiRunner.
func (a *PiCoordinatorAdapter) Builder() agent.TaskBuilder {
	b, _ := NewPiBuilder(a.runner, a.builderTemplate, a.workspaceDir)
	return b
}

// Reviewers constructs all configured DiffReviewers backed by PiRunner.
func (a *PiCoordinatorAdapter) Reviewers() []agent.DiffReviewer {
	reviewers := make([]agent.DiffReviewer, 0, len(a.reviewerTemplates))
	for _, tpl := range a.reviewerTemplates {
		r, err := NewPiReviewer(a.runner, tpl, a.workspaceDir)
		if err == nil {
			reviewers = append(reviewers, r)
		}
	}
	return reviewers
}

// Approver creates an agent.Approver wired with the PiRunner diff reviewers.
func (a *PiCoordinatorAdapter) Approver(cfg ...agent.ApproverConfig) *agent.Approver {
	var c agent.ApproverConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return agent.NewApprover(a.Reviewers(), c)
}

// CoordinatorConfig generates a complete agent.CoordinatorConfig with PiRunner agents wired.
func (a *PiCoordinatorAdapter) CoordinatorConfig(base ...agent.CoordinatorConfig) agent.CoordinatorConfig {
	var cfg agent.CoordinatorConfig
	if len(base) > 0 {
		cfg = base[0]
	}

	cfg.SpecWriter = a.SpecWriter()
	cfg.Builder = a.Builder()
	cfg.Approver = a.Approver()
	cfg.Normalize()
	return cfg
}

var jsonBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\}|\\[.*?\\])\\s*```")

// extractJSON attempts to parse JSON directly or extracts JSON enclosed in markdown code fences or curly braces.
func extractJSON(input string, v any) error {
	trimmed := strings.TrimSpace(input)
	if err := json.Unmarshal([]byte(trimmed), v); err == nil {
		return nil
	}

	// Try extracting from markdown code block
	matches := jsonBlockRegex.FindStringSubmatch(trimmed)
	if len(matches) > 1 {
		if err := json.Unmarshal([]byte(matches[1]), v); err == nil {
			return nil
		}
	}

	// Try finding the outermost JSON object: first '{' and last '}'
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		sub := trimmed[start : end+1]
		if err := json.Unmarshal([]byte(sub), v); err == nil {
			return nil
		}
	}

	return fmt.Errorf("runtime: failed to extract valid JSON from input: %s", trimmed)
}
