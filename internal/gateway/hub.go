// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import (
	"net/http"
	"sync"
)

// Hub maintains the set of active WebSocket / streaming clients and broadcasts messages to them.
type Hub struct {
	mu        sync.RWMutex
	clients   map[chan []byte]struct{}
	closed    bool
	closeOnce sync.Once
}

// NewHub initializes an active client broadcast hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan []byte]struct{}),
	}
}

// Register adds a client channel to the active broadcast list.
func (h *Hub) Register(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed && ch != nil {
		h.clients[ch] = struct{}{}
	}
}

// Unregister removes a client channel from the active broadcast list.
func (h *Hub) Unregister(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
}

// Broadcast sends a message to all connected clients without blocking.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return
	}

	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// ClientCount returns the number of currently active streaming clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close disconnects all registered clients and shuts down the hub.
func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.closed = true
		h.clients = make(map[chan []byte]struct{})
	})
}

// ServeHTTP provides a stream endpoint for connecting browser clients.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 16)
	h.Register(ch)
	defer func() {
		h.Unregister(ch)
		close(ch)
	}()

	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(msg)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
