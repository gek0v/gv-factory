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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
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
		if resolvedRes.Decision != "REJECT" && resolvedRes.Decision != "rejected" {
			t.Errorf("expected decision REJECT or rejected, got %s", resolvedRes.Decision)
		}
		if resolvedRes.Token != "token-discord-reject-99" {
			t.Errorf("expected token token-discord-reject-99, got %s", resolvedRes.Token)
		}
		if resolvedRes.User != "reviewer_dan" {
			t.Errorf("expected user reviewer_dan, got %s", resolvedRes.User)
		}
	})
}

func TestChatHandler_InteractiveGateResolution(t *testing.T) {
	authSvc := auth.NewService("hmac-secret-for-chat-tests-12345")
	runID := "run-interactive-gate-77"
	validToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(30*time.Minute))

	var slackUpdatedMsg map[string]any
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &slackUpdatedMsg)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true, "ts": "1730000000.999"}`))
	}))
	defer slackSrv.Close()

	slackAdapter := NewSlackAdapter(SlackConfig{
		BotToken:       "xoxb-test",
		SigningSecret:  "slack-secret-123",
		DefaultChannel: "C_GATE_TEST",
		BaseURL:        slackSrv.URL,
		HTTPClient:     slackSrv.Client(),
	})

	var resolvedRes GateResolution
	var resolvedMu sync.Mutex

	mockResolver := GateResolverFunc(func(ctx context.Context, res GateResolution) error {
		resolvedMu.Lock()
		resolvedRes = res
		resolvedMu.Unlock()
		return nil
	})

	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyHex := hex.EncodeToString(pubKey)

	handler := NewChatHandler(HandlerConfig{
		SlackSigningSecret: "slack-secret-123",
		DiscordPublicKey:   pubKeyHex,
		AuthService:        authSvc,
		GateResolver:       mockResolver,
		SlackAdapter:       slackAdapter,
	})

	t.Run("Slack approve_gate button with HMAC token resolves gate and updates in-place", func(t *testing.T) {
		payloadObj := map[string]any{
			"type": "block_actions",
			"user": map[string]any{
				"id":       "U_ALICE",
				"username": "alice",
			},
			"channel": map[string]any{
				"id": "C_GATE_TEST",
			},
			"message": map[string]any{
				"ts": "1730000000.999",
			},
			"actions": []map[string]any{
				{
					"action_id": "approve_gate",
					"value":     validToken,
				},
			},
		}

		payloadBytes, _ := json.Marshal(payloadObj)
		formData := url.Values{}
		formData.Set("payload", string(payloadBytes))
		body := []byte(formData.Encode())

		now := time.Now()
		nowSec := fmt.Sprintf("%d", now.Unix())
		mac := hmac.New(sha256.New, []byte("slack-secret-123"))
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
		if resolvedRes.RunID != runID {
			t.Errorf("expected extracted RunID %s, got %s", runID, resolvedRes.RunID)
		}
		if resolvedRes.Decision != "approved" && resolvedRes.Decision != "APPROVE" {
			t.Errorf("expected decision approved, got %s", resolvedRes.Decision)
		}
		if resolvedRes.Token != validToken {
			t.Errorf("expected token %s, got %s", validToken, resolvedRes.Token)
		}

		// Verify Slack message update
		text, _ := slackUpdatedMsg["text"].(string)
		if !strings.Contains(text, "✅ Gate approved by @alice") {
			t.Errorf("expected updated message to contain '✅ Gate approved by @alice', got %q", text)
		}
	})

	t.Run("Slack reject_gate button with HMAC token resolves gate as rejected", func(t *testing.T) {
		payloadObj := map[string]any{
			"type": "block_actions",
			"user": map[string]any{
				"id":       "U_BOB",
				"username": "bob",
			},
			"channel": map[string]any{
				"id": "C_GATE_TEST",
			},
			"message": map[string]any{
				"ts": "1730000000.999",
			},
			"actions": []map[string]any{
				{
					"action_id": "reject_gate",
					"value":     validToken,
				},
			},
		}

		payloadBytes, _ := json.Marshal(payloadObj)
		formData := url.Values{}
		formData.Set("payload", string(payloadBytes))
		body := []byte(formData.Encode())

		now := time.Now()
		nowSec := fmt.Sprintf("%d", now.Unix())
		mac := hmac.New(sha256.New, []byte("slack-secret-123"))
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
		if resolvedRes.RunID != runID {
			t.Errorf("expected extracted RunID %s, got %s", runID, resolvedRes.RunID)
		}
		if resolvedRes.Decision != "rejected" && resolvedRes.Decision != "REJECT" {
			t.Errorf("expected decision rejected, got %s", resolvedRes.Decision)
		}

		// Verify Slack message update
		text, _ := slackUpdatedMsg["text"].(string)
		if !strings.Contains(text, "❌ Gate rejected by @bob") {
			t.Errorf("expected updated message to contain '❌ Gate rejected by @bob', got %q", text)
		}
	})

	t.Run("Discord btn_gate_approve resolves gate and responds with in-place update", func(t *testing.T) {
		interactionObj := map[string]any{
			"type":       3,
			"channel_id": "chan-discord-gate",
			"data": map[string]any{
				"custom_id": fmt.Sprintf("btn_gate_approve:%s", validToken),
			},
			"user": map[string]any{
				"id":       "U_CAROL",
				"username": "carol",
			},
			"message": map[string]any{
				"id": "msg-discord-gate-1",
			},
		}

		body, _ := json.Marshal(interactionObj)
		ts := fmt.Sprintf("%d", time.Now().Unix())
		msg := append([]byte(ts), body...)
		sig := ed25519.Sign(privKey, msg)
		sigHex := hex.EncodeToString(sig)

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

		data := updateResp["data"].(map[string]any)
		embeds := data["embeds"].([]any)
		embed := embeds[0].(map[string]any)
		title, _ := embed["title"].(string)
		if !strings.Contains(title, "✅ Gate approved by @carol") {
			t.Errorf("expected title to contain '✅ Gate approved by @carol', got %q", title)
		}

		resolvedMu.Lock()
		defer resolvedMu.Unlock()
		if resolvedRes.RunID != runID {
			t.Errorf("expected extracted RunID %s, got %s", runID, resolvedRes.RunID)
		}
		if resolvedRes.Decision != "approved" && resolvedRes.Decision != "APPROVE" {
			t.Errorf("expected decision approved, got %s", resolvedRes.Decision)
		}
	})

	t.Run("Discord btn_gate_reject resolves gate as rejected", func(t *testing.T) {
		interactionObj := map[string]any{
			"type":       3,
			"channel_id": "chan-discord-gate",
			"data": map[string]any{
				"custom_id": fmt.Sprintf("btn_gate_reject:%s", validToken),
			},
			"user": map[string]any{
				"id":       "U_DAVE",
				"username": "dave",
			},
			"message": map[string]any{
				"id": "msg-discord-gate-1",
			},
		}

		body, _ := json.Marshal(interactionObj)
		ts := fmt.Sprintf("%d", time.Now().Unix())
		msg := append([]byte(ts), body...)
		sig := ed25519.Sign(privKey, msg)
		sigHex := hex.EncodeToString(sig)

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
		data := updateResp["data"].(map[string]any)
		embeds := data["embeds"].([]any)
		embed := embeds[0].(map[string]any)
		title, _ := embed["title"].(string)
		if !strings.Contains(title, "❌ Gate rejected by @dave") {
			t.Errorf("expected title to contain '❌ Gate rejected by @dave', got %q", title)
		}
	})
}

func TestChatHandler_OVServerResolution(t *testing.T) {
	var receivedResolvePayload map[string]string
	var resolveMu sync.Mutex

	ovServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/gates/resolve" || r.URL.Path == "/api/gate/resolve" {
			var p map[string]string
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &p)
			resolveMu.Lock()
			receivedResolvePayload = p
			resolveMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ovServer.Close()

	authSvc := auth.NewService("ovserver-test-secret-12345")
	runID := "run-ov-server-55"
	token := authSvc.GenerateApprovalToken(runID, time.Now().Add(10*time.Minute))

	handler := NewChatHandler(HandlerConfig{
		SlackSigningSecret: "slack-secret-ov",
		OVServerURL:        ovServer.URL,
		AuthService:        authSvc,
	})

	payloadObj := map[string]any{
		"type": "block_actions",
		"user": map[string]any{
			"id":       "U_TEST",
			"username": "tester",
		},
		"channel": map[string]any{
			"id": "C_TEST",
		},
		"message": map[string]any{
			"ts": "1730000000.111",
		},
		"actions": []map[string]any{
			{
				"action_id": "approve_gate",
				"value":     token,
			},
		},
	}

	payloadBytes, _ := json.Marshal(payloadObj)
	formData := url.Values{}
	formData.Set("payload", string(payloadBytes))
	body := []byte(formData.Encode())

	now := time.Now()
	nowSec := fmt.Sprintf("%d", now.Unix())
	mac := hmac.New(sha256.New, []byte("slack-secret-ov"))
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

	resolveMu.Lock()
	defer resolveMu.Unlock()
	if receivedResolvePayload["run_id"] != runID {
		t.Errorf("expected run_id %s sent to ov-server, got %s", runID, receivedResolvePayload["run_id"])
	}
	if receivedResolvePayload["token"] != token {
		t.Errorf("expected token %s sent to ov-server, got %s", token, receivedResolvePayload["token"])
	}
	if receivedResolvePayload["decision"] != "approved" {
		t.Errorf("expected decision 'approved' sent to ov-server, got %s", receivedResolvePayload["decision"])
	}
}

func TestChatHandler_EdgeCasesAndErrors(t *testing.T) {
	signingSecret := "slack-secret-edge"
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyHex := hex.EncodeToString(pubKey)

	authSvc := auth.NewService("auth-edge-secret")
	runID := "run-edge-1"
	validToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(10*time.Minute))

	var responseURLCalled bool
	var responseURLMu sync.Mutex
	responseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseURLMu.Lock()
		responseURLCalled = true
		responseURLMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer responseSrv.Close()

	handler := NewChatHandler(HandlerConfig{
		SlackSigningSecret: signingSecret,
		DiscordPublicKey:   pubKeyHex,
		AuthService:        authSvc,
		CommandExecutor: CommandExecutorFunc(func(ctx context.Context, cmd SlashCommand) (string, error) {
			if cmd.Subcommand == "fail" {
				return "", fmt.Errorf("simulated failure")
			}
			return "OK", nil
		}),
	})

	signSlack := func(body []byte) (string, string) {
		now := time.Now()
		nowSec := fmt.Sprintf("%d", now.Unix())
		mac := hmac.New(sha256.New, []byte(signingSecret))
		_, _ = mac.Write([]byte(fmt.Sprintf("v0:%s:", nowSec)))
		_, _ = mac.Write(body)
		return "v0=" + hex.EncodeToString(mac.Sum(nil)), nowSec
	}

	signDiscord := func(body []byte) (string, string) {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		msg := append([]byte(ts), body...)
		sig := ed25519.Sign(privKey, msg)
		return hex.EncodeToString(sig), ts
	}

	t.Run("Slack interactions method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/interactions", nil)
		w := httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", w.Code)
		}
	})

	t.Run("Slack interactions missing or invalid payload", func(t *testing.T) {
		// Empty body / invalid signature
		req := httptest.NewRequest(http.MethodPost, "/interactions", strings.NewReader("bad"))
		w := httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", w.Code)
		}

		// Valid sig, missing payload field
		body := []byte("foo=bar")
		sig, ts := signSlack(body)
		req = httptest.NewRequest(http.MethodPost, "/interactions", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for missing payload, got %d", w.Code)
		}

		// Valid sig, invalid JSON
		vals := url.Values{"payload": {"not-json"}}
		body = []byte(vals.Encode())
		sig, ts = signSlack(body)
		req = httptest.NewRequest(http.MethodPost, "/interactions", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for invalid json, got %d", w.Code)
		}

		// Non-block_actions type
		vals = url.Values{"payload": {`{"type":"view_submission"}`}}
		body = []byte(vals.Encode())
		sig, ts = signSlack(body)
		req = httptest.NewRequest(http.MethodPost, "/interactions", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK for view_submission, got %d", w.Code)
		}

		// User with only ID and unknown action_id plus invalid token error
		vals = url.Values{"payload": {`{"type":"block_actions","user":{"id":"U_ONLY_ID"},"actions":[{"action_id":"unknown_action"},{"action_id":"approve_gate","value":"bad-token"}]}`}}
		body = []byte(vals.Encode())
		sig, ts = signSlack(body)
		req = httptest.NewRequest(http.MethodPost, "/interactions", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", w.Code)
		}
	})

	t.Run("Slack interaction fallback to response_url and user fallbacks", func(t *testing.T) {
		payloadObj := map[string]any{
			"type": "block_actions",
			"user": map[string]any{
				"id":   "U_FALLBACK",
				"name": "john_doe",
			},
			"response_url": responseSrv.URL,
			"actions": []map[string]any{
				{
					"action_id": "approve_gate",
					"value":     validToken,
				},
			},
		}
		data, _ := json.Marshal(payloadObj)
		vals := url.Values{"payload": {string(data)}}
		body := []byte(vals.Encode())
		sig, ts := signSlack(body)

		req := httptest.NewRequest(http.MethodPost, "/interactions", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w := httptest.NewRecorder()
		handler.HandleSlackInteractions(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		responseURLMu.Lock()
		called := responseURLCalled
		responseURLMu.Unlock()
		if !called {
			t.Error("expected response_url to be called when SlackAdapter is nil")
		}
	})

	t.Run("Slack commands method not allowed and help command", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/commands", nil)
		w := httptest.NewRecorder()
		handler.HandleSlackCommands(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", w.Code)
		}

		// Help command
		vals := url.Values{"command": {"/factory"}, "text": {"help"}}
		body := []byte(vals.Encode())
		sig, ts := signSlack(body)
		req = httptest.NewRequest(http.MethodPost, "/commands", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleSlackCommands(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		// Failing command with response_url
		vals = url.Values{"command": {"/factory"}, "text": {"fail"}, "response_url": {responseSrv.URL}}
		body = []byte(vals.Encode())
		sig, ts = signSlack(body)
		req = httptest.NewRequest(http.MethodPost, "/commands", strings.NewReader(string(body)))
		req.Header.Set(HeaderSlackSignature, sig)
		req.Header.Set(HeaderSlackTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleSlackCommands(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("Discord interactions method not allowed, bad json, and Type 2 commands", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/discord", nil)
		w := httptest.NewRecorder()
		handler.HandleDiscordInteractions(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", w.Code)
		}

		// Bad JSON with valid signature
		body := []byte("not-json")
		sig, ts := signDiscord(body)
		req = httptest.NewRequest(http.MethodPost, "/discord", strings.NewReader(string(body)))
		req.Header.Set(HeaderDiscordSignature, sig)
		req.Header.Set(HeaderDiscordTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleDiscordInteractions(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", w.Code)
		}

		// Type 2 command with suboptions
		cmdObj := map[string]any{
			"type": 2,
			"data": map[string]any{
				"name": "factory",
				"options": []map[string]any{
					{
						"name": "run",
						"options": []map[string]any{
							{"name": "issue", "value": "42"},
						},
					},
				},
			},
			"member": map[string]any{
				"user": map[string]any{
					"id":       "U_MEM",
					"username": "mem_user",
				},
			},
		}
		body, _ = json.Marshal(cmdObj)
		sig, ts = signDiscord(body)
		req = httptest.NewRequest(http.MethodPost, "/discord", strings.NewReader(string(body)))
		req.Header.Set(HeaderDiscordSignature, sig)
		req.Header.Set(HeaderDiscordTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleDiscordInteractions(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK for Type 2, got %d", w.Code)
		}
		time.Sleep(50 * time.Millisecond)

		// Unknown interaction type
		unknownObj := map[string]any{"type": 99}
		body, _ = json.Marshal(unknownObj)
		sig, ts = signDiscord(body)
		req = httptest.NewRequest(http.MethodPost, "/discord", strings.NewReader(string(body)))
		req.Header.Set(HeaderDiscordSignature, sig)
		req.Header.Set(HeaderDiscordTimestamp, ts)
		w = httptest.NewRecorder()
		handler.HandleDiscordInteractions(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK for unknown type, got %d", w.Code)
		}
	})

	t.Run("dispatchGateResolution error handling", func(t *testing.T) {
		ctx := context.Background()

		// 1. Invalid HMAC token
		err := handler.dispatchGateResolution(ctx, GateResolution{
			RunID:    "run-wrong",
			Token:    "invalid-token",
			Decision: "approved",
		})
		if err == nil {
			t.Error("expected error for invalid HMAC token")
		}

		// 2. OVServer returning 500 error
		badOVSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		defer badOVSrv.Close()

		ovHandler := NewChatHandler(HandlerConfig{
			OVServerURL: badOVSrv.URL,
		})
		err = ovHandler.dispatchGateResolution(ctx, GateResolution{
			RunID:    runID,
			Token:    validToken,
			Decision: "rejected",
		})
		if err == nil {
			t.Error("expected error when ov-server returns 500")
		}

		// 3. GateResolver returning error
		failResolver := GateResolverFunc(func(ctx context.Context, res GateResolution) error {
			return fmt.Errorf("custom resolver failure")
		})
		resolverHandler := NewChatHandler(HandlerConfig{
			GateResolver: failResolver,
		})
		err = resolverHandler.dispatchGateResolution(ctx, GateResolution{
			RunID:    runID,
			Token:    validToken,
			Decision: "approved",
		})
		if err == nil {
			t.Error("expected error when GateResolver returns error")
		}

		// 4. postToResponseURL error
		if err := handler.postToResponseURL("http://invalid-url-that-will-fail.local:9999", map[string]any{"test": true}); err == nil {
			t.Error("expected error for unreachable responseURL")
		}
	})
}

