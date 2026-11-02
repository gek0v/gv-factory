// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
)

// Stats summarizes counts of indexed records across Codebase Brain tables.
type Stats struct {
	IndexedKnowledge int `json:"indexed_knowledge"`
	ActiveRules      int `json:"active_rules"`
	TaskEpisodes     int `json:"task_episodes"`
}

// GetStats queries counts of indexed knowledge records, active architectural rules, and past task episodes.
func (s *Service) GetStats(ctx context.Context) (*Stats, error) {
	db := s.getDB()
	if db == nil {
		return nil, &ConnectionError{
			Op:  "get_stats",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: errors.New("connection pool is not initialized"),
		}
	}

	const query = `
		SELECT 
			(SELECT COUNT(*) FROM codebase_knowledge) AS indexed_knowledge,
			(SELECT COUNT(*) FROM architectural_rules) AS active_rules,
			(SELECT COUNT(*) FROM task_episodes) AS task_episodes
	`

	var stats Stats
	err := db.QueryRow(ctx, query).Scan(
		&stats.IndexedKnowledge,
		&stats.ActiveRules,
		&stats.TaskEpisodes,
	)
	if err != nil {
		return nil, &ConnectionError{
			Op:  "get_stats",
			URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
			Err: err,
		}
	}

	return &stats, nil
}
