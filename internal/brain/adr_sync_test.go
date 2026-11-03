// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

func TestSyncADRs_CreateNewRules(t *testing.T) {
	tempDir := t.TempDir()

	adr1 := `# ADR 0001: Microkernel Extism

* **Estado:** Aceptado

## Context
Context 1.
## Decisión
Decision 1.
## Consecuencias
Consequences 1.
`
	adr2 := `# ADR 0002: Hatchet Workflow

* **Estado:** Aceptado

## Context
Context 2.
## Decisión
Decision 2.
## Consecuencias
Consequences 2.
`
	if err := os.WriteFile(filepath.Join(tempDir, "0001-microkernel.md"), []byte(adr1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "0002-hatchet.md"), []byte(adr2), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	createdRules := make(map[string]ArchitecturalRule)

	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			mu.Lock()
			defer mu.Unlock()

			// Check query type
			if len(args) == 1 { // GetRuleByCode
				code := args[0].(string)
				if r, exists := createdRules[code]; exists {
					return &mockRow{values: []any{r.ID, r.RuleCode, r.Title, r.Description, r.Severity, (*pgvector.Vector)(nil), r.CreatedAt, r.UpdatedAt}}
				}
				return &mockRow{err: pgx.ErrNoRows}
			}

			// CreateRule (args: rule_code, title, description, severity, vec)
			code := args[0].(string)
			title := args[1].(string)
			desc := args[2].(string)
			sev := args[3].(string)
			now := time.Now()
			r := ArchitecturalRule{
				ID:          "mock-uuid-" + code,
				RuleCode:    code,
				Title:       title,
				Description: desc,
				Severity:    sev,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			createdRules[code] = r
			return &mockRow{values: []any{r.ID, r.RuleCode, r.Title, r.Description, r.Severity, r.CreatedAt, r.UpdatedAt}}
		},
	}

	svc := &Service{
		cfg: DefaultConfig(),
		db:  mock,
	}

	ctx := context.Background()
	result, err := svc.SyncADRs(ctx, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalFiles != 2 {
		t.Errorf("expected 2 files, got %d", result.TotalFiles)
	}
	if result.Created != 2 {
		t.Errorf("expected 2 created rules, got %d", result.Created)
	}
	if result.Updated != 0 {
		t.Errorf("expected 0 updated, got %d", result.Updated)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}
}

func TestSyncADRs_IdempotentSkipAndUpdate(t *testing.T) {
	tempDir := t.TempDir()

	adr1 := `# ADR 0001: Microkernel Extism

* **Estado:** Aceptado

## Context
Context 1.
## Decisión
Decision 1.
## Consecuencias
Consequences 1.
`
	filePath := filepath.Join(tempDir, "0001-microkernel.md")
	if err := os.WriteFile(filePath, []byte(adr1), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	store := make(map[string]ArchitecturalRule)

	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			mu.Lock()
			defer mu.Unlock()

			if len(args) == 1 { // GetRuleByCode
				code := args[0].(string)
				if r, exists := store[code]; exists {
					return &mockRow{values: []any{r.ID, r.RuleCode, r.Title, r.Description, r.Severity, (*pgvector.Vector)(nil), r.CreatedAt, r.UpdatedAt}}
				}
				return &mockRow{err: pgx.ErrNoRows}
			}

			if len(args) == 5 { // CreateRule or UpdateRule
				code := args[0].(string)
				title := args[1].(string)
				desc := args[2].(string)
				sev := args[3].(string)
				now := time.Now()
				r := ArchitecturalRule{
					ID:          "mock-uuid-" + code,
					RuleCode:    code,
					Title:       title,
					Description: desc,
					Severity:    sev,
					CreatedAt:   now,
					UpdatedAt:   now,
				}
				store[code] = r
				return &mockRow{values: []any{r.ID, r.RuleCode, r.Title, r.Description, r.Severity, r.CreatedAt, r.UpdatedAt}}
			}

			return &mockRow{err: pgx.ErrNoRows}
		},
	}

	svc := &Service{
		cfg: DefaultConfig(),
		db:  mock,
	}

	ctx := context.Background()

	// 1st run: created
	r1, err := svc.SyncADRs(ctx, tempDir)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if r1.Created != 1 || r1.Skipped != 0 {
		t.Errorf("1st run: expected created=1, skipped=0; got created=%d, skipped=%d", r1.Created, r1.Skipped)
	}

	// 2nd run: identical content -> skipped
	r2, err := svc.SyncADRs(ctx, tempDir)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if r2.Created != 0 || r2.Updated != 0 || r2.Skipped != 1 {
		t.Errorf("2nd run: expected created=0, updated=0, skipped=1; got created=%d, updated=%d, skipped=%d", r2.Created, r2.Updated, r2.Skipped)
	}

	// Modify ADR decision
	adr1Modified := `# ADR 0001: Microkernel Extism

* **Estado:** Aceptado

## Context
Context 1.
## Decisión
Decision 1 MODIFIED.
## Consecuencias
Consequences 1.
`
	if err := os.WriteFile(filePath, []byte(adr1Modified), 0644); err != nil {
		t.Fatal(err)
	}

	// 3rd run: updated
	r3, err := svc.SyncADRs(ctx, tempDir)
	if err != nil {
		t.Fatalf("third run failed: %v", err)
	}
	if r3.Updated != 1 || r3.Created != 0 || r3.Skipped != 0 {
		t.Errorf("3rd run: expected updated=1, got created=%d, updated=%d, skipped=%d", r3.Created, r3.Updated, r3.Skipped)
	}
}

