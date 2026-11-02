// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

// mockBrain implements BrainKnowledgeSource for unit tests.
type mockBrain struct {
	rules           []brain.ArchitecturalRule
	ruleResults     []brain.ArchitecturalRuleResult
	codebaseResults []brain.CodebaseKnowledgeResult
	codebase        []brain.CodebaseKnowledge
	episodes        []brain.TaskEpisode

	errRules    error
	errCodebase error
	errEpisodes error

	mu          sync.Mutex
	callsRules  int
	callsCode   int
	callsEps    int
}

func (m *mockBrain) ListRules(ctx context.Context, limit int) ([]brain.ArchitecturalRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callsRules++
	if m.errRules != nil {
		return nil, m.errRules
	}
	return m.rules, nil
}

func (m *mockBrain) HybridSearchRules(ctx context.Context, q brain.HybridSearchQuery) ([]brain.ArchitecturalRuleResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callsRules++
	if m.errRules != nil {
		return nil, m.errRules
	}
	return m.ruleResults, nil
}

func (m *mockBrain) HybridSearchCodebase(ctx context.Context, q brain.HybridSearchQuery) ([]brain.CodebaseKnowledgeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callsCode++
	if m.errCodebase != nil {
		return nil, m.errCodebase
	}
	return m.codebaseResults, nil
}

func (m *mockBrain) QueryCodebaseKnowledge(ctx context.Context, embedding []float32, limit int) ([]brain.CodebaseKnowledge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callsCode++
	if m.errCodebase != nil {
		return nil, m.errCodebase
	}
	return m.codebase, nil
}

func (m *mockBrain) QuerySimilarEpisodes(ctx context.Context, embedding []float32, limit int) ([]brain.TaskEpisode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callsEps++
	if m.errEpisodes != nil {
		return nil, m.errEpisodes
	}
	return m.episodes, nil
}

// mockLLMClient implements llm.LLMClient for unit tests.
type mockLLMClient struct {
	response *llm.ChatResponse
	err      error
	mu       sync.Mutex
	calls    int
	lastReq  *llm.ChatRequest
}

func (m *mockLLMClient) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockLLMClient) StreamChatCompletion(ctx context.Context, req *llm.ChatRequest) (<-chan llm.ChatChunk, <-chan error) {
	out := make(chan llm.ChatChunk)
	errCh := make(chan error, 1)
	close(out)
	close(errCh)
	return out, errCh
}

func TestTaskContext_Validation(t *testing.T) {
	tests := []struct {
		name    string
		tc      *TaskContext
		wantErr bool
		errIs   error
	}{
		{
			name:    "nil context",
			tc:      nil,
			wantErr: true,
			errIs:   ErrNilTaskContext,
		},
		{
			name: "empty task id",
			tc: &TaskContext{
				TaskID: "",
				Title:  "Implement agent",
			},
			wantErr: true,
			errIs:   ErrEmptyTaskID,
		},
		{
			name: "whitespace task id",
			tc: &TaskContext{
				TaskID: "   ",
				Title:  "Implement agent",
			},
			wantErr: true,
			errIs:   ErrEmptyTaskID,
		},
		{
			name: "empty title",
			tc: &TaskContext{
				TaskID: "TASK-123",
				Title:  "",
			},
			wantErr: true,
			errIs:   ErrEmptyTaskTitle,
		},
		{
			name: "whitespace title",
			tc: &TaskContext{
				TaskID: "TASK-123",
				Title:  "  \t \n",
			},
			wantErr: true,
			errIs:   ErrEmptyTaskTitle,
		},
		{
			name: "valid context",
			tc: &TaskContext{
				TaskID:      "TASK-101",
				Title:       "Add auth middleware",
				Description: "Protect internal API routes",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tc.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errIs != nil && !errors.Is(err, tt.errIs) {
				t.Errorf("Validate() error = %v, expected %v", err, tt.errIs)
			}
		})
	}
}

