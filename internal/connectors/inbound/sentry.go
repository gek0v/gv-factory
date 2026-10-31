// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// SentryProvider handles signature verification and payload normalization for Sentry/observability webhooks.
type SentryProvider struct {
	Secret             string
	MaxTimestampDrift  time.Duration
	SkipFreshnessCheck bool
}

// NewSentryProvider constructs a new Sentry webhook provider.
func NewSentryProvider(secret string) *SentryProvider {
	return &SentryProvider{
		Secret:             secret,
		MaxTimestampDrift:  5 * time.Minute,
		SkipFreshnessCheck: false,
	}
}

// Name returns the provider identifier "sentry".
func (p *SentryProvider) Name() string {
	return schema.SourceSentry
}

// Verify validates cryptographic HMAC-SHA256 signature in Sentry-Hook-Signature,
// or fallback Bearer / query token using constant-time comparisons.
func (p *SentryProvider) Verify(r *http.Request, body []byte) error {
	sig := strings.TrimSpace(r.Header.Get("Sentry-Hook-Signature"))
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	queryToken := strings.TrimSpace(r.URL.Query().Get("token"))

	if sig != "" {
		if !VerifyHMACSHA256(body, []byte(p.Secret), sig) {
			return fmt.Errorf("%w: invalid Sentry-Hook-Signature HMAC", ErrInvalidSignature)
		}
	} else if authHeader != "" {
		token := authHeader
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(p.Secret)) != 1 {
			return fmt.Errorf("%w: invalid Authorization bearer token", ErrInvalidSignature)
		}
	} else if queryToken != "" {
		if subtle.ConstantTimeCompare([]byte(queryToken), []byte(p.Secret)) != 1 {
			return fmt.Errorf("%w: invalid query token", ErrInvalidSignature)
		}
	} else {
		return fmt.Errorf("%w: missing Sentry-Hook-Signature or bearer token", ErrMissingSignature)
	}

	// Verify timestamp freshness if Sentry-Hook-Timestamp is present
	tsHeader := strings.TrimSpace(r.Header.Get("Sentry-Hook-Timestamp"))
	if tsHeader != "" && !p.SkipFreshnessCheck && p.MaxTimestampDrift > 0 {
		ts, err := parseSentryTimestamp(tsHeader)
		if err == nil {
			if freshnessErr := ValidateFreshness(ts, p.MaxTimestampDrift); freshnessErr != nil {
				return freshnessErr
			}
		}
	}

	return nil
}

// Normalize parses Sentry webhook JSON payloads into canonical schema.TriggerEvent structures.
func (p *SentryProvider) Normalize(r *http.Request, body []byte) (*schema.TriggerEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}

	resource := strings.TrimSpace(r.Header.Get("Sentry-Hook-Resource"))
	if resource == "" {
		resource = getString(payload, "resource")
	}

	action := getString(payload, "action")
	if action == "" {
		action = getString(payload, "data", "action")
	}

	repo := getString(payload, "data", "issue", "project", "slug")
	if repo == "" {
		repo = getString(payload, "data", "event", "project")
	}
	if repo == "" {
		repo = getString(payload, "project_slug")
	}

	sender := getString(payload, "actor", "name")
	if sender == "" {
		sender = getString(payload, "data", "actor", "name")
	}
	if sender == "" {
		sender = "sentry"
	}

	ref := getString(payload, "data", "event", "release")
	if ref == "" {
		ref = getString(payload, "data", "issue", "metadata", "value")
	}

	var eventType string
	switch {
	case action == "created":
		eventType = schema.EventErrorCreated
	case action == "resolved":
		eventType = schema.EventAlertResolved
	case action == "triggered":
		eventType = schema.EventAlertTriggered
	case resource == "metric_alert" || resource == "event_alert":
		if action == "resolved" {
			eventType = schema.EventAlertResolved
		} else {
			eventType = schema.EventAlertTriggered
		}
	case resource != "" && action != "":
		eventType = resource + "." + action
	case resource != "":
		eventType = resource
	default:
		eventType = schema.EventAlertTriggered
	}

	var eventTime time.Time
	tsHeader := r.Header.Get("Sentry-Hook-Timestamp")
	if tsHeader != "" {
		eventTime, _ = parseSentryTimestamp(tsHeader)
	}
	if eventTime.IsZero() {
		eventTime = parseTime(getString(payload, "data", "event", "timestamp"))
	}
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}

	deliveryID := r.Header.Get("Sentry-Hook-Resource") + "-" + r.Header.Get("Sentry-Hook-Timestamp")
	if deliveryID == "-" {
		deliveryID = getString(payload, "data", "event", "event_id")
	}

	return &schema.TriggerEvent{
		ID:         deliveryID,
		Source:     schema.SourceSentry,
		EventType:  eventType,
		Repository: repo,
		Reference:  ref,
		Sender:     sender,
		Payload:    payload,
		RawPayload: body,
		Timestamp:  eventTime,
	}, nil
}

// parseSentryTimestamp parses seconds or milliseconds UNIX timestamps as strings.
func parseSentryTimestamp(raw string) (time.Time, error) {
	// Try float or int seconds
	if sec, err := strconv.ParseFloat(raw, 64); err == nil {
		if sec > 1e11 { // milliseconds
			return time.UnixMilli(int64(sec)).UTC(), nil
		}
		secInt := int64(sec)
		nsec := int64((sec - float64(secInt)) * 1e9)
		return time.Unix(secInt, nsec).UTC(), nil
	}
	// Try ISO timestamp string
	t := parseTime(raw)
	if !t.IsZero() {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unknown timestamp format: %s", raw)
}
