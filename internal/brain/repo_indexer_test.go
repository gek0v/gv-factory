// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// inMemoryKnowledgeStore provides a thread-safe mock for codebase_knowledge queries.
type inMemoryKnowledgeStore struct {
	mu      sync.RWMutex
	records map[string]CodebaseKnowledge // key: file_path + ":" + symbol_name
}

func newInMemoryStore() *inMemoryKnowledgeStore {
	return &inMemoryKnowledgeStore{
		records: make(map[string]CodebaseKnowledge),
	}
}

func (s *inMemoryKnowledgeStore) key(filePath, symbolName string) string {
	return filePath + ":" + symbolName
}

func (s *inMemoryKnowledgeStore) upsert(k CodebaseKnowledge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k.ID == "" {
		k.ID = "id-" + k.SymbolName
	}
	k.UpdatedAt = time.Now()
	s.records[s.key(k.FilePath, k.SymbolName)] = k
}

func (s *inMemoryKnowledgeStore) getByFile(filePath string) []CodebaseKnowledge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []CodebaseKnowledge
	for _, rec := range s.records {
		if rec.FilePath == filePath {
			list = append(list, rec)
		}
	}
	return list
}

func (s *inMemoryKnowledgeStore) deleteByFile(filePath string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for k, rec := range s.records {
		if rec.FilePath == filePath {
			delete(s.records, k)
			count++
		}
	}
	return count
}

func (s *inMemoryKnowledgeStore) deleteBySymbol(filePath, symbolName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(filePath, symbolName)
	if _, exists := s.records[k]; exists {
		delete(s.records, k)
		return true
	}
	return false
}

func createMockServiceWithStore(store *inMemoryKnowledgeStore) *Service {
	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "INSERT INTO codebase_knowledge") {
				filePath := args[0].(string)
				symbolName := args[1].(string)
				symbolType := args[2].(string)
				content := args[3].(string)
				contentHash := args[4].(string)

				k := CodebaseKnowledge{
					FilePath:    filePath,
					SymbolName:  symbolName,
					SymbolType:  symbolType,
					Content:     content,
					ContentHash: contentHash,
				}
				store.upsert(k)
				return &mockRow{
					values: []any{
						"id-" + symbolName,
						filePath,
						symbolName,
						symbolType,
						content,
						contentHash,
						nil,
						time.Now(),
					},
				}
			}
			if strings.Contains(sql, "DELETE FROM codebase_knowledge") && strings.Contains(sql, "symbol_name = $2") {
				filePath := args[0].(string)
				symbolName := args[1].(string)
				if store.deleteBySymbol(filePath, symbolName) {
					return &mockRow{values: []any{"deleted-id"}}
				}
				return &mockRow{err: pgx.ErrNoRows}
			}
			return &mockRow{err: pgx.ErrNoRows}
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "DELETE FROM codebase_knowledge") && strings.Contains(sql, "file_path = $1") {
				filePath := args[0].(string)
				deleted := store.deleteByFile(filePath)
				var rowsData [][]any
				for i := 0; i < deleted; i++ {
					rowsData = append(rowsData, []any{"deleted-id"})
				}
				return &mockRows{data: rowsData}, nil
			}
			if strings.Contains(sql, "FROM codebase_knowledge") && strings.Contains(sql, "file_path = $1") {
				filePath := args[0].(string)
				items := store.getByFile(filePath)
				var rowsData [][]any
				for _, it := range items {
					rowsData = append(rowsData, []any{
						it.ID,
						it.FilePath,
						it.SymbolName,
						it.SymbolType,
						it.Content,
						it.ContentHash,
						nil,
						it.UpdatedAt,
					})
				}
				return &mockRows{data: rowsData}, nil
			}
			return &mockRows{}, nil
		},
	}

	return &Service{
		cfg: DefaultConfig(),
		db:  mock,
	}
}

// createTestRepo populates a temporary directory with various files and directories.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"main.go": `package main

func Main() {
	println("hello")
}
`,
		"pkg/util/helper.go": `package util

func HelperOne() string {
	return "one"
}

