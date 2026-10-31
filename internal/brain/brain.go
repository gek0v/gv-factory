// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"time"
)

// ArchitecturalRule represents an established architecture constraint or pattern.
type ArchitecturalRule struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"`
	CreatedAt   time.Time `json:"created_at"`
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

// Service provides access to the persistent memory layer (The Brain).
type Service struct{}

// NewService constructs a new Brain service instance.
func NewService() *Service {
	return &Service{}
}

// QueryRelevantRules retrieves architectural rules matching the context query.
func (s *Service) QueryRelevantRules(ctx context.Context, query string) ([]ArchitecturalRule, error) {
	return []ArchitecturalRule{}, nil
}

// IndexFile parses and indexes a code file into AST fragments with embeddings.
func (s *Service) IndexFile(ctx context.Context, filePath string, content []byte) error {
	return nil
}
