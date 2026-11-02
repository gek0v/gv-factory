// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// mockServerDB implements brain.DBPool for testing ov-server brain endpoints.
type mockServerDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	pingFn     func(ctx context.Context) error
}

func (m *mockServerDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockServerRow{err: pgx.ErrNoRows}
}

func (m *mockServerDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockServerRows{}, nil
}

func (m *mockServerDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *mockServerDB) Close() {}

type mockServerRow struct {
	values []any
	err    error
}

func (r *mockServerRow) Scan(dest ...any) error {
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

type mockServerRows struct {
	data [][]any
	idx  int
	err  error
}

func (m *mockServerRows) Close()                                       {}
func (m *mockServerRows) Err() error                                   { return m.err }
func (m *mockServerRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockServerRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockServerRows) Next() bool {
	if m.idx < len(m.data) {
		m.idx++
		return true
	}
	return false
}
func (m *mockServerRows) Scan(dest ...any) error {
	row := &mockServerRow{values: m.data[m.idx-1]}
	return row.Scan(dest...)
}
func (m *mockServerRows) Values() ([]any, error) { return nil, nil }
func (m *mockServerRows) RawValues() [][]byte    { return nil }
func (m *mockServerRows) Conn() *pgx.Conn        { return nil }
func (m *mockServerRows) TypeMap() *pgtype.Map   { return nil }

func setupTestServer(brainSvc *brain.Service) http.Handler {
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	orch := newMockOrchestrator()
	return NewServerMux(brainSvc, gw, nil, nil, orch, nil, nil, nil)
}

func TestBrainEndpoints_NilService(t *testing.T) {
	mux := setupTestServer(nil)

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/api/v1/brain/search", `{"query":"auth"}`},
		{"POST", "/api/v1/brain/sync/adrs", `{}`},
		{"POST", "/api/v1/brain/index", `{"path":"."}`},
		{"GET", "/api/v1/brain/stats", ""},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if ep.body != "" {
				bodyReader = bytes.NewReader([]byte(ep.body))
			} else {
				bodyReader = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(ep.method, ep.path, bodyReader)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusInternalServerError && w.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 500 or 503 for nil brain service, got %d. Body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestBrainEndpoints_MethodNotAllowed(t *testing.T) {
	mockDB := &mockServerDB{}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mockDB)
	mux := setupTestServer(brainSvc)

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/brain/search"},
		{"DELETE", "/api/v1/brain/search"},
		{"GET", "/api/v1/brain/sync/adrs"},
		{"GET", "/api/v1/brain/index"},
		{"POST", "/api/v1/brain/stats"},
		{"DELETE", "/api/v1/brain/stats"},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected 405 Method Not Allowed, got %d", w.Code)
			}
		})
	}
}

func TestBrainEndpoints_Search_Validation(t *testing.T) {
	mockDB := &mockServerDB{}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mockDB)
	mux := setupTestServer(brainSvc)

	tests := []struct {
		name       string
		payload    string
		expectCode int
	}{
		{
			name:       "invalid json",
			payload:    `{invalid`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "empty query",
			payload:    `{"query": "   "}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "invalid category",
			payload:    `{"query": "auth", "category": "unknown_cat"}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "negative limit",
			payload:    `{"query": "auth", "limit": -1}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "alpha less than 0",
			payload:    `{"query": "auth", "alpha": -0.1}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "alpha greater than 1",
			payload:    `{"query": "auth", "alpha": 1.5}`,
			expectCode: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/search", bytes.NewBufferString(tc.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.expectCode {
				t.Fatalf("expected status %d, got %d: %s", tc.expectCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestBrainEndpoints_Search_Success(t *testing.T) {
	now := time.Now().UTC()
	mockDB := &mockServerDB{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			// Check if query is for codebase_knowledge or architectural_rules
			if len(args) > 0 {
				return &mockServerRows{
					data: [][]any{
						{"id-1", "internal/auth/service.go", "ValidateToken", "func", "func ValidateToken()", "hash1", now, float32(0.1), float32(0.9)},
					},
				}, nil
			}
			return &mockServerRows{}, nil
		},
	}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mockDB)
	mux := setupTestServer(brainSvc)

	t.Run("search all category returns ranked results", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/search", bytes.NewBufferString(`{
			"query": "ValidateToken",
			"category": "all",
			"limit": 5,
			"alpha": 0.7
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var results []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
			t.Fatalf("failed to decode results array: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least 1 result")
		}
		if results[0]["category"] == nil {
			t.Error("expected category field in search result")
		}
		if results[0]["score"] == nil {
			t.Error("expected score field in search result")
		}
	})

	t.Run("search knowledge category", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/search", bytes.NewBufferString(`{
			"query": "ValidateToken",
			"category": "knowledge"
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("search rules category", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/search", bytes.NewBufferString(`{
			"query": "SEC-001",
			"category": "rules"
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("search episodes category", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/search", bytes.NewBufferString(`{
			"query": "task execution",
			"category": "episodes"
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestBrainEndpoints_SyncADRs(t *testing.T) {
	tempDir := t.TempDir()
	adrPath := filepath.Join(tempDir, "0001-test-adr.md")
	adrContent := `# 1. Test ADR Decision
**Estado:** Aprobado

## Contexto
Arquitectura requerida.
`
	if err := os.WriteFile(adrPath, []byte(adrContent), 0644); err != nil {
		t.Fatalf("failed to write temp ADR: %v", err)
	}

	mockDB := &mockServerDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			// For rules upsert
			return &mockServerRow{err: pgx.ErrNoRows}
		},
	}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mockDB)
	mux := setupTestServer(brainSvc)

	t.Run("sync adrs with specified path", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"path": tempDir})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/sync/adrs", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var summary map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
			t.Fatalf("failed to decode ADR sync summary: %v", err)
		}
		if summary["total_files"] == nil {
			t.Error("expected total_files in sync response")
		}
	})

	t.Run("sync adrs invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/sync/adrs", bytes.NewReader([]byte(`{invalid`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("sync adrs non-existent path returns error", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"path": filepath.Join(tempDir, "does-not-exist")})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/sync/adrs", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500 on non-existent dir, got %d", w.Code)
		}
	})
}

