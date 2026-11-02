// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVerifyDiscordSignature(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	pubKeyHex := hex.EncodeToString(pubKey)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	body := []byte(`{"type":1}`)

	msg := append([]byte(timestamp), body...)
	sig := ed25519.Sign(privKey, msg)
	sigHex := hex.EncodeToString(sig)

	t.Run("Valid Ed25519 signature", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderDiscordSignature, sigHex)
		h.Set(HeaderDiscordTimestamp, timestamp)

		err := VerifyDiscordSignature(pubKeyHex, h, body)
		if err != nil {
			t.Fatalf("expected valid signature, got error: %v", err)
		}
	})

	t.Run("Corrupted signature", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderDiscordSignature, "deadbeef1234")
		h.Set(HeaderDiscordTimestamp, timestamp)

		err := VerifyDiscordSignature(pubKeyHex, h, body)
		if err == nil {
			t.Fatal("expected error for bad signature hex, got nil")
		}
	})

	t.Run("Wrong public key", func(t *testing.T) {
		wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)
		wrongPubHex := hex.EncodeToString(wrongPub)

		h := http.Header{}
		h.Set(HeaderDiscordSignature, sigHex)
		h.Set(HeaderDiscordTimestamp, timestamp)

		err := VerifyDiscordSignature(wrongPubHex, h, body)
		if err == nil {
			t.Fatal("expected signature mismatch for wrong public key, got nil")
		}
	})

	t.Run("Missing headers", func(t *testing.T) {
		h := http.Header{}
		err := VerifyDiscordSignature(pubKeyHex, h, body)
		if err == nil {
			t.Fatal("expected error for missing headers, got nil")
		}

		// Bad public key hex
		err = VerifyDiscordSignature("nothexpubkey!", h, body)
		if err == nil {
			t.Fatal("expected error for bad pubkey hex, got nil")
		}
	})
}