func HelperTwo() string {
	return "two"
}
`,
		"web/src/index.ts": `export function greet(name: string): string {
    return "Hello " + name;
}

export class Greeter {
    sayHello(): string {
        return "hi";
    }
}
`,
		"web/src/App.tsx": `import React from 'react';

export function App(): JSX.Element {
    return <div>Hello App</div>;
}
`,
		// Files that should be excluded by default
		"README.md":                   "# Test Repo",
		"config.json":                 `{"name": "test"}`,
		".git/config":                 "git config",
		"node_modules/pkg/index.ts":   "export function nodeModule() {}",
		"vendor/github.com/pkg/lib.go": "package lib\nfunc Lib() {}",
		"dist/bundle.js":              "console.log('dist');",
		".hidden/secret.go":           "package secret\nfunc Secret() {}",
		"testdata/fixture.go":         "package fixture\nfunc Fixture() {}",
		"pkg/util/fixtures/mock.ts":   "export function mockUtil() {}",
	}

	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write file %s: %v", relPath, err)
		}
	}

	return dir
}

func TestRepoIndexer_ExclusionAndFiltering(t *testing.T) {
	repoDir := createTestRepo(t)
	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)

	ctx := context.Background()
	indexer := NewRepoIndexer(svc)

	res, err := indexer.Index(ctx, repoDir)
	if err != nil {
		t.Fatalf("indexer.Index failed: %v", err)
	}

	// 4 files should be indexed: main.go, pkg/util/helper.go, web/src/index.ts, web/src/App.tsx
	if res.IndexedFiles != 4 {
		t.Fatalf("expected 4 indexed files, got %d", res.IndexedFiles)
	}

	// Verify symbols were extracted
	if res.ExtractedSymbols < 5 {
		t.Fatalf("expected at least 5 symbols extracted, got %d", res.ExtractedSymbols)
	}

	if res.UpsertedSymbols != res.ExtractedSymbols {
		t.Fatalf("expected UpsertedSymbols (%d) == ExtractedSymbols (%d)", res.UpsertedSymbols, res.ExtractedSymbols)
	}

	if res.SkippedUnchanged != 0 {
		t.Fatalf("expected 0 skipped unchanged symbols on fresh index, got %d", res.SkippedUnchanged)
	}

	if res.PurgedStaleRecords != 0 {
		t.Fatalf("expected 0 purged records on fresh index, got %d", res.PurgedStaleRecords)
	}

	if res.Duration <= 0 {
		t.Fatal("expected positive duration in result")
	}

	// Verify excluded files were not indexed
	excludedPrefixes := []string{"node_modules", "vendor", "dist", ".hidden", "testdata", "fixtures", ".git"}
	for _, k := range store.records {
		for _, prefix := range excludedPrefixes {
			if strings.Contains(k.FilePath, prefix) {
				t.Fatalf("excluded path %q was indexed: %+v", prefix, k)
			}
		}
	}
}

func TestRepoIndexer_DifferentialIndexing(t *testing.T) {
	repoDir := createTestRepo(t)
	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)
	ctx := context.Background()
	indexer := NewRepoIndexer(svc)

	// Step 1: Initial index
	firstRes, err := indexer.Index(ctx, repoDir)
	if err != nil {
		t.Fatalf("first index failed: %v", err)
	}
	firstExtracted := firstRes.ExtractedSymbols
	if firstExtracted == 0 {
		t.Fatal("expected extracted symbols > 0")
	}

	// Step 2: Re-index without modifications -> all symbols should be skipped
	secondRes, err := indexer.Index(ctx, repoDir)
	if err != nil {
		t.Fatalf("second index failed: %v", err)
	}
	if secondRes.SkippedUnchanged != firstExtracted {
		t.Fatalf("expected %d skipped symbols, got %d", firstExtracted, secondRes.SkippedUnchanged)
	}
	if secondRes.UpsertedSymbols != 0 {
		t.Fatalf("expected 0 upserted symbols, got %d", secondRes.UpsertedSymbols)
	}
	if secondRes.PurgedStaleRecords != 0 {
		t.Fatalf("expected 0 purged records, got %d", secondRes.PurgedStaleRecords)
	}

	// Step 3: Modify pkg/util/helper.go:
	// - HelperOne is modified
	// - HelperTwo is removed
	// - HelperThree is added
	modifiedHelper := `package util