func TestSyncADRs_ObsoleteHandlingModes(t *testing.T) {
	tempDir := t.TempDir()

	adrSuperseded := `# ADR 0003: Old Isolation

* **Estado:** Superado por ADR-0010

## Context
Old context.
## Decisión
Old decision.
`
	if err := os.WriteFile(filepath.Join(tempDir, "0003-old.md"), []byte(adrSuperseded), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("ObsoleteModeMarkInfo creates rule with INFO severity", func(t *testing.T) {
		var captured ArchitecturalRule
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if len(args) == 1 {
					return &mockRow{err: pgx.ErrNoRows}
				}
				captured = ArchitecturalRule{
					RuleCode: args[0].(string),
					Severity: args[3].(string),
				}
				return &mockRow{values: []any{"uuid-1", captured.RuleCode, "Title", "Desc", captured.Severity, time.Now(), time.Now()}}
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}

		res, err := svc.SyncADRs(ctx, tempDir, WithObsoleteMode(ObsoleteModeMarkInfo))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Created != 1 {
			t.Errorf("expected created=1, got %d", res.Created)
		}
		if captured.Severity != "INFO" {
			t.Errorf("expected INFO severity for obsolete rule, got %q", captured.Severity)
		}
		if res.Obsolete != 1 {
			t.Errorf("expected Obsolete count 1, got %d", res.Obsolete)
		}
	})

	t.Run("ObsoleteModeSkip skips rule without DB insert", func(t *testing.T) {
		mock := &mockDB{}
		svc := &Service{cfg: DefaultConfig(), db: mock}

		res, err := svc.SyncADRs(ctx, tempDir, WithObsoleteMode(ObsoleteModeSkip))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Created != 0 || res.Updated != 0 {
			t.Errorf("expected no create or update, got c=%d, u=%d", res.Created, res.Updated)
		}
		if res.Skipped != 1 {
			t.Errorf("expected skipped=1, got %d", res.Skipped)
		}
	})

	t.Run("ObsoleteModeDelete removes rule if exists", func(t *testing.T) {
		deleted := false
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if len(args) == 1 && args[0] == "ADR-0003" {
					deleted = true
					return &mockRow{values: []any{"uuid-1"}}
				}
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}

		res, err := svc.SyncADRs(ctx, tempDir, WithObsoleteMode(ObsoleteModeDelete))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !deleted {
			t.Error("expected rule to be deleted")
		}
		if res.Obsolete != 1 {
			t.Errorf("expected obsolete=1, got %d", res.Obsolete)
		}
	})
}

