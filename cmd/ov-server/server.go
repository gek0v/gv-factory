// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/telemetry"
)

// getClientIP extracts the client IP address from standard proxy headers or RemoteAddr.
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// NewServerMux configures and returns the HTTP request multiplexer for ov-server.
func NewServerMux(
	brainSvc *brain.Service,
	gw *gateway.Gateway,
	webhookIngestor http.Handler,
	chatHandler *chat.ChatHandler,
	authSvc *auth.Service,
	tel ...*telemetry.Telemetry,
) http.Handler {
	var t *telemetry.Telemetry
	if len(tel) > 0 && tel[0] != nil {
		t = tel[0]
	} else {
		t, _ = telemetry.New(context.Background(), telemetry.DefaultConfig())
	}

	mux := http.NewServeMux()

	// Metrics endpoint for Prometheus scraping
	if t != nil && t.Metrics != nil {
		mux.Handle("/metrics", t.Metrics.Handler())
	}

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

		telStatus := "disabled"
		if t != nil {
			telStatus = t.Status()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"service":   "ov-server",
			"brain":     brainStatus,
			"gateway":   gwStatus,
			"telemetry": telStatus,
		})
	}

	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/v1/health", healthHandler)

	// Readiness check endpoint
	readyHandler := func(w http.ResponseWriter, r *http.Request) {
		isReady := true
		brainStatus := "connected"
		if brainSvc == nil || brainSvc.Ping(r.Context()) != nil {
			brainStatus = "degraded"
		}

		gwStatus := "nats"
		if gw == nil || gw.IsInMemory() {
			gwStatus = "in-memory"
		}

		telStatus := "disabled"
		if t != nil {
			telStatus = t.Status()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"ready":     isReady,
			"service":   "ov-server",
			"brain":     brainStatus,
			"gateway":   gwStatus,
			"telemetry": telStatus,
		})
	}
	mux.HandleFunc("/readyz", readyHandler)

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

		now := time.Now().UTC()
		clientIP := getClientIP(r)
		userAgent := r.UserAgent()
		tokenHMACPrefix := auth.ExtractTokenPrefix(payload.Token)

		auditRec := auth.AuditRecord{
			Timestamp:       now,
			RunID:           payload.RunID,
			Decision:        payload.Decision,
			ClientIP:        clientIP,
			UserAgent:       userAgent,
			TokenHMACPrefix: tokenHMACPrefix,
		}

		var auditLogger auth.AuditLogger
		if authSvc != nil {
			auditLogger = authSvc.AuditLogger()
		}

		recordAuditAndPublish := func(status auth.AuditStatus, details string) {
			auditRec.Status = status
			auditRec.Details = details
			if auditLogger != nil {
				auditLogger.Record(auditRec)
			}
			if gw != nil {
				_ = gw.Publish(r.Context(), gateway.TopicGateAudit, gateway.Event{
					Type:      gateway.TopicGateAudit,
					RunID:     payload.RunID,
					Timestamp: now.UnixMilli(),
					Payload: map[string]any{
						"timestamp":         now.Format(time.RFC3339Nano),
						"run_id":            payload.RunID,
						"decision":          payload.Decision,
						"client_ip":         clientIP,
						"user_agent":        userAgent,
						"token_hmac_prefix": tokenHMACPrefix,
						"status":            string(status),
						"details":           details,
					},
				})
			}
		}

		// Decision validation (must be 'approved' or 'rejected')
		normalizedDecision := strings.ToLower(strings.TrimSpace(payload.Decision))
		if normalizedDecision != "approved" && normalizedDecision != "rejected" {
			recordAuditAndPublish(auth.AuditStatusInvalidDecision, "Decision must be 'approved' or 'rejected'")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "decision must be 'approved' or 'rejected'",
			})
			return
		}

		// Cryptographic HMAC token validation
		if authSvc == nil {
			recordAuditAndPublish(auth.AuditStatusInvalidSignature, "Auth service not configured on server")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
			return
		}

		tokenRunID, valid := authSvc.ValidateApprovalToken(payload.Token)
		if !valid {
			recordAuditAndPublish(auth.AuditStatusInvalidSignature, "HMAC signature verification failed or token expired")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
			return
		}

		if tokenRunID != payload.RunID {
			recordAuditAndPublish(auth.AuditStatusRunIDMismatch, "Token run ID does not match payload run ID")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "invalid or expired approval token",
			})
			return
		}

		recordAuditAndPublish(auth.AuditStatusSuccess, "Gate resolution approved cryptographically")
		log.Printf("ov-server: gate resolved for run %q with decision %q", payload.RunID, payload.Decision)

		if gw != nil {
			_ = gw.Publish(r.Context(), gateway.TopicGateResolved, gateway.Event{
				Type:      gateway.TopicGateResolved,
				RunID:     payload.RunID,
				Timestamp: now.UnixMilli(),
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

	var handler http.Handler = mux
	if t != nil && t.Metrics != nil {
		handler = telemetry.NewHTTPMiddleware(t.Metrics, t.Tracer)(mux)
	}

	return handler
}
