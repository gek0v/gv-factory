// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSanitizeIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid alphanumeric", "sandbox123", false},
		{"valid with hyphen and underscore", "run-abc_123", false},
		{"empty string", "", true},
		{"only whitespace", "   ", true},
		{"path traversal with dot dot slash", "../etc/passwd", true},
		{"path traversal with backslash", "..\\windows\\system32", true},
		{"contains forward slash", "dir/run1", true},
		{"contains backslash", "dir\\run1", true},
		{"contains dot dot", "run..id", true},
		{"special characters", "run$id*", true},
		{"spaces in between", "run 123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeIdentifier(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("SanitizeIdentifier(%q) expected error, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("SanitizeIdentifier(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.wantErr && got != tt.input {
				t.Errorf("SanitizeIdentifier(%q) = %q, want %q", tt.input, got, tt.input)
			}
		})
	}
}

func TestResolveSandboxPath(t *testing.T) {
	tempBase := t.TempDir()
	tempWt := t.TempDir()

	opts := TerminalOptions{
		BaseDir:         tempBase,
		WorktreeDir:     tempWt,
		RateLimitPerSec: 100,
		Burst:           50,
	}
	th := NewTerminalHandler(opts)

	// Case 1: Fresh sandbox created under BaseDir
	path, err := th.ResolveSandboxPath("sandbox-fresh-1")
	if err != nil {
		t.Fatalf("ResolveSandboxPath failed: %v", err)
	}
	if !strings.HasPrefix(path, tempBase) {
		t.Errorf("expected path %q to have prefix %q", path, tempBase)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Errorf("expected path %q to exist as directory", path)
	}

	// Case 2: Existing worktree in WorktreeDir
	existingWt := filepath.Join(tempWt, "wt-existing-1")
	if err := os.MkdirAll(existingWt, 0755); err != nil {
		t.Fatal(err)
	}
	wtPath, err := th.ResolveSandboxPath("wt-existing-1")
	if err != nil {
		t.Fatalf("ResolveSandboxPath for existing worktree failed: %v", err)
	}
	if wtPath != existingWt {
		t.Errorf("expected path %q, got %q", existingWt, wtPath)
	}

	// Case 3: Path traversal rejection
	_, err = th.ResolveSandboxPath("../escaped")
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(10, 3)

	// First 3 should pass due to burst
	for i := 0; i < 3; i++ {
		if !rl.Allow() {
			t.Fatalf("expected token %d to be allowed", i)
		}
	}

	// Next immediate should be rejected
	if rl.Allow() {
		t.Fatalf("expected rate limiter to reject after burst exhausted")
	}

	// Wait 250ms -> should refill ~2.5 tokens
	time.Sleep(250 * time.Millisecond)
	if !rl.Allow() {
		t.Fatalf("expected rate limiter to allow after refill")
	}
}

func readUntilSubstring(ws *websocket.Conn, needle string, timeout time.Duration) (string, error) {
	var sb strings.Builder
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, b, err := ws.ReadMessage()
		if err == nil {
			sb.Write(b)
			if strings.Contains(sb.String(), needle) {
				return sb.String(), nil
			}
		}
	}
	return sb.String(), fmt.Errorf("timeout waiting for %q in output. Received so far: %q", needle, sb.String())
}

