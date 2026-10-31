// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"net/url"
	"os"
	"regexp"
	"time"
)

const (
	// DefaultDatabaseURL matches the local PostgreSQL credentials in deployments/docker-compose.yml.
	DefaultDatabaseURL = "postgres://ov_user:ov_password@localhost:5432/ov_factory?sslmode=disable"

	defaultMaxConns    = 10
	defaultMinConns    = 2
	defaultMaxConnIdle = 5 * time.Minute
	defaultMaxConnLife = 1 * time.Hour
	defaultConnTimeout = 5 * time.Second
)

var passwordRegex = regexp.MustCompile(`(?i)(://[^:]+:)([^@]+)(@)`)

// Config defines connection and pooling settings for the Brain's PostgreSQL store.
type Config struct {
	DatabaseURL string
	MaxConns    int32
	MinConns    int32
	MaxConnIdle time.Duration
	MaxConnLife time.Duration
	ConnTimeout time.Duration
}

// DefaultConfig provides standard development connection settings.
func DefaultConfig() Config {
	return Config{
		DatabaseURL: DefaultDatabaseURL,
		MaxConns:    defaultMaxConns,
		MinConns:    defaultMinConns,
		MaxConnIdle: defaultMaxConnIdle,
		MaxConnLife: defaultMaxConnLife,
		ConnTimeout: defaultConnTimeout,
	}
}

// ConfigFromEnv initializes configuration from the environment, falling back to defaults.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if envURL := os.Getenv("DATABASE_URL"); envURL != "" {
		cfg.DatabaseURL = envURL
	}
	return cfg
}

// SanitizeDatabaseURL strips sensitive passwords from connection strings.
func SanitizeDatabaseURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "*****")
		}
		return parsed.String()
	}
	// Fallback regex masking for malformed or non-standard connection strings
	return passwordRegex.ReplaceAllString(rawURL, "${1}*****${3}")
}
