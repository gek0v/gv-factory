// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

// Standard domain errors for SpecWriter agent operations.
var (
	ErrNilTaskContext = errors.New("agent: task context cannot be nil")
	ErrEmptyTaskID    = errors.New("agent: task id is required")
	ErrEmptyTaskTitle = errors.New("agent: task title is required")
)

// TaskContext encapsulates all required and optional input parameters for a task.
type TaskContext struct {
	TaskID         string         `json:"task_id"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Labels         []string       `json:"labels"`
	RepositoryRoot string         `json:"repository_root"`
	Constraints    []string       `json:"constraints"`
	Metadata       map[string]any `json:"metadata"`
}

// Validate verifies that required fields are present and valid.
func (tc *TaskContext) Validate() error {
	if tc == nil {
		return ErrNilTaskContext
	}
	if strings.TrimSpace(tc.TaskID) == "" {
		return ErrEmptyTaskID
	}
	if strings.TrimSpace(tc.Title) == "" {
		return ErrEmptyTaskTitle
	}
	return nil
}

// Normalize trims whitespace and ensures slice/map members are non-nil.
func (tc *TaskContext) Normalize() {
	if tc == nil {
		return
	}
	tc.TaskID = strings.TrimSpace(tc.TaskID)
	tc.Title = strings.TrimSpace(tc.Title)
	tc.Description = strings.TrimSpace(tc.Description)
	tc.RepositoryRoot = strings.TrimSpace(tc.RepositoryRoot)
	if tc.Labels == nil {
		tc.Labels = []string{}
	}
	if tc.Constraints == nil {
		tc.Constraints = []string{}
	}
	if tc.Metadata == nil {
		tc.Metadata = make(map[string]any)
	}
}

// BrainKnowledgeSource defines the knowledge access contract supported by brain.Service or mocks.
type BrainKnowledgeSource interface {
	ListRules(ctx context.Context, limit int) ([]brain.ArchitecturalRule, error)
	HybridSearchRules(ctx context.Context, q brain.HybridSearchQuery) ([]brain.ArchitecturalRuleResult, error)
	HybridSearchCodebase(ctx context.Context, q brain.HybridSearchQuery) ([]brain.CodebaseKnowledgeResult, error)
	QueryCodebaseKnowledge(ctx context.Context, embedding []float32, limit int) ([]brain.CodebaseKnowledge, error)
	QuerySimilarEpisodes(ctx context.Context, embedding []float32, limit int) ([]brain.TaskEpisode, error)
}

// TestCase represents an individual TDD test scenario.
type TestCase struct {
	Name            string `json:"name"`
	Phase           string `json:"phase"` // Red, Green, Refactor
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expected_outcome"`
}

// TDDPlan outlines the testing strategy across Red, Green, and Refactor phases.
type TDDPlan struct {
	Summary   string     `json:"summary,omitempty"`
	TestCases []TestCase `json:"test_cases"`
}

