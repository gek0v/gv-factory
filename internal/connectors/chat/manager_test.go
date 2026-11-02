// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

type mockNotifier struct {
	platform        string
	notifications   []NotificationMessage
	approvalPrompts []ApprovalPromptRequest
	statusUpdates   []StatusUpdate
	mu              sync.Mutex
}

func (m *mockNotifier) Platform() string {
	return m.platform
}

func (m *mockNotifier) SendNotification(ctx context.Context, msg NotificationMessage) (*MessageRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = append(m.notifications, msg)
	return &MessageRef{Platform: m.platform, Channel: msg.Channel, MessageID: "msg-123"}, nil
}

func (m *mockNotifier) PromptApproval(ctx context.Context, req ApprovalPromptRequest) (*MessageRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalPrompts = append(m.approvalPrompts, req)
	return &MessageRef{Platform: m.platform, Channel: req.Channel, MessageID: "msg-prompt-123"}, nil
}

func (m *mockNotifier) UpdateStatus(ctx context.Context, update StatusUpdate) (*MessageRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates = append(m.statusUpdates, update)
	return &MessageRef{Platform: m.platform, Channel: update.Channel, MessageID: "msg-status-123"}, nil
}

func TestChatManager(t *testing.T) {
	mgr := NewManager()

	slackMock := &mockNotifier{platform: "slack"}
	discordMock := &mockNotifier{platform: "discord"}

	mgr.Register(slackMock)
	mgr.Register(discordMock)

	ctx := context.Background()

	t.Run("Get and Platforms", func(t *testing.T) {
		platforms := mgr.Platforms()
		if len(platforms) != 2 {
			t.Errorf("expected 2 platforms, got %d", len(platforms))
		}
		_, ok := mgr.Get("slack")
		if !ok {
			t.Error("expected slack notifier to be found")
		}
	})

	t.Run("BroadcastNotification across all notifiers", func(t *testing.T) {
		refs, err := mgr.SendNotification(ctx, "", NotificationMessage{
			Title: "Broadcast",
			Body:  "Hello all platforms",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(refs) != 2 {
			t.Errorf("expected 2 refs from broadcast, got %d", len(refs))
		}
		if len(slackMock.notifications) != 1 || len(discordMock.notifications) != 1 {
			t.Errorf("expected 1 notification in each mock")
		}
	})

	t.Run("Targeted PromptApproval to single platform", func(t *testing.T) {
		// Targeted notification
		targetRefs, err := mgr.SendNotification(ctx, "slack", NotificationMessage{
			Title: "Targeted Slack",
			Body:  "To Slack only",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(targetRefs) != 1 {
			t.Errorf("expected 1 ref, got %d", len(targetRefs))
		}

		refs, err := mgr.PromptApproval(ctx, "slack", ApprovalPromptRequest{
			RunID: "run-target-1",
			Token: "token-slack-only",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(refs) != 1 {
			t.Errorf("expected 1 ref, got %d", len(refs))
		}
		if len(slackMock.approvalPrompts) != 1 {
			t.Errorf("expected 1 prompt in slack mock")
		}
		if len(discordMock.approvalPrompts) != 0 {
			t.Errorf("expected 0 prompt in discord mock")
		}
	})

	t.Run("AttachToGateway automatically prompts on gate.approval_required", func(t *testing.T) {
		gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
		defer gw.Close()

		unsub, err := mgr.AttachToGateway(ctx, gw, "default-channel")
		if err != nil {
			t.Fatalf("failed to attach to gateway: %v", err)
		}
		defer unsub()

		err = gw.PublishTaskEvent(ctx, gateway.Event{
			Type:  gateway.TopicGateApprovalRequired,
			RunID: "run-gw-auto-prompt",
			Payload: map[string]any{
				"token":               "auto-token-123",
				"diagnostics_summary": "High risk migration change",
			},
			Timestamp: time.Now().Unix(),
		})
		if err != nil {
			t.Fatalf("failed to publish gate event: %v", err)
		}

		time.Sleep(50 * time.Millisecond)

		slackMock.mu.Lock()
		defer slackMock.mu.Unlock()
		if len(slackMock.approvalPrompts) < 2 {
			t.Errorf("expected slack mock to have received gateway gate prompt, total: %d", len(slackMock.approvalPrompts))
		}
	})

	t.Run("UpdateStatus targeted and broadcast", func(t *testing.T) {
		// Targeted
		refs, err := mgr.UpdateStatus(ctx, "slack", StatusUpdate{
			RunID:    "run-status-targeted",
			StepName: "step-1",
			Status:   "running",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(refs) != 1 {
			t.Errorf("expected 1 ref, got %d", len(refs))
		}

		// Broadcast
		refs, err = mgr.UpdateStatus(ctx, "", StatusUpdate{
			RunID:    "run-status-broadcast",
			StepName: "step-2",
			Status:   "completed",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(refs) != 2 {
			t.Errorf("expected 2 refs, got %d", len(refs))
		}
	})

	t.Run("Manager error handling and nil registrations", func(t *testing.T) {
		mgr.Register(nil) // Should no-op safely

		// Nonexistent platform error
		_, err := mgr.SendNotification(ctx, "nonexistent", NotificationMessage{Title: "hi"})
		if err == nil {
			t.Error("expected error for nonexistent platform in SendNotification")
		}
		_, err = mgr.PromptApproval(ctx, "nonexistent", ApprovalPromptRequest{RunID: "r"})
		if err == nil {
			t.Error("expected error for nonexistent platform in PromptApproval")
		}
		_, err = mgr.UpdateStatus(ctx, "nonexistent", StatusUpdate{RunID: "r"})
		if err == nil {
			t.Error("expected error for nonexistent platform in UpdateStatus")
		}

		// Nil gateway
		_, err = mgr.AttachToGateway(ctx, nil, "chan")
		if err == nil {
			t.Error("expected error for nil gateway in AttachToGateway")
		}
	})

	t.Run("parseApprovalEvent rich payload variations", func(t *testing.T) {
		now := time.Now()
		ev := gateway.Event{
			Type:  gateway.TopicGateApprovalRequired,
			RunID: "",
			Payload: map[string]any{
				"run_id":                "run-payload-id",
				"token":                 "tok-123",
				"channel":               "custom-chan",
				"task_name":             "Deploy Worker",
				"gate_reason":           "Threshold check failed",
				"diagnostics_summary":   "Diagnostic summary text",
				"canvas_url":            "https://canvas.local/diff",
				"commit_sha":            "sha123456",
				"findings_count":        float64(5),
				"critical_alerts":       float64(2),
				"high_alerts":           int64(3),
				"coverage_percent":      int(92),
				"expires_at":            float64(now.Unix()),
				"reviewer_scores":       []any{"Score: A", "Score: B"},
				"has_migration_changes": true,
				"has_auth_changes":      true,
				"has_schema_changes":    true,
				"platform":              "discord",
			},
		}

		req, platform := parseApprovalEvent(ev, "fallback-chan")
		if platform != "discord" {
			t.Errorf("expected platform discord, got %s", platform)
		}
		if req.RunID != "run-payload-id" {
			t.Errorf("expected runID run-payload-id, got %s", req.RunID)
		}
		if req.Channel != "custom-chan" {
			t.Errorf("expected channel custom-chan, got %s", req.Channel)
		}
		if req.TaskTitle != "Deploy Worker" {
			t.Errorf("expected task title Deploy Worker, got %s", req.TaskTitle)
		}
		if req.GateReason != "Threshold check failed" {
			t.Errorf("expected gate reason, got %s", req.GateReason)
		}
		if req.DiffURL != "https://canvas.local/diff" {
			t.Errorf("expected canvas_url mapped to DiffURL, got %s", req.DiffURL)
		}
		if req.FindingsCount != 5 || req.CriticalAlerts != 2 || req.HighAlerts != 3 {
			t.Errorf("unexpected counts: %d, %d, %d", req.FindingsCount, req.CriticalAlerts, req.HighAlerts)
		}
		if req.CoveragePercent != 92.0 {
			t.Errorf("expected coverage 92.0, got %f", req.CoveragePercent)
		}
		if len(req.ReviewerScores) != 2 {
			t.Errorf("expected 2 reviewer scores, got %d", len(req.ReviewerScores))
		}

		// Test tool_name fallback and url fallback
		ev2 := gateway.Event{
			Payload: map[string]any{
				"tool_name":  "db_migrate",
				"reason":     "Migration risk",
				"url":        "https://diff.local",
				"expires_at": now,
			},
		}
		req2, _ := parseApprovalEvent(ev2, "chan")
		if req2.TaskTitle != "Tool Execution: db_migrate" {
			t.Errorf("expected tool_name mapping, got %s", req2.TaskTitle)
		}
		if req2.GateReason != "Migration risk" {
			t.Errorf("expected reason mapping, got %s", req2.GateReason)
		}
		if req2.DiffURL != "https://diff.local" {
			t.Errorf("expected url mapping, got %s", req2.DiffURL)
		}
		if req2.ExpiresAt.Unix() != now.Unix() {
			t.Errorf("expected time.Time mapped directly, got %v", req2.ExpiresAt)
		}

		// Test nil payload
		reqNil, _ := parseApprovalEvent(gateway.Event{RunID: "r-nil"}, "chan")
		if reqNil.RunID != "r-nil" {
			t.Errorf("expected runID r-nil, got %s", reqNil.RunID)
		}

		// Test getIntFromPayload fallback
		if getIntFromPayload(map[string]any{"bad": "string"}, "bad") != 0 {
			t.Error("expected 0 for bad type")
		}
	})
}
