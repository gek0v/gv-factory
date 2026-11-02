// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultExclusionPatterns contains directories and file patterns ignored during repository walks.
var DefaultExclusionPatterns = []string{
	".git",
	"node_modules",
	"vendor",
	"dist",
	"build",
	".worktrees",
	"testdata",
	"fixtures",
	"__fixtures__",
}

// DefaultSupportedExtensions lists source file extensions supported for AST symbol indexing.
var DefaultSupportedExtensions = []string{
	".go",
	".ts",
	".tsx",
}

// IndexResult summarizes metrics from an AST indexing or synchronizing run.
type IndexResult struct {
	TotalFilesScanned  int           `json:"total_files_scanned"`
	IndexedFiles       int           `json:"indexed_files"`
	ExtractedSymbols   int           `json:"extracted_symbols"`
	UpsertedSymbols    int           `json:"upserted_symbols"`
	SkippedUnchanged   int           `json:"skipped_unchanged"`
	PurgedStaleRecords int           `json:"purged_stale_records"`
	Errors             []string      `json:"errors,omitempty"`
	Duration           time.Duration `json:"duration"`
}

// RepoIndexerConfig holds options controlling repository scanning and symbol extraction.
type RepoIndexerConfig struct {
	Chunker              Chunker
	DryRun               bool
	Exclusions           []string
	AdditionalExclusions []string
	SupportedExtensions  []string
}

// RepoIndexerOption is a functional configuration option for RepoIndexer.
type RepoIndexerOption func(*RepoIndexerConfig)

// WithIndexerDryRun enables or disables dry-run mode without database mutations.
func WithIndexerDryRun(dryRun bool) RepoIndexerOption {
	return func(c *RepoIndexerConfig) {
		c.DryRun = dryRun
	}
}

// WithChunker configures a custom AST Chunker implementation.
func WithChunker(chunker Chunker) RepoIndexerOption {
	return func(c *RepoIndexerConfig) {
		c.Chunker = chunker
	}
}

// WithExclusions overrides the default exclusion patterns completely.
func WithExclusions(exclusions ...string) RepoIndexerOption {
	return func(c *RepoIndexerConfig) {
		c.Exclusions = append([]string{}, exclusions...)
	}
}

// WithAdditionalExclusions appends extra exclusion patterns while retaining defaults.
func WithAdditionalExclusions(exclusions ...string) RepoIndexerOption {
	return func(c *RepoIndexerConfig) {
		c.AdditionalExclusions = append(c.AdditionalExclusions, exclusions...)
	}
}

// WithSupportedExtensions configures the file extensions eligible for parsing.
func WithSupportedExtensions(exts ...string) RepoIndexerOption {
	return func(c *RepoIndexerConfig) {
		c.SupportedExtensions = append([]string{}, exts...)
	}
}

// RepoIndexer scans repository trees and synchronizes AST symbols into codebase_knowledge.
type RepoIndexer struct {
	service *Service
	cfg     RepoIndexerConfig
}

