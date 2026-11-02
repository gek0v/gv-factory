// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"
)

// Mock helpers for pgx DBPool
type testMockRow struct {
	values []any
	err    error
}

func (r *testMockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return testMockScan(r.values, dest)
}

type testMockRows struct {
	data [][]any
	idx  int
	err  error
}

func (m *testMockRows) Close()                                       {}
func (m *testMockRows) Err() error                                   { return m.err }
func (m *testMockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *testMockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *testMockRows) Next() bool {
	if m.idx < len(m.data) {
		m.idx++
		return true
	}
	return false
}
func (m *testMockRows) Scan(dest ...any) error {
	return testMockScan(m.data[m.idx-1], dest)
}
func (m *testMockRows) Values() ([]any, error) { return nil, nil }
func (m *testMockRows) RawValues() [][]byte    { return nil }
func (m *testMockRows) Conn() *pgx.Conn        { return nil }
func (m *testMockRows) TypeMap() *pgtype.Map   { return nil }

func testMockScan(src []any, dest []any) error {
	for i, d := range dest {
		if i >= len(src) {
			break
		}
		val := src[i]
		if val == nil {
			continue
		}
		switch target := d.(type) {
		case *string:
			if s, ok := val.(string); ok {
				*target = s
			}
		case *time.Time:
			if t, ok := val.(time.Time); ok {
				*target = t
			}
		case *float32:
			if f, ok := val.(float32); ok {
				*target = f
			}
		case *float64:
			if f, ok := val.(float64); ok {
				*target = f
			}
		case *int:
			if iv, ok := val.(int); ok {
				*target = iv
			}
		case *int64:
			if iv, ok := val.(int64); ok {
				*target = iv
			}
		case **pgvector.Vector:
			if v, ok := val.(*pgvector.Vector); ok {
				*target = v
			}
		}
	}
	return nil
}

type testMockDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	pingFn     func(ctx context.Context) error
	closeFn    func()
}

func (m *testMockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &testMockRow{err: pgx.ErrNoRows}
}

func (m *testMockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &testMockRows{}, nil
}

func (m *testMockDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *testMockDB) Close() {
	if m.closeFn != nil {
		m.closeFn()
	}
}

func TestBrainToolDefinitions(t *testing.T) {
	defs := BrainToolDefinitions()
	if len(defs) != 5 {
		t.Fatalf("expected 5 tool definitions, got %d", len(defs))
	}

	expected := map[string]bool{
		ToolBrainSearch:        false,
		ToolBrainQueryRules:    false,
		ToolBrainGetKnowledge:  false,
		ToolBrainRecordEpisode: false,
		ToolBrainGetStats:      false,
	}

	for _, d := range defs {
		if _, ok := expected[d.Name]; !ok {
			t.Errorf("unexpected tool definition: %s", d.Name)
		}
		expected[d.Name] = true
		if d.Description == "" {
			t.Errorf("expected non-empty description for tool %s", d.Name)
		}
		if d.InputSchema.Type != "object" {
			t.Errorf("expected schema type object for %s, got %s", d.Name, d.InputSchema.Type)
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing tool definition: %s", name)
		}
	}
}

