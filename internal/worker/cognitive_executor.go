// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
	"github.com/gv-factory/gv-factory/internal/runtime/mcp"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

// MCPCaller abstracts tool discovery and invocation across MCP servers.
// Satisfied natively by *mcp.MCPToolRegistry.
type MCPCaller interface {
	ListTools(ctx context.Context) ([]mcp.ToolDefinition, error)
	CallToolWithContext(ctx context.Context, ec mcp.ExecutionContext, name string, arguments map[string]any) (*mcp.ToolResult, error)
}

// ToolCallStatus represents the execution state of an invoked tool call.
type ToolCallStatus string

const (
	ToolCallStatusExecuting   ToolCallStatus = "executing"
	ToolCallStatusGateWaiting ToolCallStatus = "gate-waiting"
	ToolCallStatusCompleted   ToolCallStatus = "completed"
	ToolCallStatusFailed      ToolCallStatus = "failed"
)

// ToolCallTelemetry tracks individual tool invocation lifecycle for frontend visualization.
type ToolCallTelemetry struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Arguments    any            `json:"arguments,omitempty"`
	Status       ToolCallStatus `json:"status"`
	Result       any            `json:"result,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	DurationMS   int64          `json:"durationMs,omitempty"`
	StartedAt    string         `json:"startedAt,omitempty"`
	CompletedAt  string         `json:"completedAt,omitempty"`
}

// AgentReasoningBlock represents a chunk or segment of cognitive contemplation.
type AgentReasoningBlock struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	IsStreaming bool   `json:"isStreaming,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// CognitiveExecutorConfig provides dependencies and tuning parameters for CognitiveAgentExecutor.
type CognitiveExecutorConfig struct {
	LLMClient       llm.LLMClient
	MCPCaller       MCPCaller
	Sandbox         SandboxManager
	Publisher       EventPublisher
	WorkerID        string
	DefaultModel    string
	MaxIterations   int
	DefaultTimeout  time.Duration
	StreamReasoning bool
}

// CognitiveAgentExecutor orchestrates cognitive agent steps through an autonomous LLM loop with MCP tools.
type CognitiveAgentExecutor struct {
	cfg CognitiveExecutorConfig
}

// NewCognitiveAgentExecutor initializes a CognitiveAgentExecutor with validated default configurations.
func NewCognitiveAgentExecutor(cfg CognitiveExecutorConfig) *CognitiveAgentExecutor {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 10
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "claude-3-7-sonnet-20250219"
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Minute
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = "ov-worker"
	}

	return &CognitiveAgentExecutor{
		cfg: cfg,
	}
}