func TestTaskContext_Normalize(t *testing.T) {
	tc := &TaskContext{
		TaskID:         "  TASK-202  ",
		Title:          "  Implement Webhooks  ",
		Description:    "  Handle Svix events  ",
		RepositoryRoot: "  /workspaces/app  ",
		Labels:         nil,
		Constraints:    nil,
		Metadata:       nil,
	}

	tc.Normalize()

	if tc.TaskID != "TASK-202" {
		t.Errorf("expected normalized TaskID 'TASK-202', got %q", tc.TaskID)
	}
	if tc.Title != "Implement Webhooks" {
		t.Errorf("expected normalized Title, got %q", tc.Title)
	}
	if tc.Description != "Handle Svix events" {
		t.Errorf("expected normalized Description, got %q", tc.Description)
	}
	if tc.RepositoryRoot != "/workspaces/app" {
		t.Errorf("expected normalized RepositoryRoot, got %q", tc.RepositoryRoot)
	}
	if tc.Labels == nil {
		t.Errorf("expected non-nil Labels")
	}
	if tc.Constraints == nil {
		t.Errorf("expected non-nil Constraints")
	}
	if tc.Metadata == nil {
		t.Errorf("expected non-nil Metadata")
	}
}

func TestSpecDocument_Serialization(t *testing.T) {
	doc := &SpecDocument{
		Title:                "Implement SpecWriter Agent",
		ArchitectureOverview: "Deep module inside internal/agent providing autonomous spec synthesis.",
		ImpactedFiles: []string{
			"internal/agent/spec_writer.go",
			"internal/agent/spec_writer_test.go",
		},
		ArchitecturalRulesApplied: []string{"ARCH-001", "ARCH-002"},
		PastLessonsConsidered: []string{
			"Enforce >= 90% unit test coverage and clean mock isolation.",
		},
		TDDPlan: TDDPlan{
			TestCases: []TestCase{
				{
					Name:            "TestSpecWriter_ValidateContext",
					Phase:           "Red",
					Description:     "Verify rejection of empty task ID and title",
					ExpectedOutcome: "Returns ErrEmptyTaskID or ErrEmptyTaskTitle",
				},
				{
					Name:            "TestSpecWriter_GreenImplementation",
					Phase:           "Green",
					Description:     "Pass all unit tests with full brain mock",
					ExpectedOutcome: "Deterministic spec produced without errors",
				},
				{
					Name:            "TestSpecWriter_RefactorCoverage",
					Phase:           "Refactor",
					Description:     "Ensure race safety and zero alloc bottlenecks",
					ExpectedOutcome: "Zero data races under -race",
				},
			},
		},
		AtomicTasks: []AtomicTask{
			{
				ID:           "TASK-1",
				Title:        "Define agent interfaces and contracts",
				Description:  "Create TaskContext, SpecDocument and BrainKnowledgeSource",
				Dependencies: []string{},
				AcceptanceCriteria: []string{
					"Validation rules implemented",
					"SPDX headers present",
				},
			},
			{
				ID:           "TASK-2",
				Title:        "Implement TDD test suite",
				Description:  "Write comprehensive unit tests in spec_writer_test.go",
				Dependencies: []string{"TASK-1"},
				AcceptanceCriteria: []string{
					"Red tests created and failing initially",
				},
			},
		},
	}

	// Test ToJSON
	jsonBytes, err := doc.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() returned error: %v", err)
	}
	if len(jsonBytes) == 0 {
		t.Fatal("ToJSON() returned empty bytes")
	}

	var parsed SpecDocument
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if parsed.Title != doc.Title {
		t.Errorf("parsed Title %q != expected %q", parsed.Title, doc.Title)
	}
	if len(parsed.ImpactedFiles) != 2 {
		t.Errorf("parsed ImpactedFiles length %d != 2", len(parsed.ImpactedFiles))
	}
	if len(parsed.TDDPlan.TestCases) != 3 {
		t.Errorf("parsed TestCases length %d != 3", len(parsed.TDDPlan.TestCases))
	}

	// Test ToMarkdown
	md := doc.ToMarkdown()
	if !strings.Contains(md, "# Software Design Document: Implement SpecWriter Agent") &&
		!strings.Contains(md, "# Implement SpecWriter Agent") {
		t.Errorf("markdown missing title: %s", md)
	}
	if !strings.Contains(md, "## Architecture Overview") {
		t.Errorf("markdown missing architecture overview section")
	}
	if !strings.Contains(md, "internal/agent/spec_writer.go") {
		t.Errorf("markdown missing impacted file")
	}
	if !strings.Contains(md, "ARCH-001") {
		t.Errorf("markdown missing architectural rule")
	}
	if !strings.Contains(md, "Enforce >= 90% unit test coverage") {
		t.Errorf("markdown missing past lessons")
	}
	if !strings.Contains(md, "TestSpecWriter_ValidateContext") {
		t.Errorf("markdown missing test case name")
	}
	if !strings.Contains(md, "TASK-1") {
		t.Errorf("markdown missing atomic task ID")
	}
}