func TestSyncADRs_EmbeddingOption(t *testing.T) {
	tempDir := t.TempDir()
	adr := `# ADR 0001: Vector Embedded Rule

* **Estado:** Aceptado

## Context
Vector context.
## Decisión
Vector decision.
`
	if err := os.WriteFile(filepath.Join(tempDir, "0001-vector.md"), []byte(adr), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var capturedVec *pgvector.Vector

	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) == 1 {
				return &mockRow{err: pgx.ErrNoRows}
			}
			capturedVec = args[4].(*pgvector.Vector)
			return &mockRow{values: []any{"uuid-1", args[0], args[1], args[2], args[3], time.Now(), time.Now()}}
		},
	}
	svc := &Service{cfg: DefaultConfig(), db: mock}

	mockEmbedGen := func(ctx context.Context, text string) ([]float32, error) {
		vec := make([]float32, RuleVectorDimensions)
		vec[0] = 0.99
		return vec, nil
	}

	res, err := svc.SyncADRs(ctx, tempDir, WithEmbeddingFunc(mockEmbedGen))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Created != 1 {
		t.Errorf("expected 1 created, got %d", res.Created)
	}
	if capturedVec == nil {
		t.Fatal("expected embedding vector in DB insert, got nil")
	}
	if capturedVec.Slice()[0] != 0.99 {
		t.Errorf("unexpected vector value: %v", capturedVec.Slice()[0])
	}
}

func TestSyncADRs_DryRun(t *testing.T) {
	tempDir := t.TempDir()
	adr := `# ADR 0001: Dry Run Rule

* **Estado:** Aceptado

## Context
Dry run context.
## Decisión
Dry run decision.
`
	if err := os.WriteFile(filepath.Join(tempDir, "0001-dryrun.md"), []byte(adr), 0644); err != nil {
		t.Fatal(err)
	}

	dbCalled := false
	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			dbCalled = true
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	svc := &Service{cfg: DefaultConfig(), db: mock}

	ctx := context.Background()
	res, err := svc.SyncADRs(ctx, tempDir, WithDryRun(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dbCalled {
		t.Error("DB should not be invoked when DryRun is active")
	}
	if res.Created != 1 {
		t.Errorf("expected 1 simulated created rule, got %d", res.Created)
	}
}

func TestSyncADRs_RealDocsADRDirectory(t *testing.T) {
	adrDir := filepath.Join("..", "..", "docs", "adr")

	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) == 1 {
				return &mockRow{err: pgx.ErrNoRows}
			}
			return &mockRow{values: []any{"uuid-real", args[0], args[1], args[2], args[3], time.Now(), time.Now()}}
		},
	}
	svc := &Service{cfg: DefaultConfig(), db: mock}

	ctx := context.Background()
	res, err := svc.SyncADRs(ctx, adrDir)
	if err != nil {
		t.Fatalf("failed syncing real docs/adr: %v", err)
	}

	if res.TotalFiles < 10 {
		t.Errorf("expected at least 10 ADRs in docs/adr, found %d", res.TotalFiles)
	}
	if len(res.Errors) > 0 {
		t.Errorf("unexpected errors during sync of real ADRs: %v", res.Errors)
	}
	if res.Created < 10 {
		t.Errorf("expected >= 10 created rules, got %d", res.Created)
	}
}

func TestSyncADRs_OfficialFiveADRs(t *testing.T) {
	adrDir := filepath.Join("..", "..", "docs", "adr")

	expectedRules := map[string]struct {
		filename string
		titleSub string
	}{
		"ADR-0001": {"0001-wasm-extism-plugins.md", "Microkernel Extism WASM"},
		"ADR-0002": {"0002-pgvector-brain-memory.md", "pgvector"},
		"ADR-0003": {"0003-sequential-merge-queue-merge-tree.md", "merge-tree"},
		"ADR-0004": {"0004-hitl-hmac-governance.md", "Human-in-the-Loop"},
		"ADR-0005": {"0005-multi-provider-dynamic-llm.md", "conectores LLM"},
	}

	for ruleCode, meta := range expectedRules {
		path := filepath.Join(adrDir, meta.filename)
		doc, err := ParseADRFile(path)
		if err != nil {
			t.Fatalf("failed parsing official ADR %s (%s): %v", meta.filename, ruleCode, err)
		}
		if doc.RuleCode != ruleCode {
			t.Errorf("expected rule code %s for %s, got %s", ruleCode, meta.filename, doc.RuleCode)
		}
		if doc.Status != ADRStatusAccepted {
			t.Errorf("expected status Accepted for %s, got %s", ruleCode, doc.Status)
		}
		if doc.Severity != "ERROR" {
			t.Errorf("expected severity ERROR for accepted rule %s, got %s", ruleCode, doc.Severity)
		}
		if doc.Context == "" {
			t.Errorf("missing Context in %s", meta.filename)
		}
		if doc.Decision == "" {
			t.Errorf("missing Decision in %s", meta.filename)
		}
		if doc.Consequences == "" {
			t.Errorf("missing Consequences in %s", meta.filename)
		}
		if !strings.Contains(doc.Title, meta.titleSub) {
			t.Errorf("expected title to contain %q, got %q", meta.titleSub, doc.Title)
		}
	}
}