// NewRepoIndexer creates an initialized RepoIndexer.
func NewRepoIndexer(svc *Service, opts ...RepoIndexerOption) *RepoIndexer {
	cfg := RepoIndexerConfig{
		Chunker:             NewTreeSitterChunker(),
		Exclusions:          append([]string{}, DefaultExclusionPatterns...),
		SupportedExtensions: append([]string{}, DefaultSupportedExtensions...),
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if len(cfg.AdditionalExclusions) > 0 {
		cfg.Exclusions = append(cfg.Exclusions, cfg.AdditionalExclusions...)
	}

	return &RepoIndexer{
		service: svc,
		cfg:     cfg,
	}
}

// isExcluded determines whether a relative file path matches any exclusion rule.
func (idx *RepoIndexer) isExcluded(relPath string) bool {
	cleanRel := filepath.ToSlash(filepath.Clean(relPath))
	parts := strings.Split(cleanRel, "/")

	for _, part := range parts {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}

	for _, pattern := range idx.cfg.Exclusions {
		cleanPat := strings.Trim(filepath.ToSlash(pattern), "/")
		if cleanPat == "" {
			continue
		}
		for _, part := range parts {
			if strings.EqualFold(part, cleanPat) {
				return true
			}
		}
		if strings.HasPrefix(cleanRel, cleanPat+"/") || strings.Contains(cleanRel, "/"+cleanPat+"/") {
			return true
		}
	}

	return false
}

// isSupported checks if a file's extension matches configured supported extensions.
func (idx *RepoIndexer) isSupported(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, supported := range idx.cfg.SupportedExtensions {
		if ext == strings.ToLower(supported) {
			return true
		}
	}
	return false
}

// validateService verifies that the backing service and its DB pool are operational.
func (idx *RepoIndexer) validateService() error {
	if idx.service == nil {
		return errors.New("brain: service is nil")
	}
	if !idx.cfg.DryRun {
		if idx.service.getDB() == nil {
			return &ConnectionError{
				Op:  "repo_indexer",
				URL: SanitizeDatabaseURL(idx.service.cfg.DatabaseURL),
				Err: errors.New("connection pool is not initialized"),
			}
		}
	}
	return nil
}

// Index scans a repository directory recursively and indexes all supported source files.
func (idx *RepoIndexer) Index(ctx context.Context, repoPath string) (*IndexResult, error) {
	start := time.Now()
	res := &IndexResult{}

	if err := idx.validateService(); err != nil {
		return nil, err
	}

	fileInfo, err := os.Stat(repoPath)
	if err != nil {
		return nil, fmt.Errorf("brain: failed to access repository path %s: %w", repoPath, err)
	}
	if !fileInfo.IsDir() {
		return nil, fmt.Errorf("brain: repository path %s is not a directory", repoPath)
	}

	var candidateFiles []string
	err = filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, walkErr))
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		if d.IsDir() {
			if idx.isExcluded(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		res.TotalFilesScanned++

		if idx.isExcluded(rel) {
			return nil
		}

		if !idx.isSupported(rel) {
			return nil
		}

		candidateFiles = append(candidateFiles, rel)
		return nil
	})

	if err != nil {
		res.Duration = time.Since(start)
		return res, err
	}

	for _, rel := range candidateFiles {
		if err := ctx.Err(); err != nil {
			res.Duration = time.Since(start)
			return res, err
		}
		idx.indexSingleFile(ctx, repoPath, rel, res)
	}

	res.Duration = time.Since(start)
	return res, nil
}

// indexSingleFile reads, chunks, and differentially upserts symbols for one source file.
func (idx *RepoIndexer) indexSingleFile(ctx context.Context, repoPath, relPath string, res *IndexResult) {
	normRelPath := filepath.ToSlash(filepath.Clean(relPath))
	fullPath := filepath.Join(repoPath, normRelPath)

	content, err := os.ReadFile(fullPath)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("read %s: %v", normRelPath, err))
		return
	}

	chunks, err := idx.cfg.Chunker.Chunk(ctx, normRelPath, content)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("chunk %s: %v", normRelPath, err))
		return
	}

	res.IndexedFiles++
	res.ExtractedSymbols += len(chunks)

	if idx.cfg.DryRun {
		return
	}

	existingList, err := idx.service.GetKnowledgeByFilePath(ctx, normRelPath)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("get knowledge %s: %v", normRelPath, err))
		return
	}

	existingMap := make(map[string]CodebaseKnowledge, len(existingList))
	for _, item := range existingList {
		existingMap[item.SymbolName] = item
	}

	for _, chunk := range chunks {
		existing, found := existingMap[chunk.SymbolName]
		if found && existing.ContentHash == chunk.ContentHash {
			res.SkippedUnchanged++
			delete(existingMap, chunk.SymbolName)
			continue
		}

		k := chunk.ToCodebaseKnowledge()
		if _, err := idx.service.UpsertCodebaseKnowledge(ctx, k); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("upsert symbol %s:%s: %v", normRelPath, chunk.SymbolName, err))
			continue
		}
		res.UpsertedSymbols++
		delete(existingMap, chunk.SymbolName)
	}

	// Purge symbols that were previously stored for this file but are no longer present in AST
	for staleSymbol := range existingMap {
		if err := idx.service.DeleteKnowledgeBySymbol(ctx, normRelPath, staleSymbol); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("delete stale symbol %s:%s: %v", normRelPath, staleSymbol, err))
			continue
		}
		res.PurgedStaleRecords++
	}
}