func TestSpecWriter_NilBrainNilLLM_Fallback(t *testing.T) {
	writer := NewSpecWriter(SpecWriterConfig{})

	ctx := context.Background()
	task := TaskContext{
		TaskID:      "TASK-001",
		Title:       "Build Agent SpecWriter",
		Description: "Autonomous specification designer for gv-factory",
		Constraints: []string{"Must adhere to REUSE and 90% test coverage"},
	}

	doc, err := writer.GenerateSpec(ctx, task)
	if err != nil {
		t.Fatalf("GenerateSpec failed: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil SpecDocument")
	}

	if doc.Title != task.Title {
		t.Errorf("expected Title %q, got %q", task.Title, doc.Title)
	}
	if len(doc.ImpactedFiles) == 0 {
		t.Errorf("expected at least one impacted file in fallback")
	}
	if len(doc.ArchitecturalRulesApplied) == 0 {
		t.Errorf("expected default architectural rules in fallback")
	}
	if len(doc.TDDPlan.TestCases) == 0 {
		t.Errorf("expected TDD test cases in fallback")
	}
	if len(doc.AtomicTasks) == 0 {
		t.Errorf("expected atomic tasks in fallback")
	}

	// Verify also WriteSpec alias
	doc2, err := writer.WriteSpec(ctx, task)
	if err != nil {
		t.Fatalf("WriteSpec failed: %v", err)
	}
	if doc2 == nil || doc2.Title != task.Title {
		t.Errorf("WriteSpec produced unexpected result: %+v", doc2)
	}
}

