// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"
)

func mockScan(src []any, dest []any) error {
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
		case **pgvector.Vector:
			if v, ok := val.(*pgvector.Vector); ok {
				*target = v
			}
		}
	}
	return nil
}

type mockRow struct {
	values []any
	err    error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return mockScan(r.values, dest)
}

type mockRows struct {
	data [][]any
	idx  int
	err  error
}

func (m *mockRows) Close()                                       {}
func (m *mockRows) Err() error                                   { return m.err }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) Next() bool {
	if m.idx < len(m.data) {
		m.idx++
		return true
	}
	return false
}
func (m *mockRows) Scan(dest ...any) error {
	return mockScan(m.data[m.idx-1], dest)
}
func (m *mockRows) Values() ([]any, error) { return nil, nil }
func (m *mockRows) RawValues() [][]byte    { return nil }
func (m *mockRows) Conn() *pgx.Conn        { return nil }
func (m *mockRows) TypeMap() *pgtype.Map   { return nil }

type mockDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	pingFn     func(ctx context.Context) error
	closeFn    func()
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{err: pgx.ErrNoRows}
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

func (m *mockDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *mockDB) Close() {
	if m.closeFn != nil {
		m.closeFn()
	}
}

func TestArchitecturalRule_Validate(t *testing.T) {
	validRule := ArchitecturalRule{
		RuleCode:    "ARCH-001",
		Title:       "No circular dependencies",
		Description: "Packages must not have circular imports",
		Severity:    "ERROR",
	}

	t.Run("valid rule without embedding succeeds", func(t *testing.T) {
		r := validRule
		if err := r.Validate(); err != nil {
			t.Fatalf("expected valid rule to pass, got: %v", err)
		}
		if r.Severity != "ERROR" {
			t.Fatalf("expected default severity ERROR, got %q", r.Severity)
		}
	})

	t.Run("defaults severity to ERROR if omitted", func(t *testing.T) {
		r := validRule
		r.Severity = ""
		if err := r.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Severity != "ERROR" {
			t.Fatalf("expected severity to default to ERROR, got %q", r.Severity)
		}
	})

	t.Run("fails when rule_code is empty", func(t *testing.T) {
		r := validRule
		r.RuleCode = "   "
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for empty rule_code, got nil")
		}
	})

	t.Run("fails when title is empty", func(t *testing.T) {
		r := validRule
		r.Title = ""
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for empty title, got nil")
		}
	})

	t.Run("fails when description is empty", func(t *testing.T) {
		r := validRule
		r.Description = ""
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for empty description, got nil")
		}
	})

	t.Run("fails when embedding dimension is invalid", func(t *testing.T) {
		r := validRule
		r.Embedding = make([]float32, 512) // should be 1536
		err := r.Validate()
		if err == nil {
			t.Fatal("expected error for invalid vector dimension, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got %v", err)
		}
	})

	t.Run("succeeds when embedding has exactly 1536 dimensions", func(t *testing.T) {
		r := validRule
		r.Embedding = make([]float32, RuleVectorDimensions)
		if err := r.Validate(); err != nil {
			t.Fatalf("expected 1536-dim embedding to succeed, got: %v", err)
		}
	})
}

