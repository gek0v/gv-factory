// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"encoding/json"
	"testing"
)

func TestRequestSerialization(t *testing.T) {
	req := &Request{
		JSONRPC: JSONRPCVersion,
		ID:      NewIntID(1),
		Method:  MethodInitialize,
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed Request
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed.JSONRPC != JSONRPCVersion {
		t.Errorf("Expected jsonrpc 2.0, got %s", parsed.JSONRPC)
	}
	if parsed.Method != MethodInitialize {
		t.Errorf("Expected method initialize, got %s", parsed.Method)
	}
	if parsed.ID == nil || parsed.ID.NumVal != 1 || parsed.ID.IsStr {
		t.Errorf("Expected int ID 1, got %+v", parsed.ID)
	}
	if parsed.IsNotification() {
		t.Errorf("Expected not notification")
	}

	// Test Notification
	notif := &Request{
		JSONRPC: JSONRPCVersion,
		Method:  MethodInitialized,
	}
	if !notif.IsNotification() {
		t.Errorf("Expected notification to return true")
	}
}

func TestRequestID(t *testing.T) {
	// String ID
	strID := NewStringID("test-id-123")
	if !strID.IsStr || strID.String() != "test-id-123" {
		t.Errorf("String ID failed: %+v", strID)
	}

	data, err := json.Marshal(strID)
	if err != nil {
		t.Fatalf("Marshal string ID failed: %v", err)
	}
	if string(data) != `"test-id-123"` {
		t.Errorf("Expected marshaled string id, got %s", string(data))
	}

	var parsedID RequestID
	if err := json.Unmarshal(data, &parsedID); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if !parsedID.IsStr || parsedID.StrVal != "test-id-123" {
		t.Errorf("Parsed ID mismatch: %+v", parsedID)
	}

	// Int ID
	intID := NewIntID(42)
	if intID.IsStr || intID.String() != "42" {
		t.Errorf("Int ID failed: %+v", intID)
	}
	data, err = json.Marshal(intID)
	if err != nil {
		t.Fatalf("Marshal int ID failed: %v", err)
	}
	if string(data) != "42" {
		t.Errorf("Expected 42, got %s", string(data))
	}

	var parsedIntID RequestID
	if err := json.Unmarshal(data, &parsedIntID); err != nil {
		t.Fatalf("Unmarshal int ID failed: %v", err)
	}
	if parsedIntID.IsStr || parsedIntID.NumVal != 42 {
		t.Errorf("Parsed int ID mismatch: %+v", parsedIntID)
	}

	// Invalid ID
	var invalidID RequestID
	if err := json.Unmarshal([]byte(`{"invalid":true}`), &invalidID); err == nil {
		t.Errorf("Expected error unmarshaling object as RequestID")
	}
}

func TestResponseAndError(t *testing.T) {
	rpcErr := &RPCError{
		Code:    CodeMethodNotFound,
		Message: "Method not found",
	}
	if rpcErr.Error() != "jsonrpc error (code -32601): Method not found" {
		t.Errorf("Unexpected RPCError string: %s", rpcErr.Error())
	}
	var nilErr *RPCError
	if nilErr.Error() != "<nil>" {
		t.Errorf("Unexpected nil RPCError string: %s", nilErr.Error())
	}

	resp := &Response{
		JSONRPC: JSONRPCVersion,
		ID:      NewIntID(1),
		Error:   rpcErr,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal Response failed: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal Response failed: %v", err)
	}
	if parsed.Error == nil || parsed.Error.Code != CodeMethodNotFound {
		t.Errorf("Parsed response error mismatch: %+v", parsed.Error)
	}
}

func TestToolResultTextContent(t *testing.T) {
	tr := &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: "Hello"},
			{Type: "image", Data: "base64..."},
			{Type: "text", Text: "World"},
		},
	}

	expected := "Hello\nWorld"
	if tr.TextContent() != expected {
		t.Errorf("Expected %q, got %q", expected, tr.TextContent())
	}

	var nilTR *ToolResult
	if nilTR.TextContent() != "" {
		t.Errorf("Expected empty string for nil ToolResult, got %q", nilTR.TextContent())
	}
}