func TestSpecWriter_WithBrainMock_FallbackWhenNoLLM(t *testing.T) {
	mb := &mockBrain{
		ruleResults: []brain.ArchitecturalRuleResult{
			{
				ArchitecturalRule: brain.ArchitecturalRule{
					RuleCode:    "ARCH-001",
					Title:       "Monorepo Layering",
					Description: "internal/ must not be imported outside gv-factory",
				},
				Score: 0.95,
			},
			{
				ArchitecturalRule: brain.ArchitecturalRule{
					RuleCode:    "ARCH-002",
					Title:       "TDD Red-Green-Refactor",
					Description: "Unit tests must be written first",
				},
				Score: 0.90,
			},
		},
		codebaseResults: []brain.CodebaseKnowledgeResult{
			{
				CodebaseKnowledge: brain.CodebaseKnowledge{
					FilePath:   "internal/brain/rules.go",
					SymbolName: "ListRules",
					SymbolType: "function",
				},
				Score: 0.88,
			},
			{
				CodebaseKnowledge: brain.CodebaseKnowledge{
					FilePath:   "internal/connectors/llm/types.go",
					SymbolName: "LLMClient",
					SymbolType: "interface",
				},
				Score: 0.82,
			},
		},
		episodes: []brain.TaskEpisode{
			{
				TaskName:       "Implement Brain Hybrid Search",
				Outcome:        "success",
				Summary:        "Implemented hybrid search with pgvector",
				LessonsLearned: "Use reciprocal rank fusion and normalize weights",
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		Brain: mb,
	})

	ctx := context.Background()
	task := TaskContext{
		TaskID:      "TASK-002",
		Title:       "Add SpecWriter Agent",
		Description: "Query brain knowledge and synthesize specs",
		Labels:      []string{"agent", "spec"},
	}

	doc, err := writer.GenerateSpec(ctx, task)
	if err != nil {
		t.Fatalf("GenerateSpec failed: %v", err)
	}

	// Verify brain rules were extracted
	foundRule1 := false
	foundRule2 := false
	for _, r := range doc.ArchitecturalRulesApplied {
		if strings.Contains(r, "ARCH-001") {
			foundRule1 = true
		}
		if strings.Contains(r, "ARCH-002") {
			foundRule2 = true
		}
	}
	if !foundRule1 || !foundRule2 {
		t.Errorf("expected ARCH-001 and ARCH-002 in applied rules: %v", doc.ArchitecturalRulesApplied)
	}

	// Verify impacted files gathered from codebase knowledge
	foundBrainRules := false
	foundLLMTypes := false
	for _, f := range doc.ImpactedFiles {
		if strings.Contains(f, "internal/brain/rules.go") {
			foundBrainRules = true
		}
		if strings.Contains(f, "internal/connectors/llm/types.go") {
			foundLLMTypes = true
		}
	}
	if !foundBrainRules || !foundLLMTypes {
		t.Errorf("expected codebase files in impacted files: %v", doc.ImpactedFiles)
	}

	// Verify past lessons considered
	if len(doc.PastLessonsConsidered) == 0 {
		t.Errorf("expected past lessons to be populated from episodes")
	} else if !strings.Contains(doc.PastLessonsConsidered[0], "reciprocal rank fusion") {
		t.Errorf("expected lesson learned from mock brain: %v", doc.PastLessonsConsidered)
	}
}

func TestSpecWriter_BrainErrorResilience(t *testing.T) {
	mb := &mockBrain{
		errRules:    errors.New("postgres connection refused"),
		errCodebase: errors.New("timeout querying codebase knowledge"),
		errEpisodes: errors.New("episodes table lock timeout"),
	}

	writer := NewSpecWriter(SpecWriterConfig{
		Brain: mb,
	})

	ctx := context.Background()
	task := TaskContext{
		TaskID:      "TASK-ERR-1",
		Title:       "Test Resilience",
		Description: "Brain errors should not crash SpecWriter",
	}

	doc, err := writer.GenerateSpec(ctx, task)
	if err != nil {
		t.Fatalf("expected graceful fallback despite brain errors, got error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil SpecDocument on brain error")
	}
	if doc.Title != task.Title {
		t.Errorf("expected title %q, got %q", task.Title, doc.Title)
	}
	if len(doc.ArchitecturalRulesApplied) == 0 {
		t.Errorf("expected fallback rules when brain fails")
	}
}

func TestSpecWriter_WithLLMSuccess(t *testing.T) {
	expectedDoc := SpecDocument{
		Title:                "Enhanced SDD via LLM",
		ArchitectureOverview: "Advanced synthesis using LLM prompt templates.",
		ImpactedFiles:        []string{"internal/agent/spec_writer.go"},
		ArchitecturalRulesApplied: []string{
			"ARCH-001 (Monorepo Layering)",
			"ARCH-002 (TDD Red-Green-Refactor)",
		},
		PastLessonsConsidered: []string{"Always verify REUSE compliance"},
		TDDPlan: TDDPlan{
			TestCases: []TestCase{
				{
					Name:            "TestLLMGenerated",
					Phase:           "Red",
					Description:     "Test generated by LLM",
					ExpectedOutcome: "Passes successfully",
				},
			},
		},
		AtomicTasks: []AtomicTask{
			{
				ID:                 "TASK-LLM-1",
				Title:              "LLM Atomic Task 1",
				Description:        "Execute step 1",
				Dependencies:       []string{},
				AcceptanceCriteria: []string{"Criteria 1"},
			},
		},
	}

	payload, err := json.Marshal(expectedDoc)
	if err != nil {
		t.Fatalf("failed to marshal expected doc: %v", err)
	}

	mLLM := &mockLLMClient{
		response: &llm.ChatResponse{
			ID: "resp-1",
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: string(payload),
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		LLM:   mLLM,
		Model: "claude-3-5-sonnet",
	})

	ctx := context.Background()
	task := TaskContext{
		TaskID:      "TASK-LLM-SUCCESS",
		Title:       "Enhanced SDD via LLM",
		Description: "Prompt LLM for structured document",
	}

	doc, err := writer.GenerateSpec(ctx, task)
	if err != nil {
		t.Fatalf("GenerateSpec failed: %v", err)
	}
	if doc.Title != expectedDoc.Title {
		t.Errorf("expected Title %q, got %q", expectedDoc.Title, doc.Title)
	}
	if doc.ArchitectureOverview != expectedDoc.ArchitectureOverview {
		t.Errorf("expected overview %q, got %q", expectedDoc.ArchitectureOverview, doc.ArchitectureOverview)
	}
	if len(doc.AtomicTasks) != 1 || doc.AtomicTasks[0].ID != "TASK-LLM-1" {
		t.Errorf("unexpected atomic tasks: %+v", doc.AtomicTasks)
	}
	if mLLM.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", mLLM.calls)
	}
}

