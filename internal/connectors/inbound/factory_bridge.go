// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

var checklistRegex = regexp.MustCompile(`^[\s*-]*\[([ xX])\]\s*(.*)$`)

// FactoryPipelineRunner defines the contract for executing autonomous factory pipelines.
type FactoryPipelineRunner interface {
	RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error)
}

// IssueFeedbackReporter defines the contract for posting markdown feedback to external issue trackers.
type IssueFeedbackReporter interface {
	PostIssueComment(ctx context.Context, repo string, issueNumber int, comment string) error
}

// BridgeConfig configures the autonomous factory bridge.
type BridgeConfig struct {
	TargetLabels        []string      `json:"target_labels"`
	CommandTriggers     []string      `json:"command_triggers"`
	BaseBranch          string        `json:"base_branch"`
	Async               bool          `json:"async"`
	GateApprovalBaseURL string        `json:"gate_approval_base_url"`
	CLISuggestionCmd    string        `json:"cli_suggestion_cmd"`
	Deduplicator        Deduplicator       `json:"-"`
	DeduplicationTTL    time.Duration      `json:"deduplication_ttl"`
	PRCreator           PullRequestCreator `json:"-"`
	AutoCreatePR        bool               `json:"auto_create_pr"`
}

// DefaultBridgeConfig returns production defaults for autonomous webhook triggering.
func DefaultBridgeConfig() BridgeConfig {
	return BridgeConfig{
		TargetLabels:        []string{"ready-for-agent", "factory", "autofix"},
		CommandTriggers:     []string{"@factory build", "/factory", "@factory"},
		BaseBranch:          "main",
		Async:               false,
		GateApprovalBaseURL: "",
		CLISuggestionCmd:    "ov gate approve",
		DeduplicationTTL:    24 * time.Hour,
	}
}

// BridgeResult holds the outcome of evaluating and processing a trigger event.
type BridgeResult struct {
	Handled       bool                   `json:"handled"`
	Skipped       bool                   `json:"skipped"`
	Async         bool                   `json:"async"`
	Reason        string                 `json:"reason,omitempty"`
	TaskID        string                 `json:"task_id,omitempty"`
	Branch        string                 `json:"branch,omitempty"`
	PRURL         string                 `json:"pr_url,omitempty"`
	Outcome       *agent.PipelineOutcome `json:"outcome,omitempty"`
	CommentPosted bool                   `json:"comment_posted"`
}