func TestBrainSearch_Tool(t *testing.T) {
	ctx := context.Background()

	t.Run("successful search across all categories", func(t *testing.T) {
		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				if strings.Contains(sql, "codebase_knowledge") {
					return &testMockRows{
						data: [][]any{
							{"k-1", "pkg/auth/token.go", "GenerateToken", "function", "func GenerateToken()", "hash1", time.Now(), float32(0.1), float32(0.9)},
						},
					}, nil
				}
				if strings.Contains(sql, "architectural_rules") {
					return &testMockRows{
						data: [][]any{
							{"r-1", "SEC-001", "JWT Expiration", "Tokens must expire within 1 hour", "CRITICAL", time.Now(), time.Now(), float32(0.1), float32(0.85)},
						},
					}, nil
				}
				return &testMockRows{}, nil
			},
		}

		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query":    "token auth",
			"category": "all",
			"limit":    5,
			"alpha":    0.7,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected successful result, got error: %s", res.TextContent())
		}

		content := res.TextContent()
		if !strings.Contains(content, "GenerateToken") || !strings.Contains(content, "SEC-001") {
			t.Fatalf("expected results to contain GenerateToken and SEC-001, got: %s", content)
		}
	})

	t.Run("search category knowledge only", func(t *testing.T) {
		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &testMockRows{
					data: [][]any{
						{"k-1", "pkg/brain.go", "Service", "struct", "type Service struct{}", "hash", time.Now(), float32(0.2), float32(0.8)},
					},
				}, nil
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query":    "Service struct",
			"category": "knowledge",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "Service") {
			t.Fatalf("expected knowledge match, got: %s", res.TextContent())
		}
	})

	t.Run("search category rules only", func(t *testing.T) {
		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &testMockRows{
					data: [][]any{
						{"r-1", "ARCH-002", "Layer Separation", "UI must not import DB", "HIGH", time.Now(), time.Now(), float32(0.2), float32(0.8)},
					},
				}, nil
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query":    "Layer Separation",
			"category": "rules",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "ARCH-002") {
			t.Fatalf("expected rules match, got: %s", res.TextContent())
		}
	})

	t.Run("search category episodes with embedding", func(t *testing.T) {
		emb := make([]float32, 1536)
		emb[0] = 0.5
		embRaw := make([]any, 1536)
		for i, v := range emb {
			embRaw[i] = v
		}

		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &testMockRows{
					data: [][]any{
						{"ep-1", "run-1", "BuildTask", "success", "Built successfully", "Cache warmed", time.Now(), float32(0.1)},
					},
				}, nil
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query":     "BuildTask",
			"category":  "episodes",
			"embedding": embRaw,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "BuildTask") {
			t.Fatalf("expected episode match, got: %s", res.TextContent())
		}
	})

	t.Run("search missing query parameter", func(t *testing.T) {
		mock := &testMockDB{}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)
		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"limit": 5,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "query") {
			t.Fatalf("expected query required error, got: %v", res)
		}
	})

	t.Run("search with nil brain service", func(t *testing.T) {
		adapter := NewBrainToolAdapter(nil)
		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query": "something",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "not available") {
			t.Fatalf("expected not available error, got: %v", res)
		}
	})

	t.Run("search with db error", func(t *testing.T) {
		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("connection reset by peer")
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query": "something",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error result on db failure, got: %v", res)
		}
	})
}

func TestBrainQueryRules_Tool(t *testing.T) {
	ctx := context.Background()

	t.Run("query rule by code", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{
					values: []any{"r-1", "SEC-001", "Secret Masking", "Secrets must be masked", "CRITICAL", nil, time.Now(), time.Now()},
				}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{
			"code": "SEC-001",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "Secret Masking") {
			t.Fatalf("expected rule title, got: %s", res.TextContent())
		}
	})

	t.Run("query rule by text query", func(t *testing.T) {
		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &testMockRows{
					data: [][]any{
						{"r-1", "SEC-002", "No Hardcoded Secrets", "Hardcoded secrets forbidden", "CRITICAL", time.Now(), time.Now(), float32(0.1), float32(0.9)},
					},
				}, nil
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{
			"query": "secrets",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "SEC-002") {
			t.Fatalf("expected SEC-002, got: %s", res.TextContent())
		}
	})

	t.Run("list all rules without params", func(t *testing.T) {
		mock := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &testMockRows{
					data: [][]any{
						{"r-1", "R1", "Title 1", "Desc 1", "INFO", nil, time.Now(), time.Now()},
						{"r-2", "R2", "Title 2", "Desc 2", "WARN", nil, time.Now(), time.Now()},
					},
				}, nil
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "Title 1") || !strings.Contains(res.TextContent(), "Title 2") {
			t.Fatalf("expected listed rules, got: %s", res.TextContent())
		}
	})

	t.Run("rule not found", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{err: pgx.ErrNoRows}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{
			"code": "NON-EXISTENT",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "not found") {
			t.Fatalf("expected not found error, got: %v", res)
		}
	})

	t.Run("nil brain service", func(t *testing.T) {
		adapter := NewBrainToolAdapter(nil)
		res, err := adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error for nil brain service, got: %v", res)
		}
	})
}

