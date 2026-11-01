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
}