// SyncFiles synchronizes a specific list of modified/added files and purges deleted files.
func (idx *RepoIndexer) SyncFiles(ctx context.Context, repoPath string, modifiedFiles, deletedFiles []string) (*IndexResult, error) {
	start := time.Now()
	res := &IndexResult{}

	if err := idx.validateService(); err != nil {
		return nil, err
	}

	// Purge deleted files
	for _, del := range deletedFiles {
		if err := ctx.Err(); err != nil {
			res.Duration = time.Since(start)
			return res, err
		}
		normDel := filepath.ToSlash(filepath.Clean(del))
		if !idx.isSupported(normDel) {
			continue
		}
		if idx.cfg.DryRun {
			continue
		}
		deletedCount, err := idx.service.DeleteKnowledgeByFilePath(ctx, normDel)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("delete file %s: %v", normDel, err))
			continue
		}
		res.PurgedStaleRecords += deletedCount
	}

	// Index modified / added files
	for _, mod := range modifiedFiles {
		if err := ctx.Err(); err != nil {
			res.Duration = time.Since(start)
			return res, err
		}
		normMod := filepath.ToSlash(filepath.Clean(mod))
		if idx.isExcluded(normMod) || !idx.isSupported(normMod) {
			continue
		}
		res.TotalFilesScanned++
		idx.indexSingleFile(ctx, repoPath, normMod, res)
	}

	res.Duration = time.Since(start)
	return res, nil
}

// ParseGitDiffNameStatus parses output from `git diff --name-status` or similar into modified and deleted slices.
func ParseGitDiffNameStatus(diffOutput string) (modified []string, deleted []string) {
	lines := strings.Split(diffOutput, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}

		status := fields[0]
		switch {
		case strings.HasPrefix(status, "D"):
			if len(fields) >= 2 {
				deleted = append(deleted, fields[1])
			}
		case strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C"):
			// Renamed or copied: fields[1] is old path, fields[2] is new path
			if len(fields) >= 3 {
				if strings.HasPrefix(status, "R") {
					deleted = append(deleted, fields[1])
				}
				modified = append(modified, fields[2])
			} else if len(fields) >= 2 {
				modified = append(modified, fields[1])
			}
		case strings.HasPrefix(status, "A") || strings.HasPrefix(status, "M") || strings.HasPrefix(status, "T"):
			if len(fields) >= 2 {
				modified = append(modified, fields[1])
			}
		default:
			// Fallback: If lines don't have status prefix (e.g. --name-only format)
			modified = append(modified, fields[0])
		}
	}

	return modified, deleted
}

// SyncGitDiff executes a git diff command against diffRef (e.g. "HEAD~1") and synchronizes changes.
func (idx *RepoIndexer) SyncGitDiff(ctx context.Context, repoPath string, diffRef string) (*IndexResult, error) {
	if diffRef == "" {
		diffRef = "HEAD~1"
	}

	cmd := exec.CommandContext(ctx, "git", "diff", "--name-status", diffRef)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("brain: git diff failed in %s: %w", repoPath, err)
	}

	modified, deleted := ParseGitDiffNameStatus(string(out))
	return idx.SyncFiles(ctx, repoPath, modified, deleted)
}

// SyncGitDiffOutput accepts raw output from `git diff --name-status` and synchronizes changes.
func (idx *RepoIndexer) SyncGitDiffOutput(ctx context.Context, repoPath string, diffOutput string) (*IndexResult, error) {
	modified, deleted := ParseGitDiffNameStatus(diffOutput)
	return idx.SyncFiles(ctx, repoPath, modified, deleted)
}

// IndexRepository is a convenience method on Service to scan and index a repository directory.
func (s *Service) IndexRepository(ctx context.Context, repoPath string, opts ...RepoIndexerOption) (*IndexResult, error) {
	indexer := NewRepoIndexer(s, opts...)
	return indexer.Index(ctx, repoPath)
}

// SyncFiles is a convenience method on Service to incrementally sync specific modified and deleted files.
func (s *Service) SyncFiles(ctx context.Context, repoPath string, modifiedFiles, deletedFiles []string, opts ...RepoIndexerOption) (*IndexResult, error) {
	indexer := NewRepoIndexer(s, opts...)
	return indexer.SyncFiles(ctx, repoPath, modifiedFiles, deletedFiles)
}

// SyncGitDiff is a convenience method on Service to incrementally sync git changes from a commit range.
func (s *Service) SyncGitDiff(ctx context.Context, repoPath string, diffRef string, opts ...RepoIndexerOption) (*IndexResult, error) {
	indexer := NewRepoIndexer(s, opts...)
	return indexer.SyncGitDiff(ctx, repoPath, diffRef)
}
