// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

const (
	// DefaultHybridAlpha is the default balance weight between vector similarity (0.7) and lexical FTS (0.3).
	DefaultHybridAlpha float32 = 0.7

	// DefaultHybridLimit is the default result limit when unspecified.
	DefaultHybridLimit int = 10

	// MinHybridLimit is the minimum allowable search limit.
	MinHybridLimit int = 1

	// MaxHybridLimit is the maximum allowable search limit.
	MaxHybridLimit int = 100
)

var (
	// ErrInvalidHybridQuery is returned when a hybrid search query lacks both text and embedding vector.
	ErrInvalidHybridQuery = errors.New("brain: hybrid search query requires at least query text or embedding vector")

	// ErrInvalidHybridAlpha is returned when the alpha weight is outside [0.0, 1.0].
	ErrInvalidHybridAlpha = errors.New("brain: hybrid search alpha must be between 0.0 and 1.0")
)

// Float32 returns a pointer to the provided float32 value, useful for setting query.Alpha.
func Float32(v float32) *float32 {
	return &v
}

// HybridSearchQuery specifies parameters for combined vector and lexical retrieval.
type HybridSearchQuery struct {
	Query     string    `json:"query"`
	Embedding []float32 `json:"embedding,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Alpha     *float32  `json:"alpha,omitempty"`
}

// NewHybridSearchQuery constructs a new HybridSearchQuery with default limit and alpha.
func NewHybridSearchQuery(query string) HybridSearchQuery {
	alpha := DefaultHybridAlpha
	return HybridSearchQuery{
		Query: query,
		Limit: DefaultHybridLimit,
		Alpha: &alpha,
	}
}

// WithAlpha returns a copy of the query with the specified alpha balance weight.
func (q HybridSearchQuery) WithAlpha(alpha float32) HybridSearchQuery {
	q.Alpha = &alpha
	return q
}

// WithLimit returns a copy of the query with the specified limit.
func (q HybridSearchQuery) WithLimit(limit int) HybridSearchQuery {
	q.Limit = limit
	return q
}

// WithEmbedding returns a copy of the query with the specified embedding vector.
func (q HybridSearchQuery) WithEmbedding(emb []float32) HybridSearchQuery {
	q.Embedding = emb
	return q
}

// EffectiveAlpha returns the configured alpha or DefaultHybridAlpha if unset.
func (q *HybridSearchQuery) EffectiveAlpha() float32 {
	if q.Alpha == nil {
		return DefaultHybridAlpha
	}
	return *q.Alpha
}

// EffectiveLimit returns the clamped limit between [MinHybridLimit, MaxHybridLimit] (default DefaultHybridLimit).
func (q *HybridSearchQuery) EffectiveLimit() int {
	if q.Limit <= 0 {
		return DefaultHybridLimit
	}
	if q.Limit > MaxHybridLimit {
		return MaxHybridLimit
	}
	return q.Limit
}

// Validate checks whether the search query has valid parameters.
func (q *HybridSearchQuery) Validate() error {
	trimmedQuery := strings.TrimSpace(q.Query)
	hasQuery := trimmedQuery != ""
	hasVec := len(q.Embedding) > 0

	if !hasQuery && !hasVec {
		return ErrInvalidHybridQuery
	}

	if hasVec && len(q.Embedding) != KnowledgeVectorDimensions {
		return ErrInvalidVectorDimensions
	}

	if q.Alpha != nil {
		if *q.Alpha < 0.0 || *q.Alpha > 1.0 {
			return ErrInvalidHybridAlpha
		}
	}

	return nil
}

// CodebaseKnowledgeResult wraps a CodebaseKnowledge record with hybrid search ranking scores.
type CodebaseKnowledgeResult struct {
	CodebaseKnowledge
	Score        float32 `json:"score"`         // Unified normalized confidence score [0.0, 1.0]
	VectorScore  float32 `json:"vector_score"`  // Semantic vector similarity score [0.0, 1.0]
	LexicalScore float32 `json:"lexical_score"` // Lexical full-text match score [0.0, 1.0]
}

// ArchitecturalRuleResult wraps an ArchitecturalRule record with hybrid search ranking scores.
type ArchitecturalRuleResult struct {
	ArchitecturalRule
	Score        float32 `json:"score"`         // Unified normalized confidence score [0.0, 1.0]
	VectorScore  float32 `json:"vector_score"`  // Semantic vector similarity score [0.0, 1.0]
	LexicalScore float32 `json:"lexical_score"` // Lexical full-text match score [0.0, 1.0]
}

// ComputeVectorScore converts cosine distance into similarity score clamped to [0.0, 1.0].
func ComputeVectorScore(distance float32) float32 {
	sim := 1.0 - distance
	if sim <= 0.0 {
		return 0.0
	}
	if sim >= 1.0 {
		return 1.0
	}
	return sim
}

// NormalizeLexicalScore clamps or normalizes lexical score into [0.0, 1.0].
func NormalizeLexicalScore(rawScore float32) float32 {
	if rawScore <= 0.0 {
		return 0.0
	}
	if rawScore >= 1.0 {
		return 1.0
	}
	return rawScore
}

// ComputeHybridScore combines vector similarity and lexical score using weight alpha:
// Score = alpha * vectorScore + (1 - alpha) * lexicalScore
// Result is strictly clamped within [0.0, 1.0].
func ComputeHybridScore(alpha, vectorScore, lexicalScore float32) float32 {
	score := (alpha * vectorScore) + ((1.0 - alpha) * lexicalScore)
	if score <= 0.0 {
		return 0.0
	}
	if score >= 1.0 {
		return 1.0
	}
	return score
}

// HybridSearchCodebase performs a hybrid search across codebase_knowledge combining
// semantic vector similarity (cosine distance via pgvector) and lexical keyword matching (PostgreSQL FTS).
func (s *Service) HybridSearchCodebase(ctx context.Context, q HybridSearchQuery) ([]CodebaseKnowledgeResult, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "hybrid_search_codebase",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	trimmedQuery := strings.TrimSpace(q.Query)
	hasQuery := trimmedQuery != ""
	hasVec := len(q.Embedding) > 0
	limit := q.EffectiveLimit()

	var rows pgx.Rows
	var err error
	var effectiveAlpha float32

	switch {
	case hasVec && hasQuery:
		effectiveAlpha = q.EffectiveAlpha()
		candidateLimit := limit * 2
		if candidateLimit < 20 {
			candidateLimit = 20
		}

		const hybridQuery = `
			WITH vector_candidates AS (
				SELECT id, (embedding <=> $1) AS distance
				FROM codebase_knowledge
				WHERE embedding IS NOT NULL
				ORDER BY embedding <=> $1 ASC
				LIMIT $3
			),
			lexical_candidates AS (
				SELECT id, ts_rank(fts_doc_en, plainto_tsquery('english', $2)) AS lex_score
				FROM codebase_knowledge
				WHERE fts_doc_en @@ plainto_tsquery('english', $2)
				ORDER BY lex_score DESC
				LIMIT $3
			),
			combined_candidates AS (
				SELECT id FROM vector_candidates
				UNION
				SELECT id FROM lexical_candidates
			)
			SELECT 
				k.id, k.file_path, k.symbol_name, k.symbol_type, k.content, k.content_hash, k.updated_at,
				COALESCE(vc.distance, CASE WHEN k.embedding IS NOT NULL THEN (k.embedding <=> $1) ELSE 1.0 END) AS distance,
				COALESCE(lc.lex_score, ts_rank(k.fts_doc_en, plainto_tsquery('english', $2))) AS lexical_score
			FROM combined_candidates c
			JOIN codebase_knowledge k ON k.id = c.id
			LEFT JOIN vector_candidates vc ON vc.id = c.id
			LEFT JOIN lexical_candidates lc ON lc.id = c.id
		`
		vec := pgvector.NewVector(q.Embedding)
		rows, err = db.Query(ctx, hybridQuery, vec, trimmedQuery, candidateLimit)

	case hasQuery:
		effectiveAlpha = 0.0
		const lexicalOnlyQuery = `
			SELECT 
				id, file_path, symbol_name, symbol_type, content, content_hash, updated_at,
				1.0 AS distance,
				ts_rank(fts_doc_en, plainto_tsquery('english', $1)) AS lexical_score
			FROM codebase_knowledge
			WHERE fts_doc_en @@ plainto_tsquery('english', $1)
			ORDER BY lexical_score DESC
			LIMIT $2
		`
		rows, err = db.Query(ctx, lexicalOnlyQuery, trimmedQuery, limit)

	case hasVec:
		effectiveAlpha = 1.0
		const vectorOnlyQuery = `
			SELECT 
				id, file_path, symbol_name, symbol_type, content, content_hash, updated_at,
				(embedding <=> $1) AS distance,
				0.0 AS lexical_score
			FROM codebase_knowledge
			WHERE embedding IS NOT NULL
			ORDER BY embedding <=> $1 ASC
			LIMIT $2
		`
		vec := pgvector.NewVector(q.Embedding)
		rows, err = db.Query(ctx, vectorOnlyQuery, vec, limit)
	}

	if err != nil {
		return nil, &ConnectionError{
			Op:  "hybrid_search_codebase",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var results []CodebaseKnowledgeResult
	for rows.Next() {
		var (
			res          CodebaseKnowledgeResult
			distanceVal  float32
			lexicalScore float32
		)
		if err := rows.Scan(
			&res.ID,
			&res.FilePath,
			&res.SymbolName,
			&res.SymbolType,
			&res.Content,
			&res.ContentHash,
			&res.UpdatedAt,
			&distanceVal,
			&lexicalScore,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "hybrid_search_codebase",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}

		res.Distance = distanceVal
		res.VectorScore = ComputeVectorScore(distanceVal)
		res.LexicalScore = NormalizeLexicalScore(lexicalScore)
		res.Score = ComputeHybridScore(effectiveAlpha, res.VectorScore, res.LexicalScore)

		results = append(results, res)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "hybrid_search_codebase",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].VectorScore != results[j].VectorScore {
			return results[i].VectorScore > results[j].VectorScore
		}
		return results[i].LexicalScore > results[j].LexicalScore
	})

	if len(results) > limit {
		results = results[:limit]
	}

	if results == nil {
		results = []CodebaseKnowledgeResult{}
	}

	return results, nil
}

// HybridSearchRules performs a hybrid search across architectural_rules combining
// semantic vector similarity (cosine distance via pgvector) and lexical keyword matching (PostgreSQL FTS).
func (s *Service) HybridSearchRules(ctx context.Context, q HybridSearchQuery) ([]ArchitecturalRuleResult, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "hybrid_search_rules",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	trimmedQuery := strings.TrimSpace(q.Query)
	hasQuery := trimmedQuery != ""
	hasVec := len(q.Embedding) > 0
	limit := q.EffectiveLimit()

	var rows pgx.Rows
	var err error
	var effectiveAlpha float32

	switch {
	case hasVec && hasQuery:
		effectiveAlpha = q.EffectiveAlpha()
		candidateLimit := limit * 2
		if candidateLimit < 20 {
			candidateLimit = 20
		}

		const hybridRulesQuery = `
			WITH vector_candidates AS (
				SELECT id, (embedding <=> $1) AS distance
				FROM architectural_rules
				WHERE embedding IS NOT NULL
				ORDER BY embedding <=> $1 ASC
				LIMIT $3
			),
			lexical_candidates AS (
				SELECT id, ts_rank(fts_doc_en, plainto_tsquery('english', $2)) AS lex_score
				FROM architectural_rules
				WHERE fts_doc_en @@ plainto_tsquery('english', $2)
				ORDER BY lex_score DESC
				LIMIT $3
			),
			combined_candidates AS (
				SELECT id FROM vector_candidates
				UNION
				SELECT id FROM lexical_candidates
			)
			SELECT 
				r.id, r.rule_code, r.title, r.description, r.severity, r.created_at, r.updated_at,
				COALESCE(vc.distance, CASE WHEN r.embedding IS NOT NULL THEN (r.embedding <=> $1) ELSE 1.0 END) AS distance,
				COALESCE(lc.lex_score, ts_rank(r.fts_doc_en, plainto_tsquery('english', $2))) AS lexical_score
			FROM combined_candidates c
			JOIN architectural_rules r ON r.id = c.id
			LEFT JOIN vector_candidates vc ON vc.id = c.id
			LEFT JOIN lexical_candidates lc ON lc.id = c.id
		`
		vec := pgvector.NewVector(q.Embedding)
		rows, err = db.Query(ctx, hybridRulesQuery, vec, trimmedQuery, candidateLimit)

	case hasQuery:
		effectiveAlpha = 0.0
		const lexicalRulesQuery = `
			SELECT 
				id, rule_code, title, description, severity, created_at, updated_at,
				1.0 AS distance,
				ts_rank(fts_doc_en, plainto_tsquery('english', $1)) AS lexical_score
			FROM architectural_rules
			WHERE fts_doc_en @@ plainto_tsquery('english', $1)
			ORDER BY lexical_score DESC
			LIMIT $2
		`
		rows, err = db.Query(ctx, lexicalRulesQuery, trimmedQuery, limit)

	case hasVec:
		effectiveAlpha = 1.0
		const vectorRulesQuery = `
			SELECT 
				id, rule_code, title, description, severity, created_at, updated_at,
				(embedding <=> $1) AS distance,
				0.0 AS lexical_score
			FROM architectural_rules
			WHERE embedding IS NOT NULL
			ORDER BY embedding <=> $1 ASC
			LIMIT $2
		`
		vec := pgvector.NewVector(q.Embedding)
		rows, err = db.Query(ctx, vectorRulesQuery, vec, limit)
	}

	if err != nil {
		return nil, &ConnectionError{
			Op:  "hybrid_search_rules",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var results []ArchitecturalRuleResult
	for rows.Next() {
		var (
			res          ArchitecturalRuleResult
			distanceVal  float32
			lexicalScore float32
		)
		if err := rows.Scan(
			&res.ID,
			&res.RuleCode,
			&res.Title,
			&res.Description,
			&res.Severity,
			&res.CreatedAt,
			&res.UpdatedAt,
			&distanceVal,
			&lexicalScore,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "hybrid_search_rules",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}

		res.Distance = distanceVal
		res.VectorScore = ComputeVectorScore(distanceVal)
		res.LexicalScore = NormalizeLexicalScore(lexicalScore)
		res.Score = ComputeHybridScore(effectiveAlpha, res.VectorScore, res.LexicalScore)

		results = append(results, res)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "hybrid_search_rules",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].VectorScore != results[j].VectorScore {
			return results[i].VectorScore > results[j].VectorScore
		}
		return results[i].LexicalScore > results[j].LexicalScore
	})

	if len(results) > limit {
		results = results[:limit]
	}

	if results == nil {
		results = []ArchitecturalRuleResult{}
	}

	return results, nil
}
