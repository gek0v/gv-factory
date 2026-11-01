// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

// Manager manages chat platform notifiers and bridges gateway events to chat channels.
type Manager struct {
	mu        sync.RWMutex
	notifiers map[string]ChatNotifier
}

// NewManager creates an empty chat manager.
func NewManager() *Manager {
	return &Manager{
		notifiers: make(map[string]ChatNotifier),
	}
}

// Register adds a ChatNotifier implementation for its platform.
func (m *Manager) Register(notifier ChatNotifier) {
	if notifier == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifiers[notifier.Platform()] = notifier
}

// Get retrieves a notifier by platform name (e.g. "slack", "discord").
func (m *Manager) Get(platform string) (ChatNotifier, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.notifiers[platform]
	return n, ok
}

// Platforms returns a list of all registered platform names.
func (m *Manager) Platforms() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []string
	for p := range m.notifiers {
		list = append(list, p)
	}
	return list
}

// SendNotification routes a notification message to the specified platform, or broadcasts to all if platform is empty.
func (m *Manager) SendNotification(ctx context.Context, platform string, msg NotificationMessage) ([]*MessageRef, error) {
	if platform != "" {
		n, ok := m.Get(platform)
		if !ok {
			return nil, fmt.Errorf("no notifier registered for platform %q", platform)
		}
		ref, err := n.SendNotification(ctx, msg)
		if err != nil {
			return nil, err
		}
		return []*MessageRef{ref}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	var refs []*MessageRef
	for _, n := range m.notifiers {
		ref, err := n.SendNotification(ctx, msg)
		if err == nil && ref != nil {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// PromptApproval broadcasts or targets an interactive approval card.
func (m *Manager) PromptApproval(ctx context.Context, platform string, req ApprovalPromptRequest) ([]*MessageRef, error) {
	if platform != "" {
		n, ok := m.Get(platform)
		if !ok {
			return nil, fmt.Errorf("no notifier registered for platform %q", platform)
		}
		ref, err := n.PromptApproval(ctx, req)
		if err != nil {
			return nil, err
		}
		return []*MessageRef{ref}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	var refs []*MessageRef
	for _, n := range m.notifiers {
		ref, err := n.PromptApproval(ctx, req)
		if err == nil && ref != nil {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// UpdateStatus routes an execution step status update to a platform or all registered notifiers.
func (m *Manager) UpdateStatus(ctx context.Context, platform string, update StatusUpdate) ([]*MessageRef, error) {
	if platform != "" {
		n, ok := m.Get(platform)
		if !ok {
			return nil, fmt.Errorf("no notifier registered for platform %q", platform)
		}
		ref, err := n.UpdateStatus(ctx, update)
		if err != nil {
			return nil, err
		}
		return []*MessageRef{ref}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	var refs []*MessageRef
	for _, n := range m.notifiers {
		ref, err := n.UpdateStatus(ctx, update)
		if err == nil && ref != nil {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// AttachToGateway listens to factory lifecycle events and dispatches notifications/gate prompts.
func (m *Manager) AttachToGateway(ctx context.Context, gw *gateway.Gateway, defaultChannel string) (func(), error) {
	if gw == nil {
		return nil, fmt.Errorf("gateway cannot be nil")
	}

	eventCh := make(chan gateway.Event, 64)
	unsubGate, err := gw.SubscribeTaskEvents(ctx, gateway.TopicGateApprovalRequired, eventCh)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				token, _ := ev.Payload["token"].(string)
				summary, _ := ev.Payload["diagnostics_summary"].(string)

				req := ApprovalPromptRequest{
					RunID:           ev.RunID,
					Token:           token,
					Channel:         defaultChannel,
					Summary:         summary,
					ExpiresAt:       time.Now().Add(1 * time.Hour),
					CoveragePercent: 85.0,
				}

				_, _ = m.PromptApproval(ctx, "", req)
			}
		}
	}()

	return func() {
		close(done)
		if unsubGate != nil {
			unsubGate()
		}
	}, nil
}