func TestTerminalWebSocket_InteractiveLifecycle(t *testing.T) {
	tempBase := t.TempDir()
	opts := TerminalOptions{
		BaseDir:         tempBase,
		RateLimitPerSec: 100,
		Burst:           50,
		Upgrader:        defaultTerminalUpgrader,
	}
	th := NewTerminalHandler(opts)

	mux := http.NewServeMux()
	RegisterTerminalRoutes(mux, th)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/test-run-1/terminal"

	// 1. Path traversal attempt via HTTP endpoint
	badURL := server.URL + "/api/v1/sandboxes/..%2Fbad/terminal"
	resp, err := http.Get(badURL)
	if err == nil {
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 403 or 404 for bad path, got %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// 2. Connect valid WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer ws.Close()

	// 3. Read banner
	banner, err := readUntilSubstring(ws, "⚡ GV-Factory Sandbox Interactive Terminal", 4*time.Second)
	if err != nil {
		t.Fatalf("failed reading banner: %v", err)
	}
	if !strings.Contains(banner, "test-run-1") {
		t.Errorf("banner missing sandbox id, got: %s", banner)
	}

	// 4. Test 'help' command
	if err := ws.WriteMessage(websocket.TextMessage, []byte("help\r\n")); err != nil {
		t.Fatalf("failed to write help: %v", err)
	}
	if _, err := readUntilSubstring(ws, "Available Inspection Commands", 3*time.Second); err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	// 5. Test 'pwd' command
	if err := ws.WriteMessage(websocket.TextMessage, []byte("pwd\r\n")); err != nil {
		t.Fatalf("failed to write pwd: %v", err)
	}
	if _, err := readUntilSubstring(ws, "test-run-1", 3*time.Second); err != nil {
		t.Fatalf("pwd command failed: %v", err)
	}

	// 6. Test 'echo hello-sandbox'
	if err := ws.WriteMessage(websocket.TextMessage, []byte("echo hello-sandbox\r\n")); err != nil {
		t.Fatalf("failed to write echo: %v", err)
	}
	if _, err := readUntilSubstring(ws, "hello-sandbox", 3*time.Second); err != nil {
		t.Fatalf("echo command failed: %v", err)
	}

	// 7. Test unauthorized command rejection
	if err := ws.WriteMessage(websocket.TextMessage, []byte("curl http://example.com\r\n")); err != nil {
		t.Fatalf("failed to write curl: %v", err)
	}
	if _, err := readUntilSubstring(ws, "not permitted", 3*time.Second); err != nil {
		t.Fatalf("unauthorized command failed: %v", err)
	}

	// 8. Test 'clear' command
	if err := ws.WriteMessage(websocket.TextMessage, []byte("clear\r\n")); err != nil {
		t.Fatalf("failed to write clear: %v", err)
	}
	if _, err := readUntilSubstring(ws, "\x1b[2J", 3*time.Second); err != nil {
		t.Fatalf("clear command failed: %v", err)
	}

	// 9. Test JSON command execution
	cmdJSON, _ := json.Marshal(TerminalMessage{
		Type: "command",
		Cmd:  "pwd",
	})
	if err := ws.WriteMessage(websocket.TextMessage, cmdJSON); err != nil {
		t.Fatalf("failed to write JSON cmd: %v", err)
	}
	if _, err := readUntilSubstring(ws, "test-run-1", 3*time.Second); err != nil {
		t.Fatalf("JSON command failed: %v", err)
	}

	// 10. Test JSON ping / pong
	pingJSON, _ := json.Marshal(TerminalMessage{Type: "ping"})
	if err := ws.WriteMessage(websocket.TextMessage, pingJSON); err != nil {
		t.Fatalf("failed to write ping: %v", err)
	}
	if _, err := readUntilSubstring(ws, `"type":"pong"`, 3*time.Second); err != nil {
		t.Fatalf("ping/pong failed: %v", err)
	}

	// 11. Test interactive editing (backspace & Ctrl+C)
	_ = ws.WriteMessage(websocket.TextMessage, []byte("a"))
	_ = ws.WriteMessage(websocket.TextMessage, []byte("\x7f")) // backspace
	_ = ws.WriteMessage(websocket.TextMessage, []byte("\x03")) // Ctrl+C
	if _, err := readUntilSubstring(ws, "^C", 3*time.Second); err != nil {
		t.Fatalf("interactive editing failed: %v", err)
	}

	// 12. Verify active session count
	if th.ActiveSessionsCount() != 1 {
		t.Errorf("expected 1 active session, got %d", th.ActiveSessionsCount())
	}

	// 13. Test 'exit' command
	_ = ws.WriteMessage(websocket.TextMessage, []byte("exit\r\n"))
	time.Sleep(100 * time.Millisecond)

	// Verify session cleaned up
	if th.ActiveSessionsCount() != 0 {
		t.Errorf("expected 0 active sessions after exit, got %d", th.ActiveSessionsCount())
	}
}

func TestTerminal_cdDirectoryNavigation(t *testing.T) {
	tempBase := t.TempDir()
	opts := TerminalOptions{
		BaseDir:         tempBase,
		RateLimitPerSec: 100,
		Burst:           50,
		Upgrader:        defaultTerminalUpgrader,
	}
	th := NewTerminalHandler(opts)

	// Create a subdirectory inside sandbox
	subDir := filepath.Join(tempBase, "sub-run", "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	RegisterTerminalRoutes(mux, th)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/sub-run/terminal"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()

	// Drain banner
	if _, err := readUntilSubstring(ws, "sub-run", 4*time.Second); err != nil {
		t.Fatalf("banner missing sub-run: %v", err)
	}

	// cd into src
	_ = ws.WriteMessage(websocket.TextMessage, []byte("cd src\r\n"))
	if _, err := readUntilSubstring(ws, "src", 3*time.Second); err != nil {
		t.Fatalf("cd src failed: %v", err)
	}

	// cd .. outside sandbox root should be rejected
	_ = ws.WriteMessage(websocket.TextMessage, []byte("cd ../..\r\n"))
	if _, err := readUntilSubstring(ws, "access denied", 3*time.Second); err != nil {
		t.Fatalf("cd traversal rejection failed: %v", err)
	}

	// Close all
	th.CloseAll()
	if th.ActiveSessionsCount() != 0 {
		t.Errorf("expected 0 sessions after CloseAll, got %d", th.ActiveSessionsCount())
	}
}

func TestTerminal_NewTerminalHandlerDefaults(t *testing.T) {
	th := NewTerminalHandler(TerminalOptions{})
	if th.options.BaseDir == "" {
		t.Error("expected default BaseDir to be populated")
	}
	if th.options.RateLimitPerSec != 60 {
		t.Errorf("expected default rate limit 60, got %d", th.options.RateLimitPerSec)
	}
	if th.options.Burst != 30 {
		t.Errorf("expected default burst 30, got %d", th.options.Burst)
	}
	if th.options.AllowedCommands == nil {
		t.Error("expected default AllowedCommands to be populated")
	}
}

func TestTerminal_cdNonExistent(t *testing.T) {
	tempBase := t.TempDir()
	opts := TerminalOptions{
		BaseDir:         tempBase,
		RateLimitPerSec: 100,
		Burst:           50,
		Upgrader:        defaultTerminalUpgrader,
	}
	th := NewTerminalHandler(opts)

	mux := http.NewServeMux()
	RegisterTerminalRoutes(mux, th)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/cd-none/terminal"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()

	if _, err := readUntilSubstring(ws, "cd-none", 4*time.Second); err != nil {
		t.Fatalf("banner missing: %v", err)
	}

	_ = ws.WriteMessage(websocket.TextMessage, []byte("cd nonexistent_dir\r\n"))
	if _, err := readUntilSubstring(ws, "no such file or directory", 3*time.Second); err != nil {
		t.Fatalf("expected no such file or directory: %v", err)
	}
}

func TestServer_RunsTerminalRoute(t *testing.T) {
	mux := http.NewServeMux()
	th := NewTerminalHandler(DefaultTerminalOptions())
	RegisterTerminalRoutes(mux, th)

	// Check that /api/v1/sandboxes/404 handles unknown path properly
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/run-1/unknown", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown subpath, got %d", w.Code)
	}
}

