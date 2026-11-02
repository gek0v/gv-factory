// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicVersionHeader  = "2023-06-01"
)

type anthropicClient struct {
	apiKey string
	cfg    ClientConfig
}

// NewAnthropicClient creates an LLMClient for Anthropic Claude.
func NewAnthropicClient(apiKey string, opts ...Option) LLMClient {
	cfg := ClientConfig{
		BaseURL:     defaultAnthropicBaseURL,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
		RetryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &anthropicClient{
		apiKey: apiKey,
		cfg:    cfg,
	}
}

type anthropicToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"` // "text", "tool_use", "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type anthropicWireMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content any    `json:"content"`
}

type anthropicRequest struct {
	Model         string                 `json:"model"`
	System        string                 `json:"system,omitempty"`
	Messages      []anthropicWireMessage `json:"messages"`
	Tools         []anthropicToolSchema  `json:"tools,omitempty"`
	MaxTokens     int                    `json:"max_tokens"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *anthropicClient) buildPayload(req *ChatRequest, stream bool) (*anthropicRequest, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	if len(req.Messages) == 0 {
		return nil, ErrEmptyMessages
	}
	if req.Model == "" {
		return nil, ErrMissingModel
	}

	maxTokens := 4096
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	var systemPrompts []string
	var wireMsgs []anthropicWireMessage

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem:
			if msg.Content != "" {
				systemPrompts = append(systemPrompts, msg.Content)
			}
		case RoleUser:
			wireMsgs = append(wireMsgs, anthropicWireMessage{
				Role:    "user",
				Content: msg.Content,
			})
		case RoleAssistant:
			var blocks []anthropicContentBlock
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{
					Type: "text",
					Text: msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				var inputMap map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputMap)
				}
				if inputMap == nil {
					inputMap = make(map[string]any)
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: inputMap,
				})
			}
			if len(blocks) > 0 {
				wireMsgs = append(wireMsgs, anthropicWireMessage{
					Role:    "assistant",
					Content: blocks,
				})
			} else {
				wireMsgs = append(wireMsgs, anthropicWireMessage{
					Role:    "assistant",
					Content: msg.Content,
				})
			}
		case RoleTool:
			toolCallID := ""
			content := msg.Content
			isErr := false
			if msg.ToolResult != nil {
				toolCallID = msg.ToolResult.ToolCallID
				content = msg.ToolResult.Content
				isErr = msg.ToolResult.IsError
			}
			// Anthropic formats tool_result inside a user message
			wireMsgs = append(wireMsgs, anthropicWireMessage{
				Role: "user",
				Content: []anthropicContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: toolCallID,
						Content:   content,
						IsError:   isErr,
					},
				},
			})
		}
	}

	var tools []anthropicToolSchema
	for _, tool := range req.Tools {
		tools = append(tools, anthropicToolSchema{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Parameters,
		})
	}

	return &anthropicRequest{
		Model:         req.Model,
		System:        strings.Join(systemPrompts, "\n\n"),
		Messages:      wireMsgs,
		Tools:         tools,
		MaxTokens:     maxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
		Stream:        stream,
	}, nil
}

func (c *anthropicClient) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	payload, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal anthropic request: %w", err)
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/messages"

	resp, err := ExecuteWithRetry(ctx, c.cfg.RetryPolicy, func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", anthropicVersionHeader)
		return c.cfg.HTTPClient.Do(httpReq)
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to read response body: %w", err)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("llm: failed to parse response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode >= 400 || anthropicResp.Error != nil {
		msg := fmt.Sprintf("status code %d", resp.StatusCode)
		if anthropicResp.Error != nil {
			msg = fmt.Sprintf("%s: %s", anthropicResp.Error.Type, anthropicResp.Error.Message)
		}
		return nil, fmt.Errorf("llm: anthropic api error: %s", msg)
	}

	var contentBuilder strings.Builder
	var toolCalls []ToolCall

	for _, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			contentBuilder.WriteString(block.Text)
		case "tool_use":
			argBytes, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(argBytes),
				},
			})
		}
	}

	finishReason := "stop"
	if anthropicResp.StopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if anthropicResp.StopReason == "max_tokens" {
		finishReason = "length"
	}

	return &ChatResponse{
		ID:           anthropicResp.ID,
		Model:        anthropicResp.Model,
		FinishReason: finishReason,
		Message: ChatMessage{
			Role:      RoleAssistant,
			Content:   contentBuilder.String(),
			ToolCalls: toolCalls,
		},
		Usage: UsageMetrics{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}, nil
}

func (c *anthropicClient) StreamChatCompletion(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, <-chan error) {
	chunksCh := make(chan ChatChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunksCh)
		defer close(errCh)

		emitChunk := func(chunk ChatChunk) bool {
			select {
			case <-ctx.Done():
				select {
				case errCh <- ctx.Err():
				default:
				}
				return false
			case chunksCh <- chunk:
				return true
			}
		}

		payload, err := c.buildPayload(req, true)
		if err != nil {
			errCh <- err
			return
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			errCh <- fmt.Errorf("llm: failed to marshal stream request: %w", err)
			return
		}

		url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/messages"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", anthropicVersionHeader)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.cfg.HTTPClient.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("llm: stream request failed (status %d): %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		var eventType string
		var msgID, msgModel string
		inputTokens := 0
		outputTokens := 0

		// Current tool call tracking
		activeToolID := ""
		activeToolName := ""

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}

			if strings.HasPrefix(line, "data:") {
				dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if dataStr == "" {
					continue
				}

				var dataObj map[string]any
				if err := json.Unmarshal([]byte(dataStr), &dataObj); err != nil {
					continue
				}

				switch eventType {
				case "message_start":
					if msgObj, ok := dataObj["message"].(map[string]any); ok {
						if id, ok := msgObj["id"].(string); ok {
							msgID = id
						}
						if m, ok := msgObj["model"].(string); ok {
							msgModel = m
						}
						if u, ok := msgObj["usage"].(map[string]any); ok {
							if inTok, ok := u["input_tokens"].(float64); ok {
								inputTokens = int(inTok)
							}
						}
					}
				case "content_block_start":
					if block, ok := dataObj["content_block"].(map[string]any); ok {
						if bType, _ := block["type"].(string); bType == "tool_use" {
							activeToolID, _ = block["id"].(string)
							activeToolName, _ = block["name"].(string)
						}
					}
				case "content_block_delta":
					if delta, ok := dataObj["delta"].(map[string]any); ok {
						dType, _ := delta["type"].(string)
						if dType == "text_delta" {
							if text, ok := delta["text"].(string); ok {
								if !emitChunk(ChatChunk{
									ID:           msgID,
									Model:        msgModel,
									DeltaContent: text,
								}) {
									return
								}
							}
						} else if dType == "input_json_delta" {
							if pJSON, ok := delta["partial_json"].(string); ok {
								if !emitChunk(ChatChunk{
									ID:    msgID,
									Model: msgModel,
									DeltaTool: &ToolCall{
										ID:   activeToolID,
										Type: "function",
										Function: FunctionCall{
											Name:      activeToolName,
											Arguments: pJSON,
										},
									},
								}) {
									return
								}
							}
						}
					}
				case "content_block_stop":
					activeToolID = ""
					activeToolName = ""
				case "message_delta":
					finishReason := "stop"
					if delta, ok := dataObj["delta"].(map[string]any); ok {
						if sr, ok := delta["stop_reason"].(string); ok {
							if sr == "tool_use" {
								finishReason = "tool_calls"
							} else if sr == "max_tokens" {
								finishReason = "length"
							}
						}
					}
					if u, ok := dataObj["usage"].(map[string]any); ok {
						if outTok, ok := u["output_tokens"].(float64); ok {
							outputTokens = int(outTok)
						}
					}
					if !emitChunk(ChatChunk{
						ID:           msgID,
						Model:        msgModel,
						FinishReason: finishReason,
						Usage: &UsageMetrics{
							PromptTokens:     inputTokens,
							CompletionTokens: outputTokens,
							TotalTokens:      inputTokens + outputTokens,
						},
					}) {
						return
					}
				case "message_stop":
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
			case errCh <- err:
			}
		}
	}()

	return chunksCh, errCh
}