func TestBrainGetKnowledge_Tool(t *testing.T) {
	ctx := context.Background()

	t.Run("successful lookup", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{
					values: []any{"k-1", "cmd/server/main.go", "main", "function", "func main() {}", "hash123", nil, time.Now()},
				}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainGetKnowledge, map[string]any{
			"file_path":   "cmd/server/main.go",
			"symbol_name": "main",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "func main() {}") {
			t.Fatalf("expected knowledge content, got: %s", res.TextContent())
		}
	})

	t.Run("missing parameters", func(t *testing.T) {
		mock := &testMockDB{}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)
		res, err := adapter.CallTool(ctx, ToolBrainGetKnowledge, map[string]any{
			"file_path": "cmd/server/main.go",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "symbol_name") {
			t.Fatalf("expected missing symbol_name error, got: %v", res)
		}
	})

	t.Run("symbol not found", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{err: pgx.ErrNoRows}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainGetKnowledge, map[string]any{
			"file_path":   "pkg/missing.go",
			"symbol_name": "Ghost",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "not found") {
			t.Fatalf("expected not found error, got: %v", res)
		}
	})

	t.Run("nil brain service", func(t *testing.T) {
		adapter := NewBrainToolAdapter(nil)
		res, err := adapter.CallTool(ctx, ToolBrainGetKnowledge, map[string]any{
			"file_path":   "a",
			"symbol_name": "b",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected nil service error, got: %v", res)
		}
	})
}

func TestBrainRecordEpisode_Tool(t *testing.T) {
	ctx := context.Background()

	t.Run("successful episode creation", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{
					values: []any{"ep-10", "run-42", "RefactorAuth", "success", "Refactored JWT validator", "Always check expirations", time.Now()},
				}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainRecordEpisode, map[string]any{
			"run_id":          "run-42",
			"task_name":       "RefactorAuth",
			"outcome":         "success",
			"summary":         "Refactored JWT validator",
			"lessons_learned": "Always check expirations",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}
		if !strings.Contains(res.TextContent(), "ep-10") {
			t.Fatalf("expected created episode JSON, got: %s", res.TextContent())
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		mock := &testMockDB{}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)
		res, err := adapter.CallTool(ctx, ToolBrainRecordEpisode, map[string]any{
			"run_id": "run-42",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "required") {
			t.Fatalf("expected required field error, got: %v", res)
		}
	})

	t.Run("invalid outcome value", func(t *testing.T) {
		mock := &testMockDB{}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)
		res, err := adapter.CallTool(ctx, ToolBrainRecordEpisode, map[string]any{
			"run_id":    "run-1",
			"task_name": "task-1",
			"outcome":   "maybe",
			"summary":   "some summary",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "outcome must be") {
			t.Fatalf("expected outcome validation error, got: %v", res)
		}
	})

	t.Run("nil brain service", func(t *testing.T) {
		adapter := NewBrainToolAdapter(nil)
		res, err := adapter.CallTool(ctx, ToolBrainRecordEpisode, map[string]any{
			"run_id":    "run-1",
			"task_name": "task-1",
			"outcome":   "failure",
			"summary":   "failed compile",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected nil service error, got: %v", res)
		}
	})
}

func TestBrainGetStats_Tool(t *testing.T) {
	ctx := context.Background()

	t.Run("successful stats lookup", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{
					values: []any{150, 25, 40},
				}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainGetStats, map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got: %s", res.TextContent())
		}

		var stats brain.Stats
		if err := json.Unmarshal([]byte(res.TextContent()), &stats); err != nil {
			t.Fatalf("failed to unmarshal stats: %v", err)
		}
		if stats.IndexedKnowledge != 150 || stats.ActiveRules != 25 || stats.TaskEpisodes != 40 {
			t.Fatalf("unexpected stats values: %+v", stats)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{err: errors.New("timeout querying stats")}
			},
		}
		brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
		adapter := NewBrainToolAdapter(brainSvc)

		res, err := adapter.CallTool(ctx, ToolBrainGetStats, map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error result on db failure, got: %v", res)
		}
	})

	t.Run("nil brain service", func(t *testing.T) {
		adapter := NewBrainToolAdapter(nil)
		res, err := adapter.CallTool(ctx, ToolBrainGetStats, map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("expected error on nil service, got: %v", res)
		}
	})
}

func TestBrainToolServer_UnknownToolAndContextCancellation(t *testing.T) {
	adapter := NewBrainToolAdapter(nil)

	// Unknown tool
	res, err := adapter.CallTool(context.Background(), "unknown_tool", map[string]any{})
	if err == nil {
		t.Fatalf("expected error for unknown tool, got: %v", res)
	}

	// Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = adapter.CallTool(ctx, ToolBrainGetStats, map[string]any{})
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}

