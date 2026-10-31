// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"
)

const (
	// DefaultDatabaseURL matches the local PostgreSQL credentials in deployments/docker-compose.yml.
	DefaultDatabaseURL = "postgres://localhost:5432/ov_factory?sslmode=disable"

	defaultMaxConns    = 10
	defaultMinConns    = 2
	defaultMaxConnIdle = 5 * time.Minute
	defaultMaxConnLife = 1 * time.Hour
	defaultConnTimeout = 5 * time.Second
)

var (
	passwordURLRegex = regexp.MustCompile(`(?i)(://[^:]+:)([^@]+)(@)`)
	passwordDSNRegex = regexp.MustCompile(`(?i)(password\s*=\s*)(['"]?)[^\s'"]+(['"]?)`)
)

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
	} else if user := os.Getenv("POSTGRES_USER"); user != "" {
		password := os.Getenv("POSTGRES_PASSWORD")
		host := os.Getenv("POSTGRES_HOST")
		if host == "" {
			host = "localhost"
		}
		port := os.Getenv("POSTGRES_PORT")
		if port == "" {
			port = "5432"
		}
		db := os.Getenv("POSTGRES_DB")
		if db == "" {
			db = "ov_factory"
		}
		cfg.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, db)
	}
	return cfg
}

// SanitizeDatabaseURL strips sensitive passwords from connection strings and error messages.
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
	// Fallback regex masking for malformed or non-standard connection strings and DSNs
	masked := passwordURLRegex.ReplaceAllString(rawURL, "${1}*****${3}")
	return passwordDSNRegex.ReplaceAllString(masked, "${1}${2}*****${3}")
}