func TestSpecWriter_WithLLMMarkdownCodeFence(t *testing.T) {
	expectedDoc := SpecDocument{
		Title:                "Markdown Fenced Spec",
		ArchitectureOverview: "Overview inside markdown code block",
		ImpactedFiles:        []string{"internal/agent/spec_writer.go"},
		ArchitecturalRulesApplied: []string{"ARCH-001"},
		PastLessonsConsidered: []string{"Lesson 1"},
		TDDPlan: TDDPlan{
			TestCases: []TestCase{
				{
					Name:            "TestFence",
					Phase:           "Red",
					Description:     "Test code block parsing",
					ExpectedOutcome: "Passes",
				},
			},
		},
		AtomicTasks: []AtomicTask{
			{
				ID:                 "TASK-FENCE-1",
				Title:              "Fence task",
				Description:        "Task in fence",
				Dependencies:       []string{},
				AcceptanceCriteria: []string{"Works"},
			},
		},
	}

	payload, err := json.Marshal(expectedDoc)
	if err != nil {
		t.Fatalf("failed to marshal expected doc: %v", err)
	}

	fencedContent := "Here is the Software Design Document:\n```json\n" + string(payload) + "\n```\nHope this helps!"

	mLLM := &mockLLMClient{
		response: &llm.ChatResponse{
			ID: "resp-fence",
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: fencedContent,
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		LLM: mLLM,
	})

	task := TaskContext{
		TaskID: "TASK-FENCE",
		Title:  "Markdown Fenced Spec",
	}

	doc, err := writer.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("GenerateSpec failed with fenced content: %v", err)
	}
	if doc.Title != expectedDoc.Title {
		t.Errorf("expected title %q, got %q", expectedDoc.Title, doc.Title)
	}
}

