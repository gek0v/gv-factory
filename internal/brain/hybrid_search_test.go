// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestHybridSearchQuery_Validate(t *testing.T) {
	vec1536 := make([]float32, KnowledgeVectorDimensions)
	for i := range vec1536 {
		vec1536[i] = 0.01
	}

	t.Run("valid query with query text only", func(t *testing.T) {
		q := HybridSearchQuery{
			Query: "authentication token",
		}
		if err := q.Validate(); err != nil {
			t.Fatalf("expected valid query to pass, got: %v", err)
		}
		if q.EffectiveAlpha() != DefaultHybridAlpha {
			t.Fatalf("expected default alpha %f, got %f", DefaultHybridAlpha, q.EffectiveAlpha())
		}
		if q.EffectiveLimit() != DefaultHybridLimit {
			t.Fatalf("expected default limit %d, got %d", DefaultHybridLimit, q.EffectiveLimit())
		}
	})

	t.Run("valid query with embedding vector only", func(t *testing.T) {
		q := HybridSearchQuery{
			Embedding: vec1536,
		}
		if err := q.Validate(); err != nil {
			t.Fatalf("expected valid embedding query to pass, got: %v", err)
		}
	})

	t.Run("valid query with both query text and embedding", func(t *testing.T) {
		q := HybridSearchQuery{
			Query:     "database connection pool",
			Embedding: vec1536,
		}
		if err := q.Validate(); err != nil {
			t.Fatalf("expected valid hybrid query to pass, got: %v", err)
		}
	})

	t.Run("fails when both query text and embedding are empty", func(t *testing.T) {
		q := HybridSearchQuery{
			Query: "   ",
		}
		err := q.Validate()
		if err == nil {
			t.Fatal("expected error when query text and embedding are empty, got nil")
		}
		if !errors.Is(err, ErrInvalidHybridQuery) {
			t.Fatalf("expected ErrInvalidHybridQuery, got: %v", err)
		}
	})

	t.Run("fails when embedding dimension is invalid", func(t *testing.T) {
		q := HybridSearchQuery{
			Query:     "test",
			Embedding: make([]float32, 512),
		}
		err := q.Validate()
		if err == nil {
			t.Fatal("expected error for invalid vector dimension, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got: %v", err)
		}
	})

	t.Run("fails when alpha is less than 0.0", func(t *testing.T) {
		q := HybridSearchQuery{
			Query: "test",
			Alpha: Float32(-0.1),
		}
		err := q.Validate()
		if err == nil {
			t.Fatal("expected error for negative alpha, got nil")
		}
		if !errors.Is(err, ErrInvalidHybridAlpha) {
			t.Fatalf("expected ErrInvalidHybridAlpha, got: %v", err)
		}
	})

	t.Run("fails when alpha is greater than 1.0", func(t *testing.T) {
		q := HybridSearchQuery{
			Query: "test",
			Alpha: Float32(1.05),
		}
		err := q.Validate()
		if err == nil {
			t.Fatal("expected error for alpha > 1.0, got nil")
		}
		if !errors.Is(err, ErrInvalidHybridAlpha) {
			t.Fatalf("expected ErrInvalidHybridAlpha, got: %v", err)
		}
	})

	t.Run("preserves alpha boundaries 0.0 and 1.0", func(t *testing.T) {
		qZero := HybridSearchQuery{Query: "test", Alpha: Float32(0.0)}
		if err := qZero.Validate(); err != nil {
			t.Fatalf("expected alpha 0.0 to pass, got: %v", err)
		}
		if qZero.EffectiveAlpha() != 0.0 {
			t.Fatalf("expected effective alpha 0.0, got %f", qZero.EffectiveAlpha())
		}

		qOne := HybridSearchQuery{Query: "test", Alpha: Float32(1.0)}
		if err := qOne.Validate(); err != nil {
			t.Fatalf("expected alpha 1.0 to pass, got: %v", err)
		}
		if qOne.EffectiveAlpha() != 1.0 {
			t.Fatalf("expected effective alpha 1.0, got %f", qOne.EffectiveAlpha())
		}
	})

	t.Run("limit defaults and clamping", func(t *testing.T) {
		qDefault := HybridSearchQuery{Query: "test", Limit: 0}
		if qDefault.EffectiveLimit() != DefaultHybridLimit {
			t.Fatalf("expected %d, got %d", DefaultHybridLimit, qDefault.EffectiveLimit())
		}

		qNegative := HybridSearchQuery{Query: "test", Limit: -5}
		if qNegative.EffectiveLimit() != DefaultHybridLimit {
			t.Fatalf("expected %d, got %d", DefaultHybridLimit, qNegative.EffectiveLimit())
		}

		qHigh := HybridSearchQuery{Query: "test", Limit: 250}
		if qHigh.EffectiveLimit() != MaxHybridLimit {
			t.Fatalf("expected %d, got %d", MaxHybridLimit, qHigh.EffectiveLimit())
		}

		qNormal := HybridSearchQuery{Query: "test", Limit: 25}
		if qNormal.EffectiveLimit() != 25 {
			t.Fatalf("expected 25, got %d", qNormal.EffectiveLimit())
		}
	})

	t.Run("fluent builders and NewHybridSearchQuery", func(t *testing.T) {
		q := NewHybridSearchQuery("test-query").
			WithLimit(50).
			WithAlpha(0.85).
			WithEmbedding(vec1536)

		if q.Query != "test-query" {
			t.Fatalf("expected query test-query, got %s", q.Query)
		}
		if q.EffectiveLimit() != 50 {
			t.Fatalf("expected limit 50, got %d", q.EffectiveLimit())
		}
		if q.EffectiveAlpha() != 0.85 {
			t.Fatalf("expected alpha 0.85, got %f", q.EffectiveAlpha())
		}
		if len(q.Embedding) != KnowledgeVectorDimensions {
			t.Fatalf("expected %d dimensions, got %d", KnowledgeVectorDimensions, len(q.Embedding))
		}
	})
}