// ExecuteStep implements AgentStepExecutor.
func (e *CognitiveAgentExecutor) ExecuteStep(ctx context.Context, step AgentStepPayload, task *TaskMessage) (*TaskResult, error) {
	if err := ctx.Err(); err != nil {
		return &TaskResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	if e.cfg.LLMClient == nil {
		err := errors.New("cognitive executor: LLM client is not configured")
		return &TaskResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// 1. Resolve worktree path if SandboxManager is configured
	var worktreePath string
	if e.cfg.Sandbox != nil && task != nil && task.RunID != "" {
		if wp, ok := e.cfg.Sandbox.GetWorktreePath(task.RunID); ok {
			worktreePath = wp
		}
	}

	// 2. Discover and convert MCP tools
	var llmTools []llm.ToolDefinition
	if e.cfg.MCPCaller != nil {
		mcpTools, err := e.cfg.MCPCaller.ListTools(ctx)
		if err == nil && len(mcpTools) > 0 {
			llmTools = llm.MCPToolsToLLMTools(mcpTools)
		}
	}

	// 3. Build initial conversation history
	messages := e.buildInitialConversation(step, task, worktreePath)

	// 4. Resolve model
	model := step.Model
	if model == "" {
		model = e.cfg.DefaultModel
	}

	// Setup telemetry collectors
	var (
		reasoningBlocks []AgentReasoningBlock
		toolCallsLog    []ToolCallTelemetry
		finalContent    string
		seqCounter      = new(atomic.Int64)
	)

	// Setup Chunker if publisher is available
	var chunker *StreamChunker
	if e.cfg.Publisher != nil && task != nil {
		chunker = NewStreamChunker(StreamChunkerConfig{
			TaskID:     task.TaskID,
			StepID:     task.StepID,
			RunID:      task.RunID,
			WorkerID:   e.cfg.WorkerID,
			Stream:     "stdout",
			Publisher:  e.cfg.Publisher,
			SeqCounter: seqCounter,
		})
	}

	var totalUsage llm.UsageMetrics
	iteration := 0

	for iteration < e.cfg.MaxIterations {
		if err := ctx.Err(); err != nil {
			return &TaskResult{
				Success: false,
				Error:   err.Error(),
				Data: map[string]any{
					"iterations": iteration,
					"model":      model,
				},
			}, err
		}

		iteration++
		req := &llm.ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    llmTools,
		}
		if step.Temperature > 0 {
			t := step.Temperature
			req.Temperature = &t
		}
		if step.MaxTokens > 0 {
			m := step.MaxTokens
			req.MaxTokens = &m
		}

		startedAt := time.Now().UTC().Format(time.RFC3339)

		// Synchronous inference with LLMClient
		resp, err := e.cfg.LLMClient.ChatCompletion(ctx, req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return &TaskResult{
					Success: false,
					Error:   err.Error(),
					Data: map[string]any{
						"iterations": iteration,
						"model":      model,
					},
				}, err
			}
			return &TaskResult{
				Success: false,
				Error:   fmt.Sprintf("llm inference error at iteration %d: %v", iteration, err),
				Data: map[string]any{
					"iterations": iteration,
					"model":      model,
				},
			}, fmt.Errorf("llm inference error: %w", err)
		}

		// Accumulate token usage
		totalUsage.PromptTokens += resp.Usage.PromptTokens
		totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		totalUsage.TotalTokens += resp.Usage.TotalTokens

		assistantMsg := resp.Message
		content := assistantMsg.Content
		if content != "" {
			finalContent = content
			reasoningBlocks = append(reasoningBlocks, AgentReasoningBlock{
				ID:          fmt.Sprintf("reasoning-%d", iteration),
				Content:     content,
				StartedAt:   startedAt,
				CompletedAt: time.Now().UTC().Format(time.RFC3339),
			})

			if e.cfg.StreamReasoning && chunker != nil {
				_, _ = chunker.Write([]byte(content + "\n"))
				_ = chunker.Flush()
			}
		}

		// If no tool calls requested, agent concluded task
		if len(assistantMsg.ToolCalls) == 0 {
			messages = append(messages, assistantMsg)
			break
		}

		// Append assistant response with ToolCalls to conversation
		messages = append(messages, assistantMsg)

		// Execute each requested tool call
		for _, tc := range assistantMsg.ToolCalls {
			tcStarted := time.Now()
			tcStartedStr := tcStarted.UTC().Format(time.RFC3339)

			var argsMap map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &argsMap)
			}
			if argsMap == nil {
				argsMap = make(map[string]any)
			}

			telemetryItem := ToolCallTelemetry{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: argsMap,
				Status:    ToolCallStatusExecuting,
				StartedAt: tcStartedStr,
			}

			// Stream tool execution event to live logs
			if chunker != nil {
				_, _ = chunker.Write([]byte(fmt.Sprintf("[tool_executing] %s(%s)\n", tc.Function.Name, tc.Function.Arguments)))
				_ = chunker.Flush()
			}

			var (
				toolOutput  string
				isToolError bool
			)

			if e.cfg.MCPCaller == nil {
				toolOutput = fmt.Sprintf("error: mcp caller not configured for tool %s", tc.Function.Name)
				isToolError = true
				telemetryItem.Status = ToolCallStatusFailed
				telemetryItem.ErrorMessage = toolOutput
			} else {
				ec := mcp.ExecutionContext{
					RunID:    task.RunID,
					CallerID: e.cfg.WorkerID,
					Metadata: map[string]any{
						"worktree_path": worktreePath,
						"task_id":       task.TaskID,
						"step_id":       task.StepID,
					},
				}

				mcpRes, mcpErr := e.cfg.MCPCaller.CallToolWithContext(ctx, ec, tc.Function.Name, argsMap)
				if mcpErr != nil {
					toolOutput = fmt.Sprintf("tool execution error: %v", mcpErr)
					isToolError = true
					telemetryItem.Status = ToolCallStatusFailed
					telemetryItem.ErrorMessage = mcpErr.Error()
				} else if mcpRes != nil {
					toolOutput = mcpRes.TextContent()
					if toolOutput == "" && len(mcpRes.Content) > 0 {
						toolOutput = mcpRes.Content[0].Text
					}
					isToolError = mcpRes.IsError
					if isToolError {
						telemetryItem.Status = ToolCallStatusFailed
						telemetryItem.ErrorMessage = toolOutput
					} else {
						telemetryItem.Status = ToolCallStatusCompleted
					}
				} else {
					toolOutput = "tool returned empty result"
					telemetryItem.Status = ToolCallStatusCompleted
				}
			}

			tcDuration := time.Since(tcStarted).Milliseconds()
			telemetryItem.DurationMS = tcDuration
			telemetryItem.CompletedAt = time.Now().UTC().Format(time.RFC3339)
			telemetryItem.Result = toolOutput

			toolCallsLog = append(toolCallsLog, telemetryItem)

			if chunker != nil {
				_, _ = chunker.Write([]byte(fmt.Sprintf("[tool_completed] %s: %s\n", tc.Function.Name, sandbox.MaskSecrets(toolOutput, nil))))
				_ = chunker.Flush()
			}

			// Add ToolResultMessage to conversation for self-healing next iteration
			toolMsg := llm.NewToolResultMessage(llm.ToolResult{
				ToolCallID: tc.ID,
				Content:    toolOutput,
				IsError:    isToolError,
			})
			messages = append(messages, toolMsg)
		}
	}

	if iteration >= e.cfg.MaxIterations && (len(toolCallsLog) > 0 || finalContent == "") {
		// If last message still has tool calls or loop exhausted without plain assistant finish
		lastMsg := messages[len(messages)-1]
		if len(lastMsg.ToolCalls) > 0 || lastMsg.Role == llm.RoleTool {
			err := fmt.Errorf("cognitive agent reached max iterations (%d) without completing", e.cfg.MaxIterations)
			return &TaskResult{
				Success: false,
				Error:   err.Error(),
				Data: map[string]any{
					"step_name":     step.StepName,
					"role":          step.Role,
					"action":        step.Action,
					"model":         model,
					"iterations":    iteration,
					"reasoning":     reasoningBlocks,
					"tool_calls":    toolCallsLog,
					"worktree_path": worktreePath,
					"usage":         totalUsage,
				},
			}, err
		}
	}

	res := &TaskResult{
		Success: true,
		Output:  finalContent,
		Data: map[string]any{
			"step_name":     step.StepName,
			"role":          step.Role,
			"action":        step.Action,
			"model":         model,
			"iterations":    iteration,
			"reasoning":     reasoningBlocks,
			"tool_calls":    toolCallsLog,
			"worktree_path": worktreePath,
			"usage":         totalUsage,
		},
	}

	return res, nil
}

func (e *CognitiveAgentExecutor) buildInitialConversation(step AgentStepPayload, task *TaskMessage, worktreePath string) []llm.ChatMessage {
	role := step.Role
	if role == "" {
		role = "orchestrator"
	}
	action := step.Action
	if action == "" {
		action = "execute"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are an autonomous AI software engineer in gv-factory.\nRole: %s\nAction: %s\n", role, action))
	if task != nil {
		sb.WriteString(fmt.Sprintf("Run ID: %s\nTask ID: %s\nStep ID: %s\n", task.RunID, task.TaskID, task.StepID))
	}
	if worktreePath != "" {
		sb.WriteString(fmt.Sprintf("Sandbox Worktree Workspace: %s\n", worktreePath))
	}
	sb.WriteString("Analyze the task, formulate plans, invoke authorized tools, and verify outputs.")

	systemMsg := llm.NewSystemMessage(sb.String())

	userContent := step.Prompt
	if userContent == "" {
		userContent = fmt.Sprintf("Execute step %s (%s:%s)", step.StepName, role, action)
	}
	userMsg := llm.NewUserMessage(userContent)

	return []llm.ChatMessage{systemMsg, userMsg}
}