func TestSpecWriter_WithLLMError_Fallback(t *testing.T) {
	mLLM := &mockLLMClient{
		err: errors.New("upstream provider timeout (504 Gateway Timeout)"),
	}

	writer := NewSpecWriter(SpecWriterConfig{
		LLM: mLLM,
	})

	task := TaskContext{
		TaskID:      "TASK-FALLBACK-1",
		Title:       "Fallback on LLM Timeout",
		Description: "Should produce deterministic spec without error",
	}

	doc, err := writer.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("expected fallback without error, got: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil SpecDocument")
	}
	if doc.Title != task.Title {
		t.Errorf("expected Title %q, got %q", task.Title, doc.Title)
	}
	if len(doc.TDDPlan.TestCases) == 0 {
		t.Errorf("expected fallback TDD test cases")
	}
}

func TestSpecWriter_WithLLMInvalidJSON_Fallback(t *testing.T) {
	mLLM := &mockLLMClient{
		response: &llm.ChatResponse{
			ID: "resp-bad-json",
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "Sorry, I am not able to generate JSON output right now.",
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		LLM: mLLM,
	})

	task := TaskContext{
		TaskID:      "TASK-BAD-JSON",
		Title:       "Fallback on Bad JSON",
		Description: "Should gracefully fallback to deterministic spec",
	}

	doc, err := writer.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("expected fallback without error, got: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil SpecDocument")
	}
	if doc.Title != task.Title {
		t.Errorf("expected Title %q, got %q", task.Title, doc.Title)
	}
}

func TestSpecWriter_InvalidInputContext(t *testing.T) {
	writer := NewSpecWriter(SpecWriterConfig{})

	_, err := writer.GenerateSpec(context.Background(), TaskContext{
		TaskID: "",
		Title:  "Some title",
	})
	if err == nil {
		t.Errorf("expected error on empty TaskID, got nil")
	}

	_, err = writer.GenerateSpec(context.Background(), TaskContext{
		TaskID: "TASK-1",
		Title:  "",
	})
	if err == nil {
		t.Errorf("expected error on empty Title, got nil")
	}
}

func TestSpecWriter_Concurrent_RaceSafety(t *testing.T) {
	mb := &mockBrain{
		ruleResults: []brain.ArchitecturalRuleResult{
			{
				ArchitecturalRule: brain.ArchitecturalRule{
					RuleCode: "ARCH-CONCURRENT",
					Title:    "Concurrency Safety",
				},
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		Brain: mb,
	})

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			task := TaskContext{
				TaskID:      "TASK-CONC",
				Title:       "Concurrent Spec Generation",
				Description: "Testing race conditions across goroutines",
			}
			doc, err := writer.GenerateSpec(context.Background(), task)
			if err != nil {
				t.Errorf("goroutine %d failed: %v", idx, err)
			}
			if doc == nil || doc.Title != task.Title {
				t.Errorf("goroutine %d invalid doc: %+v", idx, doc)
			}
		}(i)
	}

	wg.Wait()
}

func TestBrainKnowledgeSource_InterfaceCompatibility(t *testing.T) {
	// Verify that a real *brain.Service implements BrainKnowledgeSource.
	// This is a compile-time check using type assertion.
	var _ BrainKnowledgeSource = (*brain.Service)(nil)
	var _ BrainKnowledgeSource = (*mockBrain)(nil)
}