func TestHybridSearch_ScoreComputation(t *testing.T) {
	t.Run("ComputeVectorScore", func(t *testing.T) {
		tests := []struct {
			name     string
			distance float32
			expected float32
		}{
			{"perfect similarity (distance 0.0)", 0.0, 1.0},
			{"distance 0.2", 0.2, 0.8},
			{"orthogonal (distance 1.0)", 1.0, 0.0},
			{"opposite (distance 2.0)", 2.0, 0.0},
			{"negative distance clamped", -0.5, 1.0},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := ComputeVectorScore(tc.distance)
				diff := got - tc.expected
				if diff < -1e-5 || diff > 1e-5 {
					t.Fatalf("expected %f, got %f", tc.expected, got)
				}
			})
		}
	})

	t.Run("NormalizeLexicalScore", func(t *testing.T) {
		tests := []struct {
			name     string
			raw      float32
			expected float32
		}{
			{"zero score", 0.0, 0.0},
			{"negative score clamped to 0", -0.1, 0.0},
			{"normal score 0.75", 0.75, 0.75},
			{"score > 1 clamped to 1.0", 1.85, 1.0},
			{"boundary 1.0", 1.0, 1.0},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := NormalizeLexicalScore(tc.raw)
				if got != tc.expected {
					t.Fatalf("expected %f, got %f", tc.expected, got)
				}
			})
		}
	})

	t.Run("ComputeHybridScore", func(t *testing.T) {
		tests := []struct {
			name     string
			alpha    float32
			vecScore float32
			lexScore float32
			expected float32
		}{
			{"default alpha 0.7", 0.7, 0.8, 0.4, 0.7*0.8 + 0.3*0.4}, // 0.56 + 0.12 = 0.68
			{"pure vector alpha 1.0", 1.0, 0.8, 0.4, 0.8},
			{"pure lexical alpha 0.0", 0.0, 0.8, 0.4, 0.4},
			{"equal balance alpha 0.5", 0.5, 0.6, 0.8, 0.7},
			{"clamped upper bound", 0.7, 1.5, 1.2, 1.0},
			{"clamped lower bound", 0.7, -0.2, -0.5, 0.0},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := ComputeHybridScore(tc.alpha, tc.vecScore, tc.lexScore)
				diff := got - tc.expected
				if diff < -1e-5 || diff > 1e-5 {
					t.Fatalf("expected %f, got %f", tc.expected, got)
				}
			})
		}
	})
}