// AutonomousFactoryBridge connects inbound GitHub and GitLab events directly to FactoryPipelineRunner.
type AutonomousFactoryBridge struct {
	cfg       BridgeConfig
	runner    FactoryPipelineRunner
	publisher EventPublisher
	reporter  IssueFeedbackReporter
	prCreator PullRequestCreator
	dedup     Deduplicator
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewAutonomousFactoryBridge constructs an initialized AutonomousFactoryBridge.
func NewAutonomousFactoryBridge(
	cfg BridgeConfig,
	runner FactoryPipelineRunner,
	publisher EventPublisher,
	reporter IssueFeedbackReporter,
) *AutonomousFactoryBridge {
	if len(cfg.TargetLabels) == 0 {
		cfg.TargetLabels = []string{"ready-for-agent", "factory", "autofix"}
	}
	if len(cfg.CommandTriggers) == 0 {
		cfg.CommandTriggers = []string{"@factory build", "/factory", "@factory"}
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if cfg.CLISuggestionCmd == "" {
		cfg.CLISuggestionCmd = "ov gate approve"
	}

	dedup := cfg.Deduplicator
	if dedup == nil {
		ttl := cfg.DeduplicationTTL
		if ttl <= 0 {
			ttl = 24 * time.Hour
		}
		dedup = NewMemoryDeduplicator(ttl)
	}

	prCreator := cfg.PRCreator
	if prCreator == nil {
		if pc, ok := reporter.(PullRequestCreator); ok {
			prCreator = pc
		}
	}

	return &AutonomousFactoryBridge{
		cfg:       cfg,
		runner:    runner,
		publisher: publisher,
		reporter:  reporter,
		prCreator: prCreator,
		dedup:     dedup,
	}
}

// SetPRCreator configures or overrides the PullRequestCreator on the bridge.
func (b *AutonomousFactoryBridge) SetPRCreator(prc PullRequestCreator) {
	b.prCreator = prc
}

// PRCreator returns the active PullRequestCreator, if configured.
func (b *AutonomousFactoryBridge) PRCreator() PullRequestCreator {
	return b.prCreator
}

// Close gracefully stops the bridge and closes the deduplicator.
func (b *AutonomousFactoryBridge) Close() error {
	var err error
	b.closeOnce.Do(func() {
		b.wg.Wait()
		if b.dedup != nil {
			err = b.dedup.Close()
		}
	})
	return err
}

// Wait blocks until all asynchronous background pipeline runs have finished.
func (b *AutonomousFactoryBridge) Wait() {
	b.wg.Wait()
}

// Dispatch adapts AutonomousFactoryBridge to the Dispatcher interface.
func (b *AutonomousFactoryBridge) Dispatch(ctx context.Context, ev schema.TriggerEvent) error {
	res, err := b.HandleEvent(ctx, ev)
	if err != nil {
		return err
	}
	if res != nil && !res.Handled && !res.Skipped && res.Reason != "" {
		return errors.New(res.Reason)
	}
	return nil
}

// HandleEvent evaluates an inbound event, hydrates TaskContext, and executes the pipeline.
func (b *AutonomousFactoryBridge) HandleEvent(ctx context.Context, ev schema.TriggerEvent) (*BridgeResult, error) {
	// 1. Deduplication check
	if ev.ID != "" && b.dedup != nil && b.dedup.IsDuplicate(ctx, ev.ID) {
		return &BridgeResult{
			Handled: false,
			Skipped: true,
			Reason:  "duplicate delivery ID",
		}, nil
	}

	// 2. Trigger Recognition
	matched, reason := b.MatchesTrigger(&ev)
	if !matched {
		return &BridgeResult{
			Handled: false,
			Skipped: true,
			Reason:  reason,
		}, nil
	}

	// 3. Verify Runner
	if b.runner == nil {
		return nil, errors.New("factory pipeline runner is nil")
	}

	// 4. Hydrate TaskContext
	task, targetBranch := b.HydrateTaskContext(&ev)

	// 5. Emit TopicPipelineTriggered lifecycle event
	if b.publisher != nil {
		_ = b.publisher.PublishTaskEvent(ctx, gateway.Event{
			Type:      gateway.TopicPipelineTriggered,
			RunID:     task.TaskID,
			TaskID:    task.TaskID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"task_id":      task.TaskID,
				"title":        task.Title,
				"source":       ev.Source,
				"event_type":   ev.EventType,
				"repository":   ev.Repository,
				"issue_number": ev.IssueNumber,
				"pr_number":    ev.PRNumber,
				"sender":       ev.Sender,
				"branch":       targetBranch,
				"delivery_id":  ev.ID,
			},
		})
	}

	// 6. Pipeline Execution: Sync or Async
	if b.cfg.Async {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			_, _, _, _ = b.runAndReport(context.Background(), task, ev, targetBranch)
		}()

		return &BridgeResult{
			Handled: true,
			Async:   true,
			TaskID:  task.TaskID,
			Branch:  targetBranch,
		}, nil
	}

	// Synchronous execution
	outcome, posted, prURL, err := b.runAndReport(ctx, task, ev, targetBranch)
	if err != nil {
		return &BridgeResult{
			Handled: false,
			TaskID:  task.TaskID,
			Branch:  targetBranch,
			Reason:  err.Error(),
		}, err
	}

	return &BridgeResult{
		Handled:       true,
		Async:         false,
		TaskID:        task.TaskID,
		Branch:        targetBranch,
		PRURL:         prURL,
		Outcome:       outcome,
		CommentPosted: posted,
	}, nil
}

