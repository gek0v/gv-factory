// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MemoryStore provides a thread-safe in-memory implementation of Store.
type MemoryStore struct {
	mu           sync.RWMutex
	closed       bool
	runs         map[string]*Run
	steps        map[string][]*Step
	pendingGates map[string]*PendingGate // key: token_hash
	gateAudits   []*GateAudit
	auditSeq     int64
}

// NewMemoryStore initializes and returns a ready-to-use MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:         make(map[string]*Run),
		steps:        make(map[string][]*Step),
		pendingGates: make(map[string]*PendingGate),
		gateAudits:   make([]*GateAudit, 0),
	}
}

// Close marks the store as closed.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// HashToken calculates the SHA-256 hex string for a given token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// CreateRun inserts a new run into memory.
func (m *MemoryStore) CreateRun(ctx context.Context, run *Run) error {
	if run == nil || run.ID == "" {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrStoreClosed
	}

	if _, exists := m.runs[run.ID]; exists {
		return ErrConflict
	}

	now := time.Now().UTC()
	rCopy := deepCopyRun(run)
	if rCopy.CreatedAt.IsZero() {
		rCopy.CreatedAt = now
	}
	if rCopy.UpdatedAt.IsZero() {
		rCopy.UpdatedAt = now
	}
	if rCopy.Status == "" {
		rCopy.Status = RunStatusQueued
	}
	if rCopy.Metadata == nil {
		rCopy.Metadata = make(map[string]any)
	}

	m.runs[run.ID] = rCopy
	return nil
}

// GetRun returns a run by ID or ErrNotFound.
func (m *MemoryStore) GetRun(ctx context.Context, id string) (*Run, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrStoreClosed
	}

	r, exists := m.runs[id]
	if !exists {
		return nil, ErrNotFound
	}

	return deepCopyRun(r), nil
}

// ListRuns lists runs matching the given filter.
func (m *MemoryStore) ListRuns(ctx context.Context, filter RunFilter) ([]*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrStoreClosed
	}

	var matched []*Run
	for _, r := range m.runs {
		if filter.TemplateName != "" && r.TemplateName != filter.TemplateName {
			continue
		}
		if filter.Status != "" && r.Status != filter.Status {
			continue
		}
		if filter.InitiatedBy != "" && r.InitiatedBy != filter.InitiatedBy {
			continue
		}
		matched = append(matched, deepCopyRun(r))
	}

	// Apply offset and limit
	total := len(matched)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.Offset >= total {
		return []*Run{}, nil
	}

	matched = matched[filter.Offset:]
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}

	return matched, nil
}

// UpdateRunStatus transitions a run's status and updates timestamps.
func (m *MemoryStore) UpdateRunStatus(ctx context.Context, id string, status RunStatus, runErr error) error {
	if id == "" {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrStoreClosed
	}

	r, exists := m.runs[id]
	if !exists {
		return ErrNotFound
	}

	now := time.Now().UTC()
	r.Status = status
	r.UpdatedAt = now

	if runErr != nil {
		r.ErrorMessage = runErr.Error()
	}

	if status == RunStatusRunning && r.StartedAt == nil {
		r.StartedAt = &now
	}
	if (status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled) && r.CompletedAt == nil {
		r.CompletedAt = &now
	}

	return nil
}

// RecordStep records a new step under a run.
func (m *MemoryStore) RecordStep(ctx context.Context, step *Step) error {
	if step == nil || step.ID == "" || step.RunID == "" {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrStoreClosed
	}

	now := time.Now().UTC()
	sCopy := deepCopyStep(step)
	if sCopy.CreatedAt.IsZero() {
		sCopy.CreatedAt = now
	}
	if sCopy.Output == nil {
		sCopy.Output = make(map[string]any)
	}

	m.steps[step.RunID] = append(m.steps[step.RunID], sCopy)
	return nil
}

// ListStepsByRun returns all steps for a run in creation order.
func (m *MemoryStore) ListStepsByRun(ctx context.Context, runID string) ([]*Step, error) {
	if runID == "" {
		return nil, ErrInvalidInput
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrStoreClosed
	}

	steps, exists := m.steps[runID]
	if !exists {
		return []*Step{}, nil
	}

	result := make([]*Step, len(steps))
	for i, s := range steps {
		result[i] = deepCopyStep(s)
	}
	return result, nil
}

