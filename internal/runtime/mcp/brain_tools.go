// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gv-factory/gv-factory/internal/brain"
)

// MCP Brain Tool identifier constants.
const (
	ToolBrainSearch        = "brain_search"
	ToolBrainQueryRules    = "brain_query_rules"
	ToolBrainGetKnowledge  = "brain_get_knowledge"
	ToolBrainRecordEpisode = "brain_record_episode"
	ToolBrainGetStats      = "brain_get_stats"
)

// BrainToolDefinitions returns the canonical MCP ToolDefinition specifications for all Brain tools.
func BrainToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        ToolBrainSearch,
			Description: "Search persistent codebase brain knowledge, architectural rules, and past task execution episodes using hybrid retrieval (pgvector cosine similarity + full-text search).",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query text (natural language keywords, symbol name, or architectural concept).",
					},
					"category": map[string]any{
						"type":        "string",
						"enum":        []string{"all", "knowledge", "rules", "episodes"},
						"default":     "all",
						"description": "Category of memory to search across: 'all', 'knowledge' (AST code snippets), 'rules' (architectural guidelines), or 'episodes' (past execution learnings).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"default":     5,
						"description": "Maximum number of results to return.",
					},
					"alpha": map[string]any{
						"type":        "number",
						"default":     0.7,
						"description": "Balance weight between vector similarity (alpha) and lexical text match (1-alpha). Default: 0.7.",
					},
					"embedding": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "number"},
						"description": "Optional precomputed embedding vector (1536 float values) to boost semantic similarity or query episodes.",
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        ToolBrainQueryRules,
			Description: "Query established architectural rules, coding standards, and system design constraints from the persistent Brain.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Optional search query to match relevant architectural rules.",
					},
					"code": map[string]any{
						"type":        "string",
						"description": "Optional specific architectural rule code (e.g., 'SEC-001', 'ARCH-002') for exact retrieval.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"default":     50,
						"description": "Maximum number of rules to return when listing.",
					},
				},
			},
		},
		{
			Name:        ToolBrainGetKnowledge,
			Description: "Retrieve a specific semantic codebase knowledge fragment or AST symbol by file path and symbol name.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Repository-relative file path where the symbol is located (e.g. 'internal/auth/token.go').",
					},
					"symbol_name": map[string]any{
						"type":        "string",
						"description": "Name of the symbol (function, struct, method, interface) to inspect.",
					},
				},
				Required: []string{"file_path", "symbol_name"},
			},
		},
		{
			Name:        ToolBrainRecordEpisode,
			Description: "Record a task execution episode, review feedback, or failure root cause into persistent Brain episodic memory.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"run_id": map[string]any{
						"type":        "string",
						"description": "Factory run ID under which this task was executed.",
					},
					"task_name": map[string]any{
						"type":        "string",
						"description": "Name or identifier of the task performed.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"enum":        []string{"success", "failure", "rejected"},
						"description": "Execution outcome status: 'success', 'failure', or 'rejected'.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Concise summary of actions taken and results achieved.",
					},
					"lessons_learned": map[string]any{
						"type":        "string",
						"description": "Key takeaways, error root causes, or architectural lessons learned for future agents.",
					},
					"embedding": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "number"},
						"description": "Optional embedding vector (1536 floats) for semantic retrieval of this episode.",
					},
				},
				Required: []string{"run_id", "task_name", "outcome", "summary"},
			},
		},
		{
			Name:        ToolBrainGetStats,
			Description: "Get statistical summary of records indexed in the Codebase Brain (indexed knowledge chunks, active rules, task episodes).",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
	}
}

// BrainToolAdapter adapts a *brain.Service into callable MCP tools.
type BrainToolAdapter struct {
	brainSvc *brain.Service
}

// BrainToolServer is an alias for BrainToolAdapter for semantic clarity when used as an in-process MCP server.
type BrainToolServer = BrainToolAdapter

// NewBrainToolAdapter constructs a new BrainToolAdapter with the given brain service.
func NewBrainToolAdapter(brainSvc *brain.Service) *BrainToolAdapter {
	return &BrainToolAdapter{
		brainSvc: brainSvc,
	}
}