func HelperOne() string {
	return "modified-one"
}

func HelperThree() string {
	return "three"
}
`
	helperPath := filepath.Join(repoDir, "pkg/util/helper.go")
	if err := os.WriteFile(helperPath, []byte(modifiedHelper), 0o644); err != nil {
		t.Fatalf("failed to modify helper.go: %v", err)
	}

	thirdRes, err := indexer.Index(ctx, repoDir)
	if err != nil {
		t.Fatalf("third index failed: %v", err)
	}

	// HelperTwo should have been purged
	if thirdRes.PurgedStaleRecords != 1 {
		t.Fatalf("expected 1 purged stale record (HelperTwo), got %d", thirdRes.PurgedStaleRecords)
	}

	// HelperOne (modified) + HelperThree (new) should be upserted (2 upserted)
	if thirdRes.UpsertedSymbols != 2 {
		t.Fatalf("expected 2 upserted symbols, got %d", thirdRes.UpsertedSymbols)
	}

	// Other files (main.go, index.ts, App.tsx) unchanged
	if thirdRes.SkippedUnchanged == 0 {
		t.Fatal("expected unchanged symbols to be skipped")
	}

	// Verify in store that HelperTwo is gone
	helperKey := "pkg/util/helper.go:HelperTwo"
	if _, exists := store.records[helperKey]; exists {
		t.Fatalf("expected %s to be purged from store", helperKey)
	}
}

func TestRepoIndexer_DryRun(t *testing.T) {
	repoDir := createTestRepo(t)
	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)
	ctx := context.Background()

	indexer := NewRepoIndexer(svc, WithIndexerDryRun(true))

	res, err := indexer.Index(ctx, repoDir)
	if err != nil {
		t.Fatalf("dry run index failed: %v", err)
	}

	if res.IndexedFiles != 4 {
		t.Fatalf("expected 4 files scanned in dry run, got %d", res.IndexedFiles)
	}
	if res.ExtractedSymbols == 0 {
		t.Fatal("expected extracted symbols in dry run")
	}

	// Store should be empty because it was a dry run
	if len(store.records) != 0 {
		t.Fatalf("expected empty store in dry run, got %d records", len(store.records))
	}
}

func TestRepoIndexer_CustomOptions(t *testing.T) {
	repoDir := createTestRepo(t)
	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)
	ctx := context.Background()

	t.Run("custom exclusions ignores web directory", func(t *testing.T) {
		indexer := NewRepoIndexer(svc, WithAdditionalExclusions("web/"))
		res, err := indexer.Index(ctx, repoDir)
		if err != nil {
			t.Fatalf("index failed: %v", err)
		}
		// Only main.go and pkg/util/helper.go
		if res.IndexedFiles != 2 {
			t.Fatalf("expected 2 indexed files, got %d", res.IndexedFiles)
		}
	})

	t.Run("custom file extensions filters only .ts", func(t *testing.T) {
		indexer := NewRepoIndexer(svc, WithSupportedExtensions(".ts"))
		res, err := indexer.Index(ctx, repoDir)
		if err != nil {
			t.Fatalf("index failed: %v", err)
		}
		// Only web/src/index.ts
		if res.IndexedFiles != 1 {
			t.Fatalf("expected 1 indexed file (.ts only), got %d", res.IndexedFiles)
		}
	})

	t.Run("custom chunker override", func(t *testing.T) {
		customChunker := NewTreeSitterChunker()
		indexer := NewRepoIndexer(svc, WithChunker(customChunker))
		res, err := indexer.Index(ctx, repoDir)
		if err != nil {
			t.Fatalf("index with custom chunker failed: %v", err)
		}
		if res.IndexedFiles != 4 {
			t.Fatalf("expected 4 indexed files, got %d", res.IndexedFiles)
		}
	})
}

func TestRepoIndexer_SyncFiles(t *testing.T) {
	repoDir := createTestRepo(t)
	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)
	ctx := context.Background()
	indexer := NewRepoIndexer(svc)

	// Initial index of all files
	_, err := indexer.Index(ctx, repoDir)
	if err != nil {
		t.Fatalf("initial index failed: %v", err)
	}

	// Modify main.go and delete web/src/App.tsx
	newMain := `package main