// RecordPendingGate stores an active gate awaiting approval.
func (m *MemoryStore) RecordPendingGate(ctx context.Context, gate *PendingGate) error {
	if gate == nil || gate.RunID == "" {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrStoreClosed
	}

	tokenHash := gate.TokenHash
	if tokenHash == "" && gate.Token != "" {
		tokenHash = HashToken(gate.Token)
	}
	if tokenHash == "" {
		tokenHash = HashToken(gate.ID)
	}

	now := time.Now().UTC()
	gCopy := deepCopyPendingGate(gate)
	gCopy.TokenHash = tokenHash
	if gCopy.ID == "" {
		gCopy.ID = fmt.Sprintf("gate-%d", now.UnixNano())
	}
	if gCopy.CreatedAt.IsZero() {
		gCopy.CreatedAt = now
	}
	if gCopy.ExpiresAt.IsZero() {
		gCopy.ExpiresAt = now.Add(10 * time.Minute)
	}
	if gCopy.Status == "" {
		gCopy.Status = GateStatusPending
	}
	if gCopy.Arguments == nil {
		gCopy.Arguments = make(map[string]any)
	}

	m.pendingGates[tokenHash] = gCopy
	return nil
}

// RecordGateAudit records an immutable audit record.
func (m *MemoryStore) RecordGateAudit(ctx context.Context, entry *GateAudit) error {
	if entry == nil || entry.RunID == "" {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrStoreClosed
	}

	m.auditSeq++
	now := time.Now().UTC()
	eCopy := deepCopyGateAudit(entry)
	eCopy.ID = m.auditSeq
	if eCopy.CreatedAt.IsZero() {
		eCopy.CreatedAt = now
	}
	if eCopy.Metadata == nil {
		eCopy.Metadata = make(map[string]any)
	}

	m.gateAudits = append(m.gateAudits, eCopy)
	return nil
}

// ListPendingGates lists all gates currently in PENDING status.
func (m *MemoryStore) ListPendingGates(ctx context.Context) ([]*PendingGate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrStoreClosed
	}

	now := time.Now().UTC()
	var pending []*PendingGate
	for _, g := range m.pendingGates {
		if g.Status == GateStatusPending {
			if !g.ExpiresAt.IsZero() && now.After(g.ExpiresAt) {
				continue // Skip expired gates in pending list
			}
			pending = append(pending, deepCopyPendingGate(g))
		}
	}
	return pending, nil
}

// ResolveGate resolves a gate and generates an audit log entry.
func (m *MemoryStore) ResolveGate(ctx context.Context, token string, resolution GateResolution) (*GateAudit, error) {
	if token == "" {
		return nil, ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrStoreClosed
	}

	tokenHash := HashToken(token)
	gate, exists := m.pendingGates[tokenHash]
	if !exists {
		// Also check direct match if token passed was already a hash
		gate, exists = m.pendingGates[token]
		if !exists {
			return nil, ErrNotFound
		}
		tokenHash = token
	}

	now := time.Now().UTC()
	if gate.Status == GateStatusResolved {
		return nil, ErrGateAlreadyResolved
	}
	if !gate.ExpiresAt.IsZero() && now.After(gate.ExpiresAt) {
		gate.Status = GateStatusExpired
		return nil, ErrGateExpired
	}

	gate.Status = GateStatusResolved

	m.auditSeq++
	audit := &GateAudit{
		ID:        m.auditSeq,
		RunID:     gate.RunID,
		TokenHash: tokenHash,
		GateType:  gate.GateType,
		Action:    resolution.Decision,
		Actor:     resolution.Actor,
		Reason:    resolution.Reason,
		Metadata:  resolution.Metadata,
		CreatedAt: now,
	}
	if audit.Metadata == nil {
		audit.Metadata = make(map[string]any)
	}

	m.gateAudits = append(m.gateAudits, deepCopyGateAudit(audit))
	return audit, nil
}

// WithTx runs fn with copy-on-write rollback semantics on error.
func (m *MemoryStore) WithTx(ctx context.Context, fn func(tx Store) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrStoreClosed
	}

	// Create snapshot for rollback
	snapRuns := make(map[string]*Run, len(m.runs))
	for k, v := range m.runs {
		snapRuns[k] = deepCopyRun(v)
	}

	snapSteps := make(map[string][]*Step, len(m.steps))
	for k, v := range m.steps {
		sliceCopy := make([]*Step, len(v))
		for i, s := range v {
			sliceCopy[i] = deepCopyStep(s)
		}
		snapSteps[k] = sliceCopy
	}

	snapPending := make(map[string]*PendingGate, len(m.pendingGates))
	for k, v := range m.pendingGates {
		snapPending[k] = deepCopyPendingGate(v)
	}

	snapAudits := make([]*GateAudit, len(m.gateAudits))
	for i, a := range m.gateAudits {
		snapAudits[i] = deepCopyGateAudit(a)
	}
	snapSeq := m.auditSeq

	// Create transaction view wrapper operating on same memory under lock
	txStore := &memoryTxWrapper{parent: m}

	err := fn(txStore)
	if err != nil {
		// Rollback to snapshot
		m.runs = snapRuns
		m.steps = snapSteps
		m.pendingGates = snapPending
		m.gateAudits = snapAudits
		m.auditSeq = snapSeq
		return err
	}

	return nil
}