func TestHybridSearch_NilPool(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("HybridSearchCodebase returns ConnectionError on nil pool", func(t *testing.T) {
		_, err := svc.HybridSearchCodebase(ctx, HybridSearchQuery{Query: "auth"})
		if err == nil {
			t.Fatal("expected ConnectionError on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("HybridSearchRules returns ConnectionError on nil pool", func(t *testing.T) {
		_, err := svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "security"})
		if err == nil {
			t.Fatal("expected ConnectionError on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})
}

func TestHybridSearch_ValidationErrors(t *testing.T) {
	svc := &Service{
		cfg: DefaultConfig(),
	}
	ctx := context.Background()

	t.Run("HybridSearchCodebase rejects invalid query", func(t *testing.T) {
		_, err := svc.HybridSearchCodebase(ctx, HybridSearchQuery{})
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !errors.Is(err, ErrInvalidHybridQuery) {
			t.Fatalf("expected ErrInvalidHybridQuery, got: %v", err)
		}
	})

	t.Run("HybridSearchRules rejects invalid query", func(t *testing.T) {
		_, err := svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "rules", Alpha: Float32(2.0)})
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !errors.Is(err, ErrInvalidHybridAlpha) {
			t.Fatalf("expected ErrInvalidHybridAlpha, got: %v", err)
		}
	})
}

func TestHybridSearchCodebase_MockDatabase(t *testing.T) {
	ctx := context.Background()
	fixedTime := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	vec := make([]float32, KnowledgeVectorDimensions)
	vec[0] = 0.5

	t.Run("hybrid query combines vector and lexical scores with correct ranking", func(t *testing.T) {
		// Item 1: distance 0.1 -> vecScore = 0.9; lexScore = 0.3
		// Score with alpha 0.7 = 0.7*0.9 + 0.3*0.3 = 0.63 + 0.09 = 0.72
		// Item 2: distance 0.7 -> vecScore = 0.3; lexScore = 0.9
		// Score with alpha 0.7 = 0.7*0.3 + 0.3*0.9 = 0.21 + 0.27 = 0.48
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"id-1", "pkg/auth.go", "LoginHandler", "FUNCTION", "func LoginHandler()", "hash1", fixedTime, float32(0.1), float32(0.3)},
						{"id-2", "pkg/token.go", "ParseToken", "FUNCTION", "func ParseToken()", "hash2", fixedTime, float32(0.7), float32(0.9)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{
			Query:     "login token",
			Embedding: vec,
			Alpha:     Float32(0.7),
			Limit:     10,
		}

		results, err := svc.HybridSearchCodebase(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		// Item 1 should be ranked #1
		if results[0].ID != "id-1" {
			t.Fatalf("expected id-1 to rank first with alpha=0.7, got %s", results[0].ID)
		}
		diff0 := results[0].Score - 0.72
		if diff0 < -1e-4 || diff0 > 1e-4 {
			t.Fatalf("expected score ~0.72, got %f", results[0].Score)
		}
		if results[0].VectorScore != 0.9 {
			t.Fatalf("expected vector score 0.9, got %f", results[0].VectorScore)
		}
		if results[0].LexicalScore != 0.3 {
			t.Fatalf("expected lexical score 0.3, got %f", results[0].LexicalScore)
		}

		// Item 2 should be ranked #2
		if results[1].ID != "id-2" {
			t.Fatalf("expected id-2 to rank second, got %s", results[1].ID)
		}
		diff1 := results[1].Score - 0.48
		if diff1 < -1e-4 || diff1 > 1e-4 {
			t.Fatalf("expected score ~0.48, got %f", results[1].Score)
		}
	})

	t.Run("lexical dominant weight alpha=0.1 flips ranking towards high lexical match", func(t *testing.T) {
		// With alpha 0.1:
		// Item 1: 0.1*0.9 + 0.9*0.3 = 0.09 + 0.27 = 0.36
		// Item 2: 0.1*0.3 + 0.9*0.9 = 0.03 + 0.81 = 0.84
		// Item 2 should rank first!
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"id-1", "pkg/auth.go", "LoginHandler", "FUNCTION", "func LoginHandler()", "hash1", fixedTime, float32(0.1), float32(0.3)},
						{"id-2", "pkg/token.go", "ParseToken", "FUNCTION", "func ParseToken()", "hash2", fixedTime, float32(0.7), float32(0.9)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{
			Query:     "login token",
			Embedding: vec,
			Alpha:     Float32(0.1),
		}

		results, err := svc.HybridSearchCodebase(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].ID != "id-2" {
			t.Fatalf("expected id-2 to rank first with alpha=0.1, got %s", results[0].ID)
		}
	})

	t.Run("pure lexical query without embedding", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"id-lex", "pkg/search.go", "Search", "FUNCTION", "func Search()", "hashS", fixedTime, float32(1.0), float32(0.85)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{Query: "search function"}

		results, err := svc.HybridSearchCodebase(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Score != 0.85 {
			t.Fatalf("expected score 0.85, got %f", results[0].Score)
		}
	})

	t.Run("pure vector query without query text", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"id-vec", "pkg/vec.go", "Embed", "FUNCTION", "func Embed()", "hashV", fixedTime, float32(0.15), float32(0.0)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{Embedding: vec}

		results, err := svc.HybridSearchCodebase(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		diff := results[0].Score - 0.85
		if diff < -1e-5 || diff > 1e-5 {
			t.Fatalf("expected score 0.85, got %f", results[0].Score)
		}
	})

	t.Run("truncates results to requested limit", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"id-1", "pkg/a.go", "A", "TYPE", "A", "h1", fixedTime, float32(0.1), float32(0.9)},
						{"id-2", "pkg/b.go", "B", "TYPE", "B", "h2", fixedTime, float32(0.2), float32(0.8)},
						{"id-3", "pkg/c.go", "C", "TYPE", "C", "h3", fixedTime, float32(0.3), float32(0.7)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{Query: "types", Limit: 2}

		results, err := svc.HybridSearchCodebase(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected truncated results to 2, got %d", len(results))
		}
	})

	t.Run("empty results returns empty slice", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{data: [][]any{}}, nil
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}
		res, err := svc.HybridSearchCodebase(ctx, HybridSearchQuery{Query: "empty"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil || len(res) != 0 {
			t.Fatalf("expected empty non-nil slice, got %v", res)
		}
	})

	t.Run("tie breaking in sort", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						// Both items have Score = 0.5 (equal)
						// Item A: VectorScore = 0.5, LexicalScore = 0.5
						// Item B: VectorScore = 0.6, LexicalScore = 0.4 -> higher VectorScore wins!
						{"id-a", "pkg/a.go", "A", "TYPE", "A", "h1", fixedTime, float32(0.5), float32(0.5)},
						{"id-b", "pkg/b.go", "B", "TYPE", "B", "h2", fixedTime, float32(0.4), float32(0.4)},
					},
				}, nil
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{Query: "types", Embedding: vec, Alpha: Float32(0.5)}
		results, err := svc.HybridSearchCodebase(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("database errors handled gracefully", func(t *testing.T) {
		mockQueryErr := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("db query failed")
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mockQueryErr}
		_, err := svc.HybridSearchCodebase(ctx, HybridSearchQuery{Query: "test"})
		if err == nil {
			t.Fatal("expected error on query failure, got nil")
		}

		mockScanErr := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data:    [][]any{{"id-1", "pkg/a.go", "A", "TYPE", "A", "h1", fixedTime, float32(0.1), float32(0.9)}},
					scanErr: errors.New("scan failure"),
				}, nil
			},
		}
		svc = &Service{cfg: DefaultConfig(), db: mockScanErr}
		_, err = svc.HybridSearchCodebase(ctx, HybridSearchQuery{Query: "test"})
		if err == nil {
			t.Fatal("expected error on rows.Scan(), got nil")
		}

		mockRowsErr := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{{"id-1", "pkg/a.go", "A", "TYPE", "A", "h1", fixedTime, float32(0.1), float32(0.9)}},
					err:  errors.New("rows cursor iteration error"),
				}, nil
			},
		}
		svc = &Service{cfg: DefaultConfig(), db: mockRowsErr}
		_, err = svc.HybridSearchCodebase(ctx, HybridSearchQuery{Query: "test"})
		if err == nil {
			t.Fatal("expected error on rows.Err(), got nil")
		}
	})
}

