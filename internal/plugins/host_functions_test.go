// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

// mockBrainService implements BrainService for testing host functions.
type mockBrainService struct {
	rules           map[string]brain.ArchitecturalRule
	listErr         error
	vectorErr       error
	codeErr         error
	mu              sync.RWMutex
	lastVectorLimit int
}

func newMockBrainService() *mockBrainService {
	return &mockBrainService{
		rules: make(map[string]brain.ArchitecturalRule),
	}
}

func (m *mockBrainService) addRule(r brain.ArchitecturalRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[r.RuleCode] = r
}

func (m *mockBrainService) ListRules(ctx context.Context, limit int) ([]brain.ArchitecturalRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	var res []brain.ArchitecturalRule
	for _, r := range m.rules {
		res = append(res, r)
		if limit > 0 && len(res) >= limit {
			break
		}
	}
	return res, nil
}

func (m *mockBrainService) QueryRelevantRulesByVector(ctx context.Context, embedding []float32, limit int) ([]brain.ArchitecturalRule, error) {
	m.mu.Lock()
	m.lastVectorLimit = limit
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.vectorErr != nil {
		return nil, m.vectorErr
	}
	var res []brain.ArchitecturalRule
	for _, r := range m.rules {
		r.Distance = 0.05
		res = append(res, r)
		if limit > 0 && len(res) >= limit {
			break
		}
	}
	return res, nil
}

func (m *mockBrainService) GetRuleByCode(ctx context.Context, ruleCode string) (*brain.ArchitecturalRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.codeErr != nil {
		return nil, m.codeErr
	}
	r, ok := m.rules[ruleCode]
	if !ok {
		return nil, errors.New("rule not found")
	}
	return &r, nil
}

func TestBrainQueryRules(t *testing.T) {
	ctx := context.Background()
	mock := newMockBrainService()
	mock.addRule(brain.ArchitecturalRule{
		RuleCode:    "ARCH-001",
		Title:       "Hexagonal Seams",
		Description: "Decouple core from adapters",
		Severity:    "ERROR",
	})
	mock.addRule(brain.ArchitecturalRule{
		RuleCode:    "SEC-002",
		Title:       "Mask Secrets",
		Description: "Scrub all tokens in logs",
		Severity:    "CRITICAL",
	})

	registry := NewHostFunctionRegistry(WithBrainService(mock))

	// 1. Query by rule code
	reqCode, _ := json.Marshal(BrainQueryRequest{RuleCode: "ARCH-001"})
	outBytes, err := registry.BrainQueryRules(ctx, reqCode)
	if err != nil {
		t.Fatalf("unexpected error querying rule code: %v", err)
	}
	var rules []brain.ArchitecturalRule
	if err := json.Unmarshal(outBytes, &rules); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	if len(rules) != 1 || rules[0].RuleCode != "ARCH-001" {
		t.Errorf("expected 1 rule with code ARCH-001, got: %+v", rules)
	}

	// 2. Query non-existent rule code
	reqMissing, _ := json.Marshal(BrainQueryRequest{RuleCode: "NON-EXISTENT"})
	outBytes, err = registry.BrainQueryRules(ctx, reqMissing)
	if err != nil {
		t.Fatalf("unexpected error for missing code: %v", err)
	}
	rules = nil
	if err := json.Unmarshal(outBytes, &rules); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules for missing code, got: %d", len(rules))
	}

	// 3. Query by vector
	reqVec, _ := json.Marshal(BrainQueryRequest{
		Embedding: []float32{0.1, 0.2, 0.3},
		Limit:     5,
	})
	outBytes, err = registry.BrainQueryRules(ctx, reqVec)
	if err != nil {
		t.Fatalf("unexpected error querying by vector: %v", err)
	}
	rules = nil
	if err := json.Unmarshal(outBytes, &rules); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules returned from vector search, got: %d", len(rules))
	}

	// 4. Default query (empty payload)
	outBytes, err = registry.BrainQueryRules(ctx, []byte{})
	if err != nil {
		t.Fatalf("unexpected error querying default: %v", err)
	}
	rules = nil
	if err := json.Unmarshal(outBytes, &rules); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules listed by default, got: %d", len(rules))
	}

	// 5. Query with nil brain service
	nilRegistry := NewHostFunctionRegistry()
	outBytes, err = nilRegistry.BrainQueryRules(ctx, []byte{})
	if err != nil || string(outBytes) != "[]" {
		t.Errorf("expected empty array for nil brain service, got: %s (err: %v)", string(outBytes), err)
	}

	// 6. Query error handling
	mock.listErr = errors.New("database connection failed")
	outBytes, err = registry.BrainQueryRules(ctx, []byte{})
	if err == nil {
		t.Errorf("expected error when brain service fails, got nil")
	}
	if string(outBytes) != "[]" {
		t.Errorf("expected '[]' fallback on error, got %s", string(outBytes))
	}
}