// memoryTxWrapper delegates to parent whose lock is already held.
type memoryTxWrapper struct {
	parent *MemoryStore
}

func (w *memoryTxWrapper) CreateRun(ctx context.Context, run *Run) error {
	if run == nil || run.ID == "" {
		return ErrInvalidInput
	}
	if _, exists := w.parent.runs[run.ID]; exists {
		return ErrConflict
	}
	now := time.Now().UTC()
	rCopy := deepCopyRun(run)
	if rCopy.CreatedAt.IsZero() {
		rCopy.CreatedAt = now
	}
	if rCopy.UpdatedAt.IsZero() {
		rCopy.UpdatedAt = now
	}
	if rCopy.Status == "" {
		rCopy.Status = RunStatusQueued
	}
	if rCopy.Metadata == nil {
		rCopy.Metadata = make(map[string]any)
	}
	w.parent.runs[run.ID] = rCopy
	return nil
}

func (w *memoryTxWrapper) GetRun(ctx context.Context, id string) (*Run, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}
	r, exists := w.parent.runs[id]
	if !exists {
		return nil, ErrNotFound
	}
	return deepCopyRun(r), nil
}

func (w *memoryTxWrapper) ListRuns(ctx context.Context, filter RunFilter) ([]*Run, error) {
	var matched []*Run
	for _, r := range w.parent.runs {
		if filter.TemplateName != "" && r.TemplateName != filter.TemplateName {
			continue
		}
		if filter.Status != "" && r.Status != filter.Status {
			continue
		}
		if filter.InitiatedBy != "" && r.InitiatedBy != filter.InitiatedBy {
			continue
		}
		matched = append(matched, deepCopyRun(r))
	}
	total := len(matched)
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.Offset >= total {
		return []*Run{}, nil
	}
	matched = matched[filter.Offset:]
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}
	return matched, nil
}

func (w *memoryTxWrapper) UpdateRunStatus(ctx context.Context, id string, status RunStatus, runErr error) error {
	if id == "" {
		return ErrInvalidInput
	}
	r, exists := w.parent.runs[id]
	if !exists {
		return ErrNotFound
	}
	now := time.Now().UTC()
	r.Status = status
	r.UpdatedAt = now
	if runErr != nil {
		r.ErrorMessage = runErr.Error()
	}
	if status == RunStatusRunning && r.StartedAt == nil {
		r.StartedAt = &now
	}
	if (status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled) && r.CompletedAt == nil {
		r.CompletedAt = &now
	}
	return nil
}

func (w *memoryTxWrapper) RecordStep(ctx context.Context, step *Step) error {
	if step == nil || step.ID == "" || step.RunID == "" {
		return ErrInvalidInput
	}
	now := time.Now().UTC()
	sCopy := deepCopyStep(step)
	if sCopy.CreatedAt.IsZero() {
		sCopy.CreatedAt = now
	}
	if sCopy.Output == nil {
		sCopy.Output = make(map[string]any)
	}
	w.parent.steps[step.RunID] = append(w.parent.steps[step.RunID], sCopy)
	return nil
}

func (w *memoryTxWrapper) ListStepsByRun(ctx context.Context, runID string) ([]*Step, error) {
	if runID == "" {
		return nil, ErrInvalidInput
	}
	steps, exists := w.parent.steps[runID]
	if !exists {
		return []*Step{}, nil
	}
	result := make([]*Step, len(steps))
	for i, s := range steps {
		result[i] = deepCopyStep(s)
	}
	return result, nil
}

func (w *memoryTxWrapper) RecordPendingGate(ctx context.Context, gate *PendingGate) error {
	if gate == nil || gate.RunID == "" {
		return ErrInvalidInput
	}
	tokenHash := gate.TokenHash
	if tokenHash == "" && gate.Token != "" {
		tokenHash = HashToken(gate.Token)
	}
	if tokenHash == "" {
		tokenHash = HashToken(gate.ID)
	}
	now := time.Now().UTC()
	gCopy := deepCopyPendingGate(gate)
	gCopy.TokenHash = tokenHash
	if gCopy.ID == "" {
		gCopy.ID = fmt.Sprintf("gate-%d", now.UnixNano())
	}
	if gCopy.CreatedAt.IsZero() {
		gCopy.CreatedAt = now
	}
	if gCopy.ExpiresAt.IsZero() {
		gCopy.ExpiresAt = now.Add(10 * time.Minute)
	}
	if gCopy.Status == "" {
		gCopy.Status = GateStatusPending
	}
	if gCopy.Arguments == nil {
		gCopy.Arguments = make(map[string]any)
	}
	w.parent.pendingGates[tokenHash] = gCopy
	return nil
}