// NewBrainToolServer constructs a new in-process BrainToolServer.
func NewBrainToolServer(brainSvc *brain.Service) *BrainToolServer {
	return NewBrainToolAdapter(brainSvc)
}

// Definitions returns the MCP tool definitions exposed by this adapter.
func (a *BrainToolAdapter) Definitions() []ToolDefinition {
	return BrainToolDefinitions()
}

// CallTool routes an MCP tool call to the corresponding handler.
func (a *BrainToolAdapter) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch name {
	case ToolBrainSearch:
		return a.handleSearch(ctx, arguments)
	case ToolBrainQueryRules:
		return a.handleQueryRules(ctx, arguments)
	case ToolBrainGetKnowledge:
		return a.handleGetKnowledge(ctx, arguments)
	case ToolBrainRecordEpisode:
		return a.handleRecordEpisode(ctx, arguments)
	case ToolBrainGetStats:
		return a.handleGetStats(ctx, arguments)
	default:
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
}

// BrainSearchResult aggregates ranked results across knowledge, rules, and episodic memory.
type BrainSearchResult struct {
	Query     string                          `json:"query"`
	Category  string                          `json:"category"`
	Total     int                             `json:"total"`
	Knowledge []brain.CodebaseKnowledgeResult `json:"knowledge,omitempty"`
	Rules     []brain.ArchitecturalRuleResult `json:"rules,omitempty"`
	Episodes  []brain.TaskEpisode             `json:"episodes,omitempty"`
}

func (a *BrainToolAdapter) handleSearch(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if a == nil || a.brainSvc == nil {
		return errorToolResult("brain service is not available"), nil
	}

	query := extractString(args, "query")
	if query == "" {
		return errorToolResult("query parameter is required"), nil
	}

	category := strings.ToLower(extractString(args, "category"))
	if category == "" {
		category = "all"
	}

	limit := extractInt(args, "limit", 5)
	if limit <= 0 {
		limit = 5
	}

	alpha := extractFloat32(args, "alpha", brain.DefaultHybridAlpha)
	emb := extractFloat32Slice(args, "embedding")

	hq := brain.NewHybridSearchQuery(query).WithLimit(limit).WithAlpha(alpha)
	if len(emb) > 0 {
		hq = hq.WithEmbedding(emb)
	}

	resp := BrainSearchResult{
		Query:    query,
		Category: category,
	}

	// 1. Search Codebase Knowledge
	if category == "all" || category == "knowledge" {
		kResults, err := a.brainSvc.HybridSearchCodebase(ctx, hq)
		if err != nil {
			return errorToolResult(fmt.Sprintf("knowledge search error: %v", err)), nil
		}
		resp.Knowledge = kResults
		resp.Total += len(kResults)
	}

	// 2. Search Architectural Rules
	if category == "all" || category == "rules" {
		rResults, err := a.brainSvc.HybridSearchRules(ctx, hq)
		if err != nil {
			return errorToolResult(fmt.Sprintf("rules search error: %v", err)), nil
		}
		resp.Rules = rResults
		resp.Total += len(rResults)
	}

	// 3. Search Task Episodes (requires embedding vector)
	if (category == "all" || category == "episodes") && len(emb) == brain.EpisodeVectorDimensions {
		epResults, err := a.brainSvc.QuerySimilarEpisodes(ctx, emb, limit)
		if err != nil {
			return errorToolResult(fmt.Sprintf("episodes search error: %v", err)), nil
		}
		resp.Episodes = epResults
		resp.Total += len(epResults)
	}

	return jsonToolResult(resp)
}

