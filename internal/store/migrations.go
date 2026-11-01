// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Migration represents a single schema evolution file.
type Migration struct {
	Version string
	Name    string
	SQL     string
}

// LoadEmbeddedMigrations reads and sorts migrations from the embedded filesystem.
func LoadEmbeddedMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := fs.ReadFile(embeddedMigrations, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %q: %w", entry.Name(), err)
		}
		version := strings.Split(entry.Name(), "_")[0]
		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(data),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Name < migrations[j].Name
	})

	return migrations, nil
}

// Migrate applies pending migrations to a PostgreSQL database idempotently.
func Migrate(ctx context.Context, tx pgx.Tx) error {
	// 1. Ensure schema_migrations table exists
	initTableSQL := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := tx.Exec(ctx, initTableSQL); err != nil {
		return fmt.Errorf("failed to ensure schema_migrations table: %w", err)
	}

	// 2. Fetch already applied versions
	rows, err := tx.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading applied migrations: %w", err)
	}

	// 3. Load embedded migrations
	migrations, err := LoadEmbeddedMigrations()
	if err != nil {
		return err
	}

	// 4. Apply unapplied migrations in order
	for _, m := range migrations {
		if applied[m.Name] || applied[m.Version] {
			continue
		}

		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("failed to execute migration %q: %w", m.Name, err)
		}

		recordSQL := "INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)"
		if _, err := tx.Exec(ctx, recordSQL, m.Name, time.Now().UTC()); err != nil {
			return fmt.Errorf("failed to record migration %q: %w", m.Name, err)
		}
	}

	return nil
}