// MatchesTrigger checks whether the event matches supported event types and trigger criteria.
func (b *AutonomousFactoryBridge) MatchesTrigger(ev *schema.TriggerEvent) (bool, string) {
	if ev == nil {
		return false, "event is nil"
	}

	normType := strings.ToLower(strings.TrimSpace(ev.EventType))
	switch normType {
	case schema.EventIssueLabeled, "issues.labeled",
		schema.EventIssueOpened, "issues.opened",
		schema.EventIssueCommented, "issue_comment.created", "issue_comment",
		schema.EventPRCommented,
		schema.EventPROpened, "pull_request.opened",
		"pr.labeled", "pull_request.labeled":
		// Supported event types
	default:
		return false, fmt.Sprintf("unsupported event type %q", ev.EventType)
	}

	// Check configured labels
	labels := b.ExtractLabels(ev)
	for _, l := range labels {
		for _, target := range b.cfg.TargetLabels {
			if strings.EqualFold(l, target) {
				return true, fmt.Sprintf("matched label %q", l)
			}
		}
	}

	// Check command triggers in bodies
	bodies := b.extractBodies(ev)
	for _, body := range bodies {
		bodyLower := strings.ToLower(body)
		for _, cmd := range b.cfg.CommandTriggers {
			cmdLower := strings.ToLower(strings.TrimSpace(cmd))
			if cmdLower != "" && strings.Contains(bodyLower, cmdLower) {
				return true, fmt.Sprintf("matched command trigger %q", cmd)
			}
		}
	}

	return false, "no matching label or command trigger"
}

// ExtractLabels collects all labels from the event payload.
func (b *AutonomousFactoryBridge) ExtractLabels(ev *schema.TriggerEvent) []string {
	var labels []string
	if ev == nil || ev.Payload == nil {
		return labels
	}

	// 1. Direct label object: payload["label"]
	if lbl, ok := ev.Payload["label"].(map[string]any); ok {
		if name, ok := lbl["name"].(string); ok && name != "" {
			labels = append(labels, name)
		}
		if title, ok := lbl["title"].(string); ok && title != "" {
			labels = append(labels, title)
		}
	} else if lblStr, ok := ev.Payload["label"].(string); ok && lblStr != "" {
		labels = append(labels, lblStr)
	}

	// 2. GitHub issue.labels
	if issue, ok := ev.Payload["issue"].(map[string]any); ok {
		labels = append(labels, extractLabelSlice(issue["labels"])...)
	}

	// 3. GitHub pull_request.labels
	if pr, ok := ev.Payload["pull_request"].(map[string]any); ok {
		labels = append(labels, extractLabelSlice(pr["labels"])...)
	}

	// 4. GitLab payload["labels"]
	if glLabels, ok := ev.Payload["labels"]; ok {
		labels = append(labels, extractLabelSlice(glLabels)...)
	}

	// 5. GitLab object_attributes.labels
	if obj, ok := ev.Payload["object_attributes"].(map[string]any); ok {
		if glObjLabels, ok := obj["labels"]; ok {
			labels = append(labels, extractLabelSlice(glObjLabels)...)
		}
	}

	// Deduplicate preserving order
	seen := make(map[string]bool)
	var deduped []string
	for _, l := range labels {
		lClean := strings.TrimSpace(l)
		if lClean != "" && !seen[strings.ToLower(lClean)] {
			seen[strings.ToLower(lClean)] = true
			deduped = append(deduped, lClean)
		}
	}

	return deduped
}

func extractLabelSlice(raw any) []string {
	var res []string
	if raw == nil {
		return res
	}

	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if name, ok := m["name"].(string); ok && name != "" {
					res = append(res, name)
				} else if title, ok := m["title"].(string); ok && title != "" {
					res = append(res, title)
				}
			} else if s, ok := item.(string); ok && s != "" {
				res = append(res, s)
			}
		}
	case []string:
		res = append(res, v...)
	}

	return res
}

