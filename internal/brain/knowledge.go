// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

// KnowledgeVectorDimensions is the dimensionality of pgvector embeddings for codebase knowledge.
const KnowledgeVectorDimensions = 1536

var (
	// ErrInvalidKnowledge is returned when required fields in CodebaseKnowledge are missing or invalid.
	ErrInvalidKnowledge = errors.New("brain: invalid codebase knowledge")

	// ErrKnowledgeNotFound is returned when a codebase knowledge record does not exist.
	ErrKnowledgeNotFound = errors.New("brain: codebase knowledge not found")
)

// Validate checks whether a CodebaseKnowledge record has all required fields and valid dimensions.
// If ContentHash is empty, it computes a SHA-256 hash of Content.
func (k *CodebaseKnowledge) Validate() error {
	k.FilePath = strings.TrimSpace(k.FilePath)
	if k.FilePath == "" {
		return fmt.Errorf("%w: file_path is required", ErrInvalidKnowledge)
	}

	k.SymbolName = strings.TrimSpace(k.SymbolName)
	if k.SymbolName == "" {
		return fmt.Errorf("%w: symbol_name is required", ErrInvalidKnowledge)
	}

	k.SymbolType = strings.TrimSpace(k.SymbolType)
	if k.SymbolType == "" {
		return fmt.Errorf("%w: symbol_type is required", ErrInvalidKnowledge)
	}

	k.Content = strings.TrimSpace(k.Content)
	if k.Content == "" {
		return fmt.Errorf("%w: content is required", ErrInvalidKnowledge)
	}

	k.ContentHash = strings.TrimSpace(k.ContentHash)
	if k.ContentHash == "" {
		h := sha256.Sum256([]byte(k.Content))
		k.ContentHash = hex.EncodeToString(h[:])
	}

	if len(k.Embedding) > 0 && len(k.Embedding) != KnowledgeVectorDimensions {
		return ErrInvalidVectorDimensions
	}

	return nil
}

// UpsertCodebaseKnowledge creates or updates an AST symbol fragment in the knowledge store.
func (s *Service) UpsertCodebaseKnowledge(ctx context.Context, k CodebaseKnowledge) (*CodebaseKnowledge, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "upsert_codebase_knowledge",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	var vec *pgvector.Vector
	if len(k.Embedding) > 0 {
		v := pgvector.NewVector(k.Embedding)
		vec = &v
	}

	const query = `
		INSERT INTO codebase_knowledge (file_path, symbol_name, symbol_type, content, content_hash, embedding, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (file_path, symbol_name)
		DO UPDATE SET
			symbol_type = EXCLUDED.symbol_type,
			content = EXCLUDED.content,
			content_hash = EXCLUDED.content_hash,
			embedding = CASE
				WHEN EXCLUDED.embedding IS NOT NULL THEN EXCLUDED.embedding
				ELSE codebase_knowledge.embedding
			END,
			updated_at = NOW()
		RETURNING id, file_path, symbol_name, symbol_type, content, content_hash, embedding, updated_at
	`

	var upserted CodebaseKnowledge
	var retVec *pgvector.Vector
	err := db.QueryRow(ctx, query,
		k.FilePath,
		k.SymbolName,
		k.SymbolType,
		k.Content,
		k.ContentHash,
		vec,
	).Scan(
		&upserted.ID,
		&upserted.FilePath,
		&upserted.SymbolName,
		&upserted.SymbolType,
		&upserted.Content,
		&upserted.ContentHash,
		&retVec,
		&upserted.UpdatedAt,
	)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "upsert_codebase_knowledge",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	if retVec != nil {
		upserted.Embedding = retVec.Slice()
	} else {
		upserted.Embedding = k.Embedding
	}
	return &upserted, nil
}

// IngestChunks converts and upserts multiple CodeChunks into the codebase knowledge store.
func (s *Service) IngestChunks(ctx context.Context, chunks []CodeChunk) ([]*CodebaseKnowledge, error) {
	if len(chunks) == 0 {
		return []*CodebaseKnowledge{}, nil
	}

	results := make([]*CodebaseKnowledge, 0, len(chunks))
	for _, chunk := range chunks {
		k := chunk.ToCodebaseKnowledge()
		saved, err := s.UpsertCodebaseKnowledge(ctx, k)
		if err != nil {
			return nil, err
		}
		results = append(results, saved)
	}

	return results, nil
}

// GetKnowledgeBySymbol retrieves a codebase knowledge fragment by file path and symbol name.
func (s *Service) GetKnowledgeBySymbol(ctx context.Context, filePath, symbolName string) (*CodebaseKnowledge, error) {
	path := strings.TrimSpace(filePath)
	symbol := strings.TrimSpace(symbolName)
	if path == "" || symbol == "" {
		return nil, fmt.Errorf("%w: file_path and symbol_name are required", ErrInvalidKnowledge)
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "get_knowledge_by_symbol",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, file_path, symbol_name, symbol_type, content, content_hash, embedding, updated_at
		FROM codebase_knowledge
		WHERE file_path = $1 AND symbol_name = $2
	`

	var k CodebaseKnowledge
	var vec *pgvector.Vector
	err := db.QueryRow(ctx, query, path, symbol).Scan(
		&k.ID,
		&k.FilePath,
		&k.SymbolName,
		&k.SymbolType,
		&k.Content,
		&k.ContentHash,
		&vec,
		&k.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKnowledgeNotFound
		}
		return nil, &ConnectionError{
			Op:  "get_knowledge_by_symbol",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	if vec != nil {
		k.Embedding = vec.Slice()
	}
	return &k, nil
}

// QueryCodebaseKnowledge performs a cosine similarity search against the codebase_knowledge HNSW index.
func (s *Service) QueryCodebaseKnowledge(ctx context.Context, embedding []float32, limit int) ([]CodebaseKnowledge, error) {
	if len(embedding) != KnowledgeVectorDimensions {
		return nil, ErrInvalidVectorDimensions
	}

	if limit <= 0 {
		limit = 10
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "query_codebase_knowledge",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, file_path, symbol_name, symbol_type, content, content_hash, updated_at, (embedding <=> $1) as distance
		FROM codebase_knowledge
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1 ASC
		LIMIT $2
	`

	vec := pgvector.NewVector(embedding)
	rows, err := db.Query(ctx, query, vec, limit)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "query_codebase_knowledge",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var list []CodebaseKnowledge
	for rows.Next() {
		var k CodebaseKnowledge
		if err := rows.Scan(
			&k.ID,
			&k.FilePath,
			&k.SymbolName,
			&k.SymbolType,
			&k.Content,
			&k.ContentHash,
			&k.UpdatedAt,
			&k.Distance,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "query_codebase_knowledge",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}
		list = append(list, k)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "query_codebase_knowledge",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return list, nil
}