func TestTerminal_CtrlD_And_Empty(t *testing.T) {
	tempBase := t.TempDir()
	opts := TerminalOptions{
		BaseDir:         tempBase,
		RateLimitPerSec: 100,
		Burst:           50,
		Upgrader:        defaultTerminalUpgrader,
	}
	th := NewTerminalHandler(opts)

	mux := http.NewServeMux()
	RegisterTerminalRoutes(mux, th)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/ctrld-run/terminal"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()

	if _, err := readUntilSubstring(ws, "ctrld-run", 4*time.Second); err != nil {
		t.Fatalf("banner missing: %v", err)
	}

	// Empty Enter
	_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n"))
	if _, err := readUntilSubstring(ws, "$ ", 2*time.Second); err != nil {
		t.Fatalf("empty enter failed: %v", err)
	}

	// Ctrl+L (clear screen)
	_ = ws.WriteMessage(websocket.TextMessage, []byte("\x0c"))
	if _, err := readUntilSubstring(ws, "\x1b[2J", 2*time.Second); err != nil {
		t.Fatalf("ctrl-L failed: %v", err)
	}

	// Ctrl+D (exit)
	_ = ws.WriteMessage(websocket.TextMessage, []byte("\x04"))
	if _, err := readUntilSubstring(ws, "logout", 2*time.Second); err != nil {
		t.Fatalf("ctrl-d logout failed: %v", err)
	}
}

func TestTerminal_RateLimitExceeded(t *testing.T) {
	tempBase := t.TempDir()
	opts := TerminalOptions{
		BaseDir:         tempBase,
		RateLimitPerSec: 1,
		Burst:           1,
		Upgrader:        defaultTerminalUpgrader,
	}
	th := NewTerminalHandler(opts)

	mux := http.NewServeMux()
	RegisterTerminalRoutes(mux, th)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/sandboxes/ratelimit-run/terminal"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ws.Close()

	if _, err := readUntilSubstring(ws, "ratelimit-run", 4*time.Second); err != nil {
		t.Fatalf("banner missing: %v", err)
	}

	// Burst 1 token is consumed by first message, 2nd and 3rd immediately trigger rate limit
	_ = ws.WriteMessage(websocket.TextMessage, []byte("pwd\r\n"))
	_ = ws.WriteMessage(websocket.TextMessage, []byte("pwd\r\n"))
	_ = ws.WriteMessage(websocket.TextMessage, []byte("pwd\r\n"))

	if _, err := readUntilSubstring(ws, "Rate limit exceeded", 3*time.Second); err != nil {
		t.Fatalf("expected rate limit exceeded warning: %v", err)
	}
}
