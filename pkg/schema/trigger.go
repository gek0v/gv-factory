// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package schema

import "time"

// Supported trigger provider sources.
const (
	SourceGitHub = "github"
	SourceGitLab = "gitlab"
	SourceSentry = "sentry"
)

// Canonical trigger event types normalized across providers.
const (
	// Issue lifecycle events
	EventIssueOpened    = "issue.opened"
	EventIssueClosed    = "issue.closed"
	EventIssueReopened  = "issue.reopened"
	EventIssueLabeled   = "issue.labeled"
	EventIssueUnlabeled = "issue.unlabeled"
	EventIssueCommented = "issue.commented"
	EventIssueUpdated   = "issue.updated"

	// Pull Request / Merge Request lifecycle events
	EventPROpened      = "pr.opened"
	EventPRClosed      = "pr.closed"
	EventPRMerged      = "pr.merged"
	EventPRReopened    = "pr.reopened"
	EventPRReviewReq   = "pr.review_requested"
	EventPRSynchronize = "pr.synchronize"
	EventPRApproved    = "pr.approved"
	EventPRCommented   = "pr.commented"
	EventPRUpdated     = "pr.updated"

	// Repository and commit events
	EventPush         = "push"
	EventBranchCreate = "branch.created"
	EventBranchDelete = "branch.deleted"

	// Pipeline / CI / Workflow events
	EventPipelineStarted  = "pipeline.started"
	EventPipelineSuccess  = "pipeline.success"
	EventPipelineFailed   = "pipeline.failed"
	EventPipelineCanceled = "pipeline.canceled"

	// Observability and Alert events
	EventAlertTriggered = "alert.triggered"
	EventAlertResolved  = "alert.resolved"
	EventErrorCreated   = "error.created"
)

// TriggerEvent represents a canonical normalized event received from external webhook providers.
type TriggerEvent struct {
	ID          string         `json:"id,omitempty" yaml:"id,omitempty"`
	Source      string         `json:"source" yaml:"source"`
	EventType   string         `json:"event_type" yaml:"event_type"`
	Repository  string         `json:"repository,omitempty" yaml:"repository,omitempty"`
	Reference   string         `json:"reference,omitempty" yaml:"reference,omitempty"`
	IssueNumber int            `json:"issue_number,omitempty" yaml:"issue_number,omitempty"`
	PRNumber    int            `json:"pr_number,omitempty" yaml:"pr_number,omitempty"`
	Sender      string         `json:"sender,omitempty" yaml:"sender,omitempty"`
	Payload     map[string]any `json:"payload,omitempty" yaml:"payload,omitempty"`
	RawPayload  []byte         `json:"raw_payload,omitempty" yaml:"raw_payload,omitempty"`
	Timestamp   time.Time      `json:"timestamp" yaml:"timestamp"`
}

// UnixMilli returns the event timestamp as milliseconds since the Unix epoch.
func (t TriggerEvent) UnixMilli() int64 {
	return t.Timestamp.UnixMilli()
}
