// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPTransport(t *testing.T) {
	// Create SSE + POST mock HTTP server
	var messagePostReceived bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// SSE endpoint
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Flushing not supported", http.StatusInternalServerError)
				return
			}

			// Send endpoint event
			fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
			flusher.Flush()

			// Send a message response
			resp := Response{
				JSONRPC: JSONRPCVersion,
				ID:      NewIntID(1),
				Result:  json.RawMessage(`{"status":"pong"}`),
			}
			b, _ := json.Marshal(resp)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(b))
			flusher.Flush()

			// Keep connection open until context cancelled
			<-r.Context().Done()
			return
		}

		if r.Method == http.MethodPost {
			messagePostReceived = true
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := NewHTTPTransport(ctx, HTTPTransportConfig{
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewHTTPTransport failed: %v", err)
	}
	defer transport.Close()

	// Wait for the message from SSE
	resp, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}
	if resp.ID == nil || resp.ID.NumVal != 1 {
		t.Errorf("Unexpected response ID: %+v", resp.ID)
	}

	// Send POST request
	err = transport.Send(ctx, &Request{
		JSONRPC: JSONRPCVersion,
		ID:      NewIntID(2),
		Method:  MethodPing,
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if !messagePostReceived {
		t.Errorf("Expected message post to be received")
	}
}

func TestHTTPTransportErrors(t *testing.T) {
	ctx := context.Background()

	// Empty BaseURL
	_, err := NewHTTPTransport(ctx, HTTPTransportConfig{BaseURL: ""})
	if err == nil {
		t.Errorf("Expected error for empty BaseURL")
	}

	// Server returning 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err = NewHTTPTransport(ctx, HTTPTransportConfig{BaseURL: server.URL})
	if err == nil {
		t.Errorf("Expected error for 404 status code")
	}

	// Closed transport
	serverOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer serverOK.Close()

	tr, err := NewHTTPTransport(ctx, HTTPTransportConfig{BaseURL: serverOK.URL})
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	_ = tr.Close()

	if err := tr.Send(ctx, &Request{}); err != ErrTransportClosed {
		t.Errorf("Expected ErrTransportClosed, got %v", err)
	}

	// Test send error directly
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodPost {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
	}))
	defer errServer.Close()

	trErr, err := NewHTTPTransport(ctx, HTTPTransportConfig{BaseURL: errServer.URL})
	if err != nil {
		t.Fatalf("Failed to create trErr: %v", err)
	}
	defer trErr.Close()

	if err := trErr.Send(ctx, &Request{JSONRPC: "2.0", Method: "ping"}); err == nil {
		t.Errorf("Expected send error when HTTP server returns 500")
	}

	if err := trErr.Send(ctx, nil); !errors.Is(err, ErrNilRequest) {
		t.Errorf("Expected ErrNilRequest, got %v", err)
	}

	// Receive timeout with context
	toCtx, toCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer toCancel()
	if _, err := trErr.Receive(toCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got: %v", err)
	}
}
