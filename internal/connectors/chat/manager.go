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
				req, platform := parseApprovalEvent(ev, defaultChannel)
				_, _ = m.PromptApproval(ctx, platform, req)
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

func parseApprovalEvent(ev gateway.Event, defaultChannel string) (ApprovalPromptRequest, string) {
	p := ev.Payload
	if p == nil {
		p = make(map[string]any)
	}

	runID := ev.RunID
	if runID == "" {
		if r, ok := p["run_id"].(string); ok {
			runID = r
		}
	}

	token, _ := p["token"].(string)

	channel := defaultChannel
	if ch, ok := p["channel"].(string); ok && ch != "" {
		channel = ch
	}

	var taskTitle string
	if t, ok := p["task_title"].(string); ok {
		taskTitle = t
	} else if t, ok := p["task_name"].(string); ok {
		taskTitle = t
	} else if t, ok := p["tool_name"].(string); ok {
		taskTitle = fmt.Sprintf("Tool Execution: %s", t)
	}

	var gateReason string
	if r, ok := p["gate_reason"].(string); ok {
		gateReason = r
	} else if r, ok := p["reason"].(string); ok {
		gateReason = r
	}

	var summary string
	if s, ok := p["diagnostics_summary"].(string); ok {
		summary = s
	} else if s, ok := p["summary"].(string); ok {
		summary = s
	}

	diffURL, _ := p["diff_url"].(string)
	if diffURL == "" {
		diffURL, _ = p["canvas_url"].(string)
	}
	if diffURL == "" {
		diffURL, _ = p["url"].(string)
	}

	commitSHA, _ := p["commit_sha"].(string)

	findingsCount := getIntFromPayload(p, "findings_count")
	criticalAlerts := getIntFromPayload(p, "critical_alerts")
	highAlerts := getIntFromPayload(p, "high_alerts")

	coveragePercent := 85.0
	if cov, ok := p["coverage_percent"].(float64); ok {
		coveragePercent = cov
	} else if cov, ok := p["coverage_percent"].(int); ok {
		coveragePercent = float64(cov)
	}

	expiresAt := time.Now().Add(1 * time.Hour)
	if exp, ok := p["expires_at"].(int64); ok && exp > 0 {
		expiresAt = time.Unix(exp, 0)
	} else if exp, ok := p["expires_at"].(float64); ok && exp > 0 {
		expiresAt = time.Unix(int64(exp), 0)
	} else if exp, ok := p["expires_at"].(time.Time); ok {
		expiresAt = exp
	}

	var reviewerScores []string
	if scores, ok := p["reviewer_scores"].([]string); ok {
		reviewerScores = scores
	} else if scores, ok := p["reviewer_scores"].([]any); ok {
		for _, s := range scores {
			if str, ok := s.(string); ok {
				reviewerScores = append(reviewerScores, str)
			}
		}
	}

	hasMigration, _ := p["has_migration_changes"].(bool)
	hasAuth, _ := p["has_auth_changes"].(bool)
	hasSchema, _ := p["has_schema_changes"].(bool)

	platform, _ := p["platform"].(string)

	return ApprovalPromptRequest{
		RunID:               runID,
		Token:               token,
		Channel:             channel,
		Summary:             summary,
		TaskTitle:           taskTitle,
		GateReason:          gateReason,
		FindingsCount:       findingsCount,
		CommitSHA:           commitSHA,
		DiffURL:             diffURL,
		CoveragePercent:     coveragePercent,
		CriticalAlerts:      criticalAlerts,
		HighAlerts:          highAlerts,
		ExpiresAt:           expiresAt,
		ReviewerScores:      reviewerScores,
		HasMigrationChanges: hasMigration,
		HasAuthChanges:      hasAuth,
		HasSchemaChanges:    hasSchema,
	}, platform
}

func getIntFromPayload(p map[string]any, key string) int {
	if val, ok := p[key].(int); ok {
		return val
	}
	if val, ok := p[key].(float64); ok {
		return int(val)
	}
	if val, ok := p[key].(int64); ok {
		return int(val)
	}
	return 0
}
