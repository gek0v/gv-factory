// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package merger

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

var (
	// ErrQueueEmpty is returned when ProcessNext is called on an empty queue.
	ErrQueueEmpty = errors.New("merge queue is empty")
	// ErrNilRequest is returned when attempting to enqueue a nil request.
	ErrNilRequest = errors.New("merge request cannot be nil")
)

// MergeStatus represents the resolution state of a merge queue item.
type MergeStatus string

const (
	StatusMerged       MergeStatus = "MERGED"
	StatusDivertedToPR MergeStatus = "DIVERTED_TO_PR"
	StatusFailed       MergeStatus = "FAILED"
)

// MergeQueue coordinates sequential integration to target branches with in-memory conflict pre-validation.
type MergeQueue interface {
	Enqueue(ctx context.Context, req MergeRequest) error
	ProcessNext(ctx context.Context) (*ProcessResult, error)
	Len() int
}

// MergeRequest represents an approved factory output queued for sequential integration.
type MergeRequest struct {
	RunID        string         `json:"run_id"`
	SourceBranch string         `json:"source_branch"`
	TargetBranch string         `json:"target_branch"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// SimulationResult captures the outcome of in-memory git merge-tree evaluation.
type SimulationResult struct {
	ConflictFree  bool     `json:"conflict_free"`
	TreeSHA       string   `json:"tree_sha,omitempty"`
	ConflictFiles []string `json:"conflict_files,omitempty"`
	RawOutput     string   `json:"raw_output,omitempty"`
}

// ProcessResult captures the final outcome of processing a MergeRequest.
type ProcessResult struct {
	Request    *MergeRequest    `json:"request"`
	Status     MergeStatus      `json:"status"`
	PRURL      string           `json:"pr_url,omitempty"`
	Error      string           `json:"error,omitempty"`
	Simulation SimulationResult `json:"simulation"`
}

// EventPublisher abstracts broadcasting of merge lifecycle events.
type EventPublisher interface {
	PublishTaskEvent(ctx context.Context, ev gateway.Event) error
}

// PRDetails provides metadata needed to open a GitHub Pull Request upon conflict diversion.
type PRDetails struct {
	RunID       string `json:"run_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	HeadBranch  string `json:"head_branch"`
	BaseBranch  string `json:"base_branch"`
}

// PRResult holds details of the opened Pull Request.
type PRResult struct {
	PRNumber int    `json:"pr_number"`
	PRURL    string `json:"pr_url"`
}

// PRCreator abstracts opening GitHub Pull Requests.
type PRCreator interface {
	CreatePullRequest(ctx context.Context, details PRDetails) (*PRResult, error)
}

// GitExecutor executes Git commands in a given directory.
type GitExecutor interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// OSGitExecutor executes git via os/exec.
type OSGitExecutor struct{}

func (e *OSGitExecutor) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestVerifier verifies the tree against semantic regressions before fast-forwarding.
type TestVerifier func(ctx context.Context, repoPath string) error

// Config provides configuration for the sequential merge queue.
type Config struct {
	RepoPath   string
	BaseBranch string
	SkipPush   bool
}

// Queue coordinates sequential integration to main with in-memory conflict pre-validation.
type Queue struct {
	queueMu   sync.Mutex
	processMu sync.Mutex
	items     []*MergeRequest
	notify    chan struct{}
	git       GitExecutor
	publisher EventPublisher
	prCreator PRCreator
	verifier  TestVerifier
	config    Config
}

// NewQueue creates an initialized sequential merge queue.
func NewQueue(
	git GitExecutor,
	publisher EventPublisher,
	prCreator PRCreator,
	verifier TestVerifier,
	cfg Config,
) *Queue {
	if git == nil {
		git = &OSGitExecutor{}
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}

	return &Queue{
		items:     make([]*MergeRequest, 0),
		notify:    make(chan struct{}, 100),
		git:       git,
		publisher: publisher,
		prCreator: prCreator,
		verifier:  verifier,
		config:    cfg,
	}
}

