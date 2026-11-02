// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/runtime/mcp"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

// --- Mock implementations for testing ---

type mockLLMClientForExecutor struct {
	mu           sync.Mutex
	completions  []*llm.ChatResponse
	compErr      error
	callCount    int
	lastRequests []*llm.ChatRequest
}

func (m *mockLLMClientForExecutor) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	m.lastRequests = append(m.lastRequests, req)
	if m.compErr != nil {
		return nil, m.compErr
	}
	if len(m.completions) == 0 {
		return &llm.ChatResponse{
			ID:    "resp-default",
			Model: req.Model,
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "Default mock response",
			},
		}, nil
	}
	idx := m.callCount - 1
	if idx >= len(m.completions) {
		idx = len(m.completions) - 1
	}
	return m.completions[idx], nil
}

func (m *mockLLMClientForExecutor) StreamChatCompletion(ctx context.Context, req *llm.ChatRequest) (<-chan llm.ChatChunk, <-chan error) {
	outCh := make(chan llm.ChatChunk, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		resp, err := m.ChatCompletion(ctx, req)
		if err != nil {
			errCh <- err
			return
		}

		if resp.Message.Content != "" {
			outCh <- llm.ChatChunk{
				ID:           resp.ID,
				Model:        resp.Model,
				DeltaContent: resp.Message.Content,
			}
		}

		for _, tc := range resp.Message.ToolCalls {
			tcCopy := tc
			outCh <- llm.ChatChunk{
				ID:        resp.ID,
				Model:     resp.Model,
				DeltaTool: &tcCopy,
			}
		}
	}()

	return outCh, errCh
}

type mockMCPCallerForExecutor struct {
	mu        sync.Mutex
	tools     []mcp.ToolDefinition
	toolCalls []string
	callErr   error
	customRes map[string]*mcp.ToolResult
}

func (m *mockMCPCallerForExecutor) ListTools(ctx context.Context) ([]mcp.ToolDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tools, nil
}

func (m *mockMCPCallerForExecutor) CallToolWithContext(ctx context.Context, ec mcp.ExecutionContext, name string, arguments map[string]any) (*mcp.ToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCalls = append(m.toolCalls, name)
	if m.callErr != nil {
		return nil, m.callErr
	}
	if res, ok := m.customRes[name]; ok {
		return res, nil
	}
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: fmt.Sprintf("Result for %s", name)},
		},
	}, nil
}

type mockSandboxManagerForExecutor struct {
	worktreePath string
	hasWorktree  bool
}

func (m *mockSandboxManagerForExecutor) ProvisionWorktreeWithOptions(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error) {
	return nil, nil
}

func (m *mockSandboxManagerForExecutor) GetWorktreePath(runID string) (string, bool) {
	return m.worktreePath, m.hasWorktree
}

func (m *mockSandboxManagerForExecutor) GetEnvironment(runID string) (*sandbox.Environment, bool) {
	return nil, false
}

func (m *mockSandboxManagerForExecutor) RecycleWorktree(ctx context.Context, runID string) error {
	return nil
}

func (m *mockSandboxManagerForExecutor) Destroy(ctx context.Context, runID string) error {
	return nil
}

type mockEventPublisherForExecutor struct {
	mu     sync.Mutex
	events []gateway.Event
}

func (m *mockEventPublisherForExecutor) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

// --- Unit Tests for Red Phase ---

func TestCognitiveAgentExecutor_NilLLMClient_Fallback(t *testing.T) {
	exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient: nil,
	})

	task := &TaskMessage{
		TaskID: "task-nil-llm",
		RunID:  "run-123",
		StepID: "step-1",
	}
	step := AgentStepPayload{
		StepName: "test_step",
		Role:     "spec_writer",
		Action:   "plan",
		Prompt:   "Draft architectural design",
	}

	res, err := exec.ExecuteStep(context.Background(), step, task)
	if err == nil {
		t.Fatalf("expected error when LLMClient is nil, got nil")
	}
	if res == nil {
		t.Fatalf("expected non-nil TaskResult with failure details")
	}
	if res.Success {
		t.Errorf("expected res.Success = false")
	}
	if res.Error == "" {
		t.Errorf("expected descriptive res.Error")
	}
}

