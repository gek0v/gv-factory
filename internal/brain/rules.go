// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

// RuleVectorDimensions is the dimensionality of pgvector embeddings for architectural rules.
const RuleVectorDimensions = 1536

var (
	// ErrInvalidVectorDimensions is returned when an embedding does not match RuleVectorDimensions.
	ErrInvalidVectorDimensions = fmt.Errorf("brain: embedding must have exactly %d dimensions", RuleVectorDimensions)

	// ErrInvalidRule is returned when required fields in ArchitecturalRule are missing or invalid.
	ErrInvalidRule = errors.New("brain: invalid architectural rule")

	// ErrRuleNotFound is returned when an architectural rule does not exist.
	ErrRuleNotFound = errors.New("brain: architectural rule not found")
)

// Validate checks whether an ArchitecturalRule has all required fields and valid dimensions.
func (r *ArchitecturalRule) Validate() error {
	r.RuleCode = strings.TrimSpace(r.RuleCode)
	if r.RuleCode == "" {
		return fmt.Errorf("%w: rule_code is required", ErrInvalidRule)
	}

	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidRule)
	}

	r.Description = strings.TrimSpace(r.Description)
	if r.Description == "" {
		return fmt.Errorf("%w: description is required", ErrInvalidRule)
	}

	r.Severity = strings.TrimSpace(r.Severity)
	if r.Severity == "" {
		r.Severity = "ERROR"
	}

	if len(r.Embedding) > 0 && len(r.Embedding) != RuleVectorDimensions {
		return ErrInvalidVectorDimensions
	}

	return nil
}

// CreateRule inserts a new architectural rule into the database.
func (s *Service) CreateRule(ctx context.Context, rule ArchitecturalRule) (*ArchitecturalRule, error) {
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "create_rule",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	var vec *pgvector.Vector
	if len(rule.Embedding) > 0 {
		v := pgvector.NewVector(rule.Embedding)
		vec = &v
	}

	const query = `
		INSERT INTO architectural_rules (rule_code, title, description, severity, embedding)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, rule_code, title, description, severity, created_at, updated_at
	`

	var created ArchitecturalRule
	err := db.QueryRow(ctx, query, rule.RuleCode, rule.Title, rule.Description, rule.Severity, vec).Scan(
		&created.ID,
		&created.RuleCode,
		&created.Title,
		&created.Description,
		&created.Severity,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "create_rule",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	created.Embedding = rule.Embedding
	return &created, nil
}

// GetRuleByCode retrieves a single architectural rule by its unique rule code.
func (s *Service) GetRuleByCode(ctx context.Context, ruleCode string) (*ArchitecturalRule, error) {
	code := strings.TrimSpace(ruleCode)
	if code == "" {
		return nil, fmt.Errorf("%w: rule_code is required", ErrInvalidRule)
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "get_rule_by_code",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, rule_code, title, description, severity, embedding, created_at, updated_at
		FROM architectural_rules
		WHERE rule_code = $1
	`

	var rule ArchitecturalRule
	var vec *pgvector.Vector
	err := db.QueryRow(ctx, query, code).Scan(
		&rule.ID,
		&rule.RuleCode,
		&rule.Title,
		&rule.Description,
		&rule.Severity,
		&vec,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRuleNotFound
		}
		return nil, &ConnectionError{
			Op:  "get_rule_by_code",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	if vec != nil {
		rule.Embedding = vec.Slice()
	}
	return &rule, nil
}

// ListRules retrieves existing architectural rules up to the specified limit.
func (s *Service) ListRules(ctx context.Context, limit int) ([]ArchitecturalRule, error) {
	if limit <= 0 {
		limit = 50
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "list_rules",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, rule_code, title, description, severity, created_at, updated_at
		FROM architectural_rules
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := db.Query(ctx, query, limit)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "list_rules",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var rules []ArchitecturalRule
	for rows.Next() {
		var r ArchitecturalRule
		if err := rows.Scan(
			&r.ID,
			&r.RuleCode,
			&r.Title,
			&r.Description,
			&r.Severity,
			&r.CreatedAt,
			&r.UpdatedAt,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "list_rules",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}
		rules = append(rules, r)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "list_rules",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return rules, nil
}

// QueryRelevantRulesByVector performs a cosine similarity search against the HNSW index.
func (s *Service) QueryRelevantRulesByVector(ctx context.Context, embedding []float32, limit int) ([]ArchitecturalRule, error) {
	if len(embedding) != RuleVectorDimensions {
		return nil, ErrInvalidVectorDimensions
	}

	if limit <= 0 {
		limit = 10
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "query_relevant_rules_by_vector",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, rule_code, title, description, severity, created_at, updated_at, (embedding <=> $1) as distance
		FROM architectural_rules
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1 ASC
		LIMIT $2
	`

	vec := pgvector.NewVector(embedding)
	rows, err := db.Query(ctx, query, vec, limit)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "query_relevant_rules_by_vector",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var rules []ArchitecturalRule
	for rows.Next() {
		var r ArchitecturalRule
		if err := rows.Scan(
			&r.ID,
			&r.RuleCode,
			&r.Title,
			&r.Description,
			&r.Severity,
			&r.CreatedAt,
			&r.UpdatedAt,
			&r.Distance,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "query_relevant_rules_by_vector",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}
		rules = append(rules, r)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "query_relevant_rules_by_vector",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return rules, nil
}