func (a *BrainToolAdapter) handleQueryRules(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if a == nil || a.brainSvc == nil {
		return errorToolResult("brain service is not available"), nil
	}

	code := extractString(args, "code")
	query := extractString(args, "query")
	limit := extractInt(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}

	// Exact rule code lookup
	if code != "" {
		rule, err := a.brainSvc.GetRuleByCode(ctx, code)
		if err != nil {
			if errors.Is(err, brain.ErrRuleNotFound) {
				return errorToolResult(fmt.Sprintf("architectural rule with code %q not found", code)), nil
			}
			return errorToolResult(err.Error()), nil
		}
		return jsonToolResult([]brain.ArchitecturalRule{*rule})
	}

	// Text query search
	if query != "" {
		hq := brain.NewHybridSearchQuery(query).WithLimit(limit)
		rules, err := a.brainSvc.HybridSearchRules(ctx, hq)
		if err != nil {
			return errorToolResult(err.Error()), nil
		}
		return jsonToolResult(rules)
	}

	// List rules
	rules, err := a.brainSvc.ListRules(ctx, limit)
	if err != nil {
		return errorToolResult(err.Error()), nil
	}
	return jsonToolResult(rules)
}

func (a *BrainToolAdapter) handleGetKnowledge(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if a == nil || a.brainSvc == nil {
		return errorToolResult("brain service is not available"), nil
	}

	filePath := extractString(args, "file_path")
	symbolName := extractString(args, "symbol_name")

	if filePath == "" || symbolName == "" {
		return errorToolResult("file_path and symbol_name are required"), nil
	}

	k, err := a.brainSvc.GetKnowledgeBySymbol(ctx, filePath, symbolName)
	if err != nil {
		if errors.Is(err, brain.ErrKnowledgeNotFound) {
			return errorToolResult(fmt.Sprintf("knowledge not found for symbol %q in %q", symbolName, filePath)), nil
		}
		return errorToolResult(err.Error()), nil
	}

	return jsonToolResult(k)
}

func (a *BrainToolAdapter) handleRecordEpisode(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if a == nil || a.brainSvc == nil {
		return errorToolResult("brain service is not available"), nil
	}

	runID := extractString(args, "run_id")
	taskName := extractString(args, "task_name")
	outcome := strings.ToLower(extractString(args, "outcome"))
	summary := extractString(args, "summary")
	lessonsLearned := extractString(args, "lessons_learned")

	if runID == "" || taskName == "" || outcome == "" || summary == "" {
		return errorToolResult("run_id, task_name, outcome, and summary are required"), nil
	}

	if outcome != "success" && outcome != "failure" && outcome != "rejected" {
		return errorToolResult("outcome must be one of: success, failure, rejected"), nil
	}

	ep := brain.TaskEpisode{
		RunID:          runID,
		TaskName:       taskName,
		Outcome:        outcome,
		Summary:        summary,
		LessonsLearned: lessonsLearned,
	}

	emb := extractFloat32Slice(args, "embedding")
	if len(emb) > 0 {
		ep.Embedding = emb
	}

	created, err := a.brainSvc.CreateEpisode(ctx, ep)
	if err != nil {
		return errorToolResult(err.Error()), nil
	}

	return jsonToolResult(created)
}

func (a *BrainToolAdapter) handleGetStats(ctx context.Context, _ map[string]any) (*ToolResult, error) {
	if a == nil || a.brainSvc == nil {
		return errorToolResult("brain service is not available"), nil
	}

	stats, err := a.brainSvc.GetStats(ctx)
	if err != nil {
		return errorToolResult(err.Error()), nil
	}

	return jsonToolResult(stats)
}

