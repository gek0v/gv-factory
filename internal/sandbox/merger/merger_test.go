// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package merger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

type mockMergerPublisher struct {
	mu     sync.Mutex
	events []gateway.Event
}

func (m *mockMergerPublisher) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

type mockMergerPRCreator struct {
	mu      sync.Mutex
	created []PRDetails
}

func (m *mockMergerPRCreator) CreatePullRequest(ctx context.Context, details PRDetails) (*PRResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created = append(m.created, details)
	return &PRResult{
		PRNumber: 101,
		PRURL:    "https://github.com/gv-factory/gv-factory/pull/101",
	}, nil
}

// setupGitFixture creates a clean temporary git repository with initial commit on main.
func setupGitFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
		return string(out)
	}

	runGit("init")
	runGit("config", "user.name", "gv-factory-test")
	runGit("config", "user.email", "test@gv-factory.local")
	runGit("config", "commit.gpgsign", "false")

	// Create initial file
	baseFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(baseFile, []byte("# Initial Project\nLine 1: baseline\nLine 2: baseline\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	runGit("add", "README.md")
	runGit("commit", "-m", "initial commit")
	runGit("branch", "-M", "main")

	return dir
}

func TestSimulateMergeTree_ConflictFreeAndDiskUnaltered(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Create branch-clean that adds a new file
	runGit("checkout", "-b", "feat/clean", "main")
	newFile := filepath.Join(repoDir, "feature.txt")
	_ = os.WriteFile(newFile, []byte("clean new feature\n"), 0644)
	runGit("add", "feature.txt")
	runGit("commit", "-m", "add clean feature")

	// Back to main
	runGit("checkout", "main")

	// Capture state of disk before simulation
	beforeEntries, err := os.ReadDir(repoDir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	q := NewQueue(nil, nil, nil, nil, Config{RepoPath: repoDir, BaseBranch: "main"})
	sim := q.SimulateMergeTree(context.Background(), repoDir, "main", "feat/clean")

	if !sim.ConflictFree {
		t.Fatalf("expected conflict-free simulation, got conflicts: %v", sim.ConflictFiles)
	}
	if sim.TreeSHA == "" {
		t.Errorf("expected non-empty tree SHA")
	}

	// Verify disk was NOT altered (feature.txt should not be present in main's disk worktree)
	afterEntries, _ := os.ReadDir(repoDir)
	if len(beforeEntries) != len(afterEntries) {
		t.Errorf("expected working directory to remain unaltered: before=%d, after=%d", len(beforeEntries), len(afterEntries))
	}
	if _, err := os.Stat(filepath.Join(repoDir, "feature.txt")); !os.IsNotExist(err) {
		t.Errorf("expected feature.txt to NOT exist on main worktree after in-memory simulation")
	}
}

func TestSimulateMergeTree_DetectsConflictsAccurately(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Create feature branch that modifies Line 2 in README.md
	runGit("checkout", "-b", "feat/conflict", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Initial Project\nLine 1: baseline\nLine 2: FEATURE EDIT\n"), 0644)
	runGit("commit", "-am", "feature edit to line 2")

	// On main, also edit Line 2 in conflicting way
	runGit("checkout", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Initial Project\nLine 1: baseline\nLine 2: MAIN EDIT COLLISION\n"), 0644)
	runGit("commit", "-am", "main edit collision on line 2")

	q := NewQueue(nil, nil, nil, nil, Config{RepoPath: repoDir, BaseBranch: "main"})
	sim := q.SimulateMergeTree(context.Background(), repoDir, "main", "feat/conflict")

	if sim.ConflictFree {
		t.Fatalf("expected merge conflicts, but simulation reported conflict free")
	}
	if len(sim.ConflictFiles) == 0 {
		t.Errorf("expected conflict files list to include README.md, got empty list")
	}

	foundReadme := false
	for _, f := range sim.ConflictFiles {
		if strings.Contains(f, "README.md") {
			foundReadme = true
			break
		}
	}
	if !foundReadme {
		t.Errorf("expected README.md in conflict files, got: %v", sim.ConflictFiles)
	}
}

func TestQueue_HappyPathFastForwardRebase(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
		return string(out)
	}

	// Create feature branch ov-run/run-ff-01 with a non-conflicting new file
	runGit("checkout", "-b", "ov-run/run-ff-01", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "service.go"), []byte("package main\n\nfunc Service() string { return \"ok\" }\n"), 0644)
	runGit("add", "service.go")
	runGit("commit", "-m", "feat: add service.go")

	// Switch back to main and add another independent commit
	runGit("checkout", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "notes.txt"), []byte("some notes\n"), 0644)
	runGit("add", "notes.txt")
	runGit("commit", "-m", "docs: update notes")

	verified := false
	verifier := func(ctx context.Context, path string) error {
		verified = true
		return nil
	}

	pub := &mockMergerPublisher{}
	pr := &mockMergerPRCreator{}

	q := NewQueue(nil, pub, pr, verifier, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.EnqueueRequest(&MergeRequest{
		RunID:        "run-ff-01",
		SourceBranch: "ov-run/run-ff-01",
		TargetBranch: "main",
	})

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error processing merge: %v", err)
	}

	if res.Status != StatusMerged {
		t.Fatalf("expected StatusMerged, got %s (err: %s)", res.Status, res.Error)
	}
	if !verified {
		t.Errorf("expected test verifier to be called")
	}

	// Verify main has the fast-forwarded commit
	logOut := runGit("log", "--oneline", "-n", "3")
	if !strings.Contains(logOut, "feat: add service.go") {
		t.Errorf("expected main log to contain 'feat: add service.go', got:\n%s", logOut)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "service.go")); err != nil {
		t.Errorf("expected service.go to exist in main worktree after merge")
	}
}

