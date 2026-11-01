// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SlackConfig holds configuration parameters for the Slack adapter.
type SlackConfig struct {
	BotToken          string
	SigningSecret     string
	DefaultChannel    string
	BaseURL           string
	HTTPClient        *http.Client
	MaxTimestampDrift time.Duration
}

// SlackAdapter implements the ChatNotifier interface for Slack workspaces.
type SlackAdapter struct {
	cfg        SlackConfig
	httpClient *http.Client
	baseURL    string
}

// NewSlackAdapter creates a ready-to-use Slack adapter.
func NewSlackAdapter(cfg SlackConfig) *SlackAdapter {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://slack.com/api"
	}
	if cfg.MaxTimestampDrift <= 0 {
		cfg.MaxTimestampDrift = DefaultSlackMaxDrift
	}
	return &SlackAdapter{
		cfg:        cfg,
		httpClient: client,
		baseURL:    baseURL,
	}
}

// Platform returns "slack".
func (s *SlackAdapter) Platform() string {
	return "slack"
}

// SendNotification posts a structured message or plain text to a Slack channel or thread.
func (s *SlackAdapter) SendNotification(ctx context.Context, msg NotificationMessage) (*MessageRef, error) {
	channel := msg.Channel
	if channel == "" {
		channel = s.cfg.DefaultChannel
	}
	if channel == "" {
		return nil, ErrChannelRequired
	}

	emoji := "ℹ️"
	switch msg.Level {
	case "success":
		emoji = "✅"
	case "warning":
		emoji = "⚠️"
	case "error":
		emoji = "🚨"
	}

	text := fmt.Sprintf("%s *%s*\n%s", emoji, msg.Title, msg.Body)
	payload := map[string]any{
		"channel": channel,
		"text":    text,
	}
	if msg.ThreadTS != "" {
		payload["thread_ts"] = msg.ThreadTS
	}

	return s.postMessage(ctx, payload, channel, msg.ThreadTS)
}

// PromptApproval formats and posts an interactive Block Kit decision card.
func (s *SlackAdapter) PromptApproval(ctx context.Context, req ApprovalPromptRequest) (*MessageRef, error) {
	channel := req.Channel
	if channel == "" {
		channel = s.cfg.DefaultChannel
	}
	if channel == "" {
		return nil, ErrChannelRequired
	}

	blocks := s.buildApprovalBlocks(req)
	payload := map[string]any{
		"channel": channel,
		"text":    fmt.Sprintf("🛑 Decision Gate Required for Run %s", req.RunID),
		"blocks":  blocks,
	}
	if req.ThreadTS != "" {
		payload["thread_ts"] = req.ThreadTS
	}

	return s.postMessage(ctx, payload, channel, req.ThreadTS)
}

// UpdateStatus posts an execution step update inside a dedicated thread.
func (s *SlackAdapter) UpdateStatus(ctx context.Context, update StatusUpdate) (*MessageRef, error) {
	channel := update.Channel
	if channel == "" {
		channel = s.cfg.DefaultChannel
	}
	if channel == "" {
		return nil, ErrChannelRequired
	}

	badge := "⏳"
	switch update.Status {
	case "running":
		badge = "🔄"
	case "completed":
		badge = "✅"
	case "failed":
		badge = "❌"
	}

	text := fmt.Sprintf("%s *Step [%s]*: `%s`", badge, update.StepName, strings.ToUpper(update.Status))
	if update.Details != "" {
		text += fmt.Sprintf("\n> %s", update.Details)
	}

	payload := map[string]any{
		"channel": channel,
		"text":    text,
	}
	if update.ThreadTS != "" {
		payload["thread_ts"] = update.ThreadTS
	}

	return s.postMessage(ctx, payload, channel, update.ThreadTS)
}

