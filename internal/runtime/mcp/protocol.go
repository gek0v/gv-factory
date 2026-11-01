// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Standard JSON-RPC 2.0 version.
const JSONRPCVersion = "2.0"

// Standard MCP Protocol versions and methods.
const (
	LatestProtocolVersion = "2024-11-05"

	MethodInitialize   = "initialize"
	MethodInitialized  = "notifications/initialized"
	MethodPing         = "ping"
	MethodToolsList    = "tools/list"
	MethodToolsCall    = "tools/call"
	MethodLoggingSet   = "logging/setLevel"
)

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

var (
	ErrClientClosed            = errors.New("mcp: client is closed")
	ErrTransportClosed         = errors.New("mcp: transport closed")
	ErrTimeout                 = errors.New("mcp: operation timed out")
	ErrToolNotFound            = errors.New("mcp: tool not found")
	ErrServerNotFound          = errors.New("mcp: server not found")
	ErrExecutionLimit          = errors.New("mcp: execution limit exceeded")
	ErrInvalidResponse         = errors.New("mcp: invalid response")
	ErrNilRequest              = errors.New("mcp: nil request")
	ErrToolExecutionRejected   = errors.New("mcp: tool execution rejected by gate approval")
	ErrToolApprovalTimeout     = errors.New("mcp: tool approval timed out")
	ErrMissingExecutionContext = errors.New("mcp: missing execution context (run_id) for protected tool")
	ErrMissingAuthService      = errors.New("mcp: auth service required for tool approval gate")
	ErrMissingGateway          = errors.New("mcp: gateway required for tool approval gate")
)

// Request represents a JSON-RPC 2.0 request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID      `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification returns true if the request has no ID.
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// RequestID represents a JSON-RPC 2.0 request ID (can be number or string).
type RequestID struct {
	NumVal uint64
	StrVal string
	IsStr  bool
}

// NewIntID creates a numeric request ID.
func NewIntID(id uint64) *RequestID {
	return &RequestID{NumVal: id, IsStr: false}
}

// NewStringID creates a string request ID.
func NewStringID(id string) *RequestID {
	return &RequestID{StrVal: id, IsStr: true}
}

func (id RequestID) MarshalJSON() ([]byte, error) {
	if id.IsStr {
		return json.Marshal(id.StrVal)
	}
	return json.Marshal(id.NumVal)
}

func (id *RequestID) UnmarshalJSON(data []byte) error {
	var num uint64
	if err := json.Unmarshal(data, &num); err == nil {
		id.NumVal = num
		id.IsStr = false
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		id.StrVal = str
		id.IsStr = true
		return nil
	}
	return fmt.Errorf("invalid request id: %s", string(data))
}

func (id RequestID) String() string {
	if id.IsStr {
		return id.StrVal
	}
	return fmt.Sprintf("%d", id.NumVal)
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID      `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("jsonrpc error (code %d): %s", e.Code, e.Message)
}

// ImplementationInfo identifies client/server implementation.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities describes client capabilities.
type ClientCapabilities struct {
	Experimental map[string]any `json:"experimental,omitempty"`
	Roots        *RootsCapability `json:"roots,omitempty"`
	Sampling     map[string]any `json:"sampling,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerCapabilities describes server capabilities.
type ServerCapabilities struct {
	Experimental map[string]any  `json:"experimental,omitempty"`
	Logging      map[string]any  `json:"logging,omitempty"`
	Prompts      *CapabilityItem `json:"prompts,omitempty"`
	Resources    *CapabilityItem `json:"resources,omitempty"`
	Tools        *CapabilityItem `json:"tools,omitempty"`
}

type CapabilityItem struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeParams is sent by the client during initialization.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ImplementationInfo `json:"clientInfo"`
}

// InitializeResult is received from the server during initialization.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ImplementationInfo `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ToolDefinition represents an MCP tool definition.
type ToolDefinition struct {
	Name             string          `json:"name"`
	Description      string          `json:"description,omitempty"`
	InputSchema      ToolInputSchema `json:"inputSchema"`
	RequiresApproval bool            `json:"requiresApproval,omitempty"`
	ApprovalTimeout  time.Duration   `json:"approvalTimeout,omitempty"`
}

// ToolInputSchema defines JSON Schema for tool inputs.
type ToolInputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]any         `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// ListToolsResult is the result of tools/list method.
type ListToolsResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// CallToolParams is sent when invoking a tool.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ContentBlock is a piece of output returned from a tool call.
type ContentBlock struct {
	Type     string `json:"type"` // "text", "image", "resource"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// ToolResult represents the output from tools/call.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// TextContent returns the aggregated text content of the result.
func (tr *ToolResult) TextContent() string {
	if tr == nil {
		return ""
	}
	var res string
	for _, block := range tr.Content {
		if block.Type == "text" {
			if res != "" {
				res += "\n"
			}
			res += block.Text
		}
	}
	return res
}