// Enqueue adds an approved run to the thread-safe sequential integration queue.
func (q *Queue) Enqueue(ctx context.Context, req MergeRequest) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if req.RunID == "" && req.SourceBranch == "" {
		return ErrNilRequest
	}
	if req.TargetBranch == "" {
		req.TargetBranch = q.config.BaseBranch
	}
	if req.SourceBranch == "" && req.RunID != "" {
		req.SourceBranch = fmt.Sprintf("ov-run/%s", req.RunID)
	}

	q.queueMu.Lock()
	defer q.queueMu.Unlock()
	reqCopy := req
	q.items = append(q.items, &reqCopy)
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

// EnqueueRequest is a backward-compatible helper for pointer requests.
func (q *Queue) EnqueueRequest(req *MergeRequest) error {
	if req == nil {
		return ErrNilRequest
	}
	return q.Enqueue(context.Background(), *req)
}

// EnqueueApproval adapts MergeQueueReceiver contract from the decision gate engine.
func (q *Queue) EnqueueApproval(ctx context.Context, runID string, metadata map[string]any) error {
	return q.Enqueue(ctx, MergeRequest{
		RunID:    runID,
		Metadata: metadata,
	})
}

// Len returns the current count of queued merge requests.
func (q *Queue) Len() int {
	q.queueMu.Lock()
	defer q.queueMu.Unlock()
	return len(q.items)
}

// SimulateMergeTree executes in-memory git merge-tree simulation without touching the disk worktree.
func (q *Queue) SimulateMergeTree(ctx context.Context, repoPath, targetBranch, sourceBranch string) SimulationResult {
	baseOut, baseErr := q.git.Run(ctx, repoPath, "merge-base", targetBranch, sourceBranch)
	base := strings.TrimSpace(baseOut)

	var out string
	var err error
	if baseErr == nil && base != "" {
		out, err = q.git.Run(ctx, repoPath, "merge-tree", "--write-tree", "--merge-base="+base, targetBranch, sourceBranch)
		// Git < 2.43 does not support --merge-base with git merge-tree and fails with an unknown option error.
		// In this case, fall back gracefully to git merge-tree --write-tree targetBranch sourceBranch,
		// where Git automatically determines the common merge base.
		if err != nil && isUnknownOption(out, err, "merge-base") {
			out, err = q.git.Run(ctx, repoPath, "merge-tree", "--write-tree", targetBranch, sourceBranch)
		}
	} else {
		out, err = q.git.Run(ctx, repoPath, "merge-tree", "--write-tree", targetBranch, sourceBranch)
	}

	conflicts := parseConflicts(out)
	hasConflicts := err != nil || len(conflicts) > 0 || strings.Contains(out, "CONFLICT")

	treeSHA := ""
	if !hasConflicts {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) > 0 {
			treeSHA = strings.TrimSpace(lines[0])
		}
	}

	return SimulationResult{
		ConflictFree:  !hasConflicts,
		TreeSHA:       treeSHA,
		ConflictFiles: conflicts,
		RawOutput:     out,
	}
}

// isUnknownOption checks whether the output or error string indicates that Git rejected an unrecognized option.
func isUnknownOption(output string, err error, option string) bool {
	combined := output
	if err != nil {
		combined += " " + err.Error()
	}
	lower := strings.ToLower(combined)
	optLower := strings.ToLower(option)
	hasUnknown := strings.Contains(lower, "unknown option") ||
		strings.Contains(lower, "unrecognized option") ||
		strings.Contains(lower, "unknown switch")
	return hasUnknown && strings.Contains(lower, optLower)
}