func TestBrainToolServer_NewClient(t *testing.T) {
	mock := &testMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testMockRow{
				values: []any{10, 5, 2},
			}
		},
	}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)
	adapter := NewBrainToolAdapter(brainSvc)

	client := adapter.NewClient()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// ListTools via client
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("failed to list tools via in-process client: %v", err)
	}
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools from in-process client, got %d", len(tools))
	}

	// CallTool via client
	res, err := client.CallTool(ctx, ToolBrainGetStats, map[string]any{})
	if err != nil {
		t.Fatalf("failed to call tool via in-process client: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.TextContent())
	}
	if !strings.Contains(res.TextContent(), "indexed_knowledge") {
		t.Fatalf("expected stats JSON, got: %s", res.TextContent())
	}
}

func TestRegisterBrainTools_RegistryIntegration(t *testing.T) {
	registry := NewMCPToolRegistry(RegistryConfig{
		DefaultTimeout: 2 * time.Second,
		MaxTimeout:     5 * time.Second,
	})
	defer registry.Close()

	mock := &testMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testMockRow{
				values: []any{42, 7, 3},
			}
		},
	}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mock)

	err := RegisterBrainTools(registry, brainSvc)
	if err != nil {
		t.Fatalf("failed to register brain tools: %v", err)
	}

	ctx := context.Background()

	// 1. Verify ListTools contains brain tools
	tools, err := registry.ListTools(ctx)
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}

	foundMap := make(map[string]bool)
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "brain_") {
			foundMap[tool.Name] = true
		}
	}

	if len(foundMap) != 5 {
		t.Fatalf("expected 5 brain tools in registry, got %d: %v", len(foundMap), foundMap)
	}

	// 2. CallTool via registry
	res, err := registry.CallTool(ctx, ToolBrainGetStats, map[string]any{})
	if err != nil {
		t.Fatalf("failed to call brain_get_stats: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got: %s", res.TextContent())
	}
	if !strings.Contains(res.TextContent(), "42") {
		t.Fatalf("expected stats output with 42, got: %s", res.TextContent())
	}

	// 3. CallToolWithContext via registry
	ec := ExecutionContext{RunID: "run-exec-1"}
	res, err = registry.CallToolWithContext(ctx, ec, ToolBrainGetStats, map[string]any{})
	if err != nil {
		t.Fatalf("failed to call tool with context: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got: %s", res.TextContent())
	}

	// 4. Register nil registry error check
	if err := RegisterBrainTools(nil, brainSvc); err == nil {
		t.Fatal("expected error registering to nil registry, got nil")
	}
}

func TestMCPToolRegistry_LocalToolRegistration(t *testing.T) {
	registry := NewMCPToolRegistry(RegistryConfig{
		DefaultTimeout: 2 * time.Second,
		MaxTimeout:     5 * time.Second,
	})
	defer registry.Close()

	// Validation
	if err := registry.RegisterLocalTool(ToolDefinition{}, nil); err == nil {
		t.Fatal("expected error on empty tool name and nil handler")
	}
	if err := registry.RegisterLocalTool(ToolDefinition{Name: "test"}, nil); err == nil {
		t.Fatal("expected error on nil handler")
	}

	// Register valid local tool
	customTool := ToolDefinition{
		Name:        "custom_echo",
		Description: "Echoes input text",
		InputSchema: ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"msg": map[string]any{"type": "string"},
			},
		},
	}
	err := registry.RegisterLocalTool(customTool, func(ctx context.Context, args map[string]any) (*ToolResult, error) {
		msg, _ := args["msg"].(string)
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "echo: " + msg}},
		}, nil
	})
	if err != nil {
		t.Fatalf("failed to register local tool: %v", err)
	}

	ctx := context.Background()

	// ListTools should include custom_echo
	tools, err := registry.ListTools(ctx)
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "custom_echo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom_echo not found in ListTools")
	}

	// CallTool
	res, err := registry.CallTool(ctx, "custom_echo", map[string]any{"msg": "hello world"})
	if err != nil {
		t.Fatalf("failed to call custom_echo: %v", err)
	}
	if res.TextContent() != "echo: hello world" {
		t.Fatalf("unexpected output: %s", res.TextContent())
	}

	// Unregister local tool
	if err := registry.UnregisterLocalTool("custom_echo"); err != nil {
		t.Fatalf("failed to unregister local tool: %v", err)
	}
	if err := registry.UnregisterLocalTool("nonexistent"); err == nil {
		t.Fatal("expected error unregistering nonexistent tool")
	}
}