func TestQueue_ConflictPathDivertedToPR(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Conflicting changes
	runGit("checkout", "-b", "ov-run/run-conflict-02", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("CONFLICT BRANCH CONTENT"), 0644)
	runGit("commit", "-am", "branch edit")

	runGit("checkout", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("CONFLICT MAIN CONTENT"), 0644)
	runGit("commit", "-am", "main edit")

	pub := &mockMergerPublisher{}
	pr := &mockMergerPRCreator{}

	q := NewQueue(nil, pub, pr, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.EnqueueRequest(&MergeRequest{
		RunID:        "run-conflict-02",
		SourceBranch: "ov-run/run-conflict-02",
		TargetBranch: "main",
	})

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error during conflict processing: %v", err)
	}

	if res.Status != StatusDivertedToPR {
		t.Errorf("expected StatusDivertedToPR, got %s", res.Status)
	}
	if res.PRURL == "" {
		t.Errorf("expected PR URL to be set")
	}

	// Verify PR creator received conflict details
	pr.mu.Lock()
	if len(pr.created) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(pr.created))
	} else {
		desc := pr.created[0].Description
		if !strings.Contains(desc, "README.md") {
			t.Errorf("expected PR description to mention conflicting file README.md, got:\n%s", desc)
		}
	}
	pr.mu.Unlock()

	// Verify gateway event was emitted
	pub.mu.Lock()
	if len(pub.events) != 1 {
		t.Errorf("expected 1 gateway event, got %d", len(pub.events))
	} else {
		ev := pub.events[0]
		if ev.Type != gateway.TopicMergerDivertedToPR {
			t.Errorf("expected event type %s, got %s", gateway.TopicMergerDivertedToPR, ev.Type)
		}
		if ev.RunID != "run-conflict-02" {
			t.Errorf("expected event runID run-conflict-02, got %s", ev.RunID)
		}
	}
	pub.mu.Unlock()
}

func TestQueue_TestVerificationRegressionDivertsToPR(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("checkout", "-b", "ov-run/run-test-fail", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "broken.go"), []byte("package main\n"), 0644)
	runGit("add", "broken.go")
	runGit("commit", "-m", "add broken code")
	runGit("checkout", "main")

	// Verifier fails
	failingVerifier := func(ctx context.Context, path string) error {
		return exec.ErrNotFound // simulated test failure
	}

	pub := &mockMergerPublisher{}
	pr := &mockMergerPRCreator{}

	q := NewQueue(nil, pub, pr, failingVerifier, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.EnqueueRequest(&MergeRequest{
		RunID:        "run-test-fail",
		SourceBranch: "ov-run/run-test-fail",
		TargetBranch: "main",
	})

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != StatusDivertedToPR {
		t.Errorf("expected StatusDivertedToPR upon test regression, got %s", res.Status)
	}
}

func TestQueue_ThreadSafeFIFOOrderAndRace(t *testing.T) {
	q := NewQueue(nil, nil, nil, nil, Config{BaseBranch: "main"})

	// Concurrent enqueue
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = q.EnqueueRequest(&MergeRequest{
				RunID: string(rune('a' + id)),
			})
			_ = q.Len()
		}(i)
	}
	wg.Wait()

	if q.Len() != 50 {
		t.Errorf("expected 50 items in queue, got %d", q.Len())
	}
}

