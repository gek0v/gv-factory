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
}