func TestLogEventSecretScrubbing(t *testing.T) {
	ctx := context.Background()

	var capturedEvents []LogEvent
	var mu sync.Mutex

	sink := func(event LogEvent) {
		mu.Lock()
		defer mu.Unlock()
		capturedEvents = append(capturedEvents, event)
	}

	registry := NewHostFunctionRegistry(
		WithSecrets("super-secret-token", "db-password-xyz"),
		WithLogSink(sink),
	)

	// 1. Structured JSON log event containing secrets in message and fields
	eventPayload := LogEvent{
		Level:   "warn",
		Message: "Authentication failed with token super-secret-token for user admin",
		Fields: map[string]any{
			"auth_header": "Bearer super-secret-token",
			"db_conn":     "postgres://user:db-password-xyz@localhost/db",
			"retry_count": 3,
		},
	}
	payloadBytes, _ := json.Marshal(eventPayload)

	respBytes, err := registry.LogEvent(ctx, payloadBytes)
	if err != nil {
		t.Fatalf("unexpected error emitting log: %v", err)
	}

	var resp LogResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil || resp.Status != "ok" || !resp.Masked {
		t.Fatalf("unexpected log response: %+v", resp)
	}

	mu.Lock()
	if len(capturedEvents) != 1 {
		t.Fatalf("expected 1 captured event, got %d", len(capturedEvents))
	}
	ev := capturedEvents[0]
	mu.Unlock()

	// Verify secrets were scrubbed
	if strings.Contains(ev.Message, "super-secret-token") {
		t.Errorf("secret not masked in message: %s", ev.Message)
	}
	if !strings.Contains(ev.Message, "***") {
		t.Errorf("expected mask marker *** in message: %s", ev.Message)
	}

	authHeader := ev.Fields["auth_header"].(string)
	if strings.Contains(authHeader, "super-secret-token") {
		t.Errorf("secret not masked in field: %s", authHeader)
	}

	dbConn := ev.Fields["db_conn"].(string)
	if strings.Contains(dbConn, "db-password-xyz") {
		t.Errorf("password not masked in field: %s", dbConn)
	}

	// 2. Dynamically registered secret
	registry.RegisterSecret("transient-secret-789")
	rawMessage := []byte("Failed to connect with transient-secret-789")

	_, err = registry.LogEvent(ctx, rawMessage)
	if err != nil {
		t.Fatalf("unexpected error logging raw message: %v", err)
	}

	mu.Lock()
	if len(capturedEvents) != 2 {
		t.Fatalf("expected 2 captured events, got %d", len(capturedEvents))
	}
	ev2 := capturedEvents[1]
	mu.Unlock()

	if strings.Contains(ev2.Message, "transient-secret-789") {
		t.Errorf("dynamic secret not masked: %s", ev2.Message)
	}
	if !strings.Contains(ev2.Message, "***") {
		t.Errorf("expected mask in raw message: %s", ev2.Message)
	}
}