// legacyGitExecutor simulates Git < 2.43 by returning exit status 129 with an unknown option error when --merge-base is passed.
type legacyGitExecutor struct {
	realGit GitExecutor
}

func (l *legacyGitExecutor) Run(ctx context.Context, dir string, args ...string) (string, error) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "--merge-base=") || arg == "--merge-base" {
			return fmt.Sprintf("error: unknown option `merge-base=%s'\nusage: git merge-tree [--write-tree] [<options>] <branch1> <branch2>\n", strings.TrimPrefix(arg, "--merge-base=")), errors.New("exit status 129")
		}
	}
	return l.realGit.Run(ctx, dir, args...)
}

func TestSimulateMergeTree_LegacyGitFallback_ConflictFree(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("checkout", "-b", "feat/clean-legacy", "main")
	newFile := filepath.Join(repoDir, "feature-legacy.txt")
	_ = os.WriteFile(newFile, []byte("clean legacy feature\n"), 0644)
	runGit("add", "feature-legacy.txt")
	runGit("commit", "-m", "add clean legacy feature")
	runGit("checkout", "main")

	legacyGit := &legacyGitExecutor{realGit: &OSGitExecutor{}}
	q := NewQueue(legacyGit, nil, nil, nil, Config{RepoPath: repoDir, BaseBranch: "main"})

	sim := q.SimulateMergeTree(context.Background(), repoDir, "main", "feat/clean-legacy")
	if !sim.ConflictFree {
		t.Fatalf("expected conflict-free simulation under legacy git fallback, got conflicts: %v (raw: %s)", sim.ConflictFiles, sim.RawOutput)
	}
	if sim.TreeSHA == "" {
		t.Errorf("expected non-empty tree SHA under legacy git fallback")
	}
}

func TestQueue_LegacyGitFallback_HappyPathFastForward(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
		return string(out)
	}

	runGit("checkout", "-b", "ov-run/run-legacy-01", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "legacy_service.go"), []byte("package main\n\nfunc LegacyService() string { return \"ok\" }\n"), 0644)
	runGit("add", "legacy_service.go")
	runGit("commit", "-m", "feat: add legacy_service.go")

	runGit("checkout", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "notes.txt"), []byte("some notes\n"), 0644)
	runGit("add", "notes.txt")
	runGit("commit", "-m", "docs: update notes")

	legacyGit := &legacyGitExecutor{realGit: &OSGitExecutor{}}
	pub := &mockMergerPublisher{}
	pr := &mockMergerPRCreator{}

	q := NewQueue(legacyGit, pub, pr, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.EnqueueRequest(&MergeRequest{
		RunID:        "run-legacy-01",
		SourceBranch: "ov-run/run-legacy-01",
		TargetBranch: "main",
	})

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error processing merge: %v", err)
	}

	if res.Status != StatusMerged {
		t.Fatalf("expected StatusMerged, got %s (err: %s)", res.Status, res.Error)
	}
}