func (w *memoryTxWrapper) RecordGateAudit(ctx context.Context, entry *GateAudit) error {
	if entry == nil || entry.RunID == "" {
		return ErrInvalidInput
	}
	w.parent.auditSeq++
	now := time.Now().UTC()
	eCopy := deepCopyGateAudit(entry)
	eCopy.ID = w.parent.auditSeq
	if eCopy.CreatedAt.IsZero() {
		eCopy.CreatedAt = now
	}
	if eCopy.Metadata == nil {
		eCopy.Metadata = make(map[string]any)
	}
	w.parent.gateAudits = append(w.parent.gateAudits, eCopy)
	return nil
}

func (w *memoryTxWrapper) ListPendingGates(ctx context.Context) ([]*PendingGate, error) {
	now := time.Now().UTC()
	var pending []*PendingGate
	for _, g := range w.parent.pendingGates {
		if g.Status == GateStatusPending {
			if !g.ExpiresAt.IsZero() && now.After(g.ExpiresAt) {
				continue
			}
			pending = append(pending, deepCopyPendingGate(g))
		}
	}
	return pending, nil
}

func (w *memoryTxWrapper) ResolveGate(ctx context.Context, token string, resolution GateResolution) (*GateAudit, error) {
	if token == "" {
		return nil, ErrInvalidInput
	}
	tokenHash := HashToken(token)
	gate, exists := w.parent.pendingGates[tokenHash]
	if !exists {
		gate, exists = w.parent.pendingGates[token]
		if !exists {
			return nil, ErrNotFound
		}
		tokenHash = token
	}
	now := time.Now().UTC()
	if gate.Status == GateStatusResolved {
		return nil, ErrGateAlreadyResolved
	}
	if !gate.ExpiresAt.IsZero() && now.After(gate.ExpiresAt) {
		gate.Status = GateStatusExpired
		return nil, ErrGateExpired
	}
	gate.Status = GateStatusResolved
	w.parent.auditSeq++
	audit := &GateAudit{
		ID:        w.parent.auditSeq,
		RunID:     gate.RunID,
		TokenHash: tokenHash,
		GateType:  gate.GateType,
		Action:    resolution.Decision,
		Actor:     resolution.Actor,
		Reason:    resolution.Reason,
		Metadata:  resolution.Metadata,
		CreatedAt: now,
	}
	if audit.Metadata == nil {
		audit.Metadata = make(map[string]any)
	}
	w.parent.gateAudits = append(w.parent.gateAudits, deepCopyGateAudit(audit))
	return audit, nil
}

func (w *memoryTxWrapper) WithTx(ctx context.Context, fn func(tx Store) error) error {
	// Nested transactions reuse existing boundary
	return fn(w)
}

func (w *memoryTxWrapper) Close() error {
	return nil
}

// Deep copy helper functions to guarantee data race freedom.

func deepCopyRun(src *Run) *Run {
	if src == nil {
		return nil
	}
	dst := *src
	if src.StartedAt != nil {
		t := *src.StartedAt
		dst.StartedAt = &t
	}
	if src.CompletedAt != nil {
		t := *src.CompletedAt
		dst.CompletedAt = &t
	}
	if src.Metadata != nil {
		dst.Metadata = make(map[string]any, len(src.Metadata))
		for k, v := range src.Metadata {
			dst.Metadata[k] = v
		}
	}
	return &dst
}

func deepCopyStep(src *Step) *Step {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Output != nil {
		dst.Output = make(map[string]any, len(src.Output))
		for k, v := range src.Output {
			dst.Output[k] = v
		}
	}
	return &dst
}

func deepCopyPendingGate(src *PendingGate) *PendingGate {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Arguments != nil {
		dst.Arguments = make(map[string]any, len(src.Arguments))
		for k, v := range src.Arguments {
			dst.Arguments[k] = v
		}
	}
	return &dst
}

func deepCopyGateAudit(src *GateAudit) *GateAudit {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Metadata != nil {
		dst.Metadata = make(map[string]any, len(src.Metadata))
		for k, v := range src.Metadata {
			dst.Metadata[k] = v
		}
	}
	return &dst
}