func TestWorkspaceReadFileSecurity(t *testing.T) {
	ctx := context.Background()

	// Create a simulated worktree directory and an external directory
	worktreeRoot := t.TempDir()
	outsideDir := t.TempDir()

	// Files inside worktree
	subDir := filepath.Join(worktreeRoot, "src", "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dirs: %v", err)
	}
	appGo := filepath.Join(subDir, "app.go")
	if err := os.WriteFile(appGo, []byte("package pkg\nfunc Run() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write app.go: %v", err)
	}

	// Secret file outside worktree
	outsideSecret := filepath.Join(outsideDir, "secret.key")
	if err := os.WriteFile(outsideSecret, []byte("TOP-SECRET-KEY"), 0644); err != nil {
		t.Fatalf("failed to write outside secret: %v", err)
	}

	registry := NewHostFunctionRegistry(
		WithDefaultRunID("run-default-01"),
	)
	registry.RegisterWorktree("run-default-01", worktreeRoot)
	registry.RegisterWorktree("run-isolated-02", worktreeRoot)

	// 1. Read valid file inside worktree
	reqValid, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  "src/pkg/app.go",
	})
	respBytes, err := registry.WorkspaceReadFile(ctx, reqValid)
	if err != nil {
		t.Fatalf("unexpected error reading valid file: %v", err)
	}
	var resp WorkspaceReadResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !resp.Success || resp.Content != "package pkg\nfunc Run() {}\n" {
		t.Errorf("unexpected read response: %+v", resp)
	}

	// 2. Relative path traversal attempt (../secret.key)
	reqTraversal, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  "../secret.key",
	})
	respBytes, err = registry.WorkspaceReadFile(ctx, reqTraversal)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied for path traversal, got: %v", err)
	}
	resp = WorkspaceReadResponse{}
	_ = json.Unmarshal(respBytes, &resp)
	if resp.Success || !strings.Contains(resp.Error, "access denied") {
		t.Errorf("expected access denied response, got: %+v", resp)
	}

	// 3. Deep nested traversal attempt (src/pkg/../../../../secret.key)
	reqDeep, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  "src/pkg/../../../../secret.key",
	})
	respBytes, err = registry.WorkspaceReadFile(ctx, reqDeep)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied for deep traversal, got: %v", err)
	}
	resp = WorkspaceReadResponse{}
	_ = json.Unmarshal(respBytes, &resp)
	if resp.Success || !strings.Contains(resp.Error, "access denied") {
		t.Errorf("expected access denied response for deep traversal, got: %+v", resp)
	}

	// 4. Absolute path outside worktree
	reqAbs, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  outsideSecret,
	})
	respBytes, err = registry.WorkspaceReadFile(ctx, reqAbs)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied for absolute path, got: %v", err)
	}
	resp = WorkspaceReadResponse{}
	_ = json.Unmarshal(respBytes, &resp)
	if resp.Success || !strings.Contains(resp.Error, "access denied") {
		t.Errorf("expected access denied for absolute path, got: %+v", resp)
	}

	// 5. Empty path
	reqEmpty, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  "",
	})
	_, err = registry.WorkspaceReadFile(ctx, reqEmpty)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied for empty path, got: %v", err)
	}

	// 6. Path with null byte
	reqNull, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  "src/\x00evil.txt",
	})
	_, err = registry.WorkspaceReadFile(ctx, reqNull)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied for null byte, got: %v", err)
	}

	// 7. Missing runID with no default
	emptyReg := NewHostFunctionRegistry()
	reqNoRun, _ := json.Marshal(WorkspaceReadRequest{
		Path: "src/app.go",
	})
	_, err = emptyReg.WorkspaceReadFile(ctx, reqNoRun)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied when runID is missing, got: %v", err)
	}

	// 8. Unregistered runID
	reqUnknownRun, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "unknown-run-999",
		Path:  "src/app.go",
	})
	_, err = registry.WorkspaceReadFile(ctx, reqUnknownRun)
	if err == nil || !errors.Is(err, ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied for unregistered runID, got: %v", err)
	}

	// 9. Reading using Context RunID
	ctxWithRun := WithRunID(ctx, "run-isolated-02")
	reqPlain := []byte("src/pkg/app.go")
	respBytes, err = registry.WorkspaceReadFile(ctxWithRun, reqPlain)
	if err != nil {
		t.Fatalf("unexpected error reading via context runID: %v", err)
	}
	resp = WorkspaceReadResponse{}
	_ = json.Unmarshal(respBytes, &resp)
	if !resp.Success || resp.Content != "package pkg\nfunc Run() {}\n" {
		t.Errorf("expected successful read using context runID, got: %+v", resp)
	}

	// 10. Absolute path strictly INSIDE worktree (valid)
	absInside := filepath.Join(worktreeRoot, "src", "pkg", "app.go")
	reqAbsInside, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  absInside,
	})
	respBytes, err = registry.WorkspaceReadFile(ctx, reqAbsInside)
	if err != nil {
		t.Fatalf("unexpected error reading absolute path inside worktree: %v", err)
	}
	resp = WorkspaceReadResponse{}
	_ = json.Unmarshal(respBytes, &resp)
	if !resp.Success || resp.Content != "package pkg\nfunc Run() {}\n" {
		t.Errorf("expected successful read for valid absolute path, got: %+v", resp)
	}

	// 11. Non-existent file inside worktree
	reqNonExistent, _ := json.Marshal(WorkspaceReadRequest{
		RunID: "run-default-01",
		Path:  "src/does-not-exist.go",
	})
	_, err = registry.WorkspaceReadFile(ctx, reqNonExistent)
	if err == nil {
		t.Errorf("expected error for non-existent file, got nil")
	}
}