func TestCognitiveAgentExecutor_SingleTurn_Success(t *testing.T) {
	mockLLM := &mockLLMClientForExecutor{
		completions: []*llm.ChatResponse{
			{
				ID:    "resp-1",
				Model: "gpt-4o",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "Here is the architectural specification for the system.",
				},
				Usage: llm.UsageMetrics{
					PromptTokens:     150,
					CompletionTokens: 50,
					TotalTokens:      200,
				},
			},
		},
	}

	publisher := &mockEventPublisherForExecutor{}
	sb := &mockSandboxManagerForExecutor{
		worktreePath: "/tmp/sandbox/run-123",
		hasWorktree:  true,
	}

	exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient:       mockLLM,
		Sandbox:         sb,
		Publisher:       publisher,
		WorkerID:        "worker-node-1",
		DefaultModel:    "gpt-4o",
		MaxIterations:   5,
		StreamReasoning: true,
	})

	task := &TaskMessage{
		TaskID: "task-single-turn",
		RunID:  "run-123",
		StepID: "step-spec",
	}
	step := AgentStepPayload{
		StepName: "spec_generation",
		Role:     "spec_writer",
		Action:   "plan",
		Prompt:   "Define SDD for auth module",
	}

	res, err := exec.ExecuteStep(context.Background(), step, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected res.Success = true, got false (error: %s)", res.Error)
	}
	if res.Output != "Here is the architectural specification for the system." {
		t.Errorf("unexpected output: %s", res.Output)
	}
	if res.Data == nil {
		t.Fatalf("expected non-nil res.Data")
	}
	if iters, ok := res.Data["iterations"].(int); !ok || iters != 1 {
		t.Errorf("expected 1 iteration, got %v", res.Data["iterations"])
	}
	if res.Data["worktree_path"] != "/tmp/sandbox/run-123" {
		t.Errorf("expected worktree path in data, got %v", res.Data["worktree_path"])
	}

	// Verify events were emitted
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.events) == 0 {
		t.Errorf("expected stream events to be published")
	}
}

func TestCognitiveAgentExecutor_MultiTurn_ToolCalls_Success(t *testing.T) {
	mockLLM := &mockLLMClientForExecutor{
		completions: []*llm.ChatResponse{
			// First turn: requests tool call
			{
				ID:    "resp-turn-1",
				Model: "claude-3-7-sonnet",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "I need to inspect the codebase structure first.",
					ToolCalls: []llm.ToolCall{
						{
							ID:   "call-1",
							Type: "function",
							Function: llm.FunctionCall{
								Name:      "read_file",
								Arguments: `{"path":"cmd/main.go"}`,
							},
						},
					},
				},
			},
			// Second turn: final answer after getting tool result
			{
				ID:    "resp-turn-2",
				Model: "claude-3-7-sonnet",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "Based on cmd/main.go, the system initializes services properly.",
				},
			},
		},
	}

	mockMCP := &mockMCPCallerForExecutor{
		tools: []mcp.ToolDefinition{
			{
				Name:        "read_file",
				Description: "Read contents of a file",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]any{
						"path": map[string]any{"type": "string"},
					},
					Required: []string{"path"},
				},
			},
		},
		customRes: map[string]*mcp.ToolResult{
			"read_file": {
				Content: []mcp.ContentBlock{
					{Type: "text", Text: "package main\nfunc main() {}"},
				},
			},
		},
	}

	publisher := &mockEventPublisherForExecutor{}

	exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient:       mockLLM,
		MCPCaller:       mockMCP,
		Publisher:       publisher,
		WorkerID:        "worker-node-1",
		DefaultModel:    "claude-3-7-sonnet",
		MaxIterations:   5,
		StreamReasoning: true,
	})

	task := &TaskMessage{
		TaskID: "task-multi-turn",
		RunID:  "run-multi",
		StepID: "step-tdd",
	}
	step := AgentStepPayload{
		StepName: "inspect_and_code",
		Role:     "tdd",
		Action:   "execute",
		Prompt:   "Inspect main and add health check",
	}

	res, err := exec.ExecuteStep(context.Background(), step, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected res.Success = true, error: %s", res.Error)
	}
	if res.Output != "Based on cmd/main.go, the system initializes services properly." {
		t.Errorf("unexpected output: %s", res.Output)
	}

	if iters, ok := res.Data["iterations"].(int); !ok || iters != 2 {
		t.Errorf("expected 2 iterations, got %v", res.Data["iterations"])
	}

	toolCallsData, ok := res.Data["tool_calls"].([]ToolCallTelemetry)
	if !ok || len(toolCallsData) != 1 {
		t.Fatalf("expected 1 tool call in telemetry, got %v", res.Data["tool_calls"])
	}
	if toolCallsData[0].Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got %s", toolCallsData[0].Name)
	}
	if toolCallsData[0].Status != ToolCallStatusCompleted {
		t.Errorf("expected status 'completed', got %s", toolCallsData[0].Status)
	}

	// Verify MCP call occurred
	mockMCP.mu.Lock()
	if len(mockMCP.toolCalls) != 1 || mockMCP.toolCalls[0] != "read_file" {
		t.Errorf("expected MCP call for read_file, got %v", mockMCP.toolCalls)
	}
	mockMCP.mu.Unlock()
}