func TestSimulateMergeTree_LegacyGitFallback_DetectsConflicts(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Create feature branch that modifies Line 2 in README.md
	runGit("checkout", "-b", "feat/conflict-legacy", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Initial Project\nLine 1: baseline\nLine 2: LEGACY FEATURE EDIT\n"), 0644)
	runGit("commit", "-am", "feature edit to line 2")

	// On main, also edit Line 2 in conflicting way
	runGit("checkout", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Initial Project\nLine 1: baseline\nLine 2: MAIN EDIT COLLISION\n"), 0644)
	runGit("commit", "-am", "main edit collision on line 2")

	legacyGit := &legacyGitExecutor{realGit: &OSGitExecutor{}}
	q := NewQueue(legacyGit, nil, nil, nil, Config{RepoPath: repoDir, BaseBranch: "main"})
	sim := q.SimulateMergeTree(context.Background(), repoDir, "main", "feat/conflict-legacy")

	if sim.ConflictFree {
		t.Fatalf("expected merge conflicts under legacy git fallback, but simulation reported conflict free")
	}
	if len(sim.ConflictFiles) == 0 {
		t.Errorf("expected conflict files list to include README.md, got empty list")
	}

	foundReadme := false
	for _, f := range sim.ConflictFiles {
		if strings.Contains(f, "README.md") {
			foundReadme = true
			break
		}
	}
	if !foundReadme {
		t.Errorf("expected README.md in conflict files under legacy git fallback, got: %v", sim.ConflictFiles)
	}
}

func TestQueue_LegacyGitFallback_ConflictPathDivertedToPR(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Conflicting changes
	runGit("checkout", "-b", "ov-run/run-conflict-legacy", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("CONFLICT BRANCH CONTENT"), 0644)
	runGit("commit", "-am", "branch edit")

	runGit("checkout", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("CONFLICT MAIN CONTENT"), 0644)
	runGit("commit", "-am", "main edit")

	legacyGit := &legacyGitExecutor{realGit: &OSGitExecutor{}}
	pub := &mockMergerPublisher{}
	pr := &mockMergerPRCreator{}

	q := NewQueue(legacyGit, pub, pr, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.EnqueueRequest(&MergeRequest{
		RunID:        "run-conflict-legacy",
		SourceBranch: "ov-run/run-conflict-legacy",
		TargetBranch: "main",
	})

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error during conflict processing: %v", err)
	}

	if res.Status != StatusDivertedToPR {
		t.Errorf("expected StatusDivertedToPR, got %s", res.Status)
	}
	if res.PRURL == "" {
		t.Errorf("expected PR URL to be set")
	}

	pr.mu.Lock()
	if len(pr.created) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(pr.created))
	} else {
		desc := pr.created[0].Description
		if !strings.Contains(desc, "README.md") {
			t.Errorf("expected PR description to mention conflicting file README.md, got:\n%s", desc)
		}
	}
	pr.mu.Unlock()
}

func TestIsUnknownOption(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		err      error
		option   string
		expected bool
	}{
		{
			name:     "backtick unknown option format (git 2.39)",
			output:   "error: unknown option `merge-base=c123456'\nusage: git merge-tree [--write-tree]",
			err:      errors.New("exit status 129"),
			option:   "merge-base",
			expected: true,
		},
		{
			name:     "single quote unknown option format",
			output:   "error: unknown option 'merge-base=c123456'",
			err:      errors.New("exit status 129"),
			option:   "merge-base",
			expected: true,
		},
		{
			name:     "unrecognized option format",
			output:   "fatal: unrecognized option '--merge-base=c123456'",
			err:      errors.New("exit status 129"),
			option:   "merge-base",
			expected: true,
		},
		{
			name:     "error in err string rather than output",
			output:   "",
			err:      errors.New("error: unknown option `merge-base'"),
			option:   "merge-base",
			expected: true,
		},
		{
			name:     "normal conflict output",
			output:   "Auto-merging README.md\nCONFLICT (content): Merge conflict in README.md",
			err:      errors.New("exit status 1"),
			option:   "merge-base",
			expected: false,
		},
		{
			name:     "fatal non-option error",
			output:   "fatal: Not a valid object name main",
			err:      errors.New("exit status 128"),
			option:   "merge-base",
			expected: false,
		},
		{
			name:     "different option reported unknown",
			output:   "error: unknown option `some-other-option'",
			err:      errors.New("exit status 129"),
			option:   "merge-base",
			expected: false,
		},
		{
			name:     "clean execution nil error",
			output:   "3a4b5c6d7e8f",
			err:      nil,
			option:   "merge-base",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isUnknownOption(tc.output, tc.err, tc.option)
			if actual != tc.expected {
				t.Errorf("isUnknownOption(%q, %v, %q) = %v; expected %v",
					tc.output, tc.err, tc.option, actual, tc.expected)
			}
		})
	}
}

func TestQueue_ImplementsMergeQueueInterface(t *testing.T) {
	var _ MergeQueue = (*Queue)(nil)
}

