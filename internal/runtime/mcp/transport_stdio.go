// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// StdioTransport implements Transport over stdin/stdout pipes, typically for a child process.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader

	mu     sync.Mutex
	closed bool
}

// StdioConfig defines configuration for starting a Stdio child process.
type StdioConfig struct {
	Command string
	Args    []string
	Env     []string
	Dir     string
}

// NewStdioTransport creates and starts a StdioTransport with a spawned child process.
func NewStdioTransport(cfg StdioConfig) (*StdioTransport, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp stdio: command cannot be empty")
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("mcp stdio: failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("mcp stdio: failed to start process: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}, nil
}

// NewStdioTransportFromPipes creates a StdioTransport from existing ReadCloser and WriteCloser.
func NewStdioTransportFromPipes(r io.ReadCloser, w io.WriteCloser) *StdioTransport {
	return &StdioTransport{
		stdin:  w,
		stdout: r,
		reader: bufio.NewReader(r),
	}
}

// Send serializes and writes a JSON-RPC request newline-delimited to the standard input.
func (t *StdioTransport) Send(ctx context.Context, req *Request) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrTransportClosed
	}
	if req == nil {
		return ErrNilRequest
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("mcp stdio: marshal error: %w", err)
	}
	data = append(data, '\n')

	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("mcp stdio: write error: %w", err)
	}
	return nil
}

// Receive reads the next newline-delimited JSON-RPC response or notification.
func (t *StdioTransport) Receive(ctx context.Context) (*Response, error) {
	type readResult struct {
		line []byte
		err  error
	}

	readCh := make(chan readResult, 1)

	go func() {
		// ReadSlice / ReadBytes until newline
		line, err := t.reader.ReadBytes('\n')
		readCh <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-readCh:
		if res.err != nil {
			if errorsIsEOF(res.err) && len(res.line) == 0 {
				return nil, ErrTransportClosed
			}
			if len(res.line) == 0 {
				return nil, res.err
			}
		}

		var resp Response
		if err := json.Unmarshal(res.line, &resp); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
		return &resp, nil
	}
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

// Close closes the streams and terminates the child process if running.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	var firstErr error
	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if t.stdout != nil {
		if err := t.stdout.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if t.cmd != nil && t.cmd.Process != nil {
		// Kill process if still running
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}

	return firstErr
}
