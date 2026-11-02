// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pgvector/pgvector-go"
)

// EpisodeVectorDimensions is the dimensionality of pgvector embeddings for task episodes.
const EpisodeVectorDimensions = 1536

// ErrInvalidEpisode is returned when required fields in TaskEpisode are missing or invalid.
var ErrInvalidEpisode = errors.New("brain: invalid task episode")

// Validate checks whether a TaskEpisode has all required fields and valid dimensions.
func (ep *TaskEpisode) Validate() error {
	ep.RunID = strings.TrimSpace(ep.RunID)
	if ep.RunID == "" {
		return fmt.Errorf("%w: run_id is required", ErrInvalidEpisode)
	}

	ep.TaskName = strings.TrimSpace(ep.TaskName)
	if ep.TaskName == "" {
		return fmt.Errorf("%w: task_name is required", ErrInvalidEpisode)
	}

	ep.Outcome = strings.TrimSpace(ep.Outcome)
	if ep.Outcome == "" {
		return fmt.Errorf("%w: outcome is required", ErrInvalidEpisode)
	}

	ep.Summary = strings.TrimSpace(ep.Summary)
	if ep.Summary == "" {
		return fmt.Errorf("%w: summary is required", ErrInvalidEpisode)
	}

	if len(ep.Embedding) > 0 && len(ep.Embedding) != EpisodeVectorDimensions {
		return ErrInvalidVectorDimensions
	}

	return nil
}

// CreateEpisode persists a new task execution episode into the database.
func (s *Service) CreateEpisode(ctx context.Context, episode TaskEpisode) (*TaskEpisode, error) {
	if err := episode.Validate(); err != nil {
		return nil, err
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "create_episode",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	var vec *pgvector.Vector
	if len(episode.Embedding) > 0 {
		v := pgvector.NewVector(episode.Embedding)
		vec = &v
	}

	const query = `
		INSERT INTO task_episodes (run_id, task_name, outcome, summary, lessons_learned, embedding)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, run_id, task_name, outcome, summary, lessons_learned, created_at
	`

	var created TaskEpisode
	err := db.QueryRow(ctx, query,
		episode.RunID,
		episode.TaskName,
		episode.Outcome,
		episode.Summary,
		episode.LessonsLearned,
		vec,
	).Scan(
		&created.ID,
		&created.RunID,
		&created.TaskName,
		&created.Outcome,
		&created.Summary,
		&created.LessonsLearned,
		&created.CreatedAt,
	)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "create_episode",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	created.Embedding = episode.Embedding
	return &created, nil
}

// RecordEpisode persists a task episode into the brain database.
// It implements agent.EpisodeRecorder.
func (s *Service) RecordEpisode(ctx context.Context, episode TaskEpisode) (*TaskEpisode, error) {
	return s.CreateEpisode(ctx, episode)
}

// GetEpisodesByRunID fetches all episodic records associated with a specific factory run.
func (s *Service) GetEpisodesByRunID(ctx context.Context, runID string) ([]TaskEpisode, error) {
	id := strings.TrimSpace(runID)
	if id == "" {
		return nil, fmt.Errorf("%w: run_id is required", ErrInvalidEpisode)
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "get_episodes_by_run_id",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, run_id, task_name, outcome, summary, lessons_learned, embedding, created_at
		FROM task_episodes
		WHERE run_id = $1
		ORDER BY created_at ASC
	`

	rows, err := db.Query(ctx, query, id)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "get_episodes_by_run_id",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var episodes []TaskEpisode
	for rows.Next() {
		var ep TaskEpisode
		var vec *pgvector.Vector
		if err := rows.Scan(
			&ep.ID,
			&ep.RunID,
			&ep.TaskName,
			&ep.Outcome,
			&ep.Summary,
			&ep.LessonsLearned,
			&vec,
			&ep.CreatedAt,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "get_episodes_by_run_id",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}
		if vec != nil {
			ep.Embedding = vec.Slice()
		}
		episodes = append(episodes, ep)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "get_episodes_by_run_id",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return episodes, nil
}

// QuerySimilarEpisodes performs a cosine similarity search against the task_episodes HNSW index.
func (s *Service) QuerySimilarEpisodes(ctx context.Context, embedding []float32, limit int) ([]TaskEpisode, error) {
	if len(embedding) != EpisodeVectorDimensions {
		return nil, ErrInvalidVectorDimensions
	}

	if limit <= 0 {
		limit = 10
	}

	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "query_similar_episodes",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT id, run_id, task_name, outcome, summary, lessons_learned, created_at, (embedding <=> $1) as distance
		FROM task_episodes
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1 ASC
		LIMIT $2
	`

	vec := pgvector.NewVector(embedding)
	rows, err := db.Query(ctx, query, vec, limit)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "query_similar_episodes",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}
	defer rows.Close()

	var episodes []TaskEpisode
	for rows.Next() {
		var ep TaskEpisode
		if err := rows.Scan(
			&ep.ID,
			&ep.RunID,
			&ep.TaskName,
			&ep.Outcome,
			&ep.Summary,
			&ep.LessonsLearned,
			&ep.CreatedAt,
			&ep.Distance,
		); err != nil {
			return nil, &ConnectionError{
				Op:  "query_similar_episodes",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: err,
			}
		}
		episodes = append(episodes, ep)
	}

	if err := rows.Err(); err != nil {
		return nil, &ConnectionError{
			Op:  "query_similar_episodes",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return episodes, nil
}