func TestHostFunctionDefinitionsAndHostRegistration(t *testing.T) {
	registry := NewHostFunctionRegistry(
		WithNamespace("extism:host/user"),
		WithDefaultRunID("run-main"),
	)

	fns := registry.HostFunctions()
	if len(fns) != 3 {
		t.Fatalf("expected 3 host functions, got %d", len(fns))
	}

	names := map[string]bool{}
	for _, fn := range fns {
		names[fn.Name] = true
		if fn.Namespace != "extism:host/user" {
			t.Errorf("expected namespace 'extism:host/user', got %q for %s", fn.Namespace, fn.Name)
		}
		if len(fn.Params) != 1 {
			t.Errorf("expected 1 param for %s, got %d", fn.Name, len(fn.Params))
		}
		if len(fn.Returns) != 1 {
			t.Errorf("expected 1 return for %s, got %d", fn.Name, len(fn.Returns))
		}
	}

	expectedNames := []string{"brain_query_rules", "log_event", "workspace_read_file"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("expected host function %q to be defined", name)
		}
	}

	// Register with Host
	h := NewHost()
	defer h.Close()

	registry.RegisterWithHost(h)
	if len(h.hostFunctions) != 3 {
		t.Errorf("expected host to have 3 registered functions, got %d", len(h.hostFunctions))
	}

	// Calling RegisterRegistry on Host
	h2 := NewHost()
	defer h2.Close()
	h2.RegisterRegistry(registry)
	if len(h2.hostFunctions) != 3 {
		t.Errorf("expected host2 to have 3 registered functions, got %d", len(h2.hostFunctions))
	}
}

func TestWithSandboxManagerOption(t *testing.T) {
	mgr := sandbox.NewManager(sandbox.Config{
		BaseWorktreeDir: t.TempDir(),
	})

	registry := NewHostFunctionRegistry(WithSandboxManager(mgr))
	if registry.worktreeResolver == nil {
		t.Fatal("expected worktreeResolver to be set by WithSandboxManager")
	}

	_, err := registry.resolveWorktreeRoot("non-existent-run")
	if err == nil {
		t.Errorf("expected error resolving non-existent run in sandbox manager")
	}

	// Custom resolver option
	customReg := NewHostFunctionRegistry(
		WithWorktreeResolver(func(runID string) (string, error) {
			if runID == "custom-run" {
				return "/tmp/custom", nil
			}
			return "", errors.New("unknown")
		}),
	)
	p, err := customReg.resolveWorktreeRoot("custom-run")
	if err != nil || p != "/tmp/custom" {
		t.Errorf("custom worktree resolver failed: %v", err)
	}

	// SetDefaultRunID and empty registers
	customReg.SetDefaultRunID("custom-default")
	if customReg.defaultRunID != "custom-default" {
		t.Errorf("expected defaultRunID 'custom-default', got %q", customReg.defaultRunID)
	}
	customReg.RegisterSecret("")
	customReg.RegisterWorktree("", "")

	// Nil context RunIDFromContext
	if RunIDFromContext(nil) != "" {
		t.Errorf("expected empty string for nil context")
	}

	// LogEvent without sink (stdout default logger)
	noSinkReg := NewHostFunctionRegistry()
	_, err = noSinkReg.LogEvent(context.Background(), []byte(`{"message":"hello world"}`))
	if err != nil {
		t.Errorf("unexpected error logging without sink: %v", err)
	}

	// Nil registrations
	noSinkReg.RegisterWithHost(nil)
	var nilHost *Host
	nilHost.RegisterRegistry(noSinkReg)
	h := NewHost()
	defer h.Close()
	h.RegisterRegistry(nil)
}
