// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestMemoryCLI_UsageAndHelp(t *testing.T) {
	ctx := context.Background()

	t.Run("memory without args shows usage", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory"})
		if code != 0 {
			t.Fatalf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "query") || !strings.Contains(out.String(), "sync-adrs") || !strings.Contains(out.String(), "index") || !strings.Contains(out.String(), "stats") {
			t.Errorf("expected usage with all subcommands, got: %s", out.String())
		}
	})

	t.Run("memory unknown subcommand returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "unknown-sub"})
		if code != 2 {
			t.Fatalf("expected 2, got %d", code)
		}
	})
}

func TestMemoryCLI_Query(t *testing.T) {
	ctx := context.Background()

	t.Run("query with category and hybrid flags local fallback", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "auth", "--category", "rules", "--hybrid", "--limit", "3", "--alpha", "0.5"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Out: %s, Err: %s", code, out.String(), errBuf.String())
		}
		if !strings.Contains(out.String(), "Semantic query:") {
			t.Errorf("expected table output, got: %s", out.String())
		}
	})

	t.Run("query with remote server success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/brain/search" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]MemoryQueryResult{
				{
					Category:   "rules",
					SymbolName: "RULE-SEC-001",
					Summary:    "Authentication check",
					Score:      0.95,
				},
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "auth", "--server", ts.URL, "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var res []MemoryQueryResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if len(res) != 1 || res[0].SymbolName != "RULE-SEC-001" {
			t.Errorf("unexpected results: %+v", res)
		}
	})

	t.Run("query with remote server error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "brain down"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "auth", "--server", ts.URL})
		if code == 0 {
			t.Fatalf("expected non-zero on remote server error, got 0")
		}
	})
}

func TestMemoryCLI_Index(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(sourceFile, []byte("package main\n\nfunc Main() {}\n"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	t.Run("index local dry run formatted", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "index", tempDir, "--exclude", "vendor,tmp", "--dry-run"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if !strings.Contains(out.String(), "Repository AST Indexing") && !strings.Contains(out.String(), "Indexed") && !strings.Contains(out.String(), "Scanned") {
			t.Errorf("expected index summary, got: %s", out.String())
		}
	})

	t.Run("index local dry run json", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "index", tempDir, "--dry-run", "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var res brain.IndexResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("failed to parse index json: %v", err)
		}
	})

	t.Run("index remote server success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/brain/index" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(brain.IndexResult{
				TotalFilesScanned: 10,
				IndexedFiles:      5,
				ExtractedSymbols:  20,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "index", tempDir, "--server", ts.URL, "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var res brain.IndexResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if res.TotalFilesScanned != 10 {
			t.Errorf("expected 10 scanned, got %d", res.TotalFilesScanned)
		}
	})

	t.Run("index remote server failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "index failed"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "index", tempDir, "--server", ts.URL})
		if code == 0 {
			t.Fatalf("expected non-zero on remote server failure, got 0")
		}
	})
}

func TestMemoryCLI_SyncADRs(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	adrPath := filepath.Join(tempDir, "0001-test.md")
	_ = os.WriteFile(adrPath, []byte("# 1. Test\n**Estado:** Aprobado\n\n## Contexto\nTest\n"), 0644)

	t.Run("sync-adrs local dry run formatted", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "sync-adrs", tempDir, "--dry-run"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if !strings.Contains(out.String(), "ADR Synchronization") && !strings.Contains(out.String(), "Files") {
			t.Errorf("expected sync output, got: %s", out.String())
		}
	})

	t.Run("sync-adrs local dry run json", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "sync-adrs", tempDir, "--dry-run", "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var res brain.ADRSyncResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if res.TotalFiles < 1 {
			t.Errorf("expected at least 1 total file, got %d", res.TotalFiles)
		}
	})

	t.Run("sync-adrs remote server success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/brain/sync/adrs" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(brain.ADRSyncResult{
				TotalFiles: 4,
				Created:    2,
				Updated:    1,
				Skipped:    1,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "sync-adrs", tempDir, "--server", ts.URL, "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var res brain.ADRSyncResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if res.TotalFiles != 4 || res.Created != 2 {
			t.Errorf("unexpected sync result: %+v", res)
		}
	})

	t.Run("sync-adrs remote server failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "sync failed"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "sync-adrs", tempDir, "--server", ts.URL})
		if code == 0 {
			t.Fatalf("expected non-zero on remote server error, got 0")
		}
	})
}

