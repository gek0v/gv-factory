// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"net/url"
	"os"
	"time"
)

// Config holds PostgreSQL connection pool configuration.
type Config struct {
	DatabaseURL     string
	MaxOpenConns    int32
	MinIdleConns    int32
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultConfig returns optimal connection pool defaults for microservice workloads.
func DefaultConfig() Config {
	return Config{
		MaxOpenConns:    25,
		MinIdleConns:    5,
		ConnMaxLifetime: 1 * time.Hour,
		ConnMaxIdleTime: 15 * time.Minute,
	}
}

// ConfigFromEnv inspects environment variables and returns a populated Config.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		cfg.DatabaseURL = dbURL
	}
	return cfg
}

// SanitizeDatabaseURL strips sensitive authentication credentials from PostgreSQL connection strings.
func SanitizeDatabaseURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "postgres://***:***@invalid-url"
	}
	if u.User != nil {
		u.User = url.UserPassword("redacted", "redacted")
	}
	return u.String()
}
