// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

func TestCodebaseKnowledge_Validate(t *testing.T) {
	validKnowledge := CodebaseKnowledge{
		FilePath:   "internal/auth/auth.go",
		SymbolName: "NewService",
		SymbolType: "FUNCTION",
		Content:    "func NewService(secret string) *Service { ... }",
	}

	t.Run("valid knowledge computes content_hash if empty", func(t *testing.T) {
		k := validKnowledge
		k.ContentHash = ""
		if err := k.Validate(); err != nil {
			t.Fatalf("expected valid knowledge to pass, got: %v", err)
		}
		if k.ContentHash == "" {
			t.Fatal("expected content_hash to be automatically computed")
		}
	})

	t.Run("preserves existing content_hash", func(t *testing.T) {
		k := validKnowledge
		k.ContentHash = "custom-hash-xxh64-1234"
		if err := k.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if k.ContentHash != "custom-hash-xxh64-1234" {
			t.Fatalf("expected custom hash preserved, got %s", k.ContentHash)
		}
	})

	t.Run("fails when file_path is empty", func(t *testing.T) {
		k := validKnowledge
		k.FilePath = "  "
		if err := k.Validate(); err == nil {
			t.Fatal("expected error for empty file_path, got nil")
		}
	})

	t.Run("fails when symbol_name is empty", func(t *testing.T) {
		k := validKnowledge
		k.SymbolName = ""
		if err := k.Validate(); err == nil {
			t.Fatal("expected error for empty symbol_name, got nil")
		}
	})

	t.Run("fails when symbol_type is empty", func(t *testing.T) {
		k := validKnowledge
		k.SymbolType = ""
		if err := k.Validate(); err == nil {
			t.Fatal("expected error for empty symbol_type, got nil")
		}
	})

	t.Run("fails when content is empty", func(t *testing.T) {
		k := validKnowledge
		k.Content = "  "
		if err := k.Validate(); err == nil {
			t.Fatal("expected error for empty content, got nil")
		}
	})

	t.Run("fails when embedding dimension is invalid", func(t *testing.T) {
		k := validKnowledge
		k.Embedding = make([]float32, 500)
		err := k.Validate()
		if err == nil {
			t.Fatal("expected error for invalid vector dimension, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got %v", err)
		}
	})

	t.Run("succeeds when embedding has exactly 1536 dimensions", func(t *testing.T) {
		k := validKnowledge
		k.Embedding = make([]float32, KnowledgeVectorDimensions)
		if err := k.Validate(); err != nil {
			t.Fatalf("expected 1536-dim embedding to succeed, got: %v", err)
		}
	})
}

