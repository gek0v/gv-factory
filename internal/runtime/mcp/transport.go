// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"io"
)

// Transport defines the communication interface for sending and receiving MCP JSON-RPC messages.
type Transport interface {
	// Send sends a request to the peer.
	Send(ctx context.Context, req *Request) error
	// Receive reads the next response or notification from the peer.
	Receive(ctx context.Context) (*Response, error)
	// Close closes the transport.
	Close() error
}

// ReadWriteCloserTransport represents a transport backed by an io.ReadWriteCloser.
type ReadWriteCloserTransport struct {
	rwc io.ReadWriteCloser
}