func (b *AutonomousFactoryBridge) extractBodies(ev *schema.TriggerEvent) []string {
	var bodies []string
	if ev == nil || ev.Payload == nil {
		return bodies
	}

	// Comment bodies
	if comment, ok := ev.Payload["comment"].(map[string]any); ok {
		if body, ok := comment["body"].(string); ok && body != "" {
			bodies = append(bodies, body)
		}
	}
	// Issue bodies
	if issue, ok := ev.Payload["issue"].(map[string]any); ok {
		if body, ok := issue["body"].(string); ok && body != "" {
			bodies = append(bodies, body)
		}
		if desc, ok := issue["description"].(string); ok && desc != "" {
			bodies = append(bodies, desc)
		}
	}
	// Pull Request bodies
	if pr, ok := ev.Payload["pull_request"].(map[string]any); ok {
		if body, ok := pr["body"].(string); ok && body != "" {
			bodies = append(bodies, body)
		}
		if desc, ok := pr["description"].(string); ok && desc != "" {
			bodies = append(bodies, desc)
		}
	}
	// GitLab object_attributes (note, description)
	if obj, ok := ev.Payload["object_attributes"].(map[string]any); ok {
		if note, ok := obj["note"].(string); ok && note != "" {
			bodies = append(bodies, note)
		}
		if desc, ok := obj["description"].(string); ok && desc != "" {
			bodies = append(bodies, desc)
		}
	}

	return bodies
}

// HydrateTaskContext converts a TriggerEvent into an agent.TaskContext and computes the target branch.
func (b *AutonomousFactoryBridge) HydrateTaskContext(ev *schema.TriggerEvent) (agent.TaskContext, string) {
	targetNum := ev.IssueNumber
	isPR := false
	if targetNum == 0 && ev.PRNumber > 0 {
		targetNum = ev.PRNumber
		isPR = true
	}

	var taskID string
	var targetBranch string
	if targetNum > 0 {
		if isPR {
			taskID = fmt.Sprintf("pr-%d", targetNum)
			targetBranch = fmt.Sprintf("ov-run/pr-%d", targetNum)
		} else {
			taskID = fmt.Sprintf("issue-%d", targetNum)
			targetBranch = fmt.Sprintf("ov-run/issue-%d", targetNum)
		}
	} else if ev.ID != "" {
		taskID = fmt.Sprintf("task-%s", ev.ID)
		targetBranch = fmt.Sprintf("ov-run/%s", taskID)
	} else {
		taskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
		targetBranch = fmt.Sprintf("ov-run/%s", taskID)
	}

	// Title extraction
	var title string
	if issue, ok := ev.Payload["issue"].(map[string]any); ok {
		if t, ok := issue["title"].(string); ok && t != "" {
			title = t
		}
	}
	if title == "" {
		if pr, ok := ev.Payload["pull_request"].(map[string]any); ok {
			if t, ok := pr["title"].(string); ok && t != "" {
				title = t
			}
		}
	}
	if title == "" {
		if obj, ok := ev.Payload["object_attributes"].(map[string]any); ok {
			if t, ok := obj["title"].(string); ok && t != "" {
				title = t
			}
		}
	}
	if title == "" {
		title = fmt.Sprintf("Autonomous task for %s", taskID)
	}

	// Description and body aggregation
	var rawBody strings.Builder
	bodies := b.extractBodies(ev)
	for i, body := range bodies {
		if i > 0 {
			rawBody.WriteString("\n\n")
		}
		rawBody.WriteString(body)
	}
	fullBody := rawBody.String()

	sanitizedDesc := b.SanitizeDescription(fullBody)
	constraints := b.ParseConstraints(fullBody)
	labels := b.ExtractLabels(ev)

	metadata := map[string]any{
		"source":        ev.Source,
		"event_type":    ev.EventType,
		"repository":    ev.Repository,
		"issue_number":  ev.IssueNumber,
		"pr_number":     ev.PRNumber,
		"sender":        ev.Sender,
		"delivery_id":   ev.ID,
		"branch":        targetBranch,
		"target_branch": targetBranch,
		"base_branch":   b.cfg.BaseBranch,
	}

	task := agent.TaskContext{
		TaskID:         taskID,
		Title:          title,
		Description:    sanitizedDesc,
		Labels:         labels,
		RepositoryRoot: ev.Repository,
		Constraints:    constraints,
		Metadata:       metadata,
	}
	task.Normalize()

	return task, targetBranch
}

