// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

// NewServerMux configures and returns the HTTP request multiplexer for ov-server.
func NewServerMux(
	brainSvc *brain.Service,
	gw *gateway.Gateway,
	webhookIngestor http.Handler,
	chatHandler *chat.ChatHandler,
) http.Handler {
	mux := http.NewServeMux()

	// Health check endpoints for orchestration & load balancers
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		brainStatus := "connected"
		if brainSvc == nil || brainSvc.Ping(r.Context()) != nil {
			brainStatus = "degraded"
		}

		gwStatus := "nats"
		if gw == nil || gw.IsInMemory() {
			gwStatus = "in-memory"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"service": "ov-server",
			"brain":   brainStatus,
			"gateway": gwStatus,
		})
	}

	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/v1/health", healthHandler)

	// Inbound webhook endpoints (/api/v1/webhooks/{provider})
	if webhookIngestor != nil {
		mux.Handle("/api/v1/webhooks/", webhookIngestor)
	}

	// Interactive Chat endpoints (Slack & Discord)
	if chatHandler != nil {
		mux.HandleFunc("/api/v1/chat/slack/interactions", chatHandler.HandleSlackInteractions)
		mux.HandleFunc("/api/v1/chat/slack/commands", chatHandler.HandleSlackCommands)
		mux.HandleFunc("/api/v1/chat/discord/interactions", chatHandler.HandleDiscordInteractions)
	}

	// Real-time SSE event stream endpoints
	if gw != nil && gw.Hub() != nil {
		mux.Handle("/events", gw.Hub())
		mux.Handle("/api/v1/events", gw.Hub())
	}

	// Gate approval resolution endpoint from frontend or integrations
	mux.HandleFunc("/api/gate/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			RunID    string `json:"run_id"`
			Token    string `json:"token"`
			Decision string `json:"decision"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		log.Printf("ov-server: gate resolved for run %q with decision %q", payload.RunID, payload.Decision)

		if gw != nil {
			_ = gw.Publish(r.Context(), "gate.resolved", gateway.Event{
				Type: "gate.resolved",
				Payload: map[string]any{
					"run_id":   payload.RunID,
					"token":    payload.Token,
					"decision": payload.Decision,
				},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Gate decision recorded",
		})
	})

	return mux
}