func TestCognitiveAgentExecutor_MaxIterations_Exceeded(t *testing.T) {
	// An LLM that constantly requests tools forever
	mockLLM := &mockLLMClientForExecutor{
		completions: []*llm.ChatResponse{
			{
				ID:    "looping-turn",
				Model: "gpt-4o",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "Still searching...",
					ToolCalls: []llm.ToolCall{
						{
							ID:   "call-infinite",
							Type: "function",
							Function: llm.FunctionCall{
								Name:      "search_code",
								Arguments: `{"query":"error"}`,
							},
						},
					},
				},
			},
		},
	}

	mockMCP := &mockMCPCallerForExecutor{
		tools: []mcp.ToolDefinition{
			{Name: "search_code"},
		},
	}

	exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient:     mockLLM,
		MCPCaller:     mockMCP,
		MaxIterations: 3,
	})

	task := &TaskMessage{
		TaskID: "task-loop",
		RunID:  "run-loop",
		StepID: "step-loop",
	}
	step := AgentStepPayload{
		StepName: "infinite_search",
	}

	res, err := exec.ExecuteStep(context.Background(), step, task)
	if err == nil {
		t.Fatalf("expected error when max iterations exceeded")
	}
	if res == nil {
		t.Fatalf("expected non-nil TaskResult")
	}
	if res.Success {
		t.Errorf("expected res.Success = false")
	}
	if res.Data["iterations"] != 3 {
		t.Errorf("expected 3 iterations, got %v", res.Data["iterations"])
	}
}

func TestCognitiveAgentExecutor_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	mockLLM := &mockLLMClientForExecutor{}
	exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient: mockLLM,
	})

	task := &TaskMessage{TaskID: "task-cancel", RunID: "run-c", StepID: "step-c"}
	step := AgentStepPayload{StepName: "cancel_step"}

	res, err := exec.ExecuteStep(ctx, step, task)
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if res != nil && res.Success {
		t.Errorf("expected res.Success = false")
	}
}

