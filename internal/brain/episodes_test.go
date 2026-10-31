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

func TestTaskEpisode_Validate(t *testing.T) {
	validEpisode := TaskEpisode{
		RunID:          "run-101",
		TaskName:       "implement auth",
		Outcome:        "SUCCESS",
		Summary:        "Implemented token auth and refresh",
		LessonsLearned: "Use HMAC-SHA256 for approval tokens",
	}

	t.Run("valid episode without embedding succeeds", func(t *testing.T) {
		ep := validEpisode
		if err := ep.Validate(); err != nil {
			t.Fatalf("expected valid episode to pass, got: %v", err)
		}
	})

	t.Run("fails when run_id is empty", func(t *testing.T) {
		ep := validEpisode
		ep.RunID = "  "
		if err := ep.Validate(); err == nil {
			t.Fatal("expected error for empty run_id, got nil")
		}
	})

	t.Run("fails when task_name is empty", func(t *testing.T) {
		ep := validEpisode
		ep.TaskName = ""
		if err := ep.Validate(); err == nil {
			t.Fatal("expected error for empty task_name, got nil")
		}
	})

	t.Run("fails when outcome is empty", func(t *testing.T) {
		ep := validEpisode
		ep.Outcome = ""
		if err := ep.Validate(); err == nil {
			t.Fatal("expected error for empty outcome, got nil")
		}
	})

	t.Run("fails when summary is empty", func(t *testing.T) {
		ep := validEpisode
		ep.Summary = "  "
		if err := ep.Validate(); err == nil {
			t.Fatal("expected error for empty summary, got nil")
		}
	})

	t.Run("fails when embedding dimension is invalid", func(t *testing.T) {
		ep := validEpisode
		ep.Embedding = make([]float32, 256)
		err := ep.Validate()
		if err == nil {
			t.Fatal("expected error for invalid vector dimension, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got %v", err)
		}
	})

	t.Run("succeeds when embedding has exactly 1536 dimensions", func(t *testing.T) {
		ep := validEpisode
		ep.Embedding = make([]float32, EpisodeVectorDimensions)
		if err := ep.Validate(); err != nil {
			t.Fatalf("expected 1536-dim embedding to succeed, got: %v", err)
		}
	})
}

func TestEpisodes_NilPoolHandling(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("CreateEpisode returns ConnectionError on nil pool", func(t *testing.T) {
		ep := TaskEpisode{
			RunID:    "run-1",
			TaskName: "test",
			Outcome:  "SUCCESS",
			Summary:  "test summary",
		}
		_, err := svc.CreateEpisode(ctx, ep)
		if err == nil {
			t.Fatal("expected error on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("GetEpisodesByRunID returns ConnectionError on nil pool", func(t *testing.T) {
		_, err := svc.GetEpisodesByRunID(ctx, "run-1")
		if err == nil {
			t.Fatal("expected error on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("QuerySimilarEpisodes returns ConnectionError on nil pool", func(t *testing.T) {
		vec := make([]float32, EpisodeVectorDimensions)
		_, err := svc.QuerySimilarEpisodes(ctx, vec, 5)
		if err == nil {
			t.Fatal("expected error on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})
}

func TestEpisodes_InputValidation(t *testing.T) {
	svc := &Service{
		cfg: Config{DatabaseURL: "postgres://ov_user:pass@localhost:5432/ov_factory"},
	}
	ctx := context.Background()

	t.Run("CreateEpisode rejects invalid episode before DB access", func(t *testing.T) {
		invalidEp := TaskEpisode{
			RunID: "",
		}
		_, err := svc.CreateEpisode(ctx, invalidEp)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !errors.Is(err, ErrInvalidEpisode) {
			t.Fatalf("expected ErrInvalidEpisode, got %v", err)
		}
	})

	t.Run("GetEpisodesByRunID rejects empty run_id", func(t *testing.T) {
		_, err := svc.GetEpisodesByRunID(ctx, "   ")
		if err == nil {
			t.Fatal("expected error for empty run_id, got nil")
		}
		if !errors.Is(err, ErrInvalidEpisode) {
			t.Fatalf("expected ErrInvalidEpisode, got %v", err)
		}
	})

	t.Run("QuerySimilarEpisodes rejects invalid vector dimensions", func(t *testing.T) {
		badVec := make([]float32, 128)
		_, err := svc.QuerySimilarEpisodes(ctx, badVec, 5)
		if err == nil {
			t.Fatal("expected error for bad vector dimensions, got nil")
		}
		if !errors.Is(err, ErrInvalidVectorDimensions) {
			t.Fatalf("expected ErrInvalidVectorDimensions, got %v", err)
		}
	})
}

func TestEpisodes_MockDatabase(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateEpisode success", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 14, 0, 0, 0, time.UTC)
		vec := make([]float32, EpisodeVectorDimensions)
		vec[0] = 0.8
		ep := TaskEpisode{
			RunID:          "run-200",
			TaskName:       "fix sandbox lock",
			Outcome:        "SUCCESS",
			Summary:        "Pruned dangling git index.lock",
			LessonsLearned: "Always check worktree index.lock before spawn",
			Embedding:      vec,
		}

		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					values: []any{
						"uuid-ep-1",
						ep.RunID,
						ep.TaskName,
						ep.Outcome,
						ep.Summary,
						ep.LessonsLearned,
						fixedTime,
					},
				}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		created, err := svc.CreateEpisode(ctx, ep)
		if err != nil {
			t.Fatalf("unexpected error creating episode: %v", err)
		}
		if created.ID != "uuid-ep-1" {
			t.Fatalf("expected ID uuid-ep-1, got %s", created.ID)
		}
		if created.RunID != ep.RunID {
			t.Fatalf("expected RunID %s, got %s", ep.RunID, created.RunID)
		}
	})

	t.Run("CreateEpisode database error", func(t *testing.T) {
		mock := &mockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: errors.New("write failure")}
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		ep := TaskEpisode{
			RunID:    "run-fail",
			TaskName: "fail task",
			Outcome:  "FAILED",
			Summary:  "Failed",
		}

		_, err := svc.CreateEpisode(ctx, ep)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T", err)
		}
	})

	t.Run("GetEpisodesByRunID success and query error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 14, 0, 0, 0, time.UTC)

		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				runID := args[0].(string)
				if runID == "run-err" {
					return nil, errors.New("read error")
				}
				return &mockRows{
					data: [][]any{
						{"uuid-ep-1", runID, "task 1", "SUCCESS", "Summary 1", "Lesson 1", fixedTime},
						{"uuid-ep-2", runID, "task 2", "REJECTED", "Summary 2", "Lesson 2", fixedTime},
					},
				}, nil
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		episodes, err := svc.GetEpisodesByRunID(ctx, "run-valid")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(episodes) != 2 {
			t.Fatalf("expected 2 episodes, got %d", len(episodes))
		}
		if episodes[1].Outcome != "REJECTED" {
			t.Fatalf("expected outcome REJECTED, got %s", episodes[1].Outcome)
		}

		_, err = svc.GetEpisodesByRunID(ctx, "run-err")
		if err == nil {
			t.Fatal("expected error on read failure, got nil")
		}
	})

	t.Run("QuerySimilarEpisodes success and query error", func(t *testing.T) {
		fixedTime := time.Date(2026, 10, 31, 14, 0, 0, 0, time.UTC)
		queryVec := make([]float32, EpisodeVectorDimensions)

		mock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				limit := args[1].(int)
				if limit == 5 {
					return nil, errors.New("timeout querying vector index")
				}
				return &mockRows{
					data: [][]any{
						{"uuid-ep-1", "run-1", "task 1", "SUCCESS", "Summary 1", "Lesson 1", fixedTime, float32(0.08)},
						{"uuid-ep-2", "run-2", "task 2", "FAILED", "Summary 2", "Lesson 2", fixedTime, float32(0.72)},
					},
				}, nil
			},
		}

		svc := &Service{
			cfg: DefaultConfig(),
			db:  mock,
		}

		episodes, err := svc.QuerySimilarEpisodes(ctx, queryVec, 0) // default limit 10
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(episodes) != 2 {
			t.Fatalf("expected 2 episodes, got %d", len(episodes))
		}
		if episodes[0].Distance != 0.08 {
			t.Fatalf("expected distance 0.08, got %f", episodes[0].Distance)
		}

		_, err = svc.QuerySimilarEpisodes(ctx, queryVec, 5)
		if err == nil {
			t.Fatal("expected error on timeout, got nil")
		}
	})
}