func TestCodebaseKnowledge_NilPoolHandling(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("UpsertCodebaseKnowledge returns ConnectionError on nil pool", func(t *testing.T) {
		k := CodebaseKnowledge{
			FilePath:   "internal/brain/brain.go",
			SymbolName: "NewService",
			SymbolType: "FUNCTION",
			Content:    "func NewService() *Service",
		}
		_, err := svc.UpsertCodebaseKnowledge(ctx, k)
		if err == nil {
			t.Fatal("expected error on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("GetKnowledgeBySymbol returns ConnectionError on nil pool", func(t *testing.T) {
		_, err := svc.GetKnowledgeBySymbol(ctx, "internal/brain/brain.go", "NewService")
		if err == nil {
			t.Fatal("expected error on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("QueryCodebaseKnowledge returns ConnectionError on nil pool", func(t *testing.T) {
		vec := make([]float32, KnowledgeVectorDimensions)
		_, err := svc.QueryCodebaseKnowledge(ctx, vec, 5)
		if err == nil {
			t.Fatal("expected error on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})
}

func TestCodebaseKnowledge_InputValidation(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("UpsertCodebaseKnowledge rejects invalid struct", func(t *testing.T) {
		invalidK := CodebaseKnowledge{
			FilePath: "",
		}
		_, err := svc.UpsertCodebaseKnowledge(ctx, invalidK)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !errors.Is(err, ErrInvalidKnowledge) {
			t.Fatalf("expected ErrInvalidKnowledge, got %v", err)
		}
	})

	t.Run("GetKnowledgeBySymbol rejects empty arguments", func(t *testing.T) {
		_, err := svc.GetKnowledgeBySymbol(ctx, "", "Symbol")
		if err == nil {
			t.Fatal("expected error for empty file path, got nil")
		}
		if !errors.Is(err, ErrInvalidKnowledge) {
			t.Fatalf("expected ErrInvalidKnowledge, got %v", err)
		}

		_, err = svc.GetKnowledgeBySymbol(ctx, "path/file.go", "")
		if err == nil {
			t.Fatal("expected error for empty symbol name, got nil")
		}
		if !errors.Is(err, ErrInvalidKnowledge) {
			t.Fatalf("expected ErrInvalidKnowledge, got %v", err)
		}
	})

	t.Run("QueryCodebaseKnowledge rejects invalid vector dimensions", func(t *testing.T) {
		badVec := make([]float32, 64)
		_, err := svc.QueryCodebaseKnowledge(ctx, badVec, 5)
		if err == nil {
			t.Fatal("expected error for bad dimensions, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got %v", err)
		}
	})
}

func TestCodebaseKnowledge_MockDatabase(t *testing.T) {
	ctx := context.Background()

	t.Run("UpsertCodebaseKnowledge success", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)
		vec := make([]float32, KnowledgeVectorDimensions)
		vec[0] = 0.95
		k := CodebaseKnowledge{
			FilePath:    "internal/brain/brain.go",
			SymbolName:  "NewService",
			SymbolType:  "FUNCTION",
			Content:     "func NewService(ctx context.Context, cfg Config) (*Service, error)",
			ContentHash: "hash-1234",
			Embedding:   vec,
		}

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					values: []any{
						"uuid-k-1",
						k.FilePath,
						k.SymbolName,
						k.SymbolType,
						k.Content,
						k.ContentHash,
						(*pgvector.Vector)(nil),
						fixedTime,
					},
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		upserted, err := svc.UpsertCodebaseKnowledge(ctx, k)
		if err != nil {
			t.Fatalf("unexpected error upserting knowledge: %v", err)
		}
		if upserted.ID != "uuid-k-1" {
			t.Fatalf("expected ID uuid-k-1, got %s", upserted.ID)
		}
		if upserted.SymbolName != "NewService" {
			t.Fatalf("expected SymbolName NewService, got %s", upserted.SymbolName)
		}
	})

	t.Run("UpsertCodebaseKnowledge database error", func(t *testing.T) {
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: errors.New("db error: disk full")}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		k := CodebaseKnowledge{
			FilePath:   "internal/brain/brain.go",
			SymbolName: "NewService",
			SymbolType: "FUNCTION",
			Content:    "func NewService()",
		}

		_, err := svc.UpsertCodebaseKnowledge(ctx, k)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T", err)
		}
	})

	t.Run("GetKnowledgeBySymbol success, not found, and db error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)
		vec := pgvector.NewVector(make([]float32, KnowledgeVectorDimensions))

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				symbol := args[1].(string)
				switch symbol {
				case "NewService":
					return &mockRow{
						values: []any{
							"uuid-k-1",
							"internal/brain/brain.go",
							"NewService",
							"FUNCTION",
							"func NewService()",
							"hash-1234",
							&vec,
							fixedTime,
						},
					}
				case "NotFound":
					return &mockRow{err: pgx.ErrNoRows}
				default:
					return &mockRow{err: errors.New("connection failed")}
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		k, err := svc.GetKnowledgeBySymbol(ctx, "internal/brain/brain.go", "NewService")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if k.SymbolName != "NewService" {
			t.Fatalf("expected symbol NewService, got %s", k.SymbolName)
		}

		_, err = svc.GetKnowledgeBySymbol(ctx, "internal/brain/brain.go", "NotFound")
		if !errors.Is(err, ErrKnowledgeNotFound) {
			t.Fatalf("expected ErrKnowledgeNotFound, got %v", err)
		}

		_, err = svc.GetKnowledgeBySymbol(ctx, "internal/brain/brain.go", "ErrSymbol")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("IngestChunks empty and success", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					values: []any{
						"uuid-ingested-1",
						args[0],
						args[1],
						args[2],
						args[3],
						args[4],
						nil,
						fixedTime,
					},
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		// Empty chunks
		res, err := svc.IngestChunks(ctx, nil)
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty result, got %v, err: %v", res, err)
		}

		// Populated chunks
		chunks := []CodeChunk{
			{
				SymbolName:  "TestFn",
				SymbolType:  SymbolTypeFunction,
				FilePath:    "pkg/test.go",
				Content:     "func TestFn() {}",
				ContentHash: ComputeContentHash("func TestFn() {}"),
			},
		}

		saved, err := svc.IngestChunks(ctx, chunks)
		if err != nil {
			t.Fatalf("IngestChunks failed: %v", err)
		}
		if len(saved) != 1 || saved[0].SymbolName != "TestFn" {
			t.Fatalf("unexpected saved chunks: %+v", saved)
		}
	})

	t.Run("IngestChunks error handling", func(t *testing.T) {
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: errors.New("db error")}
			},
		}
		svc := &Service{cfg: DefaultConfig(), db: mock}
		chunks := []CodeChunk{
			{
				SymbolName:  "TestFn",
				SymbolType:  SymbolTypeFunction,
				FilePath:    "pkg/test.go",
				Content:     "func TestFn() {}",
				ContentHash: ComputeContentHash("func TestFn() {}"),
			},
		}
		_, err := svc.IngestChunks(ctx, chunks)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("QueryCodebaseKnowledge success and query error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)
		queryVec := make([]float32, KnowledgeVectorDimensions)

		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				limit := args[1].(int)
				if limit == 5 {
					return nil, errors.New("timeout querying HNSW index")
				}
				return &mockRows{
					data: [][]any{
						{"uuid-k-1", "pkg/a.go", "SymA", "TYPE", "content A", "hashA", fixedTime, float32(0.04)},
						{"uuid-k-2", "pkg/b.go", "SymB", "FUNC", "content B", "hashB", fixedTime, float32(0.68)},
					},
				}, nil
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		results, err := svc.QueryCodebaseKnowledge(ctx, queryVec, 0) // default limit 10
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].Distance != 0.04 {
			t.Fatalf("expected distance 0.04, got %f", results[0].Distance)
		}

		_, err = svc.QueryCodebaseKnowledge(ctx, queryVec, 5)
		if err == nil {
			t.Fatal("expected error on timeout, got nil")
		}
	})
}

func TestCodebaseKnowledge_LiveIntegration(t *testing.T) {
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

	vecA := make([]float32, KnowledgeVectorDimensions)
	vecA[0] = 1.0

	vecB := make([]float32, KnowledgeVectorDimensions)
	vecB[1] = 1.0

	queryVec := make([]float32, KnowledgeVectorDimensions)
	queryVec[0] = 0.99
	queryVec[1] = 0.05

	filePath := "internal/brain/brain.go"
	symbolA := "NewService"
	symbolB := "Ping"

	// 1. Initial insert of Symbol A
	kA := CodebaseKnowledge{
		FilePath:   filePath,
		SymbolName: symbolA,
		SymbolType: "FUNCTION",
		Content:    "func NewService() *Service",
		Embedding:  vecA,
	}
	createdA, err := svc.UpsertCodebaseKnowledge(ctx, kA)
	if err != nil {
		t.Fatalf("failed to insert knowledge A: %v", err)
	}
	if createdA.ID == "" {
		t.Fatal("expected generated UUID")
	}

	// 2. Upsert deduplication: update Symbol A with new content
	kAUpdated := CodebaseKnowledge{
		FilePath:    filePath,
		SymbolName:  symbolA,
		SymbolType:  "FUNCTION",
		Content:     "func NewService(ctx context.Context, cfg Config) (*Service, error)",
		ContentHash: "new-hash-v2",
		Embedding:   vecA,
	}
	updatedA, err := svc.UpsertCodebaseKnowledge(ctx, kAUpdated)
	if err != nil {
		t.Fatalf("failed to update knowledge A: %v", err)
	}
	if updatedA.ID != createdA.ID {
		t.Fatalf("expected ID to remain identical on conflict update, got %s != %s", updatedA.ID, createdA.ID)
	}
	if updatedA.ContentHash != "new-hash-v2" {
		t.Fatalf("expected content_hash to be updated, got %s", updatedA.ContentHash)
	}

	// 3. Insert Symbol B
	kB := CodebaseKnowledge{
		FilePath:   filePath,
		SymbolName: symbolB,
		SymbolType: "METHOD",
		Content:    "func (s *Service) Ping(ctx context.Context) error",
		Embedding:  vecB,
	}
	_, err = svc.UpsertCodebaseKnowledge(ctx, kB)
	if err != nil {
		t.Fatalf("failed to insert knowledge B: %v", err)
	}

	// 4. Test GetKnowledgeBySymbol
	fetchedA, err := svc.GetKnowledgeBySymbol(ctx, filePath, symbolA)
	if err != nil {
		t.Fatalf("failed to fetch knowledge A: %v", err)
	}
	if fetchedA.ContentHash != "new-hash-v2" {
		t.Fatalf("expected updated content_hash, got %s", fetchedA.ContentHash)
	}

	_, err = svc.GetKnowledgeBySymbol(ctx, filePath, "NonExistentSymbol")
	if !errors.Is(err, ErrKnowledgeNotFound) {
		t.Fatalf("expected ErrKnowledgeNotFound, got %v", err)
	}

	// 5. Test QueryCodebaseKnowledge semantic retrieval
	similar, err := svc.QueryCodebaseKnowledge(ctx, queryVec, 2)
	if err != nil {
		t.Fatalf("failed to query similar knowledge: %v", err)
	}
	if len(similar) < 2 {
		t.Fatalf("expected at least 2 similar symbols, got %d", len(similar))
	}

	if similar[0].SymbolName != symbolA {
		t.Fatalf("expected most similar symbol to be %s, got %s (distance %f vs %f)",
			symbolA, similar[0].SymbolName, similar[0].Distance, similar[1].Distance)
	}
	if similar[0].Distance >= similar[1].Distance {
		t.Fatalf("expected symbol A distance (%f) < symbol B distance (%f)",
			similar[0].Distance, similar[1].Distance)
	}
}
