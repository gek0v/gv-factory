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
}