func TestSpecWriter_ContextCanceled(t *testing.T) {
	writer := NewSpecWriter(SpecWriterConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	task := TaskContext{
		TaskID: "TASK-CANCEL",
		Title:  "Test Canceled Context",
	}

	_, err := writer.GenerateSpec(ctx, task)
	if err == nil {
		t.Errorf("expected error for canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestBrainListRulesFallback(t *testing.T) {
	// Mock brain where HybridSearchRules returns empty, but ListRules returns rules
	mb := &mockBrain{
		ruleResults: []brain.ArchitecturalRuleResult{}, // empty hybrid search
		rules: []brain.ArchitecturalRule{
			{
				RuleCode:    "ARCH-LIST-01",
				Title:       "Fallback List Rule",
				Description: "Returned via ListRules",
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		Brain: mb,
	})

	task := TaskContext{
		TaskID: "TASK-LIST-FALLBACK",
		Title:  "List Rules Fallback",
	}

	doc, err := writer.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("GenerateSpec failed: %v", err)
	}

	found := false
	for _, r := range doc.ArchitecturalRulesApplied {
		if strings.Contains(r, "ARCH-LIST-01") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ARCH-LIST-01 from ListRules fallback: %v", doc.ArchitecturalRulesApplied)
	}
}

func TestTaskContext_NormalizeNil(t *testing.T) {
	var tc *TaskContext
	// Should not panic on nil receiver
	tc.Normalize()
}

func TestSpecDocument_ToMarkdown_EmptySections(t *testing.T) {
	doc := &SpecDocument{
		Title:                     "Empty Sections Document",
		ArchitectureOverview:      "Testing empty slices formatting.",
		ImpactedFiles:             nil,
		ArchitecturalRulesApplied: nil,
		PastLessonsConsidered:     nil,
		TDDPlan: TDDPlan{
			Summary:   "TDD Plan Summary here",
			TestCases: nil,
		},
		AtomicTasks: []AtomicTask{
			{
				ID:                 "TASK-EMPTY-1",
				Title:              "Task with no deps or criteria",
				Description:        "No deps or criteria specified",
				Dependencies:       nil,
				AcceptanceCriteria: nil,
			},
		},
	}

	md := doc.ToMarkdown()
	if !strings.Contains(md, "TDD Plan Summary here") {
		t.Errorf("expected TDD plan summary in markdown")
	}
	if !strings.Contains(md, "- None specified") {
		t.Errorf("expected '- None specified' in markdown")
	}
	if !strings.Contains(md, "- **Dependencies:** None") {
		t.Errorf("expected '- **Dependencies:** None' in markdown")
	}
}

func TestSpecWriter_GatherBrainKnowledge_CodebaseFallback_And_EpisodeSummary(t *testing.T) {
	mb := &mockBrain{
		ruleResults:     nil,
		codebaseResults: nil, // hybrid search returns empty
		codebase: []brain.CodebaseKnowledge{
			{
				FilePath:   "internal/agent/fallback.go",
				SymbolName: "FallbackSymbol",
			},
		},
		episodes: []brain.TaskEpisode{
			{
				Summary: "Fallback summary when LessonsLearned is empty",
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		Brain: mb,
	})

	task := TaskContext{
		TaskID: "TASK-CODE-FALLBACK",
		Title:  "Codebase Query Fallback",
	}

	doc, err := writer.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("GenerateSpec failed: %v", err)
	}

	foundCode := false
	for _, f := range doc.ImpactedFiles {
		if strings.Contains(f, "fallback.go") {
			foundCode = true
			break
		}
	}
	if !foundCode {
		t.Errorf("expected fallback.go in impacted files: %v", doc.ImpactedFiles)
	}

	foundEpisode := false
	for _, l := range doc.PastLessonsConsidered {
		if strings.Contains(l, "Fallback summary") {
			foundEpisode = true
			break
		}
	}
	if !foundEpisode {
		t.Errorf("expected fallback episode summary: %v", doc.PastLessonsConsidered)
	}
}

func TestSpecWriter_LLM_EmptyOrNilResponse(t *testing.T) {
	// 1. Nil response from LLM
	mLLMNil := &mockLLMClient{
		response: nil,
	}
	writerNil := NewSpecWriter(SpecWriterConfig{
		LLM: mLLMNil,
	})
	task := TaskContext{
		TaskID: "TASK-LLM-NIL",
		Title:  "LLM Nil Response",
	}
	doc, err := writerNil.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("expected fallback on nil response, got error: %v", err)
	}
	if doc == nil || doc.Title != task.Title {
		t.Errorf("expected fallback spec on nil LLM response")
	}

	// 2. Empty string content from LLM
	mLLMEmpty := &mockLLMClient{
		response: &llm.ChatResponse{
			ID: "resp-empty",
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "   \n\t  ",
			},
		},
	}
	writerEmpty := NewSpecWriter(SpecWriterConfig{
		LLM: mLLMEmpty,
	})
	doc2, err := writerEmpty.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("expected fallback on empty content, got error: %v", err)
	}
	if doc2 == nil || doc2.Title != task.Title {
		t.Errorf("expected fallback spec on empty LLM content")
	}
}

