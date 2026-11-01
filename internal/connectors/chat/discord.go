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

// DiscordConfig holds configuration options for the Discord adapter.
type DiscordConfig struct {
	BotToken         string
	PublicKey        string
	DefaultChannelID string
	BaseURL          string
	HTTPClient       *http.Client
}

// DiscordAdapter implements the ChatNotifier interface for Discord servers.
type DiscordAdapter struct {
	cfg        DiscordConfig
	httpClient *http.Client
	baseURL    string
}

// NewDiscordAdapter creates an initialized Discord adapter.
func NewDiscordAdapter(cfg DiscordConfig) *DiscordAdapter {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://discord.com/api/v10"
	}
	return &DiscordAdapter{
		cfg:        cfg,
		httpClient: client,
		baseURL:    baseURL,
	}
}

// Platform returns "discord".
func (d *DiscordAdapter) Platform() string {
	return "discord"
}

// SendNotification posts a notification embed to a Discord channel.
func (d *DiscordAdapter) SendNotification(ctx context.Context, msg NotificationMessage) (*MessageRef, error) {
	channelID := msg.Channel
	if channelID == "" {
		channelID = d.cfg.DefaultChannelID
	}
	if channelID == "" {
		return nil, ErrChannelRequired
	}

	color := 0x3B82F6 // Blue info
	switch msg.Level {
	case "success":
		color = 0x10B981 // Green
	case "warning":
		color = 0xF59E0B // Amber
	case "error":
		color = 0xEF4444 // Red
	}

	embed := map[string]any{
		"title":       msg.Title,
		"description": msg.Body,
		"color":       color,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	payload := map[string]any{
		"embeds": []any{embed},
	}
	if msg.ThreadTS != "" {
		payload["message_reference"] = map[string]any{
			"message_id": msg.ThreadTS,
		}
	}

	return d.postMessage(ctx, channelID, payload)
}

// PromptApproval posts an interactive Decision Gate card with embeds and button components.
func (d *DiscordAdapter) PromptApproval(ctx context.Context, req ApprovalPromptRequest) (*MessageRef, error) {
	channelID := req.Channel
	if channelID == "" {
		channelID = d.cfg.DefaultChannelID
	}
	if channelID == "" {
		return nil, ErrChannelRequired
	}

	embed := d.buildApprovalEmbed(req)
	components := d.buildApprovalComponents(req)

	payload := map[string]any{
		"embeds":     []any{embed},
		"components": components,
	}
	if req.ThreadTS != "" {
		payload["message_reference"] = map[string]any{
			"message_id": req.ThreadTS,
		}
	}

	return d.postMessage(ctx, channelID, payload)
}

// UpdateStatus posts a step-by-step progress update inside a dedicated thread or channel.
func (d *DiscordAdapter) UpdateStatus(ctx context.Context, update StatusUpdate) (*MessageRef, error) {
	channelID := update.Channel
	if channelID == "" {
		channelID = d.cfg.DefaultChannelID
	}
	if channelID == "" {
		return nil, ErrChannelRequired
	}

	color := 0x6B7280 // Gray pending
	badge := "⏳"
	switch update.Status {
	case "running":
		color = 0x3B82F6 // Blue
		badge = "🔄"
	case "completed":
		color = 0x10B981 // Green
		badge = "✅"
	case "failed":
		color = 0xEF4444 // Red
		badge = "❌"
	}

	embed := map[string]any{
		"title":       fmt.Sprintf("%s Step [%s]: %s", badge, update.StepName, strings.ToUpper(update.Status)),
		"description": update.Details,
		"color":       color,
		"footer": map[string]any{
			"text": fmt.Sprintf("Run ID: %s", update.RunID),
		},
		"timestamp": update.Timestamp.Format(time.RFC3339),
	}

	payload := map[string]any{
		"embeds": []any{embed},
	}
	if update.ThreadTS != "" {
		payload["message_reference"] = map[string]any{
			"message_id": update.ThreadTS,
		}
	}

	return d.postMessage(ctx, channelID, payload)
}

// UpdateMessageWithDecision replaces interactive button components with a resolved banner.
func (d *DiscordAdapter) UpdateMessageWithDecision(ctx context.Context, channelID, messageID string, res GateResolution) error {
	color := 0x10B981 // Green
	statusText := "APPROVED"
	emoji := "✅"
	if res.Decision == "REJECT" {
		color = 0xEF4444 // Red
		statusText = "REJECTED"
		emoji = "❌"
	}

	embed := map[string]any{
		"title":       fmt.Sprintf("%s Decision Gate Resolved: %s", emoji, statusText),
		"description": fmt.Sprintf("Decision submitted by **%s** for Run `%s`.", res.User, res.RunID),
		"color":       color,
		"fields": []map[string]any{
			{"name": "Decision", "value": statusText, "inline": true},
			{"name": "Resolved By", "value": res.User, "inline": true},
			{"name": "Timestamp", "value": res.Timestamp.Format(time.RFC3339), "inline": true},
		},
	}

	payload := map[string]any{
		"embeds":     []any{embed},
		"components": []any{}, // Remove action buttons
	}

	return d.updateMessage(ctx, channelID, messageID, payload)
}

func (d *DiscordAdapter) buildApprovalEmbed(req ApprovalPromptRequest) map[string]any {
	fields := []map[string]any{
		{"name": "Run ID", "value": fmt.Sprintf("`%s`", req.RunID), "inline": true},
		{"name": "Coverage", "value": fmt.Sprintf("`%.1f%%`", req.CoveragePercent), "inline": true},
		{"name": "Security Alerts", "value": fmt.Sprintf("Critical: `%d` | High: `%d`", req.CriticalAlerts, req.HighAlerts), "inline": true},
	}

	if req.CommitSHA != "" {
		fields = append(fields, map[string]any{
			"name":   "Commit SHA",
			"value":  fmt.Sprintf("`%s`", req.CommitSHA),
			"inline": true,
		})
	}

	fields = append(fields, map[string]any{
		"name":   "Expires At",
		"value":  req.ExpiresAt.Format(time.RFC3339),
		"inline": true,
	})

	var risks []string
	if req.HasMigrationChanges {
		risks = append(risks, "DB Migrations")
	}
	if req.HasAuthChanges {
		risks = append(risks, "Auth logic")
	}
	if req.HasSchemaChanges {
		risks = append(risks, "Schema changes")
	}
	if len(risks) > 0 {
		fields = append(fields, map[string]any{
			"name":   "⚠️ High Risk Paths",
			"value":  strings.Join(risks, ", "),
			"inline": false,
		})
	}

	if req.Summary != "" {
		fields = append(fields, map[string]any{
			"name":   "Diagnostics Summary",
			"value":  req.Summary,
			"inline": false,
		})
	}

	return map[string]any{
		"title":       "🛑 Decision Gate: Human Approval Required",
		"description": "A workflow execution has reached a decision gate requiring manual review before merging.",
		"color":       0xF59E0B, // Amber
		"fields":      fields,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
}

func (d *DiscordAdapter) buildApprovalComponents(req ApprovalPromptRequest) []any {
	// Action Row = type 1
	// Button = type 2
	// Button styles: 3 = Success (green), 4 = Danger (red), 5 = Link
	buttons := []map[string]any{
		{
			"type":      2,
			"style":     3,
			"label":     "Approve ✅",
			"custom_id": fmt.Sprintf("gate_approve:%s", req.Token),
		},
		{
			"type":      2,
			"style":     4,
			"label":     "Reject ❌",
			"custom_id": fmt.Sprintf("gate_reject:%s", req.Token),
		},
	}

	if req.DiffURL != "" {
		buttons = append(buttons, map[string]any{
			"type":  2,
			"style": 5,
			"label": "View Diff 🔍",
			"url":   req.DiffURL,
		})
	}

	return []any{
		map[string]any{
			"type":       1,
			"components": buttons,
		},
	}
}

func (d *DiscordAdapter) postMessage(ctx context.Context, channelID string, payload map[string]any) (*MessageRef, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal discord payload: %w", err)
	}

	url := fmt.Sprintf("%s/channels/%s/messages", d.baseURL, channelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bot "+d.cfg.BotToken)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord post message failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord api error (status %d): %s", resp.StatusCode, string(body))
	}

	var discordResp struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(body, &discordResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal discord response: %w", err)
	}

	return &MessageRef{
		Platform:  "discord",
		Channel:   channelID,
		MessageID: discordResp.ID,
		Timestamp: time.Now(),
	}, nil
}

func (d *DiscordAdapter) updateMessage(ctx context.Context, channelID, messageID string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal discord patch payload: %w", err)
	}

	url := fmt.Sprintf("%s/channels/%s/messages/%s", d.baseURL, channelID, messageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bot "+d.cfg.BotToken)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord update message failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord patch message error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

