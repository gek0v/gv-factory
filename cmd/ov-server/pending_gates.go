// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/store"
)

// PendingGateInfo represents an active pending decision gate awaiting Human-in-the-Loop approval.
type PendingGateInfo struct {
	RunID           string         `json:"run_id"`
	ToolName        string         `json:"tool_name,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	Token           string         `json:"token,omitempty"`
	TokenHMACPrefix string         `json:"token_hmac_prefix"`
	Diff            string         `json:"diff,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	ExpiresAt       time.Time      `json:"expires_at"`
	TimeoutMS       int64          `json:"timeout_ms"`
	CallerID        string         `json:"caller_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
}

// PendingGateTracker tracks active pending decision gates in memory or delegates to persistent Store.
type PendingGateTracker struct {
	mu    sync.RWMutex
	store store.Store
	gates map[string]PendingGateInfo // fallback in-memory cache: key token or runID
}

// NewPendingGateTracker creates a tracker and subscribes to approval and resolution events.
func NewPendingGateTracker(ctx context.Context, gw *gateway.Gateway, st ...store.Store) *PendingGateTracker {
	var s store.Store
	if len(st) > 0 && st[0] != nil {
		s = st[0]
	}

	tracker := &PendingGateTracker{
		store: s,
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

	diff, _ := p["diff"].(string)
	reason, _ := p["reason"].(string)
	if diff == "" && args != nil {
		diff, _ = args["diff"].(string)
	}
	if reason == "" && args != nil {
		reason, _ = args["reason"].(string)
	}

	key := token
	if key == "" {
		key = runID
	}

	if args == nil {
		args = make(map[string]any)
	}
	if diff != "" {
		args["diff"] = diff
	}
	if reason != "" {
		args["reason"] = reason
	}

	t.gates[key] = PendingGateInfo{
		RunID:           runID,
		ToolName:        toolName,
		Arguments:       args,
		Token:           token,
		TokenHMACPrefix: auth.ExtractTokenPrefix(token),
		Diff:            diff,
		Reason:          reason,
		CreatedAt:       now,
		ExpiresAt:       expiresAt,
		TimeoutMS:       timeoutMS,
		CallerID:        callerID,
		SessionID:       sessionID,
	}

	if t.store != nil {
		_ = t.store.RecordPendingGate(context.Background(), &store.PendingGate{
			ID:        key,
			RunID:     runID,
			Token:     token,
			GateType:  "HUMAN_APPROVAL",
			ToolName:  toolName,
			Arguments: args,
			Status:    store.GateStatusPending,
			CreatedAt: now,
			ExpiresAt: expiresAt,
		})
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

// RecordGate stores or updates a pending gate record directly.
func (t *PendingGateTracker) RecordGate(info PendingGateInfo) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	key := info.Token
	if key == "" {
		key = info.RunID
	}
	if info.TokenHMACPrefix == "" && info.Token != "" {
		info.TokenHMACPrefix = auth.ExtractTokenPrefix(info.Token)
	}
	if info.Arguments == nil {
		info.Arguments = make(map[string]any)
	}
	if info.Diff != "" {
		info.Arguments["diff"] = info.Diff
	}
	if info.Reason != "" {
		info.Arguments["reason"] = info.Reason
	}

	t.gates[key] = info

	if t.store != nil {
		_ = t.store.RecordPendingGate(context.Background(), &store.PendingGate{
			ID:        key,
			RunID:     info.RunID,
			Token:     info.Token,
			GateType:  "HUMAN_GATE",
			ToolName:  info.ToolName,
			Arguments: info.Arguments,
			Status:    store.GateStatusPending,
			CreatedAt: info.CreatedAt,
			ExpiresAt: info.ExpiresAt,
		})
	}
}

// ListPending returns active, unexpired pending gates.
func (t *PendingGateTracker) ListPending() []PendingGateInfo {
	if t.store != nil {
		storedGates, err := t.store.ListPendingGates(context.Background())
		if err == nil && len(storedGates) > 0 {
			result := make([]PendingGateInfo, 0, len(storedGates))
			for _, g := range storedGates {
				diff := ""
				reason := ""
				if g.Arguments != nil {
					diff, _ = g.Arguments["diff"].(string)
					reason, _ = g.Arguments["reason"].(string)
				}
				result = append(result, PendingGateInfo{
					RunID:           g.RunID,
					ToolName:        g.ToolName,
					Arguments:       g.Arguments,
					Token:           g.Token,
					TokenHMACPrefix: auth.ExtractTokenPrefix(g.Token),
					Diff:            diff,
					Reason:          reason,
					CreatedAt:       g.CreatedAt,
					ExpiresAt:       g.ExpiresAt,
				})
			}
			return result
		}
	}

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