// NewClient creates an active in-process *Client communicating with this BrainToolAdapter.
func (a *BrainToolAdapter) NewClient() *Client {
	respChan := make(chan *Response, 20)
	tr := &inProcessTransport{
		receiveFunc: func(ctx context.Context) (*Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r, ok := <-respChan:
				if !ok {
					return nil, ErrTransportClosed
				}
				return r, nil
			}
		},
		sendFunc: func(ctx context.Context, req *Request) error {
			if req.IsNotification() {
				return nil
			}
			go func() {
				switch req.Method {
				case MethodInitialize:
					initRes := InitializeResult{
						ProtocolVersion: LatestProtocolVersion,
						Capabilities: ServerCapabilities{
							Tools: &CapabilityItem{},
						},
						ServerInfo: ImplementationInfo{
							Name:    "brain-tools",
							Version: "1.0.0",
						},
					}
					b, _ := json.Marshal(initRes)
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  b,
					}
				case MethodPing:
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  []byte("{}"),
					}
				case MethodToolsList:
					res := ListToolsResult{Tools: a.Definitions()}
					b, _ := json.Marshal(res)
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  b,
					}
				case MethodToolsCall:
					var p CallToolParams
					_ = json.Unmarshal(req.Params, &p)
					callRes, err := a.CallTool(ctx, p.Name, p.Arguments)
					if err != nil {
						respChan <- &Response{
							JSONRPC: JSONRPCVersion,
							ID:      req.ID,
							Error: &RPCError{
								Code:    CodeInternalError,
								Message: err.Error(),
							},
						}
					} else {
						b, _ := json.Marshal(callRes)
						respChan <- &Response{
							JSONRPC: JSONRPCVersion,
							ID:      req.ID,
							Result:  b,
						}
					}
				default:
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Error: &RPCError{
							Code:    CodeMethodNotFound,
							Message: fmt.Sprintf("method not found: %s", req.Method),
						},
					}
				}
			}()
			return nil
		},
	}
	return NewClient(tr)
}

// RegisterBrainTools registers all Brain MCP tools with the provided MCPToolRegistry as local tools.
func RegisterBrainTools(registry *MCPToolRegistry, brainSvc *brain.Service) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}

	adapter := NewBrainToolAdapter(brainSvc)
	defs := BrainToolDefinitions()

	for _, def := range defs {
		toolDef := def
		err := registry.RegisterLocalTool(toolDef, func(ctx context.Context, arguments map[string]any) (*ToolResult, error) {
			return adapter.CallTool(ctx, toolDef.Name, arguments)
		})
		if err != nil {
			return fmt.Errorf("failed to register local tool %q: %w", toolDef.Name, err)
		}
	}

	return nil
}

// In-process transport implementation.
type inProcessTransport struct {
	sendFunc    func(ctx context.Context, req *Request) error
	receiveFunc func(ctx context.Context) (*Response, error)
	closeFunc   func() error
}

func (t *inProcessTransport) Send(ctx context.Context, req *Request) error {
	if t.sendFunc != nil {
		return t.sendFunc(ctx, req)
	}
	return nil
}

func (t *inProcessTransport) Receive(ctx context.Context) (*Response, error) {
	if t.receiveFunc != nil {
		return t.receiveFunc(ctx)
	}
	return nil, nil
}

func (t *inProcessTransport) Close() error {
	if t.closeFunc != nil {
		return t.closeFunc()
	}
	return nil
}

// Parameter extraction helpers.

func extractString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func extractInt(args map[string]any, key string, defaultVal int) int {
	if args == nil {
		return defaultVal
	}
	v, ok := args[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	default:
		return defaultVal
	}
}

func extractFloat32(args map[string]any, key string, defaultVal float32) float32 {
	if args == nil {
		return defaultVal
	}
	v, ok := args[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case float32:
		return val
	case float64:
		return float32(val)
	case int:
		return float32(val)
	default:
		return defaultVal
	}
}

func extractFloat32Slice(args map[string]any, key string) []float32 {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}

	switch slice := v.(type) {
	case []float32:
		return slice
	case []float64:
		res := make([]float32, len(slice))
		for i, f := range slice {
			res[i] = float32(f)
		}
		return res
	case []any:
		res := make([]float32, len(slice))
		for i, item := range slice {
			switch n := item.(type) {
			case float32:
				res[i] = n
			case float64:
				res[i] = float32(n)
			case int:
				res[i] = float32(n)
			default:
				return nil
			}
		}
		return res
	default:
		return nil
	}
}

func errorToolResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentBlock{
			{
				Type: "text",
				Text: msg,
			},
		},
	}
}

func jsonToolResult(v any) (*ToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format tool result JSON: %w", err)
	}
	return &ToolResult{
		Content: []ContentBlock{
			{
				Type: "text",
				Text: string(data),
			},
		},
	}, nil
}