func TestBrainTools_CoverageExtras(t *testing.T) {
	ctx := context.Background()

	t.Run("NewBrainToolServer constructor", func(t *testing.T) {
		server := NewBrainToolServer(nil)
		if server == nil {
			t.Fatal("expected non-nil server")
		}
		if len(server.Definitions()) != 5 {
			t.Fatalf("expected 5 definitions, got %d", len(server.Definitions()))
		}
	})

	t.Run("handleSearch rules and episodes DB errors", func(t *testing.T) {
		// Rules error
		mockRulesErr := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				if strings.Contains(sql, "codebase_knowledge") {
					return &testMockRows{}, nil
				}
				if strings.Contains(sql, "architectural_rules") {
					return nil, errors.New("rules table corrupted")
				}
				return &testMockRows{}, nil
			},
		}
		svcRules := brain.NewServiceWithDB(brain.DefaultConfig(), mockRulesErr)
		adapterRules := NewBrainToolAdapter(svcRules)
		res, err := adapterRules.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query":    "test",
			"category": "rules",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "rules table corrupted") {
			t.Fatalf("expected rules error, got: %v", res)
		}

		// Episodes error with valid embedding length
		emb := make([]float32, brain.EpisodeVectorDimensions)
		mockEpisodesErr := &testMockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("episodes query timeout")
			},
		}
		svcEpisodes := brain.NewServiceWithDB(brain.DefaultConfig(), mockEpisodesErr)
		adapterEpisodes := NewBrainToolAdapter(svcEpisodes)
		res, err = adapterEpisodes.CallTool(ctx, ToolBrainSearch, map[string]any{
			"query":     "test",
			"category":  "episodes",
			"embedding": emb,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "episodes query timeout") {
			t.Fatalf("expected episodes error, got: %v", res)
		}
	})

	t.Run("handleQueryRules DB errors", func(t *testing.T) {
		mockErr := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{err: errors.New("db error on rule code")}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("db error on list/search rules")
			},
		}
		svc := brain.NewServiceWithDB(brain.DefaultConfig(), mockErr)
		adapter := NewBrainToolAdapter(svc)

		// Rule code DB error
		res, err := adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{"code": "ARCH-001"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "db error on rule code") {
			t.Fatalf("expected code error, got: %v", res)
		}

		// Query text DB error
		res, err = adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{"query": "search something"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "db error on list/search rules") {
			t.Fatalf("expected query error, got: %v", res)
		}

		// List DB error
		res, err = adapter.CallTool(ctx, ToolBrainQueryRules, map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "db error on list/search rules") {
			t.Fatalf("expected list error, got: %v", res)
		}
	})

	t.Run("handleGetKnowledge DB error", func(t *testing.T) {
		mockErr := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{err: errors.New("symbol query failure")}
			},
		}
		svc := brain.NewServiceWithDB(brain.DefaultConfig(), mockErr)
		adapter := NewBrainToolAdapter(svc)

		res, err := adapter.CallTool(ctx, ToolBrainGetKnowledge, map[string]any{
			"file_path":   "main.go",
			"symbol_name": "main",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "symbol query failure") {
			t.Fatalf("expected knowledge db error, got: %v", res)
		}
	})

	t.Run("handleRecordEpisode with embedding and DB error", func(t *testing.T) {
		mockErr := &testMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testMockRow{err: errors.New("episode insertion failure")}
			},
		}
		svc := brain.NewServiceWithDB(brain.DefaultConfig(), mockErr)
		adapter := NewBrainToolAdapter(svc)

		emb := make([]float32, 1536)
		res, err := adapter.CallTool(ctx, ToolBrainRecordEpisode, map[string]any{
			"run_id":    "run-1",
			"task_name": "task-1",
			"outcome":   "rejected",
			"summary":   "failed pr",
			"embedding": emb,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.TextContent(), "episode insertion failure") {
			t.Fatalf("expected episode db error, got: %v", res)
		}
	})

	t.Run("inProcessTransport client interactions and edge cases", func(t *testing.T) {
		adapter := NewBrainToolAdapter(nil)
		client := adapter.NewClient()
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Ping
		if err := client.Ping(ctx); err != nil {
			t.Fatalf("ping failed: %v", err)
		}

		// Tool call error through client (unknown tool)
		_, err := client.CallTool(ctx, "nonexistent_tool", map[string]any{})
		if err == nil {
			t.Fatal("expected error calling nonexistent tool via client, got nil")
		}

		// Directly test transport default method and notification
		transport := &inProcessTransport{}
		if err := transport.Send(ctx, &Request{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r, err := transport.Receive(ctx); r != nil || err != nil {
			t.Fatalf("unexpected receive result: %v, %v", r, err)
		}
		if err := transport.Close(); err != nil {
			t.Fatalf("unexpected close error: %v", err)
		}

		// Transport with explicit closeFunc
		closed := false
		customTr := &inProcessTransport{
			closeFunc: func() error {
				closed = true
				return nil
			},
		}
		_ = customTr.Close()
		if !closed {
			t.Fatal("expected closeFunc to be invoked")
		}
	})

	t.Run("parameter extraction helpers coverage", func(t *testing.T) {
		// extractString
		if extractString(nil, "k") != "" {
			t.Error("expected empty string for nil map")
		}
		if extractString(map[string]any{"k": nil}, "k") != "" {
			t.Error("expected empty string for nil value")
		}
		if extractString(map[string]any{"k": 123}, "k") != "123" {
			t.Error("expected string conversion for int value")
		}

		// extractInt
		if extractInt(nil, "k", 10) != 10 {
			t.Error("expected default for nil map")
		}
		if extractInt(map[string]any{"k": nil}, "k", 10) != 10 {
			t.Error("expected default for nil value")
		}
		if extractInt(map[string]any{"k": int64(42)}, "k", 10) != 42 {
			t.Error("expected 42 for int64")
		}
		if extractInt(map[string]any{"k": float64(42.5)}, "k", 10) != 42 {
			t.Error("expected 42 for float64")
		}
		if extractInt(map[string]any{"k": float32(42.5)}, "k", 10) != 42 {
			t.Error("expected 42 for float32")
		}
		if extractInt(map[string]any{"k": "not-an-int"}, "k", 10) != 10 {
			t.Error("expected default for invalid string")
		}

		// extractFloat32
		if extractFloat32(nil, "k", 0.5) != 0.5 {
			t.Error("expected default for nil map")
		}
		if extractFloat32(map[string]any{"k": nil}, "k", 0.5) != 0.5 {
			t.Error("expected default for nil value")
		}
		if extractFloat32(map[string]any{"k": float64(0.8)}, "k", 0.5) != float32(0.8) {
			t.Error("expected 0.8 for float64")
		}
		if extractFloat32(map[string]any{"k": int(2)}, "k", 0.5) != float32(2.0) {
			t.Error("expected 2.0 for int")
		}
		if extractFloat32(map[string]any{"k": "invalid"}, "k", 0.5) != 0.5 {
			t.Error("expected default for invalid string")
		}

		// extractFloat32Slice
		if extractFloat32Slice(nil, "k") != nil {
			t.Error("expected nil for nil map")
		}
		if extractFloat32Slice(map[string]any{"k": nil}, "k") != nil {
			t.Error("expected nil for nil value")
		}
		f32s := []float32{1.0, 2.0}
		if len(extractFloat32Slice(map[string]any{"k": f32s}, "k")) != 2 {
			t.Error("expected []float32 slice")
		}
		f64s := []float64{1.0, 2.0}
		if len(extractFloat32Slice(map[string]any{"k": f64s}, "k")) != 2 {
			t.Error("expected converted []float64 slice")
		}
		anys := []any{float64(1.0), float32(2.0), int(3)}
		if len(extractFloat32Slice(map[string]any{"k": anys}, "k")) != 3 {
			t.Error("expected converted []any slice")
		}
		anysInvalid := []any{"invalid-element"}
		if extractFloat32Slice(map[string]any{"k": anysInvalid}, "k") != nil {
			t.Error("expected nil for invalid element in []any")
		}
		if extractFloat32Slice(map[string]any{"k": "not-a-slice"}, "k") != nil {
			t.Error("expected nil for non-slice")
		}
	})

	t.Run("jsonToolResult error on unmarshalable value", func(t *testing.T) {
		ch := make(chan int)
		_, err := jsonToolResult(ch)
		if err == nil {
			t.Fatal("expected error on unmarshalable value (chan), got nil")
		}
	})
}
