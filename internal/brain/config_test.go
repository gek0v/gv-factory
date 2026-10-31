// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	const expectedDefaultURL = "postgres://localhost:5432/ov_factory?sslmode=disable"
	if cfg.DatabaseURL != expectedDefaultURL {
		t.Fatalf("expected default DatabaseURL %q, got %q", expectedDefaultURL, cfg.DatabaseURL)
	}

	if cfg.MaxConns <= 0 {
		t.Fatalf("expected MaxConns > 0, got %d", cfg.MaxConns)
	}
	if cfg.ConnTimeout <= 0 {
		t.Fatalf("expected ConnTimeout > 0, got %v", cfg.ConnTimeout)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("uses fallback when DATABASE_URL is unset", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		cfg := ConfigFromEnv()

		const expectedDefaultURL = "postgres://localhost:5432/ov_factory?sslmode=disable"
		if cfg.DatabaseURL != expectedDefaultURL {
			t.Fatalf("expected fallback URL %q, got %q", expectedDefaultURL, cfg.DatabaseURL)
		}
	})

	t.Run("uses environment variable when DATABASE_URL is set", func(t *testing.T) {
		customURL := "postgres://custom_user:secret_pass@db.example.com:5432/custom_db?sslmode=require"
		t.Setenv("DATABASE_URL", customURL)
		cfg := ConfigFromEnv()

		if cfg.DatabaseURL != customURL {
			t.Fatalf("expected custom URL %q, got %q", customURL, cfg.DatabaseURL)
		}
	})
}

func TestSanitizeDatabaseURL(t *testing.T) {
	t.Run("empty string returns empty", func(t *testing.T) {
		if got := SanitizeDatabaseURL(""); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("strips password from standard URL", func(t *testing.T) {
		rawURL := "postgres://myuser:super_secret_pw@db.internal:5432/mydb?sslmode=require"
		sanitized := SanitizeDatabaseURL(rawURL)

		if strings.Contains(sanitized, "super_secret_pw") {
			t.Fatalf("sanitized URL leaked password: %s", sanitized)
		}
		if !strings.Contains(sanitized, "myuser") {
			t.Fatalf("sanitized URL should preserve username: %s", sanitized)
		}
		if !strings.Contains(sanitized, "db.internal:5432/mydb") {
			t.Fatalf("sanitized URL should preserve host and database: %s", sanitized)
		}
	})

	t.Run("strips password via regex fallback on unparseable string", func(t *testing.T) {
		raw := "postgres://alice:secretpassword@bad host:::5432/db"
		sanitized := SanitizeDatabaseURL(raw)
		if strings.Contains(sanitized, "secretpassword") {
			t.Fatalf("fallback regex leaked password: %s", sanitized)
		}
		if !strings.Contains(sanitized, "alice") {
			t.Fatalf("fallback should preserve username: %s", sanitized)
		}
	})

	t.Run("preserves URL without password", func(t *testing.T) {
		raw := "postgres://localhost:5432/mydb"
		sanitized := SanitizeDatabaseURL(raw)
		if sanitized != raw {
			t.Fatalf("expected %q, got %q", raw, sanitized)
		}
	})

	t.Run("strips password from key-value DSN string", func(t *testing.T) {
		dsn := "host=localhost user=ov_user password=super_secret_pw dbname=ov_factory"
		sanitized := SanitizeDatabaseURL(dsn)
		if strings.Contains(sanitized, "super_secret_pw") {
			t.Fatalf("sanitized DSN leaked password: %s", sanitized)
		}
		if !strings.Contains(sanitized, "password=*****") {
			t.Fatalf("expected password=***** in sanitized DSN: %s", sanitized)
		}
	})
}