// SanitizeDescription strips command triggers and extra spacing from text.
func (b *AutonomousFactoryBridge) SanitizeDescription(body string) string {
	lines := strings.Split(body, "\n")
	var cleaned []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		isTrigger := false
		for _, cmd := range b.cfg.CommandTriggers {
			cmdClean := strings.ToLower(strings.TrimSpace(cmd))
			if cmdClean != "" && strings.Contains(lower, cmdClean) {
				isTrigger = true
				break
			}
		}

		if !isTrigger {
			cleaned = append(cleaned, line)
		}
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// ParseConstraints parses checklist items (- [ ] / * [ ]) and explicit constraint blocks.
func (b *AutonomousFactoryBridge) ParseConstraints(body string) []string {
	var constraints []string
	seen := make(map[string]bool)

	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" {
			return
		}
		key := strings.ToLower(c)
		if !seen[key] {
			seen[key] = true
			constraints = append(constraints, c)
		}
	}

	lines := strings.Split(body, "\n")
	inConstraintsSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Header detection
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(lower, "constraint") {
				inConstraintsSection = true
			} else {
				inConstraintsSection = false
			}
			continue
		}

		// Checklist items: - [ ] ..., * [ ] ..., - [x] ...
		if matches := checklistRegex.FindStringSubmatch(trimmed); len(matches) == 3 {
			item := strings.TrimSpace(matches[2])
			if item != "" {
				add(item)
				continue
			}
		}

		// Explicit constraint prefix: Constraint: or Constraints:
		if strings.HasPrefix(lower, "constraint:") {
			content := strings.TrimSpace(trimmed[len("constraint:"):])
			if content != "" {
				add(content)
				continue
			}
		}
		if strings.HasPrefix(lower, "constraints:") {
			content := strings.TrimSpace(trimmed[len("constraints:"):])
			if content != "" {
				add(content)
				continue
			}
			inConstraintsSection = true
			continue
		}

		// Bullets under Constraints section
		if inConstraintsSection {
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
				content := strings.TrimSpace(trimmed[2:])
				if content != "" {
					add(content)
					continue
				}
			}
		}
	}

	return constraints
}

// runAndReport executes the pipeline, emits lifecycle events, and posts issue comments.
func (b *AutonomousFactoryBridge) runAndReport(
	ctx context.Context,
	task agent.TaskContext,
	ev schema.TriggerEvent,
	targetBranch string,
) (*agent.PipelineOutcome, bool, string, error) {
	outcome, err := b.runner.RunPipeline(ctx, task)
	if err != nil {
		// Emit failure event
		if b.publisher != nil {
			_ = b.publisher.PublishTaskEvent(ctx, gateway.Event{
				Type:      gateway.TopicRunFailed,
				RunID:     task.TaskID,
				TaskID:    task.TaskID,
				Timestamp: time.Now().UnixMilli(),
				Payload: map[string]any{
					"task_id": task.TaskID,
					"error":   err.Error(),
					"branch":  targetBranch,
				},
			})
		}

		// Post error comment if reporter is available
		comment := b.FormatErrorComment(err, targetBranch, task.TaskID)
		b.postCommentIfAvailable(ctx, ev, comment)
		return nil, false, "", err
	}

	// Emit lifecycle event according to outcome decision
	if b.publisher != nil {
		var topic string
		switch {
		case outcome.IsApproved():
			topic = gateway.TopicRunCompleted
		case outcome.IsHumanGate():
			topic = gateway.TopicGateApprovalRequired
		default:
			topic = gateway.TopicRunFailed
		}

		_ = b.publisher.PublishTaskEvent(ctx, gateway.Event{
			Type:      topic,
			RunID:     task.TaskID,
			TaskID:    task.TaskID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"task_id":    task.TaskID,
				"decision":   string(outcome.Decision),
				"iterations": outcome.TotalIterations,
				"duration":   outcome.Duration.String(),
				"branch":     targetBranch,
			},
		})
	}

	// Auto create Pull Request if approved and triggered by an issue
	var prURL string
	if outcome.IsApproved() && ev.IssueNumber > 0 && ev.PRNumber == 0 && b.prCreator != nil {
		prTitle := fmt.Sprintf("feat: %s (closes #%d)", task.Title, ev.IssueNumber)
		prBody := fmt.Sprintf("Autonomous implementation for issue #%d.\n\n### Task Overview\n%s\n\n---\n*Created automatically by gv-factory.*", ev.IssueNumber, task.Description)
		if prResp, err := b.prCreator.CreatePullRequest(ctx, ev.Repository, targetBranch, b.cfg.BaseBranch, prTitle, prBody); err == nil && prResp != nil {
			prURL = prResp.HTMLURL
			if prURL == "" {
				prURL = prResp.URL
			}
		}
	}

	// Post feedback comment
	comment := b.FormatFeedbackComment(outcome, targetBranch, task.TaskID, prURL)
	posted := b.postCommentIfAvailable(ctx, ev, comment)

	return outcome, posted, prURL, nil
}