// AtomicTask represents a fine-grained, verifiable unit of execution.
type AtomicTask struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Dependencies       []string `json:"dependencies"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// SpecDocument represents the complete Software Design Document (SDD) and implementation plan.
type SpecDocument struct {
	Title                     string       `json:"title"`
	ArchitectureOverview      string       `json:"architecture_overview"`
	ImpactedFiles             []string     `json:"impacted_files"`
	ArchitecturalRulesApplied []string     `json:"architectural_rules_applied"`
	PastLessonsConsidered     []string     `json:"past_lessons_considered"`
	TDDPlan                   TDDPlan      `json:"tdd_plan"`
	AtomicTasks               []AtomicTask `json:"atomic_tasks"`
}

// ToJSON serializes the SpecDocument to indented JSON.
func (d *SpecDocument) ToJSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// ToMarkdown renders the SpecDocument as a structured Markdown document.
func (d *SpecDocument) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Software Design Document: %s\n\n", d.Title))

	sb.WriteString("## Architecture Overview\n")
	sb.WriteString(d.ArchitectureOverview)
	sb.WriteString("\n\n")

	sb.WriteString("## Impacted Files\n")
	if len(d.ImpactedFiles) == 0 {
		sb.WriteString("- None specified\n")
	} else {
		for _, file := range d.ImpactedFiles {
			sb.WriteString(fmt.Sprintf("- `%s`\n", file))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Architectural Rules Applied\n")
	if len(d.ArchitecturalRulesApplied) == 0 {
		sb.WriteString("- None specified\n")
	} else {
		for _, rule := range d.ArchitecturalRulesApplied {
			sb.WriteString(fmt.Sprintf("- %s\n", rule))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Past Lessons Considered\n")
	if len(d.PastLessonsConsidered) == 0 {
		sb.WriteString("- None specified\n")
	} else {
		for _, lesson := range d.PastLessonsConsidered {
			sb.WriteString(fmt.Sprintf("- %s\n", lesson))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## TDD Implementation Plan\n")
	if d.TDDPlan.Summary != "" {
		sb.WriteString(d.TDDPlan.Summary)
		sb.WriteString("\n\n")
	}
	if len(d.TDDPlan.TestCases) > 0 {
		sb.WriteString("| Phase | Test Case | Description | Expected Outcome |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, tc := range d.TDDPlan.TestCases {
			sb.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n", tc.Phase, tc.Name, tc.Description, tc.ExpectedOutcome))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Atomic Implementation Tasks\n")
	for _, task := range d.AtomicTasks {
		sb.WriteString(fmt.Sprintf("### [%s] %s\n", task.ID, task.Title))
		sb.WriteString(fmt.Sprintf("- **Description:** %s\n", task.Description))
		if len(task.Dependencies) > 0 {
			sb.WriteString(fmt.Sprintf("- **Dependencies:** %s\n", strings.Join(task.Dependencies, ", ")))
		} else {
			sb.WriteString("- **Dependencies:** None\n")
		}
		if len(task.AcceptanceCriteria) > 0 {
			sb.WriteString("- **Acceptance Criteria:**\n")
			for _, ac := range task.AcceptanceCriteria {
				sb.WriteString(fmt.Sprintf("  - [ ] %s\n", ac))
			}
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String()) + "\n"
}

// SpecWriterConfig configures the autonomous SpecWriter agent.
type SpecWriterConfig struct {
	Brain       BrainKnowledgeSource
	LLM         llm.LLMClient
	Model       string
	Temperature *float64
}

// SpecWriter is an autonomous agent that normalizes tasks, gathers brain memory,
// and synthesizes formal Software Design Documents (SDD) and TDD plans.
type SpecWriter struct {
	brain       BrainKnowledgeSource
	llm         llm.LLMClient
	model       string
	temperature *float64
}

// NewSpecWriter constructs a new SpecWriter agent.
func NewSpecWriter(cfg SpecWriterConfig) *SpecWriter {
	model := cfg.Model
	if model == "" {
		model = "claude-3-5-sonnet"
	}
	return &SpecWriter{
		brain:       cfg.Brain,
		llm:         cfg.LLM,
		model:       model,
		temperature: cfg.Temperature,
	}
}

// WriteSpec is an alias for GenerateSpec.
func (w *SpecWriter) WriteSpec(ctx context.Context, task TaskContext) (*SpecDocument, error) {
	return w.GenerateSpec(ctx, task)
}

// GenerateSpec normalizes the task, gathers brain memory, and synthesizes the spec.
func (w *SpecWriter) GenerateSpec(ctx context.Context, task TaskContext) (*SpecDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := task.Validate(); err != nil {
		return nil, err
	}
	task.Normalize()

	// Gather memory from Brain
	rules, codebaseFiles, pastLessons := w.gatherBrainKnowledge(ctx, task)

	// Attempt LLM Synthesis if an LLM client is configured
	if w.llm != nil {
		if doc, err := w.synthesizeWithLLM(ctx, task, rules, codebaseFiles, pastLessons); err == nil && doc != nil {
			return doc, nil
		}
	}

	// Fallback to deterministic synthesis
	return w.generateFallbackSpec(task, rules, codebaseFiles, pastLessons), nil
}

// gatherBrainKnowledge queries rules, codebase knowledge, and past episodes.
func (w *SpecWriter) gatherBrainKnowledge(ctx context.Context, task TaskContext) ([]string, []string, []string) {
	if w.brain == nil {
		return nil, nil, nil
	}

	query := task.Title
	if task.Description != "" {
		query += " " + task.Description
	}

	// 1. Gather Architectural Rules
	var rules []string
	searchQuery := brain.NewHybridSearchQuery(query).WithLimit(5)
	if ruleResults, err := w.brain.HybridSearchRules(ctx, searchQuery); err == nil && len(ruleResults) > 0 {
		for _, r := range ruleResults {
			entry := fmt.Sprintf("%s (%s)", r.RuleCode, r.Title)
			if r.Description != "" {
				entry = fmt.Sprintf("%s: %s", entry, r.Description)
			}
			rules = append(rules, entry)
		}
	} else if list, err := w.brain.ListRules(ctx, 10); err == nil && len(list) > 0 {
		for _, r := range list {
			entry := fmt.Sprintf("%s (%s)", r.RuleCode, r.Title)
			if r.Description != "" {
				entry = fmt.Sprintf("%s: %s", entry, r.Description)
			}
			rules = append(rules, entry)
		}
	}

	// 2. Gather Codebase Knowledge
	var files []string
	seenFiles := make(map[string]struct{})
	if codeResults, err := w.brain.HybridSearchCodebase(ctx, searchQuery); err == nil && len(codeResults) > 0 {
		for _, c := range codeResults {
			if _, exists := seenFiles[c.FilePath]; !exists && c.FilePath != "" {
				seenFiles[c.FilePath] = struct{}{}
				files = append(files, c.FilePath)
			}
		}
	} else if codeList, err := w.brain.QueryCodebaseKnowledge(ctx, nil, 5); err == nil && len(codeList) > 0 {
		for _, c := range codeList {
			if _, exists := seenFiles[c.FilePath]; !exists && c.FilePath != "" {
				seenFiles[c.FilePath] = struct{}{}
				files = append(files, c.FilePath)
			}
		}
	}

	// 3. Gather Past Episodes
	var pastLessons []string
	if episodes, err := w.brain.QuerySimilarEpisodes(ctx, nil, 5); err == nil && len(episodes) > 0 {
		for _, ep := range episodes {
			if ep.LessonsLearned != "" {
				pastLessons = append(pastLessons, ep.LessonsLearned)
			} else if ep.Summary != "" {
				pastLessons = append(pastLessons, ep.Summary)
			}
		}
	}

	return rules, files, pastLessons
}

// synthesizeWithLLM queries the LLMClient using a structured prompt.
func (w *SpecWriter) synthesizeWithLLM(
	ctx context.Context,
	task TaskContext,
	rules []string,
	codebaseFiles []string,
	pastLessons []string,
) (*SpecDocument, error) {
	systemPrompt := `You are the autonomous SpecWriter agent for gv-factory.