func TestRules_NilPoolHandling(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("CreateRule returns ConnectionError on nil pool", func(t *testing.T) {
		rule := ArchitecturalRule{
			RuleCode:    "ARCH-001",
			Title:       "Test",
			Description: "Test description",
		}
		_, err := svc.CreateRule(ctx, rule)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("GetRuleByCode returns ConnectionError on nil pool", func(t *testing.T) {
		_, err := svc.GetRuleByCode(ctx, "ARCH-001")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("ListRules returns ConnectionError on nil pool", func(t *testing.T) {
		_, err := svc.ListRules(ctx, 10)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("QueryRelevantRulesByVector returns ConnectionError on nil pool", func(t *testing.T) {
		vec := make([]float32, RuleVectorDimensions)
		_, err := svc.QueryRelevantRulesByVector(ctx, vec, 5)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})
}

func TestRules_InputValidation(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("CreateRule rejects invalid rule before DB access", func(t *testing.T) {
		invalidRule := ArchitecturalRule{
			RuleCode: "",
		}
		_, err := svc.CreateRule(ctx, invalidRule)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule, got %v", err)
		}
	})

	t.Run("GetRuleByCode rejects empty rule_code", func(t *testing.T) {
		_, err := svc.GetRuleByCode(ctx, "   ")
		if err == nil {
			t.Fatal("expected error for empty rule_code, got nil")
		}
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule, got %v", err)
		}
	})

	t.Run("QueryRelevantRulesByVector rejects invalid vector dimension", func(t *testing.T) {
		badVec := make([]float32, 100)
		_, err := svc.QueryRelevantRulesByVector(ctx, badVec, 5)
		if err == nil {
			t.Fatal("expected error for bad vector dimensions, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got %v", err)
		}
	})
}

func TestRules_MockDatabase(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateRule success", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
		vec := make([]float32, RuleVectorDimensions)
		vec[0] = 0.5
		rule := ArchitecturalRule{
			RuleCode:    "ARCH-001",
			Title:       "Sample Rule",
			Description: "Sample Description",
			Severity:    "ERROR",
			Embedding:   vec,
		}

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					values: []any{"uuid-1234", rule.RuleCode, rule.Title, rule.Description, rule.Severity, fixedTime, fixedTime},
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		created, err := svc.CreateRule(ctx, rule)
		if err != nil {
			t.Fatalf("unexpected error creating rule: %v", err)
		}
		if created.ID != "uuid-1234" {
			t.Fatalf("expected ID uuid-1234, got %s", created.ID)
		}
		if created.RuleCode != rule.RuleCode {
			t.Fatalf("expected RuleCode %s, got %s", rule.RuleCode, created.RuleCode)
		}
	})

	t.Run("CreateRule database error", func(t *testing.T) {
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: errors.New("unique violation")}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		rule := ArchitecturalRule{
			RuleCode:    "ARCH-002",
			Title:       "Error Rule",
			Description: "Will fail",
			Severity:    "ERROR",
		}

		_, err := svc.CreateRule(ctx, rule)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T", err)
		}
	})

	t.Run("GetRuleByCode success, not found, and db error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
		vec := pgvector.NewVector(make([]float32, RuleVectorDimensions))

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				code := args[0].(string)
				switch code {
				case "ARCH-001":
					return &mockRow{
						values: []any{"uuid-1234", "ARCH-001", "Title", "Desc", "ERROR", &vec, fixedTime, fixedTime},
					}
				case "ARCH-MISSING":
					return &mockRow{err: pgx.ErrNoRows}
				default:
					return &mockRow{err: errors.New("connection reset")}
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		rule, err := svc.GetRuleByCode(ctx, "ARCH-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rule.RuleCode != "ARCH-001" {
			t.Fatalf("expected rule code ARCH-001, got %s", rule.RuleCode)
		}

		_, err = svc.GetRuleByCode(ctx, "ARCH-MISSING")
		if !errors.Is(err, ErrRuleNotFound) {
			t.Fatalf("expected ErrRuleNotFound, got %v", err)
		}

		_, err = svc.GetRuleByCode(ctx, "ARCH-ERR")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("ListRules success and error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				limit := args[0].(int)
				if limit == 10 {
					return nil, errors.New("query failed")
				}
				return &mockRows{
					data: [][]any{
						{"uuid-1", "ARCH-001", "Title 1", "Desc 1", "ERROR", fixedTime, fixedTime},
						{"uuid-2", "ARCH-002", "Title 2", "Desc 2", "WARNING", fixedTime, fixedTime},
					},
				}, nil
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		rules, err := svc.ListRules(ctx, 0) // limit 0 defaults to 50
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rules) != 2 {
			t.Fatalf("expected 2 rules, got %d", len(rules))
		}

		_, err = svc.ListRules(ctx, 10)
		if err == nil {
			t.Fatal("expected error on query failure, got nil")
		}
	})

	t.Run("QueryRelevantRulesByVector success and error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
		queryVec := make([]float32, RuleVectorDimensions)

		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				limit := args[1].(int)
				if limit == 5 {
					return nil, errors.New("db timeout")
				}
				return &mockRows{
					data: [][]any{
						{"uuid-1", "ARCH-001", "Title 1", "Desc 1", "ERROR", fixedTime, fixedTime, float32(0.05)},
						{"uuid-2", "ARCH-002", "Title 2", "Desc 2", "WARNING", fixedTime, fixedTime, float32(0.85)},
					},
				}, nil
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		rules, err := svc.QueryRelevantRulesByVector(ctx, queryVec, 0) // default limit 10
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rules) != 2 {
			t.Fatalf("expected 2 rules, got %d", len(rules))
		}
		if rules[0].Distance != 0.05 {
			t.Fatalf("expected distance 0.05, got %f", rules[0].Distance)
		}

		_, err = svc.QueryRelevantRulesByVector(ctx, queryVec, 5)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("UpdateRule success, not found, and db error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
		vec := pgvector.NewVector(make([]float32, RuleVectorDimensions))

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				code := args[0].(string)
				switch code {
				case "ARCH-UPDATED":
					return &mockRow{
						values: []any{"uuid-updated", code, "Updated Title", "Updated Desc", "WARNING", &vec, fixedTime, fixedTime},
					}
				case "ARCH-NOTFOUND":
					return &mockRow{err: pgx.ErrNoRows}
				default:
					return &mockRow{err: errors.New("db update error")}
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		upRule := ArchitecturalRule{
			RuleCode:    "ARCH-UPDATED",
			Title:       "Updated Title",
			Description: "Updated Desc",
			Severity:    "WARNING",
		}
		updated, err := svc.UpdateRule(ctx, upRule)
		if err != nil {
			t.Fatalf("unexpected error updating rule: %v", err)
		}
		if updated.Title != "Updated Title" {
			t.Fatalf("expected Updated Title, got %s", updated.Title)
		}

		upRule.RuleCode = "ARCH-NOTFOUND"
		_, err = svc.UpdateRule(ctx, upRule)
		if !errors.Is(err, ErrRuleNotFound) {
			t.Fatalf("expected ErrRuleNotFound, got %v", err)
		}

		upRule.RuleCode = "ARCH-ERR"
		_, err = svc.UpdateRule(ctx, upRule)
		if err == nil {
			t.Fatal("expected error on DB failure, got nil")
		}
	})

	t.Run("DeleteRule and DeleteRuleByCode success and not found", func(t *testing.T) {
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				arg := args[0].(string)
				if arg == "uuid-to-delete" || arg == "ARCH-DEL" {
					return &mockRow{values: []any{arg}}
				}
				return &mockRow{err: pgx.ErrNoRows}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		if err := svc.DeleteRule(ctx, "uuid-to-delete"); err != nil {
			t.Fatalf("unexpected error deleting rule by ID: %v", err)
		}
		if err := svc.DeleteRule(ctx, "uuid-missing"); !errors.Is(err, ErrRuleNotFound) {
			t.Fatalf("expected ErrRuleNotFound, got %v", err)
		}
		if err := svc.DeleteRule(ctx, ""); err == nil {
			t.Fatal("expected error on empty ID, got nil")
		}

		if err := svc.DeleteRuleByCode(ctx, "ARCH-DEL"); err != nil {
			t.Fatalf("unexpected error deleting rule by code: %v", err)
		}
		if err := svc.DeleteRuleByCode(ctx, "ARCH-MISSING"); !errors.Is(err, ErrRuleNotFound) {
			t.Fatalf("expected ErrRuleNotFound, got %v", err)
		}
		if err := svc.DeleteRuleByCode(ctx, ""); err == nil {
			t.Fatal("expected error on empty code, got nil")
		}
	})
}

