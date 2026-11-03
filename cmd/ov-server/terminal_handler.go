// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	// validIDRegex ensures sandbox/run IDs only contain alphanumeric, dash, and underscore characters.
	validIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

	// defaultAllowedCommands defines safe inspection commands permitted in the sandbox.
	defaultAllowedCommands = map[string]bool{
		"ls":         true,
		"pwd":        true,
		"cat":        true,
		"git":        true,
		"go":         true,
		"echo":       true,
		"head":       true,
		"tail":       true,
		"grep":       true,
		"find":       true,
		"stat":       true,
		"whoami":     true,
		"env":        true,
		"tree":       true,
		"date":       true,
		"help":       true,
		"exit":       true,
		"quit":       true,
		"clear":      true,
		"cd":         true,
	}
)

var defaultTerminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow cross-origin for development & embedded UI
	},
}

// TerminalOptions configures the sandbox terminal handler.
type TerminalOptions struct {
	BaseDir         string
	WorktreeDir     string
	RateLimitPerSec int
	Burst           int
	AllowedCommands map[string]bool
	Upgrader        websocket.Upgrader
}

// DefaultTerminalOptions returns standard options for the terminal handler.
func DefaultTerminalOptions() TerminalOptions {
	baseDir := os.Getenv("OV_SANDBOX_DIR")
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "ov-sandboxes")
	}

	worktreeDir := os.Getenv("OV_WORKTREE_DIR")
	if worktreeDir == "" {
		worktreeDir = filepath.Join(os.TempDir(), "ov-worktrees")
	}

	return TerminalOptions{
		BaseDir:         baseDir,
		WorktreeDir:     worktreeDir,
		RateLimitPerSec: 60,
		Burst:           30,
		AllowedCommands: defaultAllowedCommands,
		Upgrader:        defaultTerminalUpgrader,
	}
}