func (b *AutonomousFactoryBridge) postCommentIfAvailable(ctx context.Context, ev schema.TriggerEvent, comment string) bool {
	if b.reporter == nil || ev.Repository == "" {
		return false
	}
	targetNum := ev.IssueNumber
	if targetNum == 0 {
		targetNum = ev.PRNumber
	}
	if targetNum <= 0 {
		return false
	}

	if comp, ok := b.reporter.(*CompositeIssueReporter); ok {
		err := comp.PostIssueCommentWithSource(ctx, ev.Source, ev.Repository, targetNum, comment)
		return err == nil
	}

	err := b.reporter.PostIssueComment(ctx, ev.Repository, targetNum, comment)
	return err == nil
}

// FormatFeedbackComment renders markdown feedback according to the pipeline outcome.
func (b *AutonomousFactoryBridge) FormatFeedbackComment(outcome *agent.PipelineOutcome, branch string, taskID string, prURL ...string) string {
	if outcome == nil {
		return ""
	}

	switch {
	case outcome.IsApproved():
		return b.formatApprovedComment(outcome, branch, prURL...)
	case outcome.IsHumanGate():
		return b.formatHumanGateComment(outcome, branch, taskID, prURL...)
	default:
		return b.formatFailedComment(outcome, branch)
	}
}

func (b *AutonomousFactoryBridge) formatApprovedComment(outcome *agent.PipelineOutcome, branch string, prURL ...string) string {
	var sb strings.Builder
	sb.WriteString("### 🚀 Autonomous Factory Pipeline: Approved\n\n")
	sb.WriteString("The autonomous factory pipeline successfully implemented and verified the requested task.\n\n")
	sb.WriteString(fmt.Sprintf("- **Status:** `APPROVED`\n"))
	sb.WriteString(fmt.Sprintf("- **Total Iterations:** %d\n", outcome.TotalIterations))
	sb.WriteString(fmt.Sprintf("- **Duration:** %s\n", outcome.Duration.Round(100*time.Millisecond)))
	sb.WriteString(fmt.Sprintf("- **Target Branch:** `%s`\n", branch))

	var link string
	if len(prURL) > 0 && strings.TrimSpace(prURL[0]) != "" {
		link = strings.TrimSpace(prURL[0])
	}
	if link != "" {
		sb.WriteString(fmt.Sprintf("- **Pull Request:** [%s](%s)\n", link, link))
	}

	if outcome.BuildResult != nil && len(outcome.BuildResult.FilesModified) > 0 {
		sb.WriteString("\n#### 📝 Changes Overview\n")
		sb.WriteString(fmt.Sprintf("- **Files Modified (%d):**\n", len(outcome.BuildResult.FilesModified)))
		for _, f := range outcome.BuildResult.FilesModified {
			sb.WriteString(fmt.Sprintf("  - `%s`\n", f))
		}
	}

	if outcome.Approval != nil && outcome.Approval.FeedbackSummary != "" {
		sb.WriteString(fmt.Sprintf("\n> **Reviewer Feedback:** %s\n", outcome.Approval.FeedbackSummary))
	}

	return sb.String()
}

