// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReplayDefense_TimestampFreshness(t *testing.T) {
	drift := 2 * time.Minute
	ing := NewIngestor(Config{
		SentrySecret:      testSentrySecret,
		MaxTimestampDrift: drift,
	}, nil)

	body := []byte(fixtureSentryAlert)
	sig := computeHMAC([]byte(testSentrySecret), body)

	// 1. Fresh timestamp (within drift window) -> 202
	freshTS := fmt.Sprintf("%d", time.Now().UTC().Unix())
	reqFresh := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", bytes.NewReader(body))
	reqFresh.Header.Set("Sentry-Hook-Signature", sig)
	reqFresh.Header.Set("Sentry-Hook-Timestamp", freshTS)
	recFresh := httptest.NewRecorder()
	ing.ServeHTTP(recFresh, reqFresh)
	if recFresh.Code != http.StatusAccepted {
		t.Fatalf("expected fresh webhook to be accepted (202), got %d: %s", recFresh.Code, recFresh.Body.String())
	}

	// 2. Expired timestamp (10 minutes ago) -> 401
	expiredTS := fmt.Sprintf("%d", time.Now().UTC().Add(-10*time.Minute).Unix())
	reqExpired := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", bytes.NewReader(body))
	reqExpired.Header.Set("Sentry-Hook-Signature", sig)
	reqExpired.Header.Set("Sentry-Hook-Timestamp", expiredTS)
	recExpired := httptest.NewRecorder()
	ing.ServeHTTP(recExpired, reqExpired)
	if recExpired.Code != http.StatusUnauthorized {
		t.Fatalf("expected expired timestamp to return 401 Unauthorized, got %d", recExpired.Code)
	}

	// 3. Future timestamp (10 minutes in the future) -> 401
	futureTS := fmt.Sprintf("%d", time.Now().UTC().Add(10*time.Minute).Unix())
	reqFuture := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", bytes.NewReader(body))
	reqFuture.Header.Set("Sentry-Hook-Signature", sig)
	reqFuture.Header.Set("Sentry-Hook-Timestamp", futureTS)
	recFuture := httptest.NewRecorder()
	ing.ServeHTTP(recFuture, reqFuture)
	if recFuture.Code != http.StatusUnauthorized {
		t.Fatalf("expected future timestamp to return 401 Unauthorized, got %d", recFuture.Code)
	}
}
