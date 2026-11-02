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

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

type openAIClient struct {
	apiKey string
	cfg    ClientConfig
}

// NewOpenAIClient creates an LLMClient for OpenAI.
func NewOpenAIClient(apiKey string, opts ...Option) LLMClient {
	cfg := ClientConfig{
		BaseURL:     defaultOpenAIBaseURL,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
		RetryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &openAIClient{
		apiKey: apiKey,
		cfg:    cfg,
	}
}

type openAIToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAITool struct {
	Type     string             `json:"type"` // "function"
	Function openAIToolFunction `json:"function"`
}

type openAIWireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIWireMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIWireToolCall `json:"tool_calls,omitempty"`
}

type openAIRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIWireMessage `json:"messages"`
	Tools       []openAITool        `json:"tools,omitempty"`
	ToolChoice  any                 `json:"tool_choice,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	TopP        *float64            `json:"top_p,omitempty"`
	MaxTokens   *int                `json:"max_tokens,omitempty"`
	Stop        []string            `json:"stop,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int               `json:"index"`
		Message      openAIWireMessage `json:"message"`
		FinishReason string            `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func (c *openAIClient) buildPayload(req *ChatRequest, stream bool) (*openAIRequest, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	if len(req.Messages) == 0 {
		return nil, ErrEmptyMessages
	}
	if req.Model == "" {
		return nil, ErrMissingModel
	}

	var wireMsgs []openAIWireMessage

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem:
			wireMsgs = append(wireMsgs, openAIWireMessage{
				Role:    "system",
				Content: msg.Content,
			})
		case RoleUser:
			wireMsgs = append(wireMsgs, openAIWireMessage{
				Role:    "user",
				Content: msg.Content,
			})
		case RoleAssistant:
			var tcs []openAIWireToolCall
			for _, tc := range msg.ToolCalls {
				tType := tc.Type
				if tType == "" {
					tType = "function"
				}
				wireTc := openAIWireToolCall{
					ID:   tc.ID,
					Type: tType,
				}
				wireTc.Function.Name = tc.Function.Name
				wireTc.Function.Arguments = tc.Function.Arguments
				tcs = append(tcs, wireTc)
			}
			wireMsgs = append(wireMsgs, openAIWireMessage{
				Role:      "assistant",
				Content:   msg.Content,
				ToolCalls: tcs,
			})
		case RoleTool:
			toolCallID := ""
			content := msg.Content
			if msg.ToolResult != nil {
				toolCallID = msg.ToolResult.ToolCallID
				content = msg.ToolResult.Content
			}
			wireMsgs = append(wireMsgs, openAIWireMessage{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    content,
			})
		}
	}

	var tools []openAITool
	for _, tool := range req.Tools {
		tools = append(tools, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}

	var toolChoice any
	if req.ToolChoice != "" {
		toolChoice = req.ToolChoice
	}

	return &openAIRequest{
		Model:       req.Model,
		Messages:    wireMsgs,
		Tools:       tools,
		ToolChoice:  toolChoice,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		Stream:      stream,
	}, nil
}

func (c *openAIClient) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	payload, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal openai request: %w", err)
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"

	resp, err := ExecuteWithRetry(ctx, c.cfg.RetryPolicy, func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
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

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("llm: failed to parse response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode >= 400 || openAIResp.Error != nil {
		msg := fmt.Sprintf("status code %d", resp.StatusCode)
		if openAIResp.Error != nil {
			msg = openAIResp.Error.Message
		}
		return nil, fmt.Errorf("llm: openai api error: %s", msg)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty choices in openai response")
	}

	choice := openAIResp.Choices[0]
	var toolCalls []ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	finishReason := choice.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	return &ChatResponse{
		ID:           openAIResp.ID,
		Model:        openAIResp.Model,
		FinishReason: finishReason,
		Message: ChatMessage{
			Role:      RoleAssistant,
			Content:   choice.Message.Content,
			ToolCalls: toolCalls,
		},
		Usage: UsageMetrics{
			PromptTokens:     openAIResp.Usage.PromptTokens,
			CompletionTokens: openAIResp.Usage.CompletionTokens,
			TotalTokens:      openAIResp.Usage.TotalTokens,
		},
	}, nil
}

type openAIStreamDelta struct {
	Content   string `json:"content,omitempty"`
	ToolCalls []struct {
		Index    int    `json:"index"`
		ID       string `json:"id,omitempty"`
		Type     string `json:"type,omitempty"`
		Function struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		} `json:"function"`
	} `json:"tool_calls,omitempty"`
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int               `json:"index"`
		Delta        openAIStreamDelta `json:"delta"`
		FinishReason string            `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (c *openAIClient) StreamChatCompletion(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, <-chan error) {
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

		url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
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

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "" {
				continue
			}
			if dataStr == "[DONE]" {
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) == 0 {
				if chunk.Usage != nil {
					if !emitChunk(ChatChunk{
						ID:    chunk.ID,
						Model: chunk.Model,
						Usage: &UsageMetrics{
							PromptTokens:     chunk.Usage.PromptTokens,
							CompletionTokens: chunk.Usage.CompletionTokens,
							TotalTokens:      chunk.Usage.TotalTokens,
						},
					}) {
						return
					}
				}
				continue
			}

			choice := chunk.Choices[0]
			var deltaTool *ToolCall
			if len(choice.Delta.ToolCalls) > 0 {
				tc := choice.Delta.ToolCalls[0]
				deltaTool = &ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}

			var usage *UsageMetrics
			if chunk.Usage != nil {
				usage = &UsageMetrics{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
			}

			if !emitChunk(ChatChunk{
				ID:           chunk.ID,
				Model:        chunk.Model,
				DeltaContent: choice.Delta.Content,
				DeltaTool:    deltaTool,
				FinishReason: choice.FinishReason,
				Usage:        usage,
			}) {
				return
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