func TestDiscordAdapter_SendAndPrompt(t *testing.T) {
	var receivedPostMsg map[string]any
	var receivedPatchMsg map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.Method {
		case http.MethodPost:
			_ = json.Unmarshal(body, &receivedPostMsg)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "msg-discord-12345", "channel_id": "chan-999"}`))
		case http.MethodPatch:
			_ = json.Unmarshal(body, &receivedPatchMsg)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "msg-discord-12345", "channel_id": "chan-999"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := NewDiscordAdapter(DiscordConfig{
		BotToken:         "discord-bot-token-xyz",
		PublicKey:        "11223344",
		DefaultChannelID: "chan-999",
		BaseURL:          srv.URL,
		HTTPClient:       srv.Client(),
	})

	ctx := context.Background()

	t.Run("SendNotification with embed", func(t *testing.T) {
		ref, err := adapter.SendNotification(ctx, NotificationMessage{
			Channel:  "chan-999",
			ThreadTS: "parent-msg-1",
			Title:    "PR Ready",
			Body:     "All checks passed",
			Level:    "success",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.MessageID != "msg-discord-12345" {
			t.Errorf("expected message ID msg-discord-12345, got %s", ref.MessageID)
		}
		embeds := receivedPostMsg["embeds"].([]any)
		if len(embeds) == 0 {
			t.Fatal("expected embeds in post payload")
		}
	})

	t.Run("PromptApproval formats embed and components correctly", func(t *testing.T) {
		req := ApprovalPromptRequest{
			RunID:               "run-discord-gate-01",
			Token:               "token-discord-secret",
			Channel:             "chan-999",
			Summary:             "Reviewer completed successfully",
			CommitSHA:           "commit-sha-777",
			DiffURL:             "https://github.com/org/repo/pull/5/files",
			CoveragePercent:     88.5,
			CriticalAlerts:      1,
			HighAlerts:          2,
			ExpiresAt:           time.Now().Add(30 * time.Minute),
			HasMigrationChanges: true,
		}

		ref, err := adapter.PromptApproval(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref == nil {
			t.Fatal("expected non-nil MessageRef")
		}

		comps, ok := receivedPostMsg["components"].([]any)
		if !ok || len(comps) == 0 {
			t.Fatalf("expected components array, got %v", receivedPostMsg["components"])
		}
		row := comps[0].(map[string]any)
		buttons := row["components"].([]any)
		if len(buttons) < 3 {
			t.Errorf("expected 3 buttons in action row (approve, reject, diff), got %d", len(buttons))
		}
	})

	t.Run("UpdateStatus posts progress to thread", func(t *testing.T) {
		ref, err := adapter.UpdateStatus(ctx, StatusUpdate{
			RunID:     "run-discord-status-01",
			StepName:  "builder-tdd",
			ThreadTS:  "msg-discord-12345",
			Status:    "running",
			Details:   "Executing unit test suite...",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.MessageID != "msg-discord-12345" {
			t.Errorf("expected message ID msg-discord-12345, got %s", ref.MessageID)
		}
	})

	t.Run("UpdateMessageWithDecision updates embed and clears components", func(t *testing.T) {
		err := adapter.UpdateMessageWithDecision(ctx, "chan-999", "msg-discord-12345", GateResolution{
			RunID:     "run-discord-gate-01",
			Decision:  "REJECT",
			User:      "dave",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		comps := receivedPatchMsg["components"].([]any)
		if len(comps) != 0 {
			t.Errorf("expected empty components on resolved card, got %d", len(comps))
		}
	})

	t.Run("Interactive Gate Card with embed fields (Status, Reason, Reviewer Scores) and button components", func(t *testing.T) {
		req := ApprovalPromptRequest{
			RunID:               "run-discord-gate-99",
			Token:               "hmac-discord-token-8888",
			TaskTitle:           "Production Deployment Approval",
			GateReason:          "Deployment to production requires manual signoff",
			FindingsCount:       3,
			ReviewerScores:      []string{"Lint: 5/5", "Tests: 100%"},
			CoveragePercent:     95.0,
			CriticalAlerts:      0,
			HighAlerts:          1,
			DiffURL:             "https://canvas.gv-factory.local/diff/run-discord-99",
			ExpiresAt:           time.Now().Add(20 * time.Minute),
			HasMigrationChanges: true,
			HasAuthChanges:      true,
			HasSchemaChanges:    true,
		}

		ref, err := adapter.PromptApproval(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref == nil {
			t.Fatal("expected non-nil MessageRef")
		}

		embeds := receivedPostMsg["embeds"].([]any)
		if len(embeds) == 0 {
			t.Fatal("expected embed in post payload")
		}
		embed := embeds[0].(map[string]any)
		title, _ := embed["title"].(string)
		if !strings.Contains(title, "Production Deployment Approval") {
			t.Errorf("expected embed title to contain TaskTitle, got %q", title)
		}

		fields := embed["fields"].([]any)
		var hasStatus, hasReason, hasScores bool
		for _, f := range fields {
			field := f.(map[string]any)
			fieldName, _ := field["name"].(string)
			fieldVal, _ := field["value"].(string)
			switch fieldName {
			case "Status":
				hasStatus = true
				if !strings.Contains(fieldVal, "Human Approval Required") && !strings.Contains(fieldVal, "Pending") {
					t.Errorf("expected Status field value, got %q", fieldVal)
				}
			case "Reason":
				hasReason = true
				if !strings.Contains(fieldVal, "Deployment to production requires manual signoff") {
					t.Errorf("expected Reason field value, got %q", fieldVal)
				}
			case "Reviewer Scores":
				hasScores = true
				if !strings.Contains(fieldVal, "Lint: 5/5") {
					t.Errorf("expected Reviewer Scores field value, got %q", fieldVal)
				}
			}
		}

		if !hasStatus {
			t.Error("expected 'Status' field in Discord embed")
		}
		if !hasReason {
			t.Error("expected 'Reason' field in Discord embed")
		}
		if !hasScores {
			t.Error("expected 'Reviewer Scores' field in Discord embed")
		}

		// Check Action Row buttons
		comps, ok := receivedPostMsg["components"].([]any)
		if !ok || len(comps) == 0 {
			t.Fatal("expected components in Discord payload")
		}
		row := comps[0].(map[string]any)
		buttons := row["components"].([]any)
		var hasApproveBtn, hasRejectBtn, hasDiffBtn bool
		for _, b := range buttons {
			btn := b.(map[string]any)
			cid, _ := btn["custom_id"].(string)
			if strings.HasPrefix(cid, "btn_gate_approve") {
				hasApproveBtn = true
				if !strings.Contains(cid, req.Token) {
					t.Errorf("expected btn_gate_approve custom_id to contain token, got %q", cid)
				}
				if btn["style"] != float64(3) && btn["style"] != 3 {
					t.Errorf("expected style 3 for approve, got %v", btn["style"])
				}
			} else if strings.HasPrefix(cid, "btn_gate_reject") {
				hasRejectBtn = true
				if !strings.Contains(cid, req.Token) {
					t.Errorf("expected btn_gate_reject custom_id to contain token, got %q", cid)
				}
				if btn["style"] != float64(4) && btn["style"] != 4 {
					t.Errorf("expected style 4 for reject, got %v", btn["style"])
				}
			} else if btn["style"] == float64(5) || btn["style"] == 5 {
				hasDiffBtn = true
				if btn["url"] != req.DiffURL {
					t.Errorf("expected diff url %s, got %v", req.DiffURL, btn["url"])
				}
			}
		}

		if !hasApproveBtn {
			t.Error("expected 'btn_gate_approve' button in Action Row")
		}
		if !hasRejectBtn {
			t.Error("expected 'btn_gate_reject' button in Action Row")
		}
		if !hasDiffBtn {
			t.Error("expected diff link button in Action Row")
		}
	})

	t.Run("UpdateMessageWithDecision shows verified resolution format", func(t *testing.T) {
		// Test approved format
		err := adapter.UpdateMessageWithDecision(ctx, "chan-999", "msg-discord-12345", GateResolution{
			RunID:     "run-discord-gate-01",
			Decision:  "approved",
			User:      "dave",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		embeds := receivedPatchMsg["embeds"].([]any)
		embed := embeds[0].(map[string]any)
		title := embed["title"].(string)
		if !strings.Contains(title, "✅ Gate approved by @dave") {
			t.Errorf("expected title to contain '✅ Gate approved by @dave', got %q", title)
		}

		// Test rejected format
		err = adapter.UpdateMessageWithDecision(ctx, "chan-999", "msg-discord-12345", GateResolution{
			RunID:     "run-discord-gate-01",
			Decision:  "rejected",
			User:      "carol",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		embeds = receivedPatchMsg["embeds"].([]any)
		embed = embeds[0].(map[string]any)
		title = embed["title"].(string)
		if !strings.Contains(title, "❌ Gate rejected by @carol") {
			t.Errorf("expected title to contain '❌ Gate rejected by @carol', got %q", title)
		}
	})

	t.Run("DiscordAdapter error paths", func(t *testing.T) {
		if adapter.Platform() != "discord" {
			t.Errorf("expected platform discord, got %s", adapter.Platform())
		}

		// Test levels: warning, error, default
		_, _ = adapter.SendNotification(ctx, NotificationMessage{Channel: "chan-999", Level: "warning", Title: "Warn", Body: "Warning body"})
		_, _ = adapter.SendNotification(ctx, NotificationMessage{Channel: "chan-999", Level: "error", Title: "Err", Body: "Error body"})
		_, _ = adapter.SendNotification(ctx, NotificationMessage{Channel: "chan-999", Level: "info", Title: "Info", Body: "Info body"})

		// Test statuses: running, completed, failed, unknown
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "chan-999", Status: "running", StepName: "builder", Details: "Running"})
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "chan-999", Status: "completed", StepName: "builder", Details: "Done"})
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "chan-999", Status: "failed", StepName: "builder", Details: "Failed"})
		_, _ = adapter.UpdateStatus(ctx, StatusUpdate{Channel: "chan-999", Status: "other", StepName: "builder"})

		noChanAdapter := NewDiscordAdapter(DiscordConfig{
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

		// Discord API error responses (status >= 300)
		errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message": "Missing Permissions"}`))
		}))
		defer errSrv.Close()

		errAdapter := NewDiscordAdapter(DiscordConfig{
			DefaultChannelID: "chan-1",
			BaseURL:          errSrv.URL,
			HTTPClient:       errSrv.Client(),
		})
		if _, err := errAdapter.SendNotification(ctx, NotificationMessage{Title: "hi"}); err == nil {
			t.Error("expected error when discord API returns status 403")
		}
		if err := errAdapter.UpdateMessageWithDecision(ctx, "chan-1", "msg-1", GateResolution{Decision: "approved"}); err == nil {
			t.Error("expected error when discord patch message returns status 403")
		}

		// Discord API invalid JSON response on post
		badJsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer badJsonSrv.Close()
		badAdapter := NewDiscordAdapter(DiscordConfig{
			DefaultChannelID: "chan-1",
			BaseURL:          badJsonSrv.URL,
			HTTPClient:       badJsonSrv.Client(),
		})
		if _, err := badAdapter.SendNotification(ctx, NotificationMessage{Title: "hi"}); err == nil {
			t.Error("expected error for bad json response from discord")
		}
	})
}