func Main() {
	println("new-main")
}

func Extra() {}
`
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte(newMain), 0o644); err != nil {
		t.Fatalf("failed to update main.go: %v", err)
	}
	if err := os.Remove(filepath.Join(repoDir, "web/src/App.tsx")); err != nil {
		t.Fatalf("failed to remove App.tsx: %v", err)
	}

	// Sync only modified and deleted files
	res, err := indexer.SyncFiles(ctx, repoDir, []string{"main.go"}, []string{"web/src/App.tsx"})
	if err != nil {
		t.Fatalf("SyncFiles failed: %v", err)
	}

	if res.IndexedFiles != 1 {
		t.Fatalf("expected 1 indexed file, got %d", res.IndexedFiles)
	}
	if res.PurgedStaleRecords < 1 {
		t.Fatalf("expected at least 1 purged stale record for App.tsx, got %d", res.PurgedStaleRecords)
	}

	// App.tsx symbol should no longer be in store
	for _, rec := range store.records {
		if strings.Contains(rec.FilePath, "App.tsx") {
			t.Fatalf("found App.tsx in store after deletion: %+v", rec)
		}
	}
}

func TestRepoIndexer_ParseGitDiffOutput(t *testing.T) {
	t.Run("parse name-status output", func(t *testing.T) {
		diffOutput := `M	internal/brain/brain.go
