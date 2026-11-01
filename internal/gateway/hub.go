// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import (
	"net/http"
	"sync"
)

// Hub maintains the set of active WebSocket / streaming clients and broadcasts messages to them.
type Hub struct {
	mu         sync.RWMutex
	clients    map[chan []byte]struct{}
	runClients map[string]map[chan []byte]struct{}
	closed     bool
	closeOnce  sync.Once
}

// NewHub initializes an active client broadcast hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[chan []byte]struct{}),
		runClients: make(map[string]map[chan []byte]struct{}),
	}
}

// Register adds a client channel to the active broadcast list.
func (h *Hub) Register(ch chan []byte) {
	h.RegisterRun("", ch)
}

// Unregister removes a client channel from the active broadcast list.
func (h *Hub) Unregister(ch chan []byte) {
	h.UnregisterRun("", ch)
}

// RegisterRun adds a client channel to listen to events for a specific run ID (or global if runID is empty).
func (h *Hub) RegisterRun(runID string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || ch == nil {
		return
	}
	if runID == "" {
		h.clients[ch] = struct{}{}
		return
	}
	group, exists := h.runClients[runID]
	if !exists {
		group = make(map[chan []byte]struct{})
		h.runClients[runID] = group
	}
	group[ch] = struct{}{}
}

// UnregisterRun removes a client channel from the specific run ID list (or global list if runID is empty).
func (h *Hub) UnregisterRun(runID string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if runID == "" {
		delete(h.clients, ch)
		return
	}
	if group, exists := h.runClients[runID]; exists {
		delete(group, ch)
		if len(group) == 0 {
			delete(h.runClients, runID)
		}
	}
}

// Broadcast sends a message to all connected clients without blocking.
func (h *Hub) Broadcast(msg []byte) {
	h.BroadcastRun("", msg)
}

// BroadcastRun sends a message to all global clients and to clients subscribed to the specific runID.
func (h *Hub) BroadcastRun(runID string, msg []byte) {
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

	if runID != "" {
		if group, exists := h.runClients[runID]; exists {
			for ch := range group {
				select {
				case ch <- msg:
				default:
				}
			}
		}
	}
}

// ClientCount returns the number of currently active global streaming clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// RunClientCount returns the number of currently active streaming clients for a specific run ID.
func (h *Hub) RunClientCount(runID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if runID == "" {
		return len(h.clients)
	}
	return len(h.runClients[runID])
}

// Close disconnects all registered clients and shuts down the hub.
func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.closed = true
		h.clients = make(map[chan []byte]struct{})
		h.runClients = make(map[string]map[chan []byte]struct{})
	})
}

// ServeHTTP provides a stream endpoint for connecting browser clients globally.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ServeHTTPRun(w, r, "")
}

// ServeHTTPRun provides a stream endpoint for connecting browser clients for a specific runID (or global if empty).
func (h *Hub) ServeHTTPRun(w http.ResponseWriter, r *http.Request, runID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 32)
	h.RegisterRun(runID, ch)
	defer func() {
		h.UnregisterRun(runID, ch)
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
