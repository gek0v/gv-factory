// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// GitLabProvider handles secret verification and payload normalization for GitLab webhooks.
type GitLabProvider struct {
	Secret             string
	MaxTimestampDrift  time.Duration
	SkipFreshnessCheck bool
}

// NewGitLabProvider constructs a new GitLab webhook provider.
func NewGitLabProvider(secret string) *GitLabProvider {
	return &GitLabProvider{
		Secret:             secret,
		MaxTimestampDrift:  5 * time.Minute,
		SkipFreshnessCheck: false,
	}
}

// Name returns the provider identifier "gitlab".
func (p *GitLabProvider) Name() string {
	return schema.SourceGitLab
}

// Verify validates the X-Gitlab-Token header using constant-time comparison to guard against timing side-channels.
func (p *GitLabProvider) Verify(r *http.Request, body []byte) error {
	token := r.Header.Get("X-Gitlab-Token")
	if token == "" {
		return fmt.Errorf("%w: missing X-Gitlab-Token header", ErrMissingSignature)
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(p.Secret)) != 1 {
		return fmt.Errorf("%w: invalid X-Gitlab-Token", ErrInvalidSignature)
	}

	return nil
}

// Normalize parses GitLab webhook JSON payloads into canonical schema.TriggerEvent structures.
func (p *GitLabProvider) Normalize(r *http.Request, body []byte) (*schema.TriggerEvent, error) {
	eventHeader := strings.TrimSpace(r.Header.Get("X-Gitlab-Event"))
	if eventHeader == "" {
		return nil, fmt.Errorf("%w: missing X-Gitlab-Event header", ErrMissingEventHeader)
	}

	eventUUID := r.Header.Get("X-Gitlab-Event-UUID")

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}

	objectKind := getString(payload, "object_kind")
	if objectKind == "" {
		objectKind = strings.ToLower(eventHeader)
	}

	repo := getString(payload, "project", "path_with_namespace")
	if repo == "" {
		repo = getString(payload, "project", "name")
	}

	sender := getString(payload, "user", "username")
	if sender == "" {
		sender = getString(payload, "user_username")
	}

	var (
		eventType string
		ref       string
		issueNum  int
		prNum     int
		eventTime time.Time
	)

	switch objectKind {
	case "issue", "Issue Hook":
		action := getString(payload, "object_attributes", "action")
		issueNum = getInt(payload, "object_attributes", "iid")
		switch action {
		case "open", "opened":
			eventType = schema.EventIssueOpened
		case "close", "closed":
			eventType = schema.EventIssueClosed
		case "reopen", "reopened":
			eventType = schema.EventIssueReopened
		case "update":
			eventType = schema.EventIssueUpdated
		default:
			eventType = "issue." + action
		}
		eventTime = parseTime(getString(payload, "object_attributes", "updated_at"))
		if eventTime.IsZero() {
			eventTime = parseTime(getString(payload, "object_attributes", "created_at"))
		}

	case "merge_request", "Merge Request Hook":
		action := getString(payload, "object_attributes", "action")
		prNum = getInt(payload, "object_attributes", "iid")
		ref = getString(payload, "object_attributes", "source_branch")
		switch action {
		case "open", "opened":
			eventType = schema.EventPROpened
		case "close", "closed":
			eventType = schema.EventPRClosed
		case "merge", "merged":
			eventType = schema.EventPRMerged
		case "reopen", "reopened":
			eventType = schema.EventPRReopened
		case "approved", "approval":
			eventType = schema.EventPRApproved
		case "update":
			eventType = schema.EventPRUpdated
		default:
			eventType = "pr." + action
		}
		eventTime = parseTime(getString(payload, "object_attributes", "updated_at"))
		if eventTime.IsZero() {
			eventTime = parseTime(getString(payload, "object_attributes", "created_at"))
		}

	case "pipeline", "Pipeline Hook":
		status := getString(payload, "object_attributes", "status")
		ref = getString(payload, "object_attributes", "ref")
		switch status {
		case "success":
			eventType = schema.EventPipelineSuccess
		case "failed":
			eventType = schema.EventPipelineFailed
		case "running", "pending":
			eventType = schema.EventPipelineStarted
		case "canceled", "cancelled":
			eventType = schema.EventPipelineCanceled
		default:
			eventType = "pipeline." + status
		}
		eventTime = parseTime(getString(payload, "object_attributes", "finished_at"))
		if eventTime.IsZero() {
			eventTime = parseTime(getString(payload, "object_attributes", "created_at"))
		}

	case "push", "Push Hook":
		eventType = schema.EventPush
		ref = getString(payload, "ref")
		eventTime = parseTime(getString(payload, "commits", "0", "timestamp"))

	case "note", "Note Hook":
		noteableType := getString(payload, "object_attributes", "noteable_type")
		if noteableType == "MergeRequest" {
			eventType = schema.EventPRCommented
			prNum = getInt(payload, "merge_request", "iid")
		} else {
			eventType = schema.EventIssueCommented
			issueNum = getInt(payload, "issue", "iid")
		}
		eventTime = parseTime(getString(payload, "object_attributes", "created_at"))

	default:
		eventType = strings.ToLower(strings.ReplaceAll(objectKind, " ", "."))
	}

	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	} else if !p.SkipFreshnessCheck && p.MaxTimestampDrift > 0 {
		if err := ValidateFreshness(eventTime, p.MaxTimestampDrift); err != nil {
			return nil, err
		}
	}

	return &schema.TriggerEvent{
		ID:          eventUUID,
		Source:      schema.SourceGitLab,
		EventType:   eventType,
		Repository:  repo,
		Reference:   ref,
		IssueNumber: issueNum,
		PRNumber:    prNum,
		Sender:      sender,
		Payload:     payload,
		RawPayload:  body,
		Timestamp:   eventTime,
	}, nil
}
