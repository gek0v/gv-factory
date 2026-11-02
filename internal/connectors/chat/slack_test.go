// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifySlackSignature(t *testing.T) {
	secret := "my-slack-signing-secret-1234"
	now := time.Now()
	nowSec := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"command":"/factory","text":"run bugfix"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("v0:%s:", nowSec)))
	_, _ = mac.Write(body)
	validSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	t.Run("Valid signature", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSlackSignature, validSig)
		h.Set(HeaderSlackTimestamp, nowSec)

		err := VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err != nil {
			t.Fatalf("expected valid signature, got error: %v", err)
		}
	})

	t.Run("Invalid signature mismatch", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSlackSignature, "v0=badbadbadbad")
		h.Set(HeaderSlackTimestamp, nowSec)

		err := VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err == nil {
			t.Fatal("expected signature mismatch error, got nil")
		}

		// Malformed hex in signature
		h.Set(HeaderSlackSignature, "v0=nothexsignature!")
		err = VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err == nil {
			t.Fatal("expected hex decode error, got nil")
		}

		// Invalid prefix
		h.Set(HeaderSlackSignature, "v1=123456")
		err = VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err == nil {
			t.Fatal("expected invalid prefix error, got nil")
		}
	})

	t.Run("Missing signature header", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSlackTimestamp, nowSec)

		err := VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err == nil {
			t.Fatal("expected error for missing signature, got nil")
		}
	})

	t.Run("Missing timestamp header", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSlackSignature, validSig)

		err := VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err == nil {
			t.Fatal("expected error for missing timestamp, got nil")
		}
	})

	t.Run("Expired timestamp > 5 minutes", func(t *testing.T) {
		h := http.Header{}
		oldSec := strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10)
		h.Set(HeaderSlackTimestamp, oldSec)

		macOld := hmac.New(sha256.New, []byte(secret))
		_, _ = macOld.Write([]byte(fmt.Sprintf("v0:%s:", oldSec)))
		_, _ = macOld.Write(body)
		h.Set(HeaderSlackSignature, "v0="+hex.EncodeToString(macOld.Sum(nil)))

		err := VerifySlackSignature(secret, h, body, now, 5*time.Minute)
		if err == nil {
			t.Fatal("expected error for expired timestamp, got nil")
		}
	})
}