func TestSyncADRs_CoverageBoost(t *testing.T) {
	tempDir := t.TempDir()

	// Non-md file and directory
	if err := os.WriteFile(filepath.Join(tempDir, "readme.txt"), []byte("not an adr"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tempDir, "subfolder"), 0755); err != nil {
		t.Fatal(err)
	}

	adr1 := `# ADR 0001: First Rule

* **Estado:** Aceptado

## Context
Context
`
	adr2Colliding := `# ADR 0001: Colliding Rule

* **Estado:** Aceptado

## Context
Context
`
	adrBad := `Corrupt file without header
`
	if err := os.WriteFile(filepath.Join(tempDir, "0001-first.md"), []byte(adr1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "0001-second.md"), []byte(adr2Colliding), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "0003-bad.md"), []byte(adrBad), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("custom collision handler and error paths", func(t *testing.T) {
		customCalled := false
		customHandler := func(doc *ADRDocument, seen map[string]string) string {
			customCalled = true
			return doc.RuleCode + "-CUSTOM"
		}

		mockErrGen := func(ctx context.Context, text string) ([]float32, error) {
			return nil, errors.New("simulated embed error")
		}

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if len(args) == 1 {
					return &mockRow{err: pgx.ErrNoRows}
				}
				// Simulate create error on custom rule
				if args[0] == "ADR-0001-CUSTOM" {
					return &mockRow{err: errors.New("create db error")}
				}
				return &mockRow{values: []any{"uuid-1", args[0], args[1], args[2], args[3], time.Now(), time.Now()}}
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}

		res, err := svc.SyncADRs(ctx, tempDir,
			WithCollisionHandler(customHandler),
			WithEmbeddingFunc(mockErrGen),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !customCalled {
			t.Error("expected custom collision handler to be called")
		}
		if len(res.Errors) == 0 {
			t.Error("expected error entries in result")
		}
	})

	t.Run("directory read error", func(t *testing.T) {
		svc := &Service{cfg: DefaultConfig(), db: &mockDB{}}
		_, err := svc.SyncADRs(ctx, filepath.Join(tempDir, "non-existent-dir"))
		if err == nil {
			t.Fatal("expected error on non-existent dir, got nil")
		}
	})

	t.Run("db errors in get, update and delete", func(t *testing.T) {
		adrDep := `# ADR 0005: Obsolete Deprecated

* **Estado:** Deprecated

## Context
Old
`
		if err := os.WriteFile(filepath.Join(tempDir, "0005-dep.md"), []byte(adrDep), 0644); err != nil {
			t.Fatal(err)
		}

		// DB error on GetRuleByCode
		mockGetErr := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: errors.New("fatal connection error during get")}
			},
		}
		svcGetErr := &Service{cfg: DefaultConfig(), db: mockGetErr}
		resGet, err := svcGetErr.SyncADRs(ctx, tempDir)
		if err != nil {
			t.Fatalf("unexpected fatal: %v", err)
		}
		if len(resGet.Errors) == 0 {
			t.Error("expected get errors recorded")
		}

		// DB error on UpdateRule
		mockUpdateErr := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if len(args) == 1 {
					// Exists with different title -> triggers UpdateRule
					return &mockRow{values: []any{"uuid-1", args[0], "Old Title", "Old Desc", "ERROR", (*pgvector.Vector)(nil), time.Now(), time.Now()}}
				}
				return &mockRow{err: errors.New("update db error")}
			},
		}
		svcUpdateErr := &Service{cfg: DefaultConfig(), db: mockUpdateErr}
		resUp, err := svcUpdateErr.SyncADRs(ctx, tempDir)
		if err != nil {
			t.Fatalf("unexpected fatal: %v", err)
		}
		if len(resUp.Errors) == 0 {
			t.Error("expected update errors recorded")
		}
	})

	t.Run("nil pool returns ConnectionError", func(t *testing.T) {
		svc := &Service{
			cfg: Config{DatabaseURL: "postgres://user:pass@localhost:5432/db"},
		}
		_, err := svc.SyncADRs(ctx, tempDir)
		if err == nil {
			t.Fatal("expected ConnectionError on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected ConnectionError, got %T: %v", err, err)
		}
	})
}
