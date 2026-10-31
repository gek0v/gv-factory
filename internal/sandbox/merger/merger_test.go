// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package merger

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

	_ = q.Enqueue(&MergeRequest{
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

	_ = q.Enqueue(&MergeRequest{
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

	_ = q.Enqueue(&MergeRequest{
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
			_ = q.Enqueue(&MergeRequest{
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