func TestSlackAdapter_SendAndPrompt(t *testing.T) {
	var receivedPostMsg map[string]any
	var receivedUpdateMsg map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/chat.postMessage":
			_ = json.Unmarshal(body, &receivedPostMsg)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true, "ts": "1730000000.123456"}`))
		case "/chat.update":
			_ = json.Unmarshal(body, &receivedUpdateMsg)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true, "ts": "1730000000.123456"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := NewSlackAdapter(SlackConfig{
		BotToken:       "xoxb-test-token",
		SigningSecret:  "test-secret",
		DefaultChannel: "C123456",
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
	})

	ctx := context.Background()

	t.Run("SendNotification with thread_ts", func(t *testing.T) {
		ref, err := adapter.SendNotification(ctx, NotificationMessage{
			Channel:  "C123456",
			ThreadTS: "1730000000.000001",
			Title:    "Build Passing",
			Body:     "All 12 tests passed",
			Level:    "success",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.MessageID != "1730000000.123456" {
			t.Errorf("expected messageID 1730000000.123456, got %s", ref.MessageID)
		}
		if receivedPostMsg["thread_ts"] != "1730000000.000001" {
			t.Errorf("expected thread_ts in payload, got %v", receivedPostMsg["thread_ts"])
		}
	})

	t.Run("PromptApproval formats Block Kit correctly", func(t *testing.T) {
		req := ApprovalPromptRequest{
			RunID:               "run-gate-test-01",
			Token:               "token-xyz-secret",
			Channel:             "C123456",
			Summary:             "Test coverage 92%, 0 alerts",
			CommitSHA:           "abc1234def",
			DiffURL:             "https://github.com/org/repo/pull/1/files",
			CoveragePercent:     92.5,
			CriticalAlerts:      0,
			HighAlerts:          0,
			ExpiresAt:           time.Now().Add(1 * time.Hour),
			HasMigrationChanges: true,
			HasAuthChanges:      true,
		}

		ref, err := adapter.PromptApproval(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref == nil {
			t.Fatal("expected non-nil MessageRef")
		}

		blocks, ok := receivedPostMsg["blocks"].([]any)
		if !ok || len(blocks) < 4 {
			t.Fatalf("expected at least 4 blocks, got %v", receivedPostMsg["blocks"])
		}

		actionsBlock := blocks[len(blocks)-1].(map[string]any)
		if actionsBlock["type"] != "actions" {
			t.Errorf("expected last block to be 'actions', got %v", actionsBlock["type"])
		}
		elements := actionsBlock["elements"].([]any)
		if len(elements) < 3 {
			t.Errorf("expected 3 elements in actions block (approve, reject, diff), got %d", len(elements))
		}
	})

	t.Run("UpdateStatus posts progress to thread", func(t *testing.T) {
		ref, err := adapter.UpdateStatus(ctx, StatusUpdate{
			RunID:     "run-status-01",
			StepName:  "spec-writer",
			ThreadTS:  "1730000000.123456",
			Status:    "completed",
			Details:   "Spec generated and validated successfully",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.ThreadID != "1730000000.123456" {
			t.Errorf("expected thread ID 1730000000.123456, got %s", ref.ThreadID)
		}
	})

	t.Run("UpdateMessageWithDecision updates banner", func(t *testing.T) {
		err := adapter.UpdateMessageWithDecision(ctx, "C123456", "1730000000.123456", GateResolution{
			RunID:     "run-gate-test-01",
			Decision:  "APPROVE",
			User:      "charlie",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedUpdateMsg["channel"] != "C123456" || receivedUpdateMsg["ts"] != "1730000000.123456" {
			t.Errorf("expected channel and ts in update payload, got %v", receivedUpdateMsg)
		}
	})

	t.Run("Interactive Gate Card with task title, gate reason, findings and buttons", func(t *testing.T) {
		req := ApprovalPromptRequest{
			RunID:           "run-gate-interactive-99",
			Token:           "hmac-token-secret-1234",
			TaskTitle:       "Security & Quality Gate: Payment Service",
			GateReason:      "Coverage dropped below threshold and critical alert found",
			FindingsCount:   7,
			CriticalAlerts:  2,
			HighAlerts:      5,
			CoveragePercent: 78.4,
			ReviewerScores:  []string{"Security: 3/5", "Architecture: 4/5"},
			DiffURL:         "https://canvas.gv-factory.local/diff/run-99",
			ExpiresAt:       time.Now().Add(45 * time.Minute),
		}

		ref, err := adapter.PromptApproval(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref == nil {
			t.Fatal("expected non-nil MessageRef")
		}

		blocks, ok := receivedPostMsg["blocks"].([]any)
		if !ok || len(blocks) == 0 {
			t.Fatalf("expected blocks in post payload: %v", receivedPostMsg)
		}

		// Verify header includes task title
		headerBlock := blocks[0].(map[string]any)
		headerText := headerBlock["text"].(map[string]any)["text"].(string)
		if !strings.Contains(headerText, "Security & Quality Gate: Payment Service") {
			t.Errorf("expected header text to contain task title, got %q", headerText)
		}

		// Find actions block and inspect buttons
		var foundActions map[string]any
		for _, b := range blocks {
			bm := b.(map[string]any)
			if bm["type"] == "actions" {
				foundActions = bm
				break
			}
		}
		if foundActions == nil {
			t.Fatal("expected actions block with interactive buttons")
		}

		elems := foundActions["elements"].([]any)
		var hasApprove, hasReject, hasDiff bool
		for _, el := range elems {
			btn := el.(map[string]any)
			actionID, _ := btn["action_id"].(string)
			switch actionID {
			case "approve_gate":
				hasApprove = true
				if btn["style"] != "primary" {
					t.Errorf("expected approve_gate style 'primary', got %v", btn["style"])
				}
				if btn["value"] != req.Token {
					t.Errorf("expected approve_gate value to be HMAC token, got %v", btn["value"])
				}
			case "reject_gate":
				hasReject = true
				if btn["style"] != "danger" {
					t.Errorf("expected reject_gate style 'danger', got %v", btn["style"])
				}
				if btn["value"] != req.Token {
					t.Errorf("expected reject_gate value to be HMAC token, got %v", btn["value"])
				}
			case "view_diff":
				hasDiff = true
				if btn["url"] != req.DiffURL {
					t.Errorf("expected view_diff url %s, got %v", req.DiffURL, btn["url"])
				}
			}
		}

		if !hasApprove {
			t.Error("expected 'approve_gate' button in actions block")
		}
		if !hasReject {
			t.Error("expected 'reject_gate' button in actions block")
		}
		if !hasDiff {
			t.Error("expected 'view_diff' button in actions block")
		}
	})

	t.Run("UpdateMessageWithDecision shows user resolved banner", func(t *testing.T) {
		// Test approved format
		err := adapter.UpdateMessageWithDecision(ctx, "C123456", "1730000000.123456", GateResolution{
			RunID:     "run-gate-test-01",
			Decision:  "approved",
			User:      "alice",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text, _ := receivedUpdateMsg["text"].(string)
		if !strings.Contains(text, "✅ Gate approved by @alice") {
			t.Errorf("expected banner text to contain '✅ Gate approved by @alice', got %q", text)
		}

		// Test rejected format
		err = adapter.UpdateMessageWithDecision(ctx, "C123456", "1730000000.123456", GateResolution{
			RunID:     "run-gate-test-01",
			Decision:  "rejected",
			User:      "bob",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text, _ = receivedUpdateMsg["text"].(string)
		if !strings.Contains(text, "❌ Gate rejected by @bob") {
			t.Errorf("expected banner text to contain '❌ Gate rejected by @bob', got %q", text)
		}
	})

	t.Run("SlackAdapter error paths", func(t *testing.T) {
		if adapter.Platform() != "slack" {
			t.Errorf("expected platform slack, got %s", adapter.Platform())
		}

		// Test levels: warning, error, default
		_, _ = adapter.SendNotification(ctx, NotificationMessage{Channel: "C123456", Level: "warning", Title: "Warn", Body: "Warning body"})
		_, _ = adapter.SendNotification(ctx, NotificationMessage{Channel: "C123456", Level: "error", Title: "Err", Body: "Error body"})
		_, _ = adapter.SendNotification(ctx, NotificationMessage{Channel: "C123456", Level: "other", Title: "Other", Body: "Other body"})

		// Test statuses: running, failed, other
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "C123456", Status: "running", StepName: "builder", Details: "Working"})
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "C123456", Status: "failed", StepName: "builder", Details: "Failed"})
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "C123456", Status: "unknown", StepName: "builder"})

		noChanAdapter := NewSlackAdapter(SlackConfig{
			BaseURL: srv.URL,
		})
		if _, err := noChanAdapter.SendNotification(ctx, NotificationMessage{Title: "hi"}); err != ErrChannelRequired {
			t.Errorf("expected ErrChannelRequired, got %v", err)
		}
		if _, err := noChanAdapter.PromptApproval(ctx, ApprovalPromptRequest{RunID: "r1"}); err != ErrChannelRequired {
			t.Errorf("expected ErrChannelRequired, got %v", err)
		}
		if _, err := noChanAdapter.UpdateStatus(ctx, StatusUpdate{RunID: "r1"}); err != ErrChannelRequired {
			t.Errorf("expected ErrChannelRequired, got %v", err)
		}

		// Slack API error responses
		errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "invalid_auth"}`))
		}))
		defer errSrv.Close()

		errAdapter := NewSlackAdapter(SlackConfig{
			DefaultChannel: "C1",
			BaseURL:        errSrv.URL,
			HTTPClient:     errSrv.Client(),
		})
		if _, err := errAdapter.SendNotification(ctx, NotificationMessage{Title: "hi"}); err == nil {
			t.Error("expected error when slack API returns ok:false")
		}
		if err := errAdapter.UpdateMessageWithDecision(ctx, "C1", "ts1", GateResolution{Decision: "approved"}); err == nil {
			t.Error("expected error when slack updateMessage returns ok:false")
		}

		// Slack API invalid JSON response
		badJsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer badJsonSrv.Close()
		badAdapter := NewSlackAdapter(SlackConfig{
			DefaultChannel: "C1",
			BaseURL:        badJsonSrv.URL,
			HTTPClient:     badJsonSrv.Client(),
		})
		if _, err := badAdapter.SendNotification(ctx, NotificationMessage{Title: "hi"}); err == nil {
			t.Error("expected error for bad json response")
		}
		if err := badAdapter.UpdateMessageWithDecision(ctx, "C1", "ts1", GateResolution{Decision: "approved"}); err == nil {
			t.Error("expected error for bad json update response")
		}
	})
}