func TestCognitiveAgentExecutor_ToolError_SelfHealing(t *testing.T) {
	// First turn calls tool that fails. Second turn adapts and concludes.
	mockLLM := &mockLLMClientForExecutor{
		completions: []*llm.ChatResponse{
			{
				ID: "t1",
				Message: llm.ChatMessage{
					Role: llm.RoleAssistant,
					ToolCalls: []llm.ToolCall{
						{
							ID:   "call-err",
							Type: "function",
							Function: llm.FunctionCall{
								Name:      "faulty_tool",
								Arguments: `{}`,
							},
						},
					},
				},
			},
			{
				ID: "t2",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "The tool failed due to file not found, continuing with fallback plan.",
				},
			},
		},
	}

	mockMCP := &mockMCPCallerForExecutor{
		tools:   []mcp.ToolDefinition{{Name: "faulty_tool"}},
		callErr: errors.New("file not found"),
	}

	exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient:     mockLLM,
		MCPCaller:     mockMCP,
		MaxIterations: 3,
	})

	task := &TaskMessage{TaskID: "task-err", RunID: "run-err", StepID: "step-err"}
	step := AgentStepPayload{StepName: "heal_step"}

	res, err := exec.ExecuteStep(context.Background(), step, task)
	if err != nil {
		t.Fatalf("expected successful recovery, got err: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected res.Success = true, error: %s", res.Error)
	}
	if res.Output != "The tool failed due to file not found, continuing with fallback plan." {
		t.Errorf("unexpected output: %s", res.Output)
	}

	// Verify tool call telemetry captured failure
	toolCalls := res.Data["tool_calls"].([]ToolCallTelemetry)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call telemetry, got %d", len(toolCalls))
	}
	if toolCalls[0].Status != ToolCallStatusFailed {
		t.Errorf("expected status 'failed', got %s", toolCalls[0].Status)
	}
	if toolCalls[0].ErrorMessage == "" {
		t.Errorf("expected non-empty ErrorMessage in telemetry")
	}
}

func TestCognitiveAgentExecutor_AgentStepHandler_Integration(t *testing.T) {
	mockLLM := &mockLLMClientForExecutor{
		completions: []*llm.ChatResponse{
			{
				ID: "handler-resp",
				Message: llm.ChatMessage{
					Role:    llm.RoleAssistant,
					Content: "Cognitive agent completed successfully via handler.",
				},
			},
		},
	}

	cognitiveExec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
		LLMClient: mockLLM,
	})

	handler := NewAgentStepHandler(AgentStepConfig{
		Executor: cognitiveExec,
	})

	task := &TaskMessage{
		TaskID:   "task-h1",
		RunID:    "run-h1",
		StepID:   "step-h1",
		TaskType: TaskTypeAgentStep,
		Payload:  []byte(`{"step_name":"agent_step","role":"spec_writer","prompt":"write spec"}`),
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected handler res.Success = true, error: %s", res.Error)
	}
	if res.Output != "Cognitive agent completed successfully via handler." {
		t.Errorf("unexpected output: %s", res.Output)
	}
}