func TestMemoryCLI_Stats(t *testing.T) {
	ctx := context.Background()

	t.Run("stats local fallback formatted", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "stats"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if !strings.Contains(out.String(), "Brain") && !strings.Contains(out.String(), "Knowledge") {
			t.Errorf("expected stats output, got: %s", out.String())
		}
	})

	t.Run("stats local fallback json", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "stats", "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var stats brain.Stats
		if err := json.Unmarshal(out.Bytes(), &stats); err != nil {
			t.Fatalf("failed to decode stats json: %v", err)
		}
	})

	t.Run("stats remote server success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/brain/stats" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(brain.Stats{
				IndexedKnowledge: 25,
				ActiveRules:      8,
				TaskEpisodes:     14,
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "stats", "--server", ts.URL, "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}

		var stats brain.Stats
		if err := json.Unmarshal(out.Bytes(), &stats); err != nil {
			t.Fatalf("failed to decode stats json: %v", err)
		}
		if stats.IndexedKnowledge != 25 || stats.ActiveRules != 8 || stats.TaskEpisodes != 14 {
			t.Errorf("unexpected stats: %+v", stats)
		}
	})

	t.Run("stats remote server failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "stats failed"})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "stats", "--server", ts.URL})
		if code == 0 {
			t.Fatalf("expected non-zero on remote server error, got 0")
		}
	})
}

// mockCliDB implements brain.DBPool for testing CLI memory commands with a live-like service.
type mockCliDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	pingFn     func(ctx context.Context) error
}

func (m *mockCliDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockCliRow{err: pgx.ErrNoRows}
}

func (m *mockCliDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockCliRows{}, nil
}

func (m *mockCliDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *mockCliDB) Close() {}

type mockCliRow struct {
	values []any
	err    error
}

func (r *mockCliRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i >= len(r.values) {
			break
		}
		val := r.values[i]
		if val == nil {
			continue
		}
		switch target := d.(type) {
		case *string:
			if s, ok := val.(string); ok {
				*target = s
			}
		case *int:
			if iv, ok := val.(int); ok {
				*target = iv
			}
		case *int64:
			if iv, ok := val.(int64); ok {
				*target = iv
			}
		case *float32:
			if f, ok := val.(float32); ok {
				*target = f
			}
		case *time.Time:
			if t, ok := val.(time.Time); ok {
				*target = t
			}
		}
	}
	return nil
}

type mockCliRows struct {
	data [][]any
	idx  int
	err  error
}

func (m *mockCliRows) Close()                                       {}
func (m *mockCliRows) Err() error                                   { return m.err }
func (m *mockCliRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockCliRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockCliRows) Next() bool {
	if m.idx < len(m.data) {
		m.idx++
		return true
	}
	return false
}
func (m *mockCliRows) Scan(dest ...any) error {
	row := &mockCliRow{values: m.data[m.idx-1]}
	return row.Scan(dest...)
}
func (m *mockCliRows) Values() ([]any, error) { return nil, nil }
func (m *mockCliRows) RawValues() [][]byte    { return nil }
func (m *mockCliRows) Conn() *pgx.Conn        { return nil }
func (m *mockCliRows) TypeMap() *pgtype.Map   { return nil }