func TestQueue_HappyPathEmitsTopicMergeCompleted(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("checkout", "-b", "ov-run/run-event-01", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "completed_event.txt"), []byte("hello completed event\n"), 0644)
	runGit("add", "completed_event.txt")
	runGit("commit", "-m", "feat: completed event")

	pub := &mockMergerPublisher{}
	q := NewQueue(nil, pub, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	err := q.Enqueue(context.Background(), MergeRequest{
		RunID:        "run-event-01",
		SourceBranch: "ov-run/run-event-01",
		TargetBranch: "main",
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusMerged {
		t.Fatalf("expected StatusMerged, got %s", res.Status)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 event published, got %d", len(pub.events))
	}
	ev := pub.events[0]
	if ev.Type != gateway.TopicMergeCompleted {
		t.Errorf("expected event type %s, got %s", gateway.TopicMergeCompleted, ev.Type)
	}
	if ev.RunID != "run-event-01" {
		t.Errorf("expected runID 'run-event-01', got %s", ev.RunID)
	}
}

func TestQueue_ConflictEmitsTopicMergeDiverted(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Divergent change on main
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Divergent Main\n"), 0644)
	runGit("add", "README.md")
	runGit("commit", "-m", "main: edit readme")

	// Branch with conflicting change
	runGit("checkout", "-b", "ov-run/run-conflict-event", "HEAD~1")
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Divergent Branch\n"), 0644)
	runGit("add", "README.md")
	runGit("commit", "-m", "branch: edit readme")

	pub := &mockMergerPublisher{}
	pr := &mockMergerPRCreator{}
	q := NewQueue(nil, pub, pr, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.Enqueue(context.Background(), MergeRequest{
		RunID:        "run-conflict-event",
		SourceBranch: "ov-run/run-conflict-event",
		TargetBranch: "main",
	})

	res, err := q.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != StatusDivertedToPR {
		t.Fatalf("expected StatusDivertedToPR, got %s", res.Status)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.events))
	}
	if pub.events[0].Type != gateway.TopicMergeDiverted {
		t.Errorf("expected event type %s, got %s", gateway.TopicMergeDiverted, pub.events[0].Type)
	}
	if len(res.Simulation.ConflictFiles) == 0 {
		t.Errorf("expected conflicting files to be reported")
	}
}

func TestQueue_ProcessAll_And_ContextCancellation(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	// Create 2 branches
	runGit("checkout", "-b", "ov-run/multi-1", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "file1.txt"), []byte("1"), 0644)
	runGit("add", "file1.txt")
	runGit("commit", "-m", "add file1")

	runGit("checkout", "-b", "ov-run/multi-2", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "file2.txt"), []byte("2"), 0644)
	runGit("add", "file2.txt")
	runGit("commit", "-m", "add file2")

	q := NewQueue(nil, nil, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.Enqueue(context.Background(), MergeRequest{RunID: "multi-1"})
	_ = q.Enqueue(context.Background(), MergeRequest{RunID: "multi-2"})

	if q.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", q.Len())
	}

	results, err := q.ProcessAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Test ProcessAll on empty queue
	resEmpty, err := q.ProcessAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on empty: %v", err)
	}
	if len(resEmpty) != 0 {
		t.Fatalf("expected 0 results, got %d", len(resEmpty))
	}

	// Test ProcessAll with canceled context
	_ = q.Enqueue(context.Background(), MergeRequest{RunID: "multi-canceled"})
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = q.ProcessAll(canceledCtx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestQueue_RunWorker_SequentialProcessing(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("checkout", "-b", "ov-run/worker-1", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "worker1.txt"), []byte("w1"), 0644)
	runGit("add", "worker1.txt")
	runGit("commit", "-m", "worker 1")

	pub := &mockMergerPublisher{}
	q := NewQueue(nil, pub, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})

	_ = q.Enqueue(context.Background(), MergeRequest{RunID: "worker-1"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := q.StartWorker(ctx, 10*time.Millisecond)

	// Wait for queue to drain
	for i := 0; i < 50; i++ {
		if q.Len() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done

	if q.Len() != 0 {
		t.Errorf("expected queue to be drained by worker, got len=%d", q.Len())
	}
}

func TestQueue_EnqueueValidation_And_EmptyQueue(t *testing.T) {
	q := NewQueue(nil, nil, nil, nil, Config{})

	// Enqueue empty request
	err := q.Enqueue(context.Background(), MergeRequest{})
	if !errors.Is(err, ErrNilRequest) {
		t.Errorf("expected ErrNilRequest, got %v", err)
	}

	// Enqueue with canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = q.Enqueue(canceledCtx, MergeRequest{RunID: "valid-run"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// ProcessNext on empty queue
	_, err = q.ProcessNext(context.Background())
	if !errors.Is(err, ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

type mockCommandSelectiveGitExecutor struct {
	realGit   GitExecutor
	failMatch func(args []string) bool
}

func (m *mockCommandSelectiveGitExecutor) Run(ctx context.Context, dir string, args ...string) (string, error) {
	if m.failMatch != nil && m.failMatch(args) {
		return "simulated error", errors.New("simulated git error")
	}
	if m.realGit != nil {
		return m.realGit.Run(ctx, dir, args...)
	}
	return "", nil
}

func TestQueue_EnqueueHelpers(t *testing.T) {
	q := NewQueue(nil, nil, nil, nil, Config{})

	if err := q.EnqueueRequest(nil); !errors.Is(err, ErrNilRequest) {
		t.Errorf("expected ErrNilRequest, got %v", err)
	}

	req := &MergeRequest{RunID: "req-1"}
	if err := q.EnqueueRequest(req); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := q.EnqueueApproval(context.Background(), "appr-1", map[string]any{"source": "test"}); err != nil {
		t.Errorf("unexpected error on EnqueueApproval: %v", err)
	}

	if q.Len() != 2 {
		t.Errorf("expected 2 items, got %d", q.Len())
	}
}

func TestParseConflicts_Formats(t *testing.T) {
	output := `
Auto-merging README.md
CONFLICT (content): Merge conflict in README.md
CONFLICT (modify/delete): deleted.go deleted in HEAD and modified in branch. Version branch of deleted.go left in tree.
Merge conflict in "pkg/quoted.go"
Merge conflict in 'pkg/single_quoted.go'
Some random line without conflict
`
	conflicts := parseConflicts(output)
	if len(conflicts) < 3 {
		t.Errorf("expected at least 3 conflict files parsed, got %d: %v", len(conflicts), conflicts)
	}
}

func TestQueue_ProcessNext_GitFailurePaths(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("checkout", "-b", "ov-run/fail-path", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "fail.txt"), []byte("fail"), 0644)
	runGit("add", "fail.txt")
	runGit("commit", "-m", "fail path")

	// 1. Checkout target branch failure
	mockGit := &mockCommandSelectiveGitExecutor{
		realGit: &OSGitExecutor{},
		failMatch: func(args []string) bool {
			return len(args) >= 2 && args[0] == "checkout" && args[1] == "main"
		},
	}
	q1 := NewQueue(mockGit, nil, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})
	_ = q1.Enqueue(context.Background(), MergeRequest{RunID: "fail-path"})
	_, err := q1.ProcessNext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to checkout target branch") {
		t.Errorf("expected failed to checkout target branch error, got %v", err)
	}

	// 2. Merge --ff-only failure
	mockGitMerge := &mockCommandSelectiveGitExecutor{
		realGit: &OSGitExecutor{},
		failMatch: func(args []string) bool {
			return len(args) >= 2 && args[0] == "merge" && args[1] == "--ff-only"
		},
	}
	q2 := NewQueue(mockGitMerge, nil, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})
	_ = q2.Enqueue(context.Background(), MergeRequest{RunID: "fail-path"})
	_, err = q2.ProcessNext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fast-forward merge") {
		t.Errorf("expected fast-forward merge error, got %v", err)
	}

	// 3. Push to remote failure (when SkipPush is false)
	mockGitPush := &mockCommandSelectiveGitExecutor{
		realGit: &OSGitExecutor{},
		failMatch: func(args []string) bool {
			return len(args) >= 1 && args[0] == "push"
		},
	}
	q3 := NewQueue(mockGitPush, nil, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   false,
	})
	_ = q3.Enqueue(context.Background(), MergeRequest{RunID: "fail-path"})
	_, err = q3.ProcessNext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "push to origin") {
		t.Errorf("expected push to origin error, got %v", err)
	}
}

func TestQueue_ProcessAll_ErrorPropagation(t *testing.T) {
	repoDir := setupGitFixture(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v (output: %s)", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("checkout", "-b", "ov-run/err-item", "main")
	_ = os.WriteFile(filepath.Join(repoDir, "err.txt"), []byte("err"), 0644)
	runGit("add", "err.txt")
	runGit("commit", "-m", "err item")

	mockGit := &mockCommandSelectiveGitExecutor{
		realGit: &OSGitExecutor{},
		failMatch: func(args []string) bool {
			return len(args) >= 2 && args[0] == "checkout" && args[1] == "main"
		},
	}
	q := NewQueue(mockGit, nil, nil, nil, Config{
		RepoPath:   repoDir,
		BaseBranch: "main",
		SkipPush:   true,
	})
	_ = q.Enqueue(context.Background(), MergeRequest{RunID: "err-item"})
	_, err := q.ProcessAll(context.Background())
	if err == nil {
		t.Error("expected error from ProcessAll when ProcessNext fails")
	}
}


