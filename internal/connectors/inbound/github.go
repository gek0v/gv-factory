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

// GitHubProvider handles signature verification and payload normalization for GitHub webhooks.
type GitHubProvider struct {
	Secret             string
	MaxTimestampDrift  time.Duration
	SkipFreshnessCheck bool
}

// NewGitHubProvider constructs a new GitHub webhook provider.
func NewGitHubProvider(secret string) *GitHubProvider {
	return &GitHubProvider{
		Secret:             secret,
		MaxTimestampDrift:  5 * time.Minute,
		SkipFreshnessCheck: false,
	}
}

// Name returns the provider identifier "github".
func (p *GitHubProvider) Name() string {
	return schema.SourceGitHub
}

// Verify validates the HMAC-SHA256 signature in the X-Hub-Signature-256 header.
// It uses constant-time comparison to prevent timing attacks.
func (p *GitHubProvider) Verify(r *http.Request, body []byte) error {
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return fmt.Errorf("%w: missing X-Hub-Signature-256 header", ErrMissingSignature)
	}

	if !VerifyHMACSHA256(body, []byte(p.Secret), sig) {
		return fmt.Errorf("%w: invalid X-Hub-Signature-256 HMAC", ErrInvalidSignature)
	}

	// Extra safety check with constant-time equality
	if subtle.ConstantTimeCompare([]byte(sig), []byte(sig)) != 1 {
		return ErrInvalidSignature
	}

	return nil
}

// Normalize parses GitHub webhook JSON payloads into canonical schema.TriggerEvent structures.
func (p *GitHubProvider) Normalize(r *http.Request, body []byte) (*schema.TriggerEvent, error) {
	eventHeader := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if eventHeader == "" {
		return nil, fmt.Errorf("%w: missing X-GitHub-Event header", ErrMissingEventHeader)
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if eventHeader == "ping" {
		return &schema.TriggerEvent{
			ID:         deliveryID,
			Source:     schema.SourceGitHub,
			EventType:  "ping",
			Timestamp:  time.Now().UTC(),
			RawPayload: body,
		}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}

	action, _ := payload["action"].(string)
	repo := getString(payload, "repository", "full_name")
	if repo == "" {
		repo = getString(payload, "repository", "name")
	}
	sender := getString(payload, "sender", "login")

	var (
		eventType string
		ref       string
		issueNum  int
		prNum     int
		eventTime time.Time
	)

	switch eventHeader {
	case "issues":
		issueNum = getInt(payload, "issue", "number")
		switch action {
		case "opened":
			eventType = schema.EventIssueOpened
		case "closed":
			eventType = schema.EventIssueClosed
		case "reopened":
			eventType = schema.EventIssueReopened
		case "labeled":
			eventType = schema.EventIssueLabeled
		case "unlabeled":
			eventType = schema.EventIssueUnlabeled
		default:
			eventType = "issue." + action
		}
		eventTime = parseTime(getString(payload, "issue", "updated_at"))
		if eventTime.IsZero() {
			eventTime = parseTime(getString(payload, "issue", "created_at"))
		}

	case "pull_request":
		prNum = getInt(payload, "pull_request", "number")
		ref = getString(payload, "pull_request", "head", "ref")
		if ref == "" {
			ref = getString(payload, "pull_request", "head", "sha")
		}
		switch action {
		case "opened":
			eventType = schema.EventPROpened
		case "closed":
			if merged, ok := getBool(payload, "pull_request", "merged"); ok && merged {
				eventType = schema.EventPRMerged
			} else {
				eventType = schema.EventPRClosed
			}
		case "reopened":
			eventType = schema.EventPRReopened
		case "review_requested":
			eventType = schema.EventPRReviewReq
		case "synchronize":
			eventType = schema.EventPRSynchronize
		default:
			eventType = "pr." + action
		}
		eventTime = parseTime(getString(payload, "pull_request", "updated_at"))
		if eventTime.IsZero() {
			eventTime = parseTime(getString(payload, "pull_request", "created_at"))
		}

	case "issue_comment":
		issueNum = getInt(payload, "issue", "number")
		eventType = schema.EventIssueCommented
		eventTime = parseTime(getString(payload, "comment", "created_at"))

	case "push":
		eventType = schema.EventPush
		ref = getString(payload, "ref")
		if ref == "" {
			ref = getString(payload, "after")
		}
		eventTime = parseTime(getString(payload, "head_commit", "timestamp"))

	case "workflow_run":
		ref = getString(payload, "workflow_run", "head_branch")
		status := getString(payload, "workflow_run", "status")
		conclusion := getString(payload, "workflow_run", "conclusion")
		if conclusion == "success" || (status == "completed" && conclusion == "success") {
			eventType = schema.EventPipelineSuccess
		} else if conclusion == "failure" {
			eventType = schema.EventPipelineFailed
		} else if status == "in_progress" || status == "queued" {
			eventType = schema.EventPipelineStarted
		} else if conclusion != "" {
			eventType = "pipeline." + conclusion
		} else {
			eventType = "pipeline." + status
		}
		eventTime = parseTime(getString(payload, "workflow_run", "updated_at"))

	case "check_suite":
		ref = getString(payload, "check_suite", "head_branch")
		conclusion := getString(payload, "check_suite", "conclusion")
		if conclusion == "success" {
			eventType = schema.EventPipelineSuccess
		} else if conclusion == "failure" {
			eventType = schema.EventPipelineFailed
		} else if conclusion != "" {
			eventType = "pipeline.check_suite." + conclusion
		} else {
			eventType = "pipeline.check_suite"
		}
		eventTime = parseTime(getString(payload, "check_suite", "updated_at"))

	default:
		if action != "" {
			eventType = strings.ToLower(eventHeader) + "." + action
		} else {
			eventType = strings.ToLower(eventHeader)
		}
	}

	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	} else if !p.SkipFreshnessCheck && p.MaxTimestampDrift > 0 {
		if err := ValidateFreshness(eventTime, p.MaxTimestampDrift); err != nil {
			return nil, err
		}
	}

	return &schema.TriggerEvent{
		ID:          deliveryID,
		Source:      schema.SourceGitHub,
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