func TestCognitiveAgentExecutor_CoverageBoost(t *testing.T) {
	t.Run("LLM inference error returns error", func(t *testing.T) {
		mockLLM := &mockLLMClientForExecutor{
			compErr: errors.New("provider rate limit 429"),
		}
		exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
			LLMClient: mockLLM,
		})
		task := &TaskMessage{TaskID: "task-err-inf", RunID: "run-e", StepID: "step-e"}
		step := AgentStepPayload{StepName: "err_step", Temperature: 0.7, MaxTokens: 100}

		res, err := exec.ExecuteStep(context.Background(), step, task)
		if err == nil {
			t.Fatalf("expected error from inference failure")
		}
		if res.Success {
			t.Errorf("expected res.Success = false")
		}
	})

	t.Run("LLM inference context canceled during completion", func(t *testing.T) {
		mockLLM := &mockLLMClientForExecutor{
			compErr: context.Canceled,
		}
		exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
			LLMClient: mockLLM,
		})
		task := &TaskMessage{TaskID: "task-ctx-err", RunID: "run-c", StepID: "step-c"}
		step := AgentStepPayload{StepName: "ctx_step"}

		res, err := exec.ExecuteStep(context.Background(), step, task)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled error, got %v", err)
		}
		if res.Success {
			t.Errorf("expected res.Success = false")
		}
	})

	t.Run("Tool call when MCP caller is nil", func(t *testing.T) {
		mockLLM := &mockLLMClientForExecutor{
			completions: []*llm.ChatResponse{
				{
					ID: "t1",
					Message: llm.ChatMessage{
						Role: llm.RoleAssistant,
						ToolCalls: []llm.ToolCall{
							{
								ID:   "call-no-mcp",
								Type: "function",
								Function: llm.FunctionCall{
									Name: "test_tool",
								},
							},
						},
					},
				},
				{
					ID: "t2",
					Message: llm.ChatMessage{
						Role:    llm.RoleAssistant,
						Content: "Done after MCP nil fallback.",
					},
				},
			},
		}

		exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
			LLMClient: mockLLM,
			MCPCaller: nil,
		})
		task := &TaskMessage{TaskID: "task-no-mcp", RunID: "run-nm", StepID: "step-nm"}
		step := AgentStepPayload{StepName: "no_mcp"}

		res, err := exec.ExecuteStep(context.Background(), step, task)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Success {
			t.Errorf("expected res.Success = true")
		}
		toolCalls := res.Data["tool_calls"].([]ToolCallTelemetry)
		if len(toolCalls) != 1 || toolCalls[0].Status != ToolCallStatusFailed {
			t.Errorf("expected tool call to fail due to nil MCP caller")
		}
	})

	t.Run("Tool call returning empty result and MCP tool with isError true", func(t *testing.T) {
		mockLLM := &mockLLMClientForExecutor{
			completions: []*llm.ChatResponse{
				{
					ID: "t1",
					Message: llm.ChatMessage{
						Role: llm.RoleAssistant,
						ToolCalls: []llm.ToolCall{
							{
								ID:   "call-empty",
								Type: "function",
								Function: llm.FunctionCall{
									Name:      "empty_tool",
									Arguments: `invalid-json`,
								},
							},
							{
								ID:   "call-mcp-err",
								Type: "function",
								Function: llm.FunctionCall{
									Name: "mcp_flagged_err",
								},
							},
						},
					},
				},
				{
					ID: "t2",
					Message: llm.ChatMessage{
						Role:    llm.RoleAssistant,
						Content: "Recovered from tools.",
					},
				},
			},
		}

		mockMCP := &mockMCPCallerForExecutor{
			tools: []mcp.ToolDefinition{
				{Name: "empty_tool"},
				{Name: "mcp_flagged_err"},
			},
			customRes: map[string]*mcp.ToolResult{
				"empty_tool": nil,
				"mcp_flagged_err": {
					IsError: true,
					Content: []mcp.ContentBlock{
						{Type: "text", Text: "explicit mcp error message"},
					},
				},
			},
		}

		publisher := &mockEventPublisherForExecutor{}
		exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
			LLMClient: mockLLM,
			MCPCaller: mockMCP,
			Publisher: publisher,
		})
		task := &TaskMessage{TaskID: "task-edge", RunID: "run-e", StepID: "step-e"}
		step := AgentStepPayload{StepName: "edge_step"}

		res, err := exec.ExecuteStep(context.Background(), step, task)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Success {
			t.Errorf("expected res.Success = true")
		}
	})

	t.Run("Context canceled inside loop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		mockLLM := &mockLLMClientForExecutor{
			completions: []*llm.ChatResponse{
				{
					ID: "t1",
					Message: llm.ChatMessage{
						Role: llm.RoleAssistant,
						ToolCalls: []llm.ToolCall{
							{
								ID:   "call-cancel-loop",
								Type: "function",
								Function: llm.FunctionCall{
									Name: "any_tool",
								},
							},
						},
					},
				},
			},
		}
		mockMCP := &mockMCPCallerForExecutor{
			tools: []mcp.ToolDefinition{{Name: "any_tool"}},
		}

		exec := NewCognitiveAgentExecutor(CognitiveExecutorConfig{
			LLMClient: mockLLM,
			MCPCaller: mockMCP,
		})
		task := &TaskMessage{TaskID: "task-cl", RunID: "run-cl", StepID: "step-cl"}
		step := AgentStepPayload{StepName: "cancel_in_loop"}

		// Trigger cancel on first tool call
		mockMCP.customRes = map[string]*mcp.ToolResult{
			"any_tool": {
				Content: []mcp.ContentBlock{{Type: "text", Text: "tool ok"}},
			},
		}

		// Run in background and cancel right away
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()

		_, _ = exec.ExecuteStep(ctx, step, task)
	})
}