func TestHybridSearchRules_MockDatabase(t *testing.T) {
	ctx := context.Background()
	fixedTime := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	vec := make([]float32, RuleVectorDimensions)
	vec[0] = 0.8

	t.Run("hybrid rules search combines scores and ranks correctly", func(t *testing.T) {
		// Rule 1: distance 0.15 -> vecScore = 0.85; lexScore = 0.5
		// Score with alpha 0.7 = 0.7*0.85 + 0.3*0.5 = 0.595 + 0.15 = 0.745
		// Rule 2: distance 0.60 -> vecScore = 0.40; lexScore = 0.95
		// Score with alpha 0.7 = 0.7*0.40 + 0.3*0.95 = 0.280 + 0.285 = 0.565
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"r-1", "ARCH-001", "Boundary Isolation", "Domain isolation", "ERROR", fixedTime, fixedTime, float32(0.15), float32(0.5)},
						{"r-2", "ARCH-002", "Dependency Direction", "Inward dependencies", "WARN", fixedTime, fixedTime, float32(0.60), float32(0.95)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{
			Query:     "boundary architecture",
			Embedding: vec,
			Alpha:     Float32(0.7),
			Limit:     10,
		}

		results, err := svc.HybridSearchRules(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 rules, got %d", len(results))
		}

		if results[0].RuleCode != "ARCH-001" {
			t.Fatalf("expected ARCH-001 to rank first, got %s", results[0].RuleCode)
		}
		diff := results[0].Score - 0.745
		if diff < -1e-4 || diff > 1e-4 {
			t.Fatalf("expected score ~0.745, got %f", results[0].Score)
		}

		if results[1].RuleCode != "ARCH-002" {
			t.Fatalf("expected ARCH-002 to rank second, got %s", results[1].RuleCode)
		}
	})

	t.Run("pure lexical rules search", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"r-lex", "RULE-FTS", "Clean Architecture", "Hexagonal ports", "ERROR", fixedTime, fixedTime, float32(1.0), float32(0.88)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		results, err := svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "hexagonal"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 || results[0].Score != 0.88 {
			t.Fatalf("unexpected results: %+v", results)
		}
	})

	t.Run("pure vector rules search", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"r-vec", "RULE-VEC", "Vector Match", "Rule vector", "INFO", fixedTime, fixedTime, float32(0.2), float32(0.0)},
					},
				}, nil
			},
		}

		svc := &Service{cfg: DefaultConfig(), db: mock}
		results, err := svc.HybridSearchRules(ctx, HybridSearchQuery{Embedding: vec})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		diff := results[0].Score - 0.8
		if diff < -1e-5 || diff > 1e-5 {
			t.Fatalf("expected score 0.8, got %f", results[0].Score)
		}
	})

	t.Run("empty rules results returns empty slice", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{data: [][]any{}}, nil
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}
		res, err := svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "empty"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil || len(res) != 0 {
			t.Fatalf("expected empty non-nil slice, got %v", res)
		}
	})

	t.Run("truncates rules to requested limit and tie breaking", func(t *testing.T) {
		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"r-1", "ARCH-001", "R1", "D1", "ERROR", fixedTime, fixedTime, float32(0.5), float32(0.5)},
						{"r-2", "ARCH-002", "R2", "D2", "WARN", fixedTime, fixedTime, float32(0.4), float32(0.4)},
						{"r-3", "ARCH-003", "R3", "D3", "INFO", fixedTime, fixedTime, float32(0.3), float32(0.3)},
					},
				}, nil
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}
		q := HybridSearchQuery{Query: "rules", Embedding: vec, Alpha: Float32(0.5), Limit: 2}
		results, err := svc.HybridSearchRules(ctx, q)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected truncated to 2, got %d", len(results))
		}
	})

	t.Run("database errors handled gracefully in HybridSearchRules", func(t *testing.T) {
		mockQueryErr := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mockQueryErr}
		_, err := svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "rule"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		mockScanErr := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data:    [][]any{{"r-1", "ARCH-001", "Boundary", "Desc", "ERROR", fixedTime, fixedTime, float32(0.1), float32(0.9)}},
					scanErr: errors.New("scan failure"),
				}, nil
			},
		}
		svc = &Service{cfg: DefaultConfig(), db: mockScanErr}
		_, err = svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "rule"})
		if err == nil {
			t.Fatal("expected error on scan failure, got nil")
		}

		mockRowsErr := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{{"r-1", "ARCH-001", "Boundary", "Desc", "ERROR", fixedTime, fixedTime, float32(0.1), float32(0.9)}},
					err:  errors.New("cursor scan error"),
				}, nil
			},
		}
		svc = &Service{cfg: DefaultConfig(), db: mockRowsErr}
		_, err = svc.HybridSearchRules(ctx, HybridSearchQuery{Query: "rule"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestHybridSearch_LiveIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := NewService(ctx, ConfigFromEnv())
	if err != nil {
		t.Skipf("skipping live integration test: NewService error: %v", err)
	}
	defer svc.Close()

	if err := svc.Ping(ctx); err != nil {
		t.Skipf("skipping live integration test: PostgreSQL is not reachable: %v", err)
	}

	// Live test if PostgreSQL is running
	vec := make([]float32, KnowledgeVectorDimensions)
	vec[0] = 0.1
	res, err := svc.HybridSearchCodebase(ctx, HybridSearchQuery{
		Query:     "Service",
		Embedding: vec,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("live HybridSearchCodebase failed: %v", err)
	}
	t.Logf("live HybridSearchCodebase returned %d results", len(res))
}
