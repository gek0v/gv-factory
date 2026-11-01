// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type pipeReadWriteCloser struct {
	io.ReadCloser
	io.WriteCloser
}

func (p *pipeReadWriteCloser) Close() error {
	err1 := p.ReadCloser.Close()
	err2 := p.WriteCloser.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func TestStdioTransportFromPipes(t *testing.T) {
	// Setup two-way communication between mock client and mock server using pipes
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	clientTransport := NewStdioTransportFromPipes(clientR, clientW)
	defer clientTransport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Server goroutine to mimic an MCP server answering ping
	go func() {
		decoder := json.NewDecoder(serverR)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return
		}
		if req.Method == MethodPing {
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			}
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			_, _ = serverW.Write(data)
		}
	}()

	req := &Request{
		JSONRPC: JSONRPCVersion,
		ID:      NewIntID(1),
		Method:  MethodPing,
	}

	if err := clientTransport.Send(ctx, req); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	resp, err := clientTransport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	if resp.ID == nil || resp.ID.NumVal != 1 {
		t.Errorf("Response ID mismatch: %+v", resp.ID)
	}
}

func TestStdioTransportWithChildProcess(t *testing.T) {
	// Create a temporary mock executable script or binary
	// Since go test is available, we can compile a mini mock server or use a go run script.
	tempDir := t.TempDir()
	mockSrc := `package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type Msg struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      any             ` + "`json:\"id,omitempty\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params,omitempty\"`" + `
}

type Resp struct {
	JSONRPC string ` + "`json:\"jsonrpc\"`" + `
	ID      any    ` + "`json:\"id\"`" + `
	Result  any    ` + "`json:\"result\"`" + `
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		var msg Msg
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Method == "initialize" {
			r := Resp{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]string{
						"name":    "mock-server",
						"version": "1.0",
					},
					"capabilities": map[string]any{
						"tools": map[string]any{},
					},
				},
			}
			b, _ := json.Marshal(r)
			os.Stdout.Write(append(b, '\n'))
		} else if msg.Method == "tools/list" {
			r := Resp{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Result: map[string]any{
					"tools": []map[string]any{
						{
							"name":        "mock_tool",
							"description": "A mock tool",
							"inputSchema": map[string]any{
								"type": "object",
							},
						},
					},
				},
			}
			b, _ := json.Marshal(r)
			os.Stdout.Write(append(b, '\n'))
		} else if msg.Method == "tools/call" {
			r := Resp{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Result: map[string]any{
					"content": []map[string]any{
						{
							"type": "text",
							"text": "mock execution success",
						},
					},
					"isError": false,
				},
			}
			b, _ := json.Marshal(r)
			os.Stdout.Write(append(b, '\n'))
		}
	}
}
`
	srcFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(srcFile, []byte(mockSrc), 0644); err != nil {
		t.Fatalf("Failed to write mock src: %v", err)
	}

	transport, err := NewStdioTransport(StdioConfig{
		Command: "go",
		Args:    []string{"run", srcFile},
	})
	if err != nil {
		t.Fatalf("NewStdioTransport failed: %v", err)
	}
	defer transport.Close()

	client := NewClient(transport)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Handshake Initialize
	initResult, err := client.Initialize(ctx, ImplementationInfo{
		Name:    "test-client",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if initResult.ServerInfo.Name != "mock-server" {
		t.Errorf("Expected server mock-server, got %s", initResult.ServerInfo.Name)
	}

	// 2. Tools list
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "mock_tool" {
		t.Errorf("Tools mismatch: %+v", tools)
	}

	// 3. Call tool
	res, err := client.CallTool(ctx, "mock_tool", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if res.TextContent() != "mock execution success" {
		t.Errorf("Unexpected tool output: %s", res.TextContent())
	}
}

func TestStdioTransportErrors(t *testing.T) {
	// Empty command error
	_, err := NewStdioTransport(StdioConfig{Command: ""})
	if err == nil {
		t.Errorf("Expected error for empty command")
	}

	// Non-existent command
	_, err = NewStdioTransport(StdioConfig{Command: "non_existent_binary_xyz_12345"})
	if err == nil {
		t.Errorf("Expected error for non-existent binary")
	}

	// Closed transport error
	clientR, _ := io.Pipe()
	_, clientW := io.Pipe()
	tr := NewStdioTransportFromPipes(clientR, clientW)
	_ = tr.Close()

	ctx := context.Background()
	err = tr.Send(ctx, &Request{})
	if err != ErrTransportClosed {
		t.Errorf("Expected ErrTransportClosed on Send, got %v", err)
	}

	// Nil request error
	trOpen := NewStdioTransportFromPipes(clientR, clientW)
	if err := trOpen.Send(ctx, nil); err != ErrNilRequest {
		t.Errorf("Expected ErrNilRequest, got %v", err)
	}
	_ = trOpen.Close()
}
