// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EmbeddingFunc is an optional closure that generates a 1536-dimensional embedding vector for text.
type EmbeddingFunc func(ctx context.Context, text string) ([]float32, error)

// ADRSyncConfig holds options controlling the ADR synchronization process.
type ADRSyncConfig struct {
	DryRun           bool
	ObsoleteMode     ObsoleteHandlingMode
	EmbeddingGen     EmbeddingFunc
	CollisionHandler func(doc *ADRDocument, seenCodes map[string]string) string
}

// ADRSyncOption is a functional configuration option for SyncADRs.
type ADRSyncOption func(*ADRSyncConfig)

// WithDryRun enables or disables dry-run simulation mode without database mutations.
func WithDryRun(dryRun bool) ADRSyncOption {
	return func(c *ADRSyncConfig) {
		c.DryRun = dryRun
	}
}

// WithObsoleteMode configures how superseded or deprecated ADRs are handled.
func WithObsoleteMode(mode ObsoleteHandlingMode) ADRSyncOption {
	return func(c *ADRSyncConfig) {
		c.ObsoleteMode = mode
	}
}

// WithEmbeddingFunc configures an optional embedding vector generator.
func WithEmbeddingFunc(fn EmbeddingFunc) ADRSyncOption {
	return func(c *ADRSyncConfig) {
		c.EmbeddingGen = fn
	}
}

// WithCollisionHandler provides a custom function to disambiguate colliding rule codes.
func WithCollisionHandler(fn func(doc *ADRDocument, seenCodes map[string]string) string) ADRSyncOption {
	return func(c *ADRSyncConfig) {
		c.CollisionHandler = fn
	}
}

// ADRSyncResult summarizes the outcomes of the ADR synchronization run.
type ADRSyncResult struct {
	TotalFiles int                 `json:"total_files"`
	Created    int                 `json:"created"`
	Updated    int                 `json:"updated"`
	Skipped    int                 `json:"skipped"`
	Obsolete   int                 `json:"obsolete"`
	Errors     []string            `json:"errors,omitempty"`
	Rules      []ArchitecturalRule `json:"rules,omitempty"`
	Duration   time.Duration       `json:"duration"`
}

// SyncADRs scans an ADR directory and synchronizes all markdown ADRs to the architectural_rules store.
func (s *Service) SyncADRs(ctx context.Context, dir string, opts ...ADRSyncOption) (*ADRSyncResult, error) {
	start := time.Now()

	cfg := ADRSyncConfig{
		ObsoleteMode: ObsoleteModeMarkInfo,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if !cfg.DryRun {
		db := s.getDB()
		if db == nil {
			return nil, &ConnectionError{
				Op:  "sync_adrs",
				URL: SanitizeDatabaseURL(s.cfg.DatabaseURL),
				Err: errors.New("connection pool is not initialized"),
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("brain: failed to read ADR directory %s: %w", dir, err)
	}

	result := &ADRSyncResult{}
	seenCodes := make(map[string]string) // rule_code -> filename

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}

		result.TotalFiles++
		fullPath := filepath.Join(dir, entry.Name())

		doc, err := ParseADRFile(fullPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}

		// Check for code collision in the same batch
		if prevFile, exists := seenCodes[doc.RuleCode]; exists && prevFile != entry.Name() {
			if cfg.CollisionHandler != nil {
				doc.RuleCode = cfg.CollisionHandler(doc, seenCodes)
			} else {
				// Default disambiguation: append slug from filename
				slug := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				slug = strings.TrimPrefix(slug, fmt.Sprintf("%04d-", doc.Number))
				slug = strings.ReplaceAll(strings.ToUpper(slug), "-", "_")
				doc.RuleCode = fmt.Sprintf("%s_%s", doc.RuleCode, slug)
			}
		}
		seenCodes[doc.RuleCode] = entry.Name()

		// Handle superseded or deprecated documents
		isObsolete := doc.Status == ADRStatusSuperseded || doc.Status == ADRStatusDeprecated
		if isObsolete {
			result.Obsolete++
			switch cfg.ObsoleteMode {
			case ObsoleteModeSkip:
				result.Skipped++
				continue
			case ObsoleteModeDelete:
				if !cfg.DryRun {
					if err := s.DeleteRuleByCode(ctx, doc.RuleCode); err != nil && !errors.Is(err, ErrRuleNotFound) {
						result.Errors = append(result.Errors, fmt.Sprintf("delete %s: %v", doc.RuleCode, err))
					}
				}
				continue
			case ObsoleteModeMarkInfo:
				doc.Severity = "INFO"
			}
		}

		// Optional embedding generation
		var embedding []float32
		if cfg.EmbeddingGen != nil {
			embedText := fmt.Sprintf("%s\n\n%s", doc.Title, doc.Description)
			vec, err := cfg.EmbeddingGen(ctx, embedText)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("embedding %s: %v", doc.RuleCode, err))
			} else {
				embedding = vec
			}
		}

		rulePayload := ArchitecturalRule{
			RuleCode:    doc.RuleCode,
			Title:       doc.Title,
			Description: doc.Description,
			Severity:    doc.Severity,
			Embedding:   embedding,
		}

		if cfg.DryRun {
			// In dry run, check if it would create or update
			result.Created++
			result.Rules = append(result.Rules, rulePayload)
			continue
		}

		// Check if rule already exists in database
		existing, err := s.GetRuleByCode(ctx, doc.RuleCode)
		if err != nil {
			if errors.Is(err, ErrRuleNotFound) {
				// Create new rule
				created, createErr := s.CreateRule(ctx, rulePayload)
				if createErr != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("create %s: %v", doc.RuleCode, createErr))
					continue
				}
				result.Created++
				result.Rules = append(result.Rules, *created)
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("get %s: %v", doc.RuleCode, err))
			}
			continue
		}

		// Rule exists: check if content actually changed (deep idempotency check)
		hasChanged := existing.Title != rulePayload.Title ||
			existing.Description != rulePayload.Description ||
			existing.Severity != rulePayload.Severity ||
			(len(rulePayload.Embedding) > 0 && len(existing.Embedding) == 0)

		if !hasChanged {
			result.Skipped++
			result.Rules = append(result.Rules, *existing)
			continue
		}

		// Update existing rule
		updated, updateErr := s.UpdateRule(ctx, rulePayload)
		if updateErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("update %s: %v", doc.RuleCode, updateErr))
			continue
		}
		result.Updated++
		result.Rules = append(result.Rules, *updated)
	}

	result.Duration = time.Since(start)
	return result, nil
}
