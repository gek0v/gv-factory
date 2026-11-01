// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"

	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/telemetry"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// Default limits and timeouts for webhook ingestion.
const (
	DefaultMaxBodySize       = 10 * 1024 * 1024 // 10MB
	DefaultMaxTimestampDrift = 5 * time.Minute
)

// Config holds credentials and parameters for inbound webhook ingestion.
type Config struct {
	GitHubSecret          string
	GitLabSecret          string
	SentrySecret          string
	MaxBodySize           int64
	MaxTimestampDrift     time.Duration
	DisableFreshnessCheck bool
}

// Ingestor handles incoming webhook HTTP requests, verifying cryptographic signatures,
// protecting against resource exhaustion and replay attacks, and normalizing vendor events.
type Ingestor struct {
	cfg        Config
	dispatcher Dispatcher
	providers  map[string]Provider
	mu         sync.RWMutex
}

// NewIngestor creates an initialized Ingestor with default or configured providers.
func NewIngestor(cfg Config, dispatcher Dispatcher) *Ingestor {
	if cfg.MaxBodySize <= 0 {
		cfg.MaxBodySize = DefaultMaxBodySize
	}
	if cfg.MaxTimestampDrift <= 0 {
		cfg.MaxTimestampDrift = DefaultMaxTimestampDrift
	}

	ing := &Ingestor{
		cfg:        cfg,
		dispatcher: dispatcher,
		providers:  make(map[string]Provider),
	}

	// Register standard providers
	gh := NewGitHubProvider(cfg.GitHubSecret)
	gh.MaxTimestampDrift = cfg.MaxTimestampDrift
	gh.SkipFreshnessCheck = cfg.DisableFreshnessCheck
	ing.RegisterProvider(gh)

	gl := NewGitLabProvider(cfg.GitLabSecret)
	gl.MaxTimestampDrift = cfg.MaxTimestampDrift
	gl.SkipFreshnessCheck = cfg.DisableFreshnessCheck
	ing.RegisterProvider(gl)

	sentry := NewSentryProvider(cfg.SentrySecret)
	sentry.MaxTimestampDrift = cfg.MaxTimestampDrift
	sentry.SkipFreshnessCheck = cfg.DisableFreshnessCheck
	ing.RegisterProvider(sentry)

	return ing
}

// RegisterProvider safely adds or updates a provider implementation.
func (i *Ingestor) RegisterProvider(p Provider) {
	if p == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.providers[strings.ToLower(p.Name())] = p
}

// GetProvider retrieves a provider by its case-insensitive name.
func (i *Ingestor) GetProvider(name string) (Provider, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	p, ok := i.providers[strings.ToLower(name)]
	return p, ok
}

// ProcessWebhook directly validates, normalizes, and dispatches a webhook payload without requiring an active HTTP connection.
func (i *Ingestor) ProcessWebhook(ctx context.Context, providerName string, headers http.Header, body []byte) (*schema.TriggerEvent, error) {
	if headers != nil {
		ctx = telemetry.ExtractHTTP(ctx, headers)
	}

	provider, ok := i.GetProvider(providerName)
	if !ok {
		return nil, ErrUnsupportedProvider
	}
	if int64(len(body)) > i.cfg.MaxBodySize {
		return nil, ErrPayloadTooLarge
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/webhooks/"+providerName, nil)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		req.Header = headers.Clone()
	}

	if err := provider.Verify(req, body); err != nil {
		return nil, err
	}

	ev, err := provider.Normalize(req, body)
	if err != nil {
		return nil, err
	}

	if ev.Payload == nil {
		ev.Payload = make(map[string]any)
	}
	telemetry.InjectPayload(ctx, ev.Payload)

	if ev.EventType != "ping" && i.dispatcher != nil {
		if err := i.dispatcher.Dispatch(ctx, *ev); err != nil {
			return nil, err
		}
	}

	return ev, nil
}

// ServeHTTP handles incoming HTTP requests on /api/v1/webhooks/{provider}.
func (i *Ingestor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := telemetry.ExtractHTTP(r.Context(), r.Header)
	r = r.WithContext(ctx)

	if r.Method != http.MethodPost {
		i.writeError(w, ErrMethodNotAllowed)
		return
	}

	providerName := i.extractProviderName(r)
	if providerName == "" {
		i.writeError(w, ErrUnsupportedProvider)
		return
	}

	provider, ok := i.GetProvider(providerName)
	if !ok {
		i.writeError(w, ErrUnsupportedProvider)
		return
	}

	// Buffer payload with maximum body limit (http.MaxBytesReader)
	r.Body = http.MaxBytesReader(w, r.Body, i.cfg.MaxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || strings.Contains(strings.ToLower(err.Error()), "too large") {
			i.writeError(w, ErrPayloadTooLarge)
			return
		}
		i.writeError(w, ErrInvalidPayload)
		return
	}

	// Cryptographic signature verification
	if err := provider.Verify(r, body); err != nil {
		i.writeError(w, err)
		return
	}

	// Payload normalization into schema.TriggerEvent
	ev, err := provider.Normalize(r, body)
	if err != nil {
		i.writeError(w, err)
		return
	}

	if ev.Payload == nil {
		ev.Payload = make(map[string]any)
	}
	telemetry.InjectPayload(ctx, ev.Payload)

	// Fast acknowledgement for ping events
	if ev.EventType == "ping" {
		i.writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"message": "pong",
		})
		return
	}

	// Event routing & dispatch
	if i.dispatcher != nil {
		if err := i.dispatcher.Dispatch(ctx, *ev); err != nil {
			i.writeError(w, err)
			return
		}
	}

	i.writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "accepted",
		"event_id":   ev.ID,
		"source":     ev.Source,
		"event_type": ev.EventType,
	})
}

// extractProviderName parses provider from PathValue, URL segments, or query parameters.
func (i *Ingestor) extractProviderName(r *http.Request) string {
	if val := r.PathValue("provider"); val != "" {
		return strings.TrimPrefix(val, ":")
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	for idx, part := range parts {
		if part == "webhooks" && idx+1 < len(parts) {
			target := parts[idx+1]
			target = strings.TrimPrefix(target, ":")
			return target
		}
	}

	if q := r.URL.Query().Get("provider"); q != "" {
		return strings.TrimPrefix(q, ":")
	}

	return ""
}

func (i *Ingestor) writeError(w http.ResponseWriter, err error) {
	status := HTTPStatusCode(err)
	i.writeJSON(w, status, map[string]any{
		"error": err.Error(),
		"code":  status,
	})
}

func (i *Ingestor) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