// TerminalMessage represents an incoming or outgoing WebSocket message.
type TerminalMessage struct {
	Type string `json:"type"` // "stdin", "stdout", "stderr", "command", "resize", "ping", "pong", "error", "exit"
	Data string `json:"data,omitempty"`
	Cmd  string `json:"cmd,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// rateLimiter implements a simple token-bucket rate limiter.
type rateLimiter struct {
	mu         sync.Mutex
	rate       float64
	capacity   float64
	tokens     float64
	lastUpdate time.Time
}

func newRateLimiter(ratePerSec int, burst int) *rateLimiter {
	return &rateLimiter{
		rate:       float64(ratePerSec),
		capacity:   float64(burst),
		tokens:     float64(burst),
		lastUpdate: time.Now(),
	}
}

func (rl *rateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastUpdate).Seconds()
	rl.lastUpdate = now

	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.capacity {
		rl.tokens = rl.capacity
	}

	if rl.tokens >= 1.0 {
		rl.tokens -= 1.0
		return true
	}
	return false
}

// TerminalSession manages an active WebSocket terminal connection.
type TerminalSession struct {
	ID         string
	SandboxDir string
	CurrentDir string
	Conn       *websocket.Conn
	Options    TerminalOptions
	limiter    *rateLimiter
	writeMu    sync.Mutex
	cancelFunc context.CancelFunc
	ctx        context.Context
}

// TerminalHandler manages WebSocket terminal sessions for sandboxes.
type TerminalHandler struct {
	mu       sync.RWMutex
	options  TerminalOptions
	sessions map[string]*TerminalSession
}

// NewTerminalHandler creates a new TerminalHandler with the given options.
func NewTerminalHandler(opts TerminalOptions) *TerminalHandler {
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Join(os.TempDir(), "ov-sandboxes")
	}
	if opts.RateLimitPerSec <= 0 {
		opts.RateLimitPerSec = 60
	}
	if opts.Burst <= 0 {
		opts.Burst = 30
	}
	if opts.AllowedCommands == nil {
		opts.AllowedCommands = defaultAllowedCommands
	}

	return &TerminalHandler{
		options:  opts,
		sessions: make(map[string]*TerminalSession),
	}
}

// SanitizeIdentifier validates that an identifier is safe against path traversal.
func SanitizeIdentifier(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", errors.New("empty identifier")
	}
	if strings.Contains(trimmed, "..") || strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("invalid identifier %q: path traversal attempt detected", trimmed)
	}
	if !validIDRegex.MatchString(trimmed) {
		return "", fmt.Errorf("invalid identifier %q: must contain only alphanumeric, dash or underscore", trimmed)
	}
	return trimmed, nil
}

// ResolveSandboxPath resolves and validates the absolute directory for a sandbox.
func (th *TerminalHandler) ResolveSandboxPath(id string) (string, error) {
	safeID, err := SanitizeIdentifier(id)
	if err != nil {
		return "", err
	}

	// 1. Check if it exists in base sandbox dir
	baseAbs, err := filepath.Abs(th.options.BaseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base dir: %w", err)
	}

	targetBase := filepath.Clean(filepath.Join(baseAbs, safeID))
	if !isSubdirectory(baseAbs, targetBase) {
		return "", errors.New("path traversal detected")
	}

	// 2. Check if it exists in worktree dir
	if th.options.WorktreeDir != "" {
		wtAbs, err := filepath.Abs(th.options.WorktreeDir)
		if err == nil {
			targetWt := filepath.Clean(filepath.Join(wtAbs, safeID))
			if isSubdirectory(wtAbs, targetWt) {
				if info, statErr := os.Stat(targetWt); statErr == nil && info.IsDir() {
					return targetWt, nil
				}
			}
		}
	}

	// 3. Fallback to base sandbox dir, ensuring it exists
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return "", fmt.Errorf("failed to create sandbox directory: %w", err)
	}

	return targetBase, nil
}

func isSubdirectory(base, target string) bool {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}

// ServeTerminalHTTP handles the WebSocket connection for a given sandbox/run ID.
func (th *TerminalHandler) ServeTerminalHTTP(w http.ResponseWriter, r *http.Request, id string) {
	sandboxPath, err := th.ResolveSandboxPath(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Forbidden: %v", err), http.StatusForbidden)
		return
	}

	conn, err := th.options.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote error response
		return
	}

	sessionCtx, sessionCancel := context.WithCancel(r.Context())
	session := &TerminalSession{
		ID:         id,
		SandboxDir: sandboxPath,
		CurrentDir: sandboxPath,
		Conn:       conn,
		Options:    th.options,
		limiter:    newRateLimiter(th.options.RateLimitPerSec, th.options.Burst),
		cancelFunc: sessionCancel,
		ctx:        sessionCtx,
	}

	sessionKey := fmt.Sprintf("%s-%d", id, time.Now().UnixNano())
	th.mu.Lock()
	th.sessions[sessionKey] = session
	th.mu.Unlock()

	defer func() {
		sessionCancel()
		_ = conn.Close()
		th.mu.Lock()
		delete(th.sessions, sessionKey)
		th.mu.Unlock()
	}()

	session.run()
}

// SendOutput writes raw or formatted terminal output to the client.
func (s *TerminalSession) SendOutput(text string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.Conn.WriteMessage(websocket.TextMessage, []byte(text))
}

// SendJSON writes a structured message to the client.
func (s *TerminalSession) SendJSON(msg TerminalMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.Conn.WriteJSON(msg)
}

func (s *TerminalSession) run() {
	// Print interactive welcome banner
	banner := fmt.Sprintf("\r\n\x1b[1;36m⚡ GV-Factory Sandbox Interactive Terminal\x1b[0m\r\n"+
		"\x1b[90mSandbox ID:\x1b[0m  %s\r\n"+
		"\x1b[90mDirectory:\x1b[0m   %s\r\n"+
		"\x1b[90mAllowed cmds:\x1b[0m ls, pwd, cat, git, go, echo, head, tail, grep, find, stat, env, clear, exit\r\n"+
		"\x1b[33mType 'help' for inspection details or 'exit' to quit.\x1b[0m\r\n\r\n$ ",
		s.ID, s.CurrentDir)
	_ = s.SendOutput(banner)

	var lineBuffer []byte
	var currentCmdCancel context.CancelFunc
	var cmdMu sync.Mutex

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		msgType, payload, err := s.Conn.ReadMessage()
		if err != nil {
			return
		}

		// Rate limiting check
		if !s.limiter.Allow() {
			_ = s.SendOutput("\r\n\x1b[31m[Rate limit exceeded. Throttling input...]\x1b[0m\r\n$ ")
			continue
		}

		if msgType == websocket.TextMessage || msgType == websocket.BinaryMessage {
			rawText := string(payload)

			// Check if message is a JSON envelope
			if strings.HasPrefix(strings.TrimSpace(rawText), "{") {
				var msg TerminalMessage
				if jsonErr := json.Unmarshal(payload, &msg); jsonErr == nil {
					switch msg.Type {
					case "command":
						s.executeCommandLine(msg.Cmd, &cmdMu, &currentCmdCancel)
						continue
					case "resize":
						// Acknowledge resize
						continue
					case "ping":
						_ = s.SendJSON(TerminalMessage{Type: "pong"})
						continue
					case "stdin":
						rawText = msg.Data
					}
				}
			}

			// Process interactive character stream
			for i := 0; i < len(rawText); i++ {
				ch := rawText[i]

				switch ch {
				case '\r', '\n':
					// Execute line
					_ = s.SendOutput("\r\n")
					cmdStr := strings.TrimSpace(string(lineBuffer))
					lineBuffer = lineBuffer[:0]

					if cmdStr != "" {
						s.executeCommandLine(cmdStr, &cmdMu, &currentCmdCancel)
					} else {
						_ = s.SendOutput("$ ")
					}

				case '\x03': // Ctrl+C
					cmdMu.Lock()
					if currentCmdCancel != nil {
						currentCmdCancel()
						currentCmdCancel = nil
					}
					cmdMu.Unlock()
					lineBuffer = lineBuffer[:0]
					_ = s.SendOutput("^C\r\n$ ")

				case '\x04': // Ctrl+D
					if len(lineBuffer) == 0 {
						_ = s.SendOutput("\r\nlogout\r\n")
						return
					}

				case '\x7f', '\b': // Backspace
					if len(lineBuffer) > 0 {
						lineBuffer = lineBuffer[:len(lineBuffer)-1]
						_ = s.SendOutput("\b \b")
					}

				case '\x0c': // Ctrl+L (clear screen)
					_ = s.SendOutput("\x1b[2J\x1b[H$ " + string(lineBuffer))

				default:
					// Printable character
					if ch >= 32 {
						lineBuffer = append(lineBuffer, ch)
						_ = s.SendOutput(string(ch))
					}
				}
			}
		}
	}
}

func (s *TerminalSession) executeCommandLine(cmdStr string, cmdMu *sync.Mutex, currentCancel *context.CancelFunc) {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		_ = s.SendOutput("$ ")
		return
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		_ = s.SendOutput("$ ")
		return
	}

	cmdName := parts[0]

	// Builtin commands
	switch cmdName {
	case "exit", "quit":
		_ = s.SendOutput("Session terminated.\r\n")
		s.cancelFunc()
		return

	case "clear":
		_ = s.SendOutput("\x1b[2J\x1b[H$ ")
		return

	case "help":
		helpText := "\r\n\x1b[1mAvailable Inspection Commands:\x1b[0m\r\n" +
			"  \x1b[32mpwd\x1b[0m                     Print current sandbox directory\r\n" +
			"  \x1b[32mcd <dir>\x1b[0m                Navigate within the sandbox\r\n" +
			"  \x1b[32mls [-la] [dir]\x1b[0m          List files in sandbox\r\n" +
			"  \x1b[32mcat <file>\x1b[0m              Display file contents\r\n" +
			"  \x1b[32mgit status\x1b[0m              Inspect git status of worktree\r\n" +
			"  \x1b[32mgit diff\x1b[0m                Show changes in worktree\r\n" +
			"  \x1b[32mgit log [-n N]\x1b[0m          Inspect recent commits\r\n" +
			"  \x1b[32mgo test [-v] [path]\x1b[0m     Run tests in sandbox\r\n" +
			"  \x1b[32mgo version\x1b[0m              Display Go toolchain version\r\n" +
			"  \x1b[32mclear\x1b[0m                   Clear terminal screen\r\n" +
			"  \x1b[32mexit\x1b[0m                    Disconnect from sandbox\r\n\r\n$ "
		_ = s.SendOutput(helpText)
		return

	case "cd":
		target := "."
		if len(parts) > 1 {
			target = parts[1]
		}
		newDir := filepath.Clean(filepath.Join(s.CurrentDir, target))
		if !isSubdirectory(s.SandboxDir, newDir) {
			_ = s.SendOutput("\x1b[31mcd: access denied: cannot navigate outside sandbox root\x1b[0m\r\n$ ")
			return
		}
		info, err := os.Stat(newDir)
		if err != nil || !info.IsDir() {
			_ = s.SendOutput(fmt.Sprintf("\x1b[31mcd: no such file or directory: %s\x1b[0m\r\n$ ", target))
			return
		}
		s.CurrentDir = newDir
		_ = s.SendOutput(fmt.Sprintf("\x1b[90m%s\x1b[0m\r\n$ ", s.CurrentDir))
		return

	case "pwd":
		_ = s.SendOutput(fmt.Sprintf("%s\r\n$ ", s.CurrentDir))
		return
	}

	// Verify allowed inspection command
	if !s.Options.AllowedCommands[cmdName] {
		_ = s.SendOutput(fmt.Sprintf("\x1b[31mCommand %q not permitted. Only safe inspection commands are allowed (type 'help').\x1b[0m\r\n$ ", cmdName))
		return
	}

	// Execute command safely in CurrentDir
	cmdCtx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	cmdMu.Lock()
	*currentCancel = cancel
	cmdMu.Unlock()

	defer func() {
		cancel()
		cmdMu.Lock()
		*currentCancel = nil
		cmdMu.Unlock()
	}()

	cmd := exec.CommandContext(cmdCtx, cmdName, parts[1:]...)
	cmd.Dir = s.CurrentDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = s.SendOutput(fmt.Sprintf("\x1b[31mError creating stdout pipe: %v\x1b[0m\r\n$ ", err))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = s.SendOutput(fmt.Sprintf("\x1b[31mError creating stderr pipe: %v\x1b[0m\r\n$ ", err))
		return
	}

	if err := cmd.Start(); err != nil {
		_ = s.SendOutput(fmt.Sprintf("\x1b[31mError starting command %s: %v\x1b[0m\r\n$ ", cmdName, err))
		return
	}

	var streamWg sync.WaitGroup
	streamOutput := func(r io.Reader, isErr bool) {
		defer streamWg.Done()
		scanner := bufio.NewScanner(r)
		// Set large buffer for verbose command output
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			var formatted string
			if isErr {
				formatted = fmt.Sprintf("\x1b[31m%s\x1b[0m\r\n", line)
			} else {
				formatted = fmt.Sprintf("%s\r\n", line)
			}
			_ = s.SendOutput(formatted)
		}
	}

	streamWg.Add(2)
	go streamOutput(stdoutPipe, false)
	go streamOutput(stderrPipe, true)

	streamWg.Wait()
	waitErr := cmd.Wait()

	if waitErr != nil {
		if errors.Is(cmdCtx.Err(), context.Canceled) {
			_ = s.SendOutput("\x1b[33m[Command interrupted]\x1b[0m\r\n")
		} else if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			_ = s.SendOutput("\x1b[31m[Command timed out after 60s]\x1b[0m\r\n")
		} else {
			_ = s.SendOutput(fmt.Sprintf("\x1b[90m[Exit code: %v]\x1b[0m\r\n", waitErr))
		}
	}

	_ = s.SendOutput("$ ")
}

// ActiveSessionsCount returns the current count of connected terminal sessions.
func (th *TerminalHandler) ActiveSessionsCount() int {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return len(th.sessions)
}

// CloseAll terminates all active sessions gracefully.
func (th *TerminalHandler) CloseAll() {
	th.mu.Lock()
	defer th.mu.Unlock()
	for _, sess := range th.sessions {
		sess.cancelFunc()
		_ = sess.Conn.Close()
	}
	th.sessions = make(map[string]*TerminalSession)
}

// RegisterTerminalRoutes mounts the terminal endpoints onto the provided http.ServeMux.
func RegisterTerminalRoutes(mux *http.ServeMux, th *TerminalHandler) {
	mux.HandleFunc("/api/v1/sandboxes/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/sandboxes/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 2 && parts[1] == "terminal" {
			th.ServeTerminalHTTP(w, r, parts[0])
			return
		}
		http.NotFound(w, r)
	})
}