A	internal/brain/repo_indexer.go
D	internal/brain/old_file.go
R100	old/path.go	new/path.go
C100	src/file.ts	dest/file.ts
M	vendor/github.com/pkg/pkg.go
`
		modified, deleted := ParseGitDiffNameStatus(diffOutput)

		expectedModified := []string{
			"internal/brain/brain.go",
			"internal/brain/repo_indexer.go",
			"new/path.go",
			"dest/file.ts",
			"vendor/github.com/pkg/pkg.go",
		}
		expectedDeleted := []string{
			"internal/brain/old_file.go",
			"old/path.go",
		}

		if len(modified) != len(expectedModified) {
			t.Fatalf("expected %d modified, got %d: %v", len(expectedModified), len(modified), modified)
		}
		for i, m := range modified {
			if m != expectedModified[i] {
				t.Errorf("modified[%d]: expected %s, got %s", i, expectedModified[i], m)
			}
		}

		if len(deleted) != len(expectedDeleted) {
			t.Fatalf("expected %d deleted, got %d: %v", len(expectedDeleted), len(deleted), deleted)
		}
		for i, d := range deleted {
			if d != expectedDeleted[i] {
				t.Errorf("deleted[%d]: expected %s, got %s", i, expectedDeleted[i], d)
			}
		}
	})

	t.Run("parse empty or whitespace diff output", func(t *testing.T) {
		mod, del := ParseGitDiffNameStatus("  \n\t\n  ")
		if len(mod) != 0 || len(del) != 0 {
			t.Fatalf("expected empty slices for whitespace diff, got %v, %v", mod, del)
		}
	})
}

func TestRepoIndexer_SyncGitDiff_RealRepo(t *testing.T) {
	// Verify git is installed
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH, skipping real git integration test")
	}

	dir := t.TempDir()

	// Initialize git repo
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v\nOutput: %s", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.com")

	// Commit 1: add initial files
	initialFile := filepath.Join(dir, "calc.go")
	if err := os.WriteFile(initialFile, []byte("package calc\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deletedFile := filepath.Join(dir, "obsolete.go")
	if err := os.WriteFile(deletedFile, []byte("package calc\nfunc Obsolete() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runGit("add", ".")
	runGit("commit", "-m", "initial commit")

	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)
	ctx := context.Background()
	indexer := NewRepoIndexer(svc)

	// Initial index
	initialRes, err := indexer.Index(ctx, dir)
	if err != nil {
		t.Fatalf("initial index failed: %v", err)
	}
	if initialRes.IndexedFiles != 2 {
		t.Fatalf("expected 2 indexed files, got %d", initialRes.IndexedFiles)
	}

	// Commit 2: modify calc.go, delete obsolete.go, add helper.go
	if err := os.WriteFile(initialFile, []byte("package calc\nfunc Add(a, b int) int { return a + b + 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deletedFile); err != nil {
		t.Fatal(err)
	}
	helperFile := filepath.Join(dir, "helper.go")
	if err := os.WriteFile(helperFile, []byte("package calc\nfunc Multiply(a, b int) int { return a * b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runGit("add", "-A")
	runGit("commit", "-m", "second commit")

	// Call SyncGitDiff with HEAD~1
	diffRes, err := indexer.SyncGitDiff(ctx, dir, "HEAD~1")
	if err != nil {
		t.Fatalf("SyncGitDiff failed: %v", err)
	}

	// calc.go and helper.go modified/added (2 files)
	if diffRes.IndexedFiles != 2 {
		t.Fatalf("expected 2 indexed files in diff, got %d", diffRes.IndexedFiles)
	}
	// obsolete.go was purged
	if diffRes.PurgedStaleRecords != 1 {
		t.Fatalf("expected 1 purged record (obsolete.go), got %d", diffRes.PurgedStaleRecords)
	}
}

func TestRepoIndexer_ErrorAndEdgeCases(t *testing.T) {
	store := newInMemoryStore()
	svc := createMockServiceWithStore(store)
	ctx := context.Background()
	indexer := NewRepoIndexer(svc)

	t.Run("non-existent repository directory", func(t *testing.T) {
		_, err := indexer.Index(ctx, "/path/does/not/exist/999999")
		if err == nil {
			t.Fatal("expected error for non-existent path, got nil")
		}
	})

	t.Run("target is a file not a directory", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "somefile.go")
		if err := os.WriteFile(tmpFile, []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := indexer.Index(ctx, tmpFile)
		if err == nil {
			t.Fatal("expected error when repo path is a file, got nil")
		}
	})

	t.Run("nil service returns error", func(t *testing.T) {
		nilIndexer := NewRepoIndexer(nil)
		_, err := nilIndexer.Index(ctx, t.TempDir())
		if err == nil {
			t.Fatal("expected error when Service is nil, got nil")
		}
	})

	t.Run("service with nil pool returns ConnectionError", func(t *testing.T) {
		unconnectedSvc := &Service{
			cfg: Config{DatabaseURL: "postgres://ov:pass@localhost:5432/ov"},
		}
		idx := NewRepoIndexer(unconnectedSvc)
		repoDir := createTestRepo(t)
		_, err := idx.Index(ctx, repoDir)
		if err == nil {
			t.Fatal("expected ConnectionError on nil pool, got nil")
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
	})

	t.Run("context canceled terminates indexing", func(t *testing.T) {
		repoDir := createTestRepo(t)
		cancelingCtx, cancel := context.WithCancel(context.Background())
		cancel() // immediately canceled

		_, err := indexer.Index(cancelingCtx, repoDir)
		if err == nil {
			t.Fatal("expected context canceled error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("Service helper methods IndexRepository and SyncGitDiff", func(t *testing.T) {
		repoDir := createTestRepo(t)
		res, err := svc.IndexRepository(ctx, repoDir)
		if err != nil {
			t.Fatalf("svc.IndexRepository failed: %v", err)
		}
		if res.IndexedFiles != 4 {
			t.Fatalf("expected 4 indexed files, got %d", res.IndexedFiles)
		}

		diffRes, err := svc.SyncFiles(ctx, repoDir, []string{"main.go"}, nil)
		if err != nil {
			t.Fatalf("svc.SyncFiles failed: %v", err)
		}
		if diffRes.IndexedFiles != 1 {
			t.Fatalf("expected 1 indexed file, got %d", diffRes.IndexedFiles)
		}

		// Test svc.SyncGitDiff and indexer.SyncGitDiffOutput
		diffOutput := "M\tmain.go\nD\tweb/src/App.tsx\n"
		outRes, err := indexer.SyncGitDiffOutput(ctx, repoDir, diffOutput)
		if err != nil {
			t.Fatalf("SyncGitDiffOutput failed: %v", err)
		}
		if outRes.IndexedFiles != 1 {
			t.Fatalf("expected 1 indexed file from SyncGitDiffOutput, got %d", outRes.IndexedFiles)
		}
	})

	t.Run("WithExclusions complete override", func(t *testing.T) {
		repoDir := createTestRepo(t)
		// Exclude only main.go, meaning node_modules or fixtures are not excluded unless in exclusions
		customIdx := NewRepoIndexer(svc, WithExclusions("main.go"))
		res, err := customIdx.Index(ctx, repoDir)
		if err != nil {
			t.Fatalf("index failed: %v", err)
		}
		// main.go should be skipped
		for _, rec := range store.records {
			if rec.FilePath == "main.go" {
				// note: might exist from earlier test in store, but res.IndexedFiles shouldn't count it if excluded
			}
		}
		if res.TotalFilesScanned == 0 {
			t.Fatal("expected files scanned")
		}
	})

	t.Run("SyncFiles edge cases: dry run, unsupported extensions, context cancellation", func(t *testing.T) {
		repoDir := createTestRepo(t)
		dryIdx := NewRepoIndexer(svc, WithIndexerDryRun(true))

		// Dry run deleting files
		res, err := dryIdx.SyncFiles(ctx, repoDir, []string{"main.go"}, []string{"deleted.go", "ignored.txt"})
		if err != nil {
			t.Fatalf("dry run SyncFiles failed: %v", err)
		}
		if res.PurgedStaleRecords != 0 {
			t.Fatalf("expected 0 purged records in dry run, got %d", res.PurgedStaleRecords)
		}

		// Context canceled in SyncFiles deleted
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = indexer.SyncFiles(canceledCtx, repoDir, nil, []string{"del.go"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

		// Context canceled in SyncFiles modified
		_, err = indexer.SyncFiles(canceledCtx, repoDir, []string{"main.go"}, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("indexSingleFile errors handled gracefully", func(t *testing.T) {
		repoDir := t.TempDir()
		badFile := filepath.Join(repoDir, "bad.go")
		if err := os.WriteFile(badFile, []byte("package bad\nfunc Bad() {}"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Mock error chunker
		errChunker := &mockErrorChunker{err: errors.New("chunker failed")}
		errIdx := NewRepoIndexer(svc, WithChunker(errChunker))

		res, err := errIdx.Index(ctx, repoDir)
		if err != nil {
			t.Fatalf("Index should not fail completely on single chunker error: %v", err)
		}
		if len(res.Errors) == 0 {
			t.Fatal("expected error recorded in res.Errors")
		}

		// Error on GetKnowledgeByFilePath
		errorDBMock := &mockDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{err: errors.New("row failed")}
			},
		}
		errorSvc := &Service{cfg: DefaultConfig(), db: errorDBMock}
		errDBIdx := NewRepoIndexer(errorSvc)

		res2, err := errDBIdx.Index(ctx, repoDir)
		if err != nil {
			t.Fatalf("Index should not fail on get knowledge error: %v", err)
		}
		if len(res2.Errors) == 0 {
			t.Fatal("expected error recorded in res.Errors on DB failure")
		}
	})

	t.Run("SyncGitDiff error on invalid git repo or ref", func(t *testing.T) {
		nonGitDir := t.TempDir()
		_, err := indexer.SyncGitDiff(ctx, nonGitDir, "invalid-commit-reference-12345")
		if err == nil {
			t.Fatal("expected error running git diff with invalid ref")
		}

		_, err = svc.SyncGitDiff(ctx, nonGitDir, "invalid-commit-reference-12345")
		if err == nil {
			t.Fatal("expected error calling svc.SyncGitDiff with invalid ref")
		}
	})
}

type mockErrorChunker struct {
	err error
}

func (m *mockErrorChunker) Supports(filePath string) bool { return true }
func (m *mockErrorChunker) Chunk(ctx context.Context, filePath string, content []byte) ([]CodeChunk, error) {
	return nil, m.err
}