// parseConflicts extracts conflicted file paths from git merge-tree output.
func parseConflicts(output string) []string {
	seen := make(map[string]struct{})
	var files []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		var file string
		if strings.Contains(line, "Merge conflict in ") {
			parts := strings.Split(line, "Merge conflict in ")
			if len(parts) > 1 {
				file = strings.TrimSpace(parts[1])
			}
		} else if strings.HasPrefix(line, "CONFLICT (") {
			parts := strings.Split(line, "): ")
			if len(parts) > 1 {
				tokens := strings.Fields(parts[1])
				if len(tokens) > 0 {
					file = strings.TrimSpace(tokens[len(tokens)-1])
				}
			}
		}

		if file != "" {
			file = strings.Trim(file, `"'`)
			if _, exists := seen[file]; !exists {
				seen[file] = struct{}{}
				files = append(files, file)
			}
		}
	}
	return files
}

// ProcessNext pops the head item from the FIFO queue and processes it through
// in-memory git merge-tree pre-validation, rebase, testing, and fast-forward merge.
func (q *Queue) ProcessNext(ctx context.Context) (*ProcessResult, error) {
	q.queueMu.Lock()
	if len(q.items) == 0 {
		q.queueMu.Unlock()
		return nil, ErrQueueEmpty
	}
	req := q.items[0]
	q.items = q.items[1:]
	q.queueMu.Unlock()

	q.processMu.Lock()
	defer q.processMu.Unlock()

	repo := q.config.RepoPath
	target := req.TargetBranch
	source := req.SourceBranch

	// 1. In-memory simulation: git merge-tree --write-tree
	sim := q.SimulateMergeTree(ctx, repo, target, source)
	if !sim.ConflictFree {
		return q.divertToPR(ctx, req, sim, "merge_conflict", "Merge conflicts detected during in-memory git merge-tree pre-validation")
	}

	// 2. Happy Path: Checkout source and rebase onto target
	if _, err := q.git.Run(ctx, repo, "checkout", source); err != nil {
		return q.divertToPR(ctx, req, sim, "checkout_failed", fmt.Sprintf("Failed to checkout source branch %q: %v", source, err))
	}

	if rebaseOut, err := q.git.Run(ctx, repo, "rebase", target); err != nil {
		_, _ = q.git.Run(ctx, repo, "rebase", "--abort")
		return q.divertToPR(ctx, req, sim, "rebase_conflict", fmt.Sprintf("Rebase onto %s failed: %v\nOutput: %s", target, err, rebaseOut))
	}

	// 3. Verification tests to guard against semantic regressions
	if q.verifier != nil {
		if err := q.verifier(ctx, repo); err != nil {
			return q.divertToPR(ctx, req, sim, "test_regression", fmt.Sprintf("Verification test suite failed after rebase: %v", err))
		}
	}

	// 4. Fast-forward merge onto target branch
	if _, err := q.git.Run(ctx, repo, "checkout", target); err != nil {
		return nil, fmt.Errorf("failed to checkout target branch %q: %w", target, err)
	}

	if mergeOut, err := q.git.Run(ctx, repo, "merge", "--ff-only", source); err != nil {
		return nil, fmt.Errorf("fast-forward merge of %s into %s failed: %w (output: %s)", source, target, err, mergeOut)
	}

	// 5. Push to remote
	if !q.config.SkipPush {
		if pushOut, err := q.git.Run(ctx, repo, "push", "origin", target); err != nil {
			return nil, fmt.Errorf("push to origin %s failed: %w (output: %s)", target, err, pushOut)
		}
	}

	// 6. Emit gateway.TopicMergeCompleted event over internal/gateway
	if q.publisher != nil {
		_ = q.publisher.PublishTaskEvent(ctx, gateway.Event{
			Type:  gateway.TopicMergeCompleted,
			RunID: req.RunID,
			Payload: map[string]any{
				"run_id":        req.RunID,
				"source_branch": source,
				"target_branch": target,
				"status":        string(StatusMerged),
				"tree_sha":      sim.TreeSHA,
			},
			Timestamp: time.Now().Unix(),
		})
	}

	return &ProcessResult{
		Request:    req,
		Status:     StatusMerged,
		Simulation: sim,
	}, nil
}