func TestTaskEpisodes_LiveIntegration(t *testing.T) {
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

	vecA := make([]float32, EpisodeVectorDimensions)
	vecA[0] = 1.0

	vecB := make([]float32, EpisodeVectorDimensions)
	vecB[1] = 1.0

	queryVec := make([]float32, EpisodeVectorDimensions)
	queryVec[0] = 0.98
	queryVec[1] = 0.08

	runID := "test-run-ep-live"

	ep1 := TaskEpisode{
		RunID:          runID,
		TaskName:       "fix sandbox worktree",
		Outcome:        "SUCCESS",
		Summary:        "Pruned stale git index locks before mount",
		LessonsLearned: "Stale index.lock blocks git operations",
		Embedding:      vecA,
	}
	created1, err := svc.CreateEpisode(ctx, ep1)
	if err != nil {
		t.Fatalf("failed to create episode 1: %v", err)
	}
	if created1.ID == "" {
		t.Fatal("expected generated UUID for created episode 1")
	}

	ep2 := TaskEpisode{
		RunID:          runID,
		TaskName:       "unrelated task",
		Outcome:        "REJECTED",
		Summary:        "Wrong interface design",
		LessonsLearned: "Always verify interface seams first",
		Embedding:      vecB,
	}
	created2, err := svc.CreateEpisode(ctx, ep2)
	if err != nil {
		t.Fatalf("failed to create episode 2: %v", err)
	}
	if created2.ID == "" {
		t.Fatal("expected generated UUID for created episode 2")
	}

	// Test GetEpisodesByRunID
	episodes, err := svc.GetEpisodesByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("failed to get episodes by run ID: %v", err)
	}
	if len(episodes) < 2 {
		t.Fatalf("expected at least 2 episodes for run %s, got %d", runID, len(episodes))
	}

	// Test QuerySimilarEpisodes
	similar, err := svc.QuerySimilarEpisodes(ctx, queryVec, 2)
	if err != nil {
		t.Fatalf("failed to query similar episodes: %v", err)
	}
	if len(similar) < 2 {
		t.Fatalf("expected at least 2 similar episodes, got %d", len(similar))
	}

	if similar[0].ID != created1.ID {
		t.Fatalf("expected most similar episode to be %s, got %s (distance %f vs %f)",
			created1.ID, similar[0].ID, similar[0].Distance, similar[1].Distance)
	}
	if similar[0].Distance >= similar[1].Distance {
		t.Fatalf("expected episode 1 distance (%f) < episode 2 distance (%f)",
			similar[0].Distance, similar[1].Distance)
	}
}
