// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HandlerConfig aggregates configurations and dependencies for the chat HTTP handlers.
type HandlerConfig struct {
	SlackSigningSecret string
	DiscordPublicKey   string
	MaxTimestampDrift  time.Duration
	GateResolver       GateResolver
	CommandExecutor    CommandExecutor
	SlackAdapter       *SlackAdapter
	DiscordAdapter     *DiscordAdapter
}

// ChatHandler routes incoming webhook interactions and slash commands from chat platforms.
type ChatHandler struct {
	cfg        HandlerConfig
	httpClient *http.Client
}

// NewChatHandler constructs an initialized ChatHandler.
func NewChatHandler(cfg HandlerConfig) *ChatHandler {
	if cfg.MaxTimestampDrift <= 0 {
		cfg.MaxTimestampDrift = DefaultSlackMaxDrift
	}
	return &ChatHandler{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// HandleSlackInteractions processes button clicks and modal callbacks from Slack.
func (h *ChatHandler) HandleSlackInteractions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if err := VerifySlackSignature(h.cfg.SlackSigningSecret, r.Header, body, time.Now(), h.cfg.MaxTimestampDrift); err != nil {
		http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
		return
	}

	formVals, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "Malformed form payload", http.StatusBadRequest)
		return
	}

	payloadRaw := formVals.Get("payload")
	if payloadRaw == "" {
		http.Error(w, "Missing payload field", http.StatusBadRequest)
		return
	}

	var payload struct {
		Type string `json:"type"`
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"user"`
		Channel struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channel"`
		Message struct {
			TS string `json:"ts"`
		} `json:"message"`
		ResponseURL string `json:"response_url"`
		Actions     []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		} `json:"actions"`
	}

	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		http.Error(w, "Invalid JSON in payload", http.StatusBadRequest)
		return
	}

	if payload.Type != "block_actions" {
		w.WriteHeader(http.StatusOK)
		return
	}

	username := payload.User.Username
	if username == "" {
		username = payload.User.Name
	}
	if username == "" {
		username = payload.User.ID
	}

	for _, act := range payload.Actions {
		var decision string
		switch act.ActionID {
		case "gate_approve":
			decision = "APPROVE"
		case "gate_reject":
			decision = "REJECT"
		default:
			continue
		}

		res := GateResolution{
			Token:     act.Value,
			Decision:  decision,
			User:      username,
			UserID:    payload.User.ID,
			Channel:   payload.Channel.ID,
			MessageID: payload.Message.TS,
			Timestamp: time.Now(),
		}

		if h.cfg.GateResolver != nil {
			if err := h.cfg.GateResolver.ResolveGate(r.Context(), res); err != nil {
				log.Printf("chat: error resolving gate: %v", err)
			}
		}

		if h.cfg.SlackAdapter != nil && payload.Channel.ID != "" && payload.Message.TS != "" {
			_ = h.cfg.SlackAdapter.UpdateMessageWithDecision(r.Context(), payload.Channel.ID, payload.Message.TS, res)
		} else if payload.ResponseURL != "" {
			_ = h.postToResponseURL(payload.ResponseURL, map[string]any{
				"text":             fmt.Sprintf("Decision gate %s by <@%s>", decision, username),
				"replace_original": true,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// HandleSlackCommands handles incoming slash commands (e.g. /factory run, /factory status).
func (h *ChatHandler) HandleSlackCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if err := VerifySlackSignature(h.cfg.SlackSigningSecret, r.Header, body, time.Now(), h.cfg.MaxTimestampDrift); err != nil {
		http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
		return
	}

	formVals, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "Malformed form payload", http.StatusBadRequest)
		return
	}

	command := formVals.Get("command")
	text := formVals.Get("text")
	userID := formVals.Get("user_id")
	userName := formVals.Get("user_name")
	channelID := formVals.Get("channel_id")
	responseURL := formVals.Get("response_url")

	slashCmd, err := ParseSlashCommand("slack", command, text, userID, userName, channelID, responseURL)
	if err != nil {
		http.Error(w, "Failed to parse command", http.StatusBadRequest)
		return
	}

	// Immediate ephemeral acknowledgment within 3 seconds
	ackText := fmt.Sprintf("⏳ Command `%s %s` received. Processing...", command, text)
	if slashCmd.Subcommand == "help" {
		ackText = HelpMessage()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"response_type": "ephemeral",
		"text":          ackText,
	})

	// Dispatch async execution if not help
	if slashCmd.Subcommand != "help" && h.cfg.CommandExecutor != nil {
		go func(cmd SlashCommand, resURL string) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			output, err := h.cfg.CommandExecutor.ExecuteCommand(ctx, cmd)
			if err != nil {
				output = fmt.Sprintf("❌ Error executing `/factory %s`: %v", cmd.Subcommand, err)
			}
			if resURL != "" {
				_ = h.postToResponseURL(resURL, map[string]any{
					"response_type": "in_channel",
					"text":          output,
				})
			}
		}(*slashCmd, responseURL)
	}
}
// HandleDiscordInteractions handles slash commands, ping validations, and component interactions from Discord.
func (h *ChatHandler) HandleDiscordInteractions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if err := VerifyDiscordSignature(h.cfg.DiscordPublicKey, r.Header, body); err != nil {
		http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
		return
	}

	var interaction struct {
		Type int    `json:"type"`
		ID   string `json:"id"`
		Data struct {
			Name     string `json:"name"`
			CustomID string `json:"custom_id"`
			Options  []struct {
				Name    string `json:"name"`
				Value   any    `json:"value"`
				Options []struct {
					Name  string `json:"name"`
					Value any    `json:"value"`
				} `json:"options"`
			} `json:"options"`
		} `json:"data"`
		Member struct {
			User struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"user"`
		} `json:"member"`
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		ChannelID string `json:"channel_id"`
		Message   struct {
			ID string `json:"id"`
		} `json:"message"`
	}

	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Discord Type 1: PING -> PONG
	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":1}`))
		return
	}

	username := interaction.Member.User.Username
	userID := interaction.Member.User.ID
	if username == "" {
		username = interaction.User.Username
		userID = interaction.User.ID
	}

	// Discord Type 3: MESSAGE_COMPONENT (Button Clicks)
	if interaction.Type == 3 {
		customID := interaction.Data.CustomID
		var decision, token string
		if strings.HasPrefix(customID, "gate_approve:") {
			decision = "APPROVE"
			token = strings.TrimPrefix(customID, "gate_approve:")
		} else if strings.HasPrefix(customID, "gate_reject:") {
			decision = "REJECT"
			token = strings.TrimPrefix(customID, "gate_reject:")
		}

		if decision != "" && token != "" {
			res := GateResolution{
				Token:     token,
				Decision:  decision,
				User:      username,
				UserID:    userID,
				Channel:   interaction.ChannelID,
				MessageID: interaction.Message.ID,
				Timestamp: time.Now(),
			}

			if h.cfg.GateResolver != nil {
				if err := h.cfg.GateResolver.ResolveGate(r.Context(), res); err != nil {
					log.Printf("chat: error resolving discord gate: %v", err)
				}
			}

			// Respond with Type 7: UPDATE_MESSAGE
			statusEmoji := "✅"
			statusText := "APPROVED"
			color := 0x10B981 // Green
			if decision == "REJECT" {
				statusEmoji = "❌"
				statusText = "REJECTED"
				color = 0xEF4444 // Red
			}

			responsePayload := map[string]any{
				"type": 7, // UPDATE_MESSAGE
				"data": map[string]any{
					"embeds": []any{
						map[string]any{
							"title":       fmt.Sprintf("%s Decision Gate Resolved: %s", statusEmoji, statusText),
							"description": fmt.Sprintf("Decision submitted by **%s**.", username),
							"color":       color,
							"fields": []map[string]any{
								{"name": "Decision", "value": statusText, "inline": true},
								{"name": "Resolved By", "value": username, "inline": true},
								{"name": "Timestamp", "value": time.Now().Format(time.RFC3339), "inline": true},
							},
						},
					},
					"components": []any{}, // Remove action buttons
				},
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(responsePayload)
			return
		}
	}

	// Discord Type 2: APPLICATION_COMMAND (Slash command)
	if interaction.Type == 2 {
		subcommand := "help"
		args := ""
		if len(interaction.Data.Options) > 0 {
			subcommand = interaction.Data.Options[0].Name
			var argList []string
			for _, subOpt := range interaction.Data.Options[0].Options {
				argList = append(argList, fmt.Sprintf("--%s=%v", subOpt.Name, subOpt.Value))
			}
			args = strings.Join(argList, " ")
		}

		slashCmd, _ := ParseSlashCommand("discord", "/"+interaction.Data.Name, subcommand+" "+args, userID, username, interaction.ChannelID, "")

		// Respond with Type 4: CHANNEL_MESSAGE_WITH_SOURCE
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": 4,
			"data": map[string]any{
				"content": fmt.Sprintf("⏳ Processing `/factory %s`...", subcommand),
				"flags":   64, // Ephemeral
			},
		})

		if h.cfg.CommandExecutor != nil {
			go func(cmd SlashCommand) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				_, _ = h.cfg.CommandExecutor.ExecuteCommand(ctx, cmd)
			}(*slashCmd)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *ChatHandler) postToResponseURL(targetURL string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