func TestSpecWriter_LLM_AutoFillFields(t *testing.T) {
	mb := &mockBrain{
		ruleResults: []brain.ArchitecturalRuleResult{
			{
				ArchitecturalRule: brain.ArchitecturalRule{
					RuleCode: "ARCH-AUTOFLL",
					Title:    "Autofill Rule",
				},
			},
		},
		codebaseResults: []brain.CodebaseKnowledgeResult{
			{
				CodebaseKnowledge: brain.CodebaseKnowledge{
					FilePath: "internal/autofill/file.go",
				},
			},
		},
	}

	// LLM returns JSON missing Title, ImpactedFiles, and ArchitecturalRulesApplied
	partialDoc := SpecDocument{
		Title:                "", // empty
		ArchitectureOverview: "Overview without title",
		ImpactedFiles:        nil,
		ArchitecturalRulesApplied: nil,
		TDDPlan: TDDPlan{
			TestCases: []TestCase{
				{
					Name:  "TestSomething",
					Phase: "Red",
				},
			},
		},
		AtomicTasks: []AtomicTask{
			{
				ID:    "T-1",
				Title: "Task 1",
			},
		},
	}
	payload, _ := json.Marshal(partialDoc)

	mLLM := &mockLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: string(payload),
			},
		},
	}

	writer := NewSpecWriter(SpecWriterConfig{
		Brain: mb,
		LLM:   mLLM,
	})

	task := TaskContext{
		TaskID: "TASK-AUTOFLL",
		Title:  "Autofill Target Title",
	}

	doc, err := writer.GenerateSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("GenerateSpec failed: %v", err)
	}
	if doc.Title != "Autofill Target Title" {
		t.Errorf("expected auto-filled title, got: %q", doc.Title)
	}
	if len(doc.ImpactedFiles) == 0 || !strings.Contains(doc.ImpactedFiles[0], "file.go") {
		t.Errorf("expected auto-filled impacted files from codebase: %v", doc.ImpactedFiles)
	}
	if len(doc.ArchitecturalRulesApplied) == 0 || !strings.Contains(doc.ArchitecturalRulesApplied[0], "ARCH-AUTOFLL") {
		t.Errorf("expected auto-filled rules from brain: %v", doc.ArchitecturalRulesApplied)
	}
}

func TestHelpers_FormattingAndSanitization(t *testing.T) {
	// formatList with empty list
	res := formatList(nil)
	if res != "- None recorded" {
		t.Errorf("expected '- None recorded', got %q", res)
	}

	// sanitizePackageName edge cases
	pkgEmpty := sanitizePackageName("---!@#$---")
	if pkgEmpty != "module" {
		t.Errorf("expected 'module', got %q", pkgEmpty)
	}

	pkgNormal := sanitizePackageName("Build Spec Writer Agent")
	if pkgNormal != "build_spec_writer_agent" {
		t.Errorf("expected 'build_spec_writer_agent', got %q", pkgNormal)
	}

	// toCamelCase edge cases
	camelEmpty := toCamelCase("---!@#$---")
	if camelEmpty != "Feature" {
		t.Errorf("expected 'Feature', got %q", camelEmpty)
	}

	camelNormal := toCamelCase("implement auth tokens")
	if camelNormal != "ImplementAuthTokens" {
		t.Errorf("expected 'ImplementAuthTokens', got %q", camelNormal)
	}
}