Your mission is to produce a comprehensive Software Design Document (SDD) and an atomic TDD implementation plan.
Return ONLY valid JSON strictly matching the SpecDocument structure with no extraneous text:
{
  "title": string,
  "architecture_overview": string,
  "impacted_files": [string],
  "architectural_rules_applied": [string],
  "past_lessons_considered": [string],
  "tdd_plan": {
    "summary": string,
    "test_cases": [
      {
        "name": string,
        "phase": "Red"|"Green"|"Refactor",
        "description": string,
        "expected_outcome": string
      }
    ]
  },
  "atomic_tasks": [
    {
      "id": string,
      "title": string,
      "description": string,
      "dependencies": [string],
      "acceptance_criteria": [string]
    }
  ]
}`

	userPrompt := fmt.Sprintf(`Task ID: %s
Title: %s
Description: %s
Repository Root: %s
Labels: %s
Constraints: %s

Architectural Rules in Memory:
%s

Relevant Codebase Files:
%s

Past Lessons Learned:
%s

Generate the complete JSON Software Design Document.`,
		task.TaskID,
		task.Title,
		task.Description,
		task.RepositoryRoot,
		strings.Join(task.Labels, ", "),
		strings.Join(task.Constraints, "; "),
		formatList(rules),
		formatList(codebaseFiles),
		formatList(pastLessons),
	)

	req := &llm.ChatRequest{
		Model: w.model,
		Messages: []llm.ChatMessage{
			llm.NewSystemMessage(systemPrompt),
			llm.NewUserMessage(userPrompt),
		},
		Temperature: w.temperature,
	}

	resp, err := w.llm.ChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return nil, errors.New("empty response from LLM")
	}

	cleanJSON := extractJSON(resp.Message.Content)
	var doc SpecDocument
	if err := json.Unmarshal([]byte(cleanJSON), &doc); err != nil {
		return nil, fmt.Errorf("failed to parse LLM JSON: %w", err)
	}

	if doc.Title == "" {
		doc.Title = task.Title
	}
	if len(doc.ImpactedFiles) == 0 && len(codebaseFiles) > 0 {
		doc.ImpactedFiles = codebaseFiles
	}
	if len(doc.ArchitecturalRulesApplied) == 0 && len(rules) > 0 {
		doc.ArchitecturalRulesApplied = rules
	}

	return &doc, nil
}

// generateFallbackSpec builds a deterministic, structured SDD when LLM is unavailable or fails.
func (w *SpecWriter) generateFallbackSpec(
	task TaskContext,
	rules []string,
	codebaseFiles []string,
	pastLessons []string,
) *SpecDocument {
	// 1. Fallback Architectural Rules
	appliedRules := rules
	if len(appliedRules) == 0 {
		appliedRules = []string{
			"ARCH-001 (Monorepo Layering - internal packages private to gv-factory)",
			"ARCH-002 (TDD Red-Green-Refactor with >= 90% unit test coverage)",
			"ARCH-003 (REUSE Apache-2.0 License Header Compliance)",
			"ARCH-004 (Zero Secret Exposure & Log Sanitization)",
		}
	}

	// 2. Fallback Impacted Files
	impactedFiles := codebaseFiles
	if len(impactedFiles) == 0 {
		pkgName := sanitizePackageName(task.Title)
		impactedFiles = []string{
			fmt.Sprintf("internal/%s/%s.go", pkgName, pkgName),
			fmt.Sprintf("internal/%s/%s_test.go", pkgName, pkgName),
		}
	}

	// 3. Fallback Past Lessons
	lessons := pastLessons
	if len(lessons) == 0 {
		lessons = []string{
			"Enforce >= 90% unit test coverage with isolated mock boundaries.",
			"Validate context cancellation and edge conditions in all public methods.",
			"Apply SPDX Apache-2.0 headers to all newly created Go and configuration files.",
		}
	}

	// 4. Fallback Architecture Overview
	overview := fmt.Sprintf(
		"Architectural implementation plan for '%s'. This specification enforces modular deep package design, "+
			"strict input validation, and resilient error recovery. Implementation adheres to project invariants "+
			"with isolated dependencies and deterministic fallback handling.",
		task.Title,
	)
	if task.Description != "" {
		overview += fmt.Sprintf(" Task context: %s.", task.Description)
	}

	// 5. Fallback TDD Plan
	tddPlan := TDDPlan{
		Summary: "Comprehensive 3-phase TDD execution plan enforcing unit testing before production implementation.",
		TestCases: []TestCase{
			{
				Name:            fmt.Sprintf("Test%s_InputValidation", toCamelCase(task.Title)),
				Phase:           "Red",
				Description:     "Verify input parameters, reject nil contexts, and ensure proper boundary validation.",
				ExpectedOutcome: "Returns appropriate validation errors for malformed inputs.",
			},
			{
				Name:            fmt.Sprintf("Test%s_CoreExecution", toCamelCase(task.Title)),
				Phase:           "Green",
				Description:     "Implement minimal logic to satisfy business requirements and pass core unit tests.",
				ExpectedOutcome: "Execution completes successfully without errors.",
			},
			{
				Name:            fmt.Sprintf("Test%s_RaceAndEdgeCases", toCamelCase(task.Title)),
				Phase:           "Refactor",
				Description:     "Test concurrent invocation, context cancellation, and verify zero data races under -race.",
				ExpectedOutcome: "Zero data races, >= 90% coverage achieved.",
			},
		},
	}

	// 6. Fallback Atomic Tasks
	atomicTasks := []AtomicTask{
		{
			ID:          "TASK-1",
			Title:       "Define interfaces and failing unit tests (TDD Red)",
			Description: "Specify structs, public interfaces, and write initial tests asserting expected behavior and boundary conditions.",
			Dependencies: []string{},
			AcceptanceCriteria: []string{
				"Unit tests written in *_test.go",
				"Tests fail initially as expected",
				"SPDX Apache-2.0 headers present",
			},
		},
		{
			ID:          "TASK-2",
			Title:       "Implement core logic to pass test suite (TDD Green)",
			Description: "Implement the minimal necessary functionality to make all unit tests pass.",
			Dependencies: []string{"TASK-1"},
			AcceptanceCriteria: []string{
				"All unit tests pass",
				"Core interfaces fully satisfied",
				"Edge cases and error paths handled gracefully",
			},
		},
		{
			ID:          "TASK-3",
			Title:       "Refactor, verify race conditions and test coverage",
			Description: "Optimize implementation, run race detector, and ensure test coverage meets or exceeds 90%.",
			Dependencies: []string{"TASK-2"},
			AcceptanceCriteria: []string{
				"Unit test coverage >= 90%",
				"go test -race ./... passes with 0 data races",
			},
		},
		{
			ID:          "TASK-4",
			Title:       "Verify REUSE compliance and commit hygiene",
			Description: "Validate copyright headers using reuse lint and prepare Conventional Commits.",
			Dependencies: []string{"TASK-3"},
			AcceptanceCriteria: []string{
				"reuse lint passes with 0 violations",
				"Conventional Commit format followed",
			},
		},
	}

	return &SpecDocument{
		Title:                     task.Title,
		ArchitectureOverview:      overview,
		ImpactedFiles:             impactedFiles,
		ArchitecturalRulesApplied: appliedRules,
		PastLessonsConsidered:     lessons,
		TDDPlan:                   tddPlan,
		AtomicTasks:               atomicTasks,
	}
}

// formatList formats a string slice into bullet points.
func formatList(items []string) string {
	if len(items) == 0 {
		return "- None recorded"
	}
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString(fmt.Sprintf("- %s\n", it))
	}
	return strings.TrimSpace(sb.String())
}

// extractJSON extracts raw JSON from an LLM response that may be wrapped in Markdown code fences.
var codeFenceRegex = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if matches := codeFenceRegex.FindStringSubmatch(s); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	// If no code fence is found, return the trimmed text
	return s
}

// sanitizePackageName converts a task title into a safe lowercase package identifier.
func sanitizePackageName(title string) string {
	lower := strings.ToLower(title)
	reg := regexp.MustCompile("[^a-z0-9]+")
	pkg := reg.ReplaceAllString(lower, "_")
	pkg = strings.Trim(pkg, "_")
	if pkg == "" {
		return "module"
	}
	return pkg
}

// toCamelCase converts a space or punctuation-delimited string into CamelCase.
func toCamelCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})
	var sb strings.Builder
	for _, w := range words {
		if len(w) > 0 {
			sb.WriteString(strings.ToUpper(w[:1]) + strings.ToLower(w[1:]))
		}
	}
	res := sb.String()
	if res == "" {
		return "Feature"
	}
	return res
}
