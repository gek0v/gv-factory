// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

// PendingGateInfo represents an active pending decision gate awaiting Human-in-the-Loop approval.
type PendingGateInfo struct {
	RunID           string         `json:"run_id"`
	ToolName        string         `json:"tool_name,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	Token           string         `json:"token,omitempty"`
	TokenHMACPrefix string         `json:"token_hmac_prefix"`
	CreatedAt       time.Time      `json:"created_at"`
	ExpiresAt       time.Time      `json:"expires_at"`
	TimeoutMS       int64          `json:"timeout_ms"`
	CallerID        string         `json:"caller_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
}

// PendingGateTracker tracks active pending decision gates in memory.
type PendingGateTracker struct {
	mu    sync.RWMutex
	gates map[string]PendingGateInfo // key: token (or runID if token empty)
}

// NewPendingGateTracker creates a tracker and subscribes to approval and resolution events.
func NewPendingGateTracker(ctx context.Context, gw *gateway.Gateway) *PendingGateTracker {
	tracker := &PendingGateTracker{
		gates: make(map[string]PendingGateInfo),
	}

	if gw == nil {
		return tracker
	}

	approvalCh := make(chan gateway.Event, 50)
	resolvedCh := make(chan gateway.Event, 50)

	_, _ = gw.SubscribeTaskEvents(ctx, gateway.TopicGateApprovalRequired, approvalCh)
	_, _ = gw.SubscribeTaskEvents(ctx, gateway.TopicGateResolved, resolvedCh)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-approvalCh:
				if !ok {
					return
				}
				tracker.handleApprovalRequired(ev)
			case ev, ok := <-resolvedCh:
				if !ok {
					return
				}
				tracker.handleResolved(ev)
			}
		}
	}()

	return tracker
}

func (t *PendingGateTracker) handleApprovalRequired(ev gateway.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := ev.Payload
	runID := ev.RunID
	if runID == "" {
		if r, ok := p["run_id"].(string); ok {
			runID = r
		}
	}

	token, _ := p["token"].(string)
	toolName, _ := p["tool_name"].(string)
	callerID, _ := p["caller_id"].(string)
	sessionID, _ := p["session_id"].(string)

	var args map[string]any
	if a, ok := p["arguments"].(map[string]any); ok {
		args = a
	}

	var timeoutMS int64
	if to, ok := p["timeout_ms"].(float64); ok {
		timeoutMS = int64(to)
	} else if toInt, ok := p["timeout_ms"].(int64); ok {
		timeoutMS = toInt
	} else if toInt, ok := p["timeout_ms"].(int); ok {
		timeoutMS = int64(toInt)
	}

	now := time.Now().UTC()
	var expiresAt time.Time
	if exp, ok := p["expires_at"].(float64); ok && exp > 0 {
		expiresAt = time.Unix(int64(exp), 0).UTC()
	} else if expInt, ok := p["expires_at"].(int64); ok && expInt > 0 {
		expiresAt = time.Unix(expInt, 0).UTC()
	} else if timeoutMS > 0 {
		expiresAt = now.Add(time.Duration(timeoutMS) * time.Millisecond)
	} else {
		expiresAt = now.Add(5 * time.Minute)
	}

	key := token
	if key == "" {
		key = runID
	}

	t.gates[key] = PendingGateInfo{
		RunID:           runID,
		ToolName:        toolName,
		Arguments:       args,
		Token:           token,
		TokenHMACPrefix: auth.ExtractTokenPrefix(token),
		CreatedAt:       now,
		ExpiresAt:       expiresAt,
		TimeoutMS:       timeoutMS,
		CallerID:        callerID,
		SessionID:       sessionID,
	}
}

func (t *PendingGateTracker) handleResolved(ev gateway.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := ev.Payload
	token, _ := p["token"].(string)
	runID := ev.RunID
	if runID == "" {
		if r, ok := p["run_id"].(string); ok {
			runID = r
		}
	}

	if token != "" {
		delete(t.gates, token)
	}
	// Also remove by runID if any
	for k, g := range t.gates {
		if g.RunID == runID {
			delete(t.gates, k)
		}
	}
}

// ListPending returns active, unexpired pending gates.
func (t *PendingGateTracker) ListPending() []PendingGateInfo {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UTC()
	result := make([]PendingGateInfo, 0, len(t.gates))

	for k, g := range t.gates {
		if !g.ExpiresAt.IsZero() && now.After(g.ExpiresAt) {
			delete(t.gates, k)
			continue
		}
		result = append(result, g)
	}

	return result
}
