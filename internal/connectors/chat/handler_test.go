// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChatHandler_SlackInteractions(t *testing.T) {
	signingSecret := "slack-test-secret-999"
	var resolvedRes GateResolution
	var resolvedMu sync.Mutex

	mockResolver := GateResolverFunc(func(ctx context.Context, res GateResolution) error {
		resolvedMu.Lock()
		resolvedRes = res
		resolvedMu.Unlock()
		return nil
	})

	handler := NewChatHandler(HandlerConfig{
		SlackSigningSecret: signingSecret,
		GateResolver:       mockResolver,
	})

	t.Run("Approve button click resolves gate", func(t *testing.T) {
		payloadObj := map[string]any{
			"type": "block_actions",
			"user": map[string]any{
				"id":       "U12345",
				"username": "alice",
			},
			"channel": map[string]any{
				"id": "C12345",
			},
			"message": map[string]any{
				"ts": "1730000000.123456",
			},
			"actions": []map[string]any{
				{
					"action_id": "gate_approve",
					"value":     "token-run-101-approved",
				},
			},
		}

		payloadBytes, _ := json.Marshal(payloadObj)
		formData := url.Values{}
		formData.Set("payload", string(payloadBytes))
		body := []byte(formData.Encode())

		now := time.Now()
		nowSec := fmt.Sprintf("%d", now.Unix())
		mac := hmac.New(sha256.New, []byte(signingSecret))
		_, _ = mac.Write([]byte(fmt.Sprintf("v0:%s:", nowSec)))
		_, _ = mac.Write(body)
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/slack/interactions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, nowSec)

		w := httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		resolvedMu.Lock()
		defer resolvedMu.Unlock()
		if resolvedRes.Decision != "APPROVE" {
			t.Errorf("expected decision APPROVE, got %s", resolvedRes.Decision)
		}
		if resolvedRes.Token != "token-run-101-approved" {
			t.Errorf("expected token token-run-101-approved, got %s", resolvedRes.Token)
		}
		if resolvedRes.User != "alice" {
			t.Errorf("expected user alice, got %s", resolvedRes.User)
		}
	})
}

func TestChatHandler_SlackCommands(t *testing.T) {
	signingSecret := "slack-test-cmd-secret"
	cmdExecuted := make(chan SlashCommand, 1)

	mockExecutor := CommandExecutorFunc(func(ctx context.Context, cmd SlashCommand) (string, error) {
		cmdExecuted <- cmd
		return "Pipeline triggered successfully!", nil
	})

	handler := NewChatHandler(HandlerConfig{
		SlackSigningSecret: signingSecret,
		CommandExecutor:    mockExecutor,
	})

	t.Run("Valid /factory run command returns fast ack and executes async", func(t *testing.T) {
		formData := url.Values{}
		formData.Set("command", "/factory")
		formData.Set("text", "run bugfix-express --issue 42 --branch feat/fix")
		formData.Set("user_id", "U999")
		formData.Set("user_name", "bob")
		formData.Set("channel_id", "C888")
		body := []byte(formData.Encode())

		now := time.Now()
		nowSec := fmt.Sprintf("%d", now.Unix())
		mac := hmac.New(sha256.New, []byte(signingSecret))
		_, _ = mac.Write([]byte(fmt.Sprintf("v0:%s:", nowSec)))
		_, _ = mac.Write(body)
		sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/slack/commands", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, nowSec)

		w := httptest.NewRecorder()
		handler.HandleSlackCommands(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var ackResp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &ackResp); err != nil {
			t.Fatalf("failed to decode json ack: %v", err)
		}
		if ackResp["response_type"] != "ephemeral" {
			t.Errorf("expected ephemeral response_type, got %v", ackResp["response_type"])
		}

		select {
		case executed := <-cmdExecuted:
			if executed.Subcommand != "run" {
				t.Errorf("expected subcommand 'run', got %s", executed.Subcommand)
			}
			if executed.Template != "bugfix-express" {
				t.Errorf("expected template 'bugfix-express', got %s", executed.Template)
			}
			if executed.Issue != "42" {
				t.Errorf("expected issue '42', got %s", executed.Issue)
			}
			if executed.Branch != "feat/fix" {
				t.Errorf("expected branch 'feat/fix', got %s", executed.Branch)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for async command execution")
		}
	})

	t.Run("Invalid signature returns 401 Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/slack/commands", strings.NewReader("command=/factory"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(HeaderSlackSignature, "v0=invalid-signature")
		req.Header.Set(HeaderSlackTimestamp, fmt.Sprintf("%d", time.Now().Unix()))

		w := httptest.NewRecorder()
		handler.HandleSlackCommands(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
		}
	})
}
func TestChatHandler_DiscordInteractions(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	pubKeyHex := hex.EncodeToString(pubKey)

	var resolvedRes GateResolution
	var resolvedMu sync.Mutex

	mockResolver := GateResolverFunc(func(ctx context.Context, res GateResolution) error {
		resolvedMu.Lock()
		resolvedRes = res
		resolvedMu.Unlock()
		return nil
	})

	handler := NewChatHandler(HandlerConfig{
		DiscordPublicKey: pubKeyHex,
		GateResolver:     mockResolver,
	})

	signDiscordReq := func(body []byte) (string, string) {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		msg := append([]byte(ts), body...)
		sig := ed25519.Sign(privKey, msg)
		return hex.EncodeToString(sig), ts
	}

	t.Run("Discord Type 1 PING returns PONG Type 1", func(t *testing.T) {
		body := []byte(`{"type":1}`)
		sigHex, ts := signDiscordReq(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/discord/interactions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderDiscordSignature, sigHex)
		req.Header.Set(HeaderDiscordTimestamp, ts)

		w := httptest.NewRecorder()
		handler.HandleDiscordInteractions(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		var pongResp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &pongResp)
		if pongResp["type"] != float64(1) {
			t.Errorf("expected type 1 pong, got %v", pongResp["type"])
		}
	})

	t.Run("Discord Type 3 Button Click resolves gate and returns Type 7 update", func(t *testing.T) {
		interactionObj := map[string]any{
			"type":       3,
			"channel_id": "chan-discord-777",
			"data": map[string]any{
				"custom_id": "gate_reject:token-discord-reject-99",
			},
			"user": map[string]any{
				"id":       "U_DISCORD_1",
				"username": "reviewer_dan",
			},
			"message": map[string]any{
				"id": "msg-to-update-111",
			},
		}

		body, _ := json.Marshal(interactionObj)
		sigHex, ts := signDiscordReq(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/discord/interactions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderDiscordSignature, sigHex)
		req.Header.Set(HeaderDiscordTimestamp, ts)

		w := httptest.NewRecorder()
		handler.HandleDiscordInteractions(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var updateResp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &updateResp)
		if updateResp["type"] != float64(7) {
			t.Errorf("expected type 7 update message, got %v", updateResp["type"])
		}

		resolvedMu.Lock()
		defer resolvedMu.Unlock()
		if resolvedRes.Decision != "REJECT" {
			t.Errorf("expected decision REJECT, got %s", resolvedRes.Decision)
		}
		if resolvedRes.Token != "token-discord-reject-99" {
			t.Errorf("expected token token-discord-reject-99, got %s", resolvedRes.Token)
		}
		if resolvedRes.User != "reviewer_dan" {
			t.Errorf("expected user reviewer_dan, got %s", resolvedRes.User)
		}
	})
}