func (b *AutonomousFactoryBridge) formatHumanGateComment(outcome *agent.PipelineOutcome, branch string, taskID string, prURL ...string) string {
	var sb strings.Builder
	sb.WriteString("### ⚠️ Autonomous Factory Pipeline: Human Approval Required\n\n")
	sb.WriteString("The autonomous pipeline completed the task implementation, but human approval is required before merging.\n\n")
	sb.WriteString(fmt.Sprintf("- **Status:** `HUMAN_GATE`\n"))
	sb.WriteString(fmt.Sprintf("- **Total Iterations:** %d\n", outcome.TotalIterations))
	sb.WriteString(fmt.Sprintf("- **Duration:** %s\n", outcome.Duration.Round(100*time.Millisecond)))
	sb.WriteString(fmt.Sprintf("- **Target Branch:** `%s`\n", branch))
	var link string
	if len(prURL) > 0 && strings.TrimSpace(prURL[0]) != "" {
		link = strings.TrimSpace(prURL[0])
	}
	if link != "" {
		sb.WriteString(fmt.Sprintf("- **Pull Request:** [%s](%s)\n", link, link))
	}
	sb.WriteString("- **Required Action:** Manual sign-off required by an authorized reviewer.\n")

	if outcome.Approval != nil {
		sb.WriteString("\n#### 🔍 Findings & Gate Summary\n")
		if outcome.Approval.GateReason != "" {
			sb.WriteString(fmt.Sprintf("- **Gate Reason:** %s\n", outcome.Approval.GateReason))
		}
		if outcome.Approval.FeedbackSummary != "" {
			sb.WriteString(fmt.Sprintf("- **Feedback Summary:** %s\n", outcome.Approval.FeedbackSummary))
		}

		if len(outcome.Approval.ReviewerResults) > 0 {
			sb.WriteString("\n#### 📋 Reviewer Scores & Results\n")
			for _, r := range outcome.Approval.ReviewerResults {
				status := "FAILED"
				if r.Passed {
					status = "PASSED"
				}
				sb.WriteString(fmt.Sprintf("- **%s**: Status %s (Score: %.1f%%) - %s\n", r.ReviewerName, status, r.Score, r.Summary))
			}
		}
	}

	sb.WriteString("\n#### 🔐 Approval Instructions\n")
	if b.cfg.GateApprovalBaseURL != "" {
		approvalURL := fmt.Sprintf("%s?run_id=%s", strings.TrimRight(b.cfg.GateApprovalBaseURL, "/"), taskID)
		sb.WriteString(fmt.Sprintf("- **Web UI / Gate URL:** [Approve Pipeline Run](%s)\n", approvalURL))
	}
	sb.WriteString(fmt.Sprintf("- **CLI Command:**\n  ```bash\n  %s --run-id %s\n  ```\n", b.cfg.CLISuggestionCmd, taskID))

	return sb.String()
}

func (b *AutonomousFactoryBridge) formatFailedComment(outcome *agent.PipelineOutcome, branch string) string {
	var sb strings.Builder
	sb.WriteString("### ❌ Autonomous Factory Pipeline: Failed\n\n")
	sb.WriteString("The autonomous factory pipeline encountered an unrecoverable failure during execution.\n\n")
	sb.WriteString(fmt.Sprintf("- **Status:** `FAILED`\n"))
	sb.WriteString(fmt.Sprintf("- **Total Iterations:** %d\n", outcome.TotalIterations))
	sb.WriteString(fmt.Sprintf("- **Duration:** %s\n", outcome.Duration.Round(100*time.Millisecond)))
	sb.WriteString(fmt.Sprintf("- **Target Branch:** `%s`\n", branch))

	if outcome.ErrorMessage != "" {
		sb.WriteString(fmt.Sprintf("\n#### 💥 Error Details\n```\n%s\n```\n", outcome.ErrorMessage))
	}

	if outcome.BuildResult != nil && outcome.BuildResult.ErrorMessage != "" {
		sb.WriteString(fmt.Sprintf("\n#### 🧪 Failed Tests / Diagnostics\n- %s\n", outcome.BuildResult.ErrorMessage))
	}

	return sb.String()
}

// FormatErrorComment formats a markdown comment when pipeline execution fails with an error.
func (b *AutonomousFactoryBridge) FormatErrorComment(err error, branch string, taskID string) string {
	var sb strings.Builder
	sb.WriteString("### ❌ Autonomous Factory Pipeline: Execution Error\n\n")
	sb.WriteString(fmt.Sprintf("- **Task ID:** `%s`\n", taskID))
	sb.WriteString(fmt.Sprintf("- **Target Branch:** `%s`\n", branch))
	sb.WriteString(fmt.Sprintf("\n#### 💥 Error Details\n```\n%v\n```\n", err))
	return sb.String()
}