func TestMemoryCLI_ConnectedService(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	mockDB := &mockCliDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockCliRow{values: []any{100, 20, 15}}
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "codebase_knowledge") {
				return &mockCliRows{
					data: [][]any{
						{"id-1", "pkg/auth.go", "AuthFunc", "func", "content of auth", "hash1", now, float32(0.2), float32(0.8)},
					},
				}, nil
			}
			if strings.Contains(sql, "architectural_rules") {
				return &mockCliRows{
					data: [][]any{
						{"id-2", "SEC-002", "Security Rule", "Must use TLS", "ERROR", now, now, float32(0.3), float32(0.7)},
					},
				}, nil
			}
			return &mockCliRows{}, nil
		},
	}

	oldFactory := newBrainService
	defer func() { newBrainService = oldFactory }()
	newBrainService = func(ctx context.Context) (*brain.Service, error) {
		return brain.NewServiceWithDB(brain.DefaultConfig(), mockDB), nil
	}

	t.Run("query connected hybrid search knowledge and rules", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "AuthFunc", "--hybrid", "--category", "all", "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		var results []MemoryQueryResult
		if err := json.Unmarshal(out.Bytes(), &results); err != nil {
			t.Fatalf("failed to decode results: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least 1 result from connected service")
		}
	})

	t.Run("query connected non-hybrid vector search", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "AuthFunc", "--category", "knowledge"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if !strings.Contains(out.String(), "pkg/auth.go") {
			t.Errorf("expected pkg/auth.go in output, got: %s", out.String())
		}
	})

	t.Run("stats connected service", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "stats", "--json"})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		var s brain.Stats
		if err := json.Unmarshal(out.Bytes(), &s); err != nil {
			t.Fatalf("failed to parse stats: %v", err)
		}
		if s.IndexedKnowledge != 100 || s.ActiveRules != 20 || s.TaskEpisodes != 15 {
			t.Errorf("unexpected stats: %+v", s)
		}
	})

	t.Run("truncate and helpers", func(t *testing.T) {
		s := truncateString("short", 20)
		if s != "short" {
			t.Errorf("expected 'short', got %s", s)
		}
		long := truncateString("this is a very long string that should be cut off", 15)
		if len(long) > 15 {
			t.Errorf("expected <= 15 chars, got %s", long)
		}
	})

	t.Run("reorderArgs edge cases", func(t *testing.T) {
		args := []string{"--", "positional"}
		re := reorderArgs(args, nil)
		if len(re) != 1 || re[0] != "positional" {
			t.Errorf("unexpected reorderArgs with --: %v", re)
		}

		argsEqual := []string{"cmd", "--server=http://localhost:8080", "arg"}
		reEqual := reorderArgs(argsEqual, map[string]bool{"server": true})
		if len(reEqual) != 3 {
			t.Errorf("unexpected reorderArgs with =: %v", reEqual)
		}
	})

	t.Run("query invalid flags and empty query", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "-invalid-flag-123"})
		if code != 2 {
			t.Errorf("expected 2 on invalid flag, got %d", code)
		}

		codeEmpty := app.Run(ctx, []string{"memory", "query", "   "})
		if codeEmpty != 2 {
			t.Errorf("expected 2 on empty query, got %d", codeEmpty)
		}
	})

	t.Run("query empty results table output", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "auth", "--threshold", "0.99"})
		if code != 0 {
			t.Errorf("expected 0, got %d", code)
		}
		if !strings.Contains(out.String(), "No matching symbols") {
			t.Errorf("expected no matching symbols message, got: %s", out.String())
		}
	})

	t.Run("query table formatting with long paths and symbols", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]MemoryQueryResult{
				{
					Category:   "knowledge",
					FilePath:   "very/long/nested/directory/structure/and/file_name_that_is_quite_long.go",
					SymbolName: "VeryLongSymbolNameExceedingTwentyTwoCharactersHere",
					Summary:    "Short summary here",
					Score:      0.88,
				},
			})
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "long", "--server", ts.URL})
		if code != 0 {
			t.Fatalf("expected 0, got %d. Err: %s", code, errBuf.String())
		}
		if !strings.Contains(out.String(), "...") {
			t.Errorf("expected truncated path in output, got: %s", out.String())
		}
	})

	t.Run("query server invalid response payload", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{not-json-array`))
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "term", "--server", ts.URL})
		if code != 1 {
			t.Errorf("expected 1 on bad json response, got %d", code)
		}
	})

	t.Run("index and sync invalid flags and bad path", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)

		codeIdxFlag := app.Run(ctx, []string{"memory", "index", "-invalid-flag"})
		if codeIdxFlag != 2 {
			t.Errorf("expected 2 on invalid index flag, got %d", codeIdxFlag)
		}

		codeIdxErr := app.Run(ctx, []string{"memory", "index", "path-that-does-not-exist-xyz"})
		if codeIdxErr != 1 {
			t.Errorf("expected 1 on non-existent index dir, got %d", codeIdxErr)
		}

		codeSyncFlag := app.Run(ctx, []string{"memory", "sync-adrs", "-invalid-flag"})
		if codeSyncFlag != 2 {
			t.Errorf("expected 2 on invalid sync flag, got %d", codeSyncFlag)
		}

		codeSyncErr := app.Run(ctx, []string{"memory", "sync-adrs", "path-that-does-not-exist-xyz"})
		if codeSyncErr != 1 {
			t.Errorf("expected 1 on non-existent sync dir, got %d", codeSyncErr)
		}

		codeStatsFlag := app.Run(ctx, []string{"memory", "stats", "-invalid-flag"})
		if codeStatsFlag != 2 {
			t.Errorf("expected 2 on invalid stats flag, got %d", codeStatsFlag)
		}
	})

	t.Run("index and sync bad json from server", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{bad-json`))
		}))
		defer ts.Close()

		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)

		codeIdx := app.Run(ctx, []string{"memory", "index", "--server", ts.URL})
		if codeIdx != 1 {
			t.Errorf("expected 1 on bad json from index server, got %d", codeIdx)
		}

		codeSync := app.Run(ctx, []string{"memory", "sync-adrs", "--server", ts.URL})
		if codeSync != 1 {
			t.Errorf("expected 1 on bad json from sync server, got %d", codeSync)
		}

		codeStats := app.Run(ctx, []string{"memory", "stats", "--server", ts.URL})
		if codeStats != 1 {
			t.Errorf("expected 1 on bad json from stats server, got %d", codeStats)
		}
	})

	t.Run("fallbackASTQuery category filter branches", func(t *testing.T) {
		know := fallbackASTQuery("approval", "knowledge")
		if len(know) == 0 {
			t.Errorf("expected knowledge results")
		}
		rules := fallbackASTQuery("rule", "rules")
		if len(rules) == 0 {
			t.Errorf("expected rules results")
		}
		ep := fallbackASTQuery("run", "episodes")
		if len(ep) == 0 {
			t.Errorf("expected episodes results")
		}
	})
}