// UpdateMessageWithDecision replaces interactive buttons with a resolved status banner.
func (s *SlackAdapter) UpdateMessageWithDecision(ctx context.Context, channel, messageTS string, res GateResolution) error {
	statusEmoji := "✅"
	statusText := "APPROVED"
	if res.Decision == "REJECT" {
		statusEmoji = "❌"
		statusText = "REJECTED"
	}

	banner := fmt.Sprintf("%s *Decision Gate %s* by <@%s>", statusEmoji, statusText, res.User)
	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]any{
				"type": "plain_text",
				"text": fmt.Sprintf("Decision Gate Resolved: %s", statusText),
			},
		},
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf("%s\n*Run ID:* `%s` | *Resolved At:* %s", banner, res.RunID, res.Timestamp.Format(time.RFC3339)),
			},
		},
	}

	payload := map[string]any{
		"channel": channel,
		"ts":      messageTS,
		"text":    banner,
		"blocks":  blocks,
	}

	return s.updateMessage(ctx, payload)
}

func (s *SlackAdapter) buildApprovalBlocks(req ApprovalPromptRequest) []map[string]any {
	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]any{
				"type":  "plain_text",
				"text":  "🛑 Decision Gate: Human Approval Required",
				"emoji": true,
			},
		},
		{
			"type": "section",
			"fields": []map[string]any{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Run ID:*\n`%s`", req.RunID),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Coverage:*\n`%.1f%%`", req.CoveragePercent),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Security Alerts:*\nCritical: `%d` | High: `%d`", req.CriticalAlerts, req.HighAlerts),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Expires At:*\n`%s`", req.ExpiresAt.Format(time.Kitchen)),
				},
			},
		},
	}

	if req.CommitSHA != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Commit SHA:* `%s`", req.CommitSHA),
			},
		})
	}

	if req.Summary != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Reviewer Diagnostics:*\n%s", req.Summary),
			},
		})
	}

	var risks []string
	if req.HasMigrationChanges {
		risks = append(risks, "Database Migration changes")
	}
	if req.HasAuthChanges {
		risks = append(risks, "Authentication logic changes")
	}
	if req.HasSchemaChanges {
		risks = append(risks, "Schema changes")
	}
	if len(risks) > 0 {
		blocks = append(blocks, map[string]any{
			"type": "context",
			"elements": []map[string]any{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("⚠️ *High-Risk Invariants Detected:* %s", strings.Join(risks, ", ")),
				},
			},
		})
	}

	actionsElements := []map[string]any{
		{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": "Approve ✅"},
			"style":     "primary",
			"action_id": "gate_approve",
			"value":     req.Token,
		},
		{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": "Reject ❌"},
			"style":     "danger",
			"action_id": "gate_reject",
			"value":     req.Token,
		},
	}

	if req.DiffURL != "" {
		actionsElements = append(actionsElements, map[string]any{
			"type":      "button",
			"text":      map[string]any{"type": "plain_text", "text": "View Diff 🔍"},
			"url":       req.DiffURL,
			"action_id": "gate_diff",
		})
	}

	blocks = append(blocks, map[string]any{
		"type":     "actions",
		"elements": actionsElements,
	})

	return blocks
}

func (s *SlackAdapter) postMessage(ctx context.Context, payload map[string]any, channel, threadTS string) (*MessageRef, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal slack payload: %w", err)
	}

	url := s.baseURL + "/chat.postMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if s.cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.BotToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack postMessage failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		TS    string `json:"ts"`
	}
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal slack response: %w", err)
	}
	if !slackResp.OK {
		return nil, fmt.Errorf("slack api error: %s", slackResp.Error)
	}

	return &MessageRef{
		Platform:  "slack",
		Channel:   channel,
		MessageID: slackResp.TS,
		ThreadID:  threadTS,
		Timestamp: time.Now(),
	}, nil
}

func (s *SlackAdapter) updateMessage(ctx context.Context, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack update payload: %w", err)
	}

	url := s.baseURL + "/chat.update"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if s.cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.BotToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack chat.update failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return fmt.Errorf("failed to unmarshal slack update response: %w", err)
	}
	if !slackResp.OK {
		return fmt.Errorf("slack api error: %s", slackResp.Error)
	}

	return nil
}

