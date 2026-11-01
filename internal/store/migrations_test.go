// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"strings"
	"testing"
)

func TestLoadEmbeddedMigrations(t *testing.T) {
	migrations, err := LoadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("failed to load embedded migrations: %v", err)
	}

	if len(migrations) == 0 {
		t.Fatal("expected at least one embedded migration, got 0")
	}

	m1 := migrations[0]
	if m1.Name != "001_initial_schema.sql" {
		t.Fatalf("expected first migration to be 001_initial_schema.sql, got %q", m1.Name)
	}
	if !strings.Contains(m1.SQL, "CREATE TABLE IF NOT EXISTS runs") {
		t.Fatalf("migration SQL does not contain runs table creation: %s", m1.SQL)
	}
	if !strings.Contains(m1.SQL, "CREATE TABLE IF NOT EXISTS run_steps") {
		t.Fatalf("migration SQL does not contain run_steps table creation: %s", m1.SQL)
	}
	if !strings.Contains(m1.SQL, "CREATE TABLE IF NOT EXISTS pending_gates") {
		t.Fatalf("migration SQL does not contain pending_gates table creation: %s", m1.SQL)
	}
	if !strings.Contains(m1.SQL, "CREATE TABLE IF NOT EXISTS gate_audits") {
		t.Fatalf("migration SQL does not contain gate_audits table creation: %s", m1.SQL)
	}
}