// divertToPR handles the conflict path: aborts auto-merge, pushes transient branch,
// opens a Pull Request with detailed conflict analysis, and emits gateway event.
func (q *Queue) divertToPR(
	ctx context.Context,
	req *MergeRequest,
	sim SimulationResult,
	reason string,
	details string,
) (*ProcessResult, error) {
	repo := q.config.RepoPath
	source := req.SourceBranch
	target := req.TargetBranch

	// 1. Push transient branch ov-run/<run_id> to remote
	if !q.config.SkipPush {
		_, _ = q.git.Run(ctx, repo, "push", "origin", source)
	}

	// 2. Open GitHub Pull Request with detailed conflict analysis from git merge-tree
	var prURL string
	if q.prCreator != nil {
		conflictsList := "None detected in pre-validation"
		if len(sim.ConflictFiles) > 0 {
			conflictsList = "- `" + strings.Join(sim.ConflictFiles, "`\n- `") + "`"
		}

		desc := fmt.Sprintf("## Automatic Merge Diverted to Pull Request\n\n"+
			"**Run ID:** `%s`\n"+
			"**Source Branch:** `%s`\n"+
			"**Target Branch:** `%s`\n"+
			"**Diversion Reason:** `%s`\n\n"+
			"### Conflict Details:\n%s\n\n"+
			"### In-Memory git merge-tree Conflicting Files:\n%s\n\n"+
			"### Raw Git Diagnostic Output:\n```\n%s\n```\n",
			req.RunID,
			source,
			target,
			reason,
			details,
			conflictsList,
			sim.RawOutput,
		)

		prRes, err := q.prCreator.CreatePullRequest(ctx, PRDetails{
			RunID:       req.RunID,
			Title:       fmt.Sprintf("%s: Merge Conflict Diverted to PR (%s -> %s)", req.RunID, source, target),
			Description: desc,
			HeadBranch:  source,
			BaseBranch:  target,
		})
		if err == nil && prRes != nil {
			prURL = prRes.PRURL
		}
	}

	// 3. Emit merger.diverted_to_pr event over internal/gateway
	if q.publisher != nil {
		_ = q.publisher.PublishTaskEvent(ctx, gateway.Event{
			Type:  gateway.TopicMergeDiverted,
			RunID: req.RunID,
			Payload: map[string]any{
				"run_id":         req.RunID,
				"source_branch":  source,
				"target_branch":  target,
				"reason":         reason,
				"conflict_files": sim.ConflictFiles,
				"pr_url":         prURL,
			},
			Timestamp: time.Now().Unix(),
		})
	}

	return &ProcessResult{
		Request:    req,
		Status:     StatusDivertedToPR,
		PRURL:      prURL,
		Error:      details,
		Simulation: sim,
	}, nil
}

// ProcessAll processes all queued requests in sequential order.
func (q *Queue) ProcessAll(ctx context.Context) ([]*ProcessResult, error) {
	var results []*ProcessResult
	for q.Len() > 0 {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		res, err := q.ProcessNext(ctx)
		if err != nil {
			if errors.Is(err, ErrQueueEmpty) {
				break
			}
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// RunWorker continuously processes queued merge requests sequentially until ctx is canceled.
func (q *Queue) RunWorker(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.notify:
		case <-ticker.C:
		}

		for q.Len() > 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			_, err := q.ProcessNext(ctx)
			if err != nil && errors.Is(err, ErrQueueEmpty) {
				break
			}
		}
	}
}

// StartWorker launches a background goroutine running RunWorker and returns a channel that closes when stopped.
func (q *Queue) StartWorker(ctx context.Context, pollInterval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = q.RunWorker(ctx, pollInterval)
	}()
	return done
}
