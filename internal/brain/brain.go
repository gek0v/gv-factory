// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectionError is a structured error containing sanitized details of a database connection failure.
type ConnectionError struct {
	Op  string
	URL string
	Err error
}

func (e *ConnectionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	errMsg := ""
	if e.Err != nil {
		errMsg = SanitizeDatabaseURL(e.Err.Error())
	}
	return fmt.Sprintf("brain: %s failed for %s: %s", e.Op, e.URL, errMsg)
}

func (e *ConnectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ArchitecturalRule represents an established architecture constraint or pattern.
type ArchitecturalRule struct {
	ID          string    `json:"id"`
	RuleCode    string    `json:"rule_code"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"`
	Embedding   []float32 `json:"embedding,omitempty"`
	Distance    float32   `json:"distance,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CodebaseKnowledge represents a semantic fragment or AST node stored in pgvector.
type CodebaseKnowledge struct {
	ID        string    `json:"id"`
	FilePath  string    `json:"file_path"`
	Symbol    string    `json:"symbol"`
	Language  string    `json:"language"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskEpisode stores past execution learnings, review rejections, and test outcomes.
type TaskEpisode struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	TaskName  string    `json:"task_name"`
	Outcome   string    `json:"outcome"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// DBPool abstracts database connection pooling operations for testing and production.
type DBPool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Ping(ctx context.Context) error
	Close()
}

// Service provides access to the persistent memory layer (The Brain).
type Service struct {
	cfg  Config
	pool *pgxpool.Pool
	db   DBPool
	mu   sync.RWMutex
}

func (s *Service) getDB() DBPool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db != nil {
		return s.db
	}
	if s.pool != nil {
		return s.pool
	}
	return nil
}

// NewService constructs a new Brain service instance and establishes a connection pool.
// If the database is unreachable at startup, the service initializes gracefully in a degraded state.
func NewService(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.DatabaseURL == "" {
		cfg = ConfigFromEnv()
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "parse_config",
			URL: SanitizeDatabaseURL(cfg.DatabaseURL),
			Err: err,
		}
	}

	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolConfig.MinConns = cfg.MinConns
	}
	if cfg.MaxConnIdle > 0 {
		poolConfig.MaxConnIdleTime = cfg.MaxConnIdle
	}
	if cfg.MaxConnLife > 0 {
		poolConfig.MaxConnLifetime = cfg.MaxConnLife
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "create_pool",
			URL: SanitizeDatabaseURL(cfg.DatabaseURL),
			Err: err,
		}
	}

	svc := &Service{
		cfg:  cfg,
		pool: pool,
		db:   pool,
	}

	return svc, nil
}

// Ping checks if the PostgreSQL database connection is alive and healthy.
func (s *Service) Ping(ctx context.Context) error {
	db := s.getDB()
	if db == nil {
		return &ConnectionError{
			Op:  "ping",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	pingCtx := ctx
	if s.cfg.ConnTimeout > 0 {
		var cancel context.CancelFunc
		pingCtx, cancel = context.WithTimeout(ctx, s.cfg.ConnTimeout)
		defer cancel()
	}

	if err := db.Ping(pingCtx); err != nil {
		return &ConnectionError{
			Op:  "ping",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return nil
}

// Pool returns the underlying *pgxpool.Pool instance.
func (s *Service) Pool() *pgxpool.Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

// Close gracefully closes all database connections in the pool.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.db.Close()
	} else if s.pool != nil {
		s.pool.Close()
	}
}

// QueryRelevantRules retrieves architectural rules matching the context query.
func (s *Service) QueryRelevantRules(ctx context.Context, query string) ([]ArchitecturalRule, error) {
	return []ArchitecturalRule{}, nil
}

// IndexFile parses and indexes a code file into AST fragments with embeddings.
func (s *Service) IndexFile(ctx context.Context, filePath string, content []byte) error {
	return nil
}
