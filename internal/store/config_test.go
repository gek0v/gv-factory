// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"os"
	"testing"
)

func TestSanitizeDatabaseURL(t *testing.T) {
	raw := "postgres://user:secret123@localhost:5432/factory?sslmode=disable"
	sanitized := SanitizeDatabaseURL(raw)

	if sanitized == raw {
		t.Fatalf("sanitized URL should not be equal to raw URL with password")
	}
	if sanitized != "postgres://redacted:redacted@localhost:5432/factory?sslmode=disable" {
		t.Fatalf("unexpected sanitized URL: %s", sanitized)
	}

	if SanitizeDatabaseURL("") != "" {
		t.Fatalf("expected empty string for empty input")
	}
}

func TestConfigFromEnv(t *testing.T) {
	orig := os.Getenv("DATABASE_URL")
	defer os.Setenv("DATABASE_URL", orig)

	os.Setenv("DATABASE_URL", "postgres://test:test@127.0.0.1:5432/db")
	cfg := ConfigFromEnv()
	if cfg.DatabaseURL != "postgres://test:test@127.0.0.1:5432/db" {
		t.Fatalf("expected DATABASE_URL to be populated, got %s", cfg.DatabaseURL)
	}
}

func TestNewPostgresStore_EmptyURL(t *testing.T) {
	ctx := context.Background()
	_, err := NewPostgresStore(ctx, Config{DatabaseURL: ""})
	if err == nil {
		t.Fatal("expected error for empty database URL, got nil")
	}
}

func TestNewPostgresStore_ConfigBranches(t *testing.T) {
	// Invalid connection URL syntax
	ctx := context.Background()
	_, err := NewPostgresStore(ctx, Config{DatabaseURL: "::not-a-valid-url::"})
	if err == nil {
		t.Fatal("expected error parsing invalid URL syntax")
	}

	// Default config values
	cfg := DefaultConfig()
	if cfg.MaxOpenConns != 25 || cfg.MinIdleConns != 5 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