func TestArchitecturalRules_LiveIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := NewService(ctx, ConfigFromEnv())
	if err != nil {
		t.Skipf("skipping live integration test: NewService error: %v", err)
	}
	defer svc.Close()

	if pingErr := svc.Ping(ctx); pingErr != nil {
		t.Skipf("skipping live integration test: PostgreSQL is not reachable: %v", pingErr)
	}

	// Prepare distinct test vectors
	vecA := make([]float32, RuleVectorDimensions)
	vecA[0] = 1.0 // Direction: axis 0

	vecB := make([]float32, RuleVectorDimensions)
	vecB[1] = 1.0 // Direction: axis 1

	queryVec := make([]float32, RuleVectorDimensions)
	queryVec[0] = 0.99 // Highly aligned with vecA
	queryVec[1] = 0.05

	ruleCode1 := "TEST-RULE-VEC-A"
	ruleCode2 := "TEST-RULE-VEC-B"

	// Insert rules
	rule1 := ArchitecturalRule{
		RuleCode:    ruleCode1,
		Title:       "Rule A Title",
		Description: "Rule A Description",
		Severity:    "ERROR",
		Embedding:   vecA,
	}
	created1, err := svc.CreateRule(ctx, rule1)
	if err != nil {
		t.Fatalf("failed to create rule 1: %v", err)
	}
	if created1.ID == "" {
		t.Fatal("expected generated UUID for created rule 1")
	}

	rule2 := ArchitecturalRule{
		RuleCode:    ruleCode2,
		Title:       "Rule B Title",
		Description: "Rule B Description",
		Severity:    "WARNING",
		Embedding:   vecB,
	}
	created2, err := svc.CreateRule(ctx, rule2)
	if err != nil {
		t.Fatalf("failed to create rule 2: %v", err)
	}
	if created2.ID == "" {
		t.Fatal("expected generated UUID for created rule 2")
	}

	// Test GetRuleByCode
	fetched1, err := svc.GetRuleByCode(ctx, ruleCode1)
	if err != nil {
		t.Fatalf("failed to fetch rule 1 by code: %v", err)
	}
	if fetched1.RuleCode != ruleCode1 {
		t.Fatalf("expected ruleCode %q, got %q", ruleCode1, fetched1.RuleCode)
	}
	if len(fetched1.Embedding) != RuleVectorDimensions {
		t.Fatalf("expected embedding dimension %d, got %d", RuleVectorDimensions, len(fetched1.Embedding))
	}

	// Test GetRuleByCode with non-existent code
	_, err = svc.GetRuleByCode(ctx, "NON-EXISTENT-RULE")
	if !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("expected ErrRuleNotFound, got %v", err)
	}

	// Test ListRules
	rules, err := svc.ListRules(ctx, 10)
	if err != nil {
		t.Fatalf("failed to list rules: %v", err)
	}
	if len(rules) < 2 {
		t.Fatalf("expected at least 2 rules in list, got %d", len(rules))
	}

	// Test QueryRelevantRulesByVector
	similarRules, err := svc.QueryRelevantRulesByVector(ctx, queryVec, 2)
	if err != nil {
		t.Fatalf("failed to query relevant rules: %v", err)
	}
	if len(similarRules) < 2 {
		t.Fatalf("expected at least 2 similar rules, got %d", len(similarRules))
	}

	// Rule A should have smaller cosine distance to queryVec than Rule B
	if similarRules[0].RuleCode != ruleCode1 {
		t.Fatalf("expected most similar rule to be %q, got %q (distance: %f vs %f)",
			ruleCode1, similarRules[0].RuleCode, similarRules[0].Distance, similarRules[1].Distance)
	}
	if similarRules[0].Distance >= similarRules[1].Distance {
		t.Fatalf("expected rule 1 distance (%f) < rule 2 distance (%f)",
			similarRules[0].Distance, similarRules[1].Distance)
	}
}