func TestBrainEndpoints_Index(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(sourceFile, []byte("package main\n\nfunc Run() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	mockDB := &mockServerDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockServerRow{values: []any{"id-1", sourceFile, "Run", "func", "content", "hash", time.Now()}}
		},
	}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mockDB)
	mux := setupTestServer(brainSvc)

	t.Run("index valid path dry run", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{
			"path":       tempDir,
			"exclusions": []string{"vendor"},
			"dry_run":    true,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/index", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var res brain.IndexResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode IndexResult: %v", err)
		}
		if res.TotalFilesScanned < 1 {
			t.Errorf("expected at least 1 file scanned, got %d", res.TotalFilesScanned)
		}
	})

	t.Run("index invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/index", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("index non-existent directory", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"path": filepath.Join(tempDir, "does-not-exist")})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/index", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", w.Code)
		}
	})
}

func TestBrainEndpoints_Stats(t *testing.T) {
	mockDB := &mockServerDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockServerRow{values: []any{42, 10, 7}}
		},
	}
	brainSvc := brain.NewServiceWithDB(brain.DefaultConfig(), mockDB)
	mux := setupTestServer(brainSvc)

	t.Run("stats success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/brain/stats", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var stats brain.Stats
		if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
			t.Fatalf("failed to decode stats: %v", err)
		}
		if stats.IndexedKnowledge != 42 || stats.ActiveRules != 10 || stats.TaskEpisodes != 7 {
			t.Errorf("unexpected stats: %+v", stats)
		}
	})

	t.Run("stats database error", func(t *testing.T) {
		errDB := &mockServerDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockServerRow{err: errors.New("query failure")}
			},
		}
		errSvc := brain.NewServiceWithDB(brain.DefaultConfig(), errDB)
		errMux := setupTestServer(errSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/brain/stats", nil)
		w := httptest.NewRecorder()
		errMux.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on db error, got %d", w.Code)
		}
	})
}

func TestBrainEndpoints_CoverageBoost(t *testing.T) {
	t.Run("search error handling for specific categories", func(t *testing.T) {
		errDB := &mockServerDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("db query error")
			},
		}
		errSvc := brain.NewServiceWithDB(brain.DefaultConfig(), errDB)
		mux := setupTestServer(errSvc)

		for _, cat := range []string{"knowledge", "rules", "episodes"} {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/brain/search", bytes.NewBufferString(`{
				"query": "test",
				"category": "`+cat+`"
			}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500 for cat %s, got %d", cat, w.Code)
			}
		}
	})

	t.Run("helpers coverage", func(t *testing.T) {
		short := truncateSummary("short text", 50)
		if short != "short text" {
			t.Errorf("expected 'short text', got %q", short)
		}
		withNewlines := truncateSummary("line1\nline2\nline3", 10)
		if len(withNewlines) > 10 {
			t.Errorf("expected max 10 chars, got %q", withNewlines)
		}

		vecEmpty := computeDeterministicEmbedding("", 1536)
		if len(vecEmpty) != 1536 || vecEmpty[0] != 1.0 {
			t.Errorf("expected unit vector for empty text")
		}
	})
}

