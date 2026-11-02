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

const defaultOllamaBaseURL = "http://localhost:11434"

// GenerateRequest represents a prompt-based completion request for Ollama /api/generate.
type GenerateRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	System      string   `json:"system,omitempty"`
	Template    string   `json:"template,omitempty"`
	Context     []int    `json:"context,omitempty"`
	Stream      bool     `json:"stream"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// GenerateResponse encapsulates the response from Ollama /api/generate.
type GenerateResponse struct {
	Model      string       `json:"model"`
	CreatedAt  string       `json:"created_at"`
	Response   string       `json:"response"`
	Done       bool         `json:"done"`
	DoneReason string       `json:"done_reason"`
	Context    []int        `json:"context,omitempty"`
	Usage      UsageMetrics `json:"usage"`
}

// OllamaClient defines the extended client interface supporting native Ollama chat and generation.
type OllamaClient interface {
	LLMClient
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
	StreamGenerate(ctx context.Context, req *GenerateRequest) (<-chan ChatChunk, <-chan error)
}

type ollamaClientImpl struct {
	cfg ClientConfig
}

// NewOllamaClient creates an OllamaClient for native Ollama API endpoints (/api/chat, /api/generate).
func NewOllamaClient(opts ...Option) OllamaClient {
	cfg := ClientConfig{
		BaseURL:     defaultOllamaBaseURL,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
		RetryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Normalize BaseURL by stripping trailing slashes or /v1 suffix if supplied by legacy configs
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/v1")

	return &ollamaClientImpl{cfg: cfg}
}

type ollamaWireChatMessage struct {
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	ToolCalls []ollamaWireToolCall `json:"tool_calls,omitempty"`
}

type ollamaWireToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type ollamaWireTool struct {
	Type     string         `json:"type"`
	Function ToolDefinition `json:"function"`
}

type ollamaChatWireRequest struct {
	Model    string                  `json:"model"`
	Messages []ollamaWireChatMessage `json:"messages"`
	Stream   bool                    `json:"stream"`
	Format   string                  `json:"format,omitempty"`
	Options  map[string]any          `json:"options,omitempty"`
	Tools    []ollamaWireTool        `json:"tools,omitempty"`
}

type ollamaChatWireResponse struct {
	Model           string                `json:"model"`
	CreatedAt       string                `json:"created_at"`
	Message         ollamaWireChatMessage `json:"message"`
	Done            bool                  `json:"done"`
	DoneReason      string                `json:"done_reason"`
	PromptEvalCount int                   `json:"prompt_eval_count"`
	EvalCount       int                   `json:"eval_count"`
	TotalDuration   int64                 `json:"total_duration"`
	Error           string                `json:"error,omitempty"`
}

type ollamaGenerateWireRequest struct {
	Model    string         `json:"model"`
	Prompt   string         `json:"prompt"`
	System   string         `json:"system,omitempty"`
	Template string         `json:"template,omitempty"`
	Context  []int          `json:"context,omitempty"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type ollamaGenerateWireResponse struct {
	Model           string `json:"model"`
	CreatedAt       string `json:"created_at"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	Context         []int  `json:"context,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error,omitempty"`
}

func (c *ollamaClientImpl) buildChatPayload(req *ChatRequest, stream bool) (*ollamaChatWireRequest, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	if len(req.Messages) == 0 {
		return nil, ErrEmptyMessages
	}
	if req.Model == "" {
		return nil, ErrMissingModel
	}

	wireMsgs := make([]ollamaWireChatMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := msg.Content
		if msg.Role == RoleTool && msg.ToolResult != nil {
			content = msg.ToolResult.Content
		}

		wireMsg := ollamaWireChatMessage{
			Role:    string(msg.Role),
			Content: content,
		}

		if len(msg.ToolCalls) > 0 {
			wireCalls := make([]ollamaWireToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				var argsMap map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &argsMap)
				}
				var call ollamaWireToolCall
				call.Function.Name = tc.Function.Name
				call.Function.Arguments = argsMap
				wireCalls = append(wireCalls, call)
			}
			wireMsg.ToolCalls = wireCalls
		}

		wireMsgs = append(wireMsgs, wireMsg)
	}

	options := make(map[string]any)
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}
	if req.MaxTokens != nil {
		options["num_predict"] = *req.MaxTokens
	}
	if len(req.Stop) > 0 {
		options["stop"] = req.Stop
	}

	var wireTools []ollamaWireTool
	if len(req.Tools) > 0 {
		wireTools = make([]ollamaWireTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			wireTools = append(wireTools, ollamaWireTool{
				Type:     "function",
				Function: t,
			})
		}
	}

	var optsMap map[string]any
	if len(options) > 0 {
		optsMap = options
	}

	return &ollamaChatWireRequest{
		Model:    req.Model,
		Messages: wireMsgs,
		Stream:   stream,
		Options:  optsMap,
		Tools:    wireTools,
	}, nil
}

// ChatCompletion processes a non-streaming chat completion request against native Ollama.
func (c *ollamaClientImpl) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	payload, err := c.buildChatPayload(req, false)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	endpoint := c.cfg.BaseURL + "/api/chat"

	httpResp, err := ExecuteWithRetry(ctx, c.cfg.RetryPolicy, func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		return c.cfg.HTTPClient.Do(httpReq)
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to read response body: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		var errObj struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(respBytes, &errObj); jsonErr == nil && errObj.Error != "" {
			return nil, fmt.Errorf("ollama: api error (%d): %s", httpResp.StatusCode, errObj.Error)
		}
		return nil, fmt.Errorf("ollama: api error with status %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var wireResp ollamaChatWireResponse
	if err := json.Unmarshal(respBytes, &wireResp); err != nil {
		return nil, fmt.Errorf("ollama: failed to decode response: %w", err)
	}

	if wireResp.Error != "" {
		return nil, fmt.Errorf("ollama: returned error: %s", wireResp.Error)
	}

	var toolCalls []ToolCall
	if len(wireResp.Message.ToolCalls) > 0 {
		for i, tc := range wireResp.Message.ToolCalls {
			argsBytes, _ := json.Marshal(tc.Function.Arguments)
			toolCalls = append(toolCalls, ToolCall{
				ID:   fmt.Sprintf("call_%d", i),
				Type: "function",
				Function: FunctionCall{
					Name:      tc.Function.Name,
					Arguments: string(argsBytes),
				},
			})
		}
	}

	finishReason := wireResp.DoneReason
	if finishReason == "" && wireResp.Done {
		finishReason = "stop"
	}

	usage := UsageMetrics{
		PromptTokens:     wireResp.PromptEvalCount,
		CompletionTokens: wireResp.EvalCount,
		TotalTokens:      wireResp.PromptEvalCount + wireResp.EvalCount,
	}

	return &ChatResponse{
		ID:    fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
		Model: wireResp.Model,
		Message: ChatMessage{
			Role:      Role(wireResp.Message.Role),
			Content:   wireResp.Message.Content,
			ToolCalls: toolCalls,
		},
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

// StreamChatCompletion opens a streaming channel to native Ollama emitting chunks as NDJSON.
func (c *ollamaClientImpl) StreamChatCompletion(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, <-chan error) {
	chunkCh := make(chan ChatChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		if ctx.Err() != nil {
			errCh <- ctx.Err()
			return
		}

		payload, err := c.buildChatPayload(req, true)
		if err != nil {
			errCh <- err
			return
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			errCh <- fmt.Errorf("ollama: failed to marshal stream request: %w", err)
			return
		}

		endpoint := c.cfg.BaseURL + "/api/chat"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}

		httpResp, err := c.cfg.HTTPClient.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			body, _ := io.ReadAll(httpResp.Body)
			errCh <- fmt.Errorf("ollama: stream failed with status %d: %s", httpResp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(httpResp.Body)
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var chunkWire ollamaChatWireResponse
			if err := json.Unmarshal([]byte(line), &chunkWire); err != nil {
				continue
			}

			chunk := ChatChunk{
				Model:        chunkWire.Model,
				DeltaContent: chunkWire.Message.Content,
			}

			if chunkWire.Done {
				chunk.FinishReason = chunkWire.DoneReason
				if chunk.FinishReason == "" {
					chunk.FinishReason = "stop"
				}
				chunk.Usage = &UsageMetrics{
					PromptTokens:     chunkWire.PromptEvalCount,
					CompletionTokens: chunkWire.EvalCount,
					TotalTokens:      chunkWire.PromptEvalCount + chunkWire.EvalCount,
				}
			}

			select {
			case chunkCh <- chunk:
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("ollama: stream read error: %w", err)
		}
	}()

	return chunkCh, errCh
}

// Generate executes a completion request against Ollama /api/generate.
func (c *ollamaClientImpl) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req == nil {
		return nil, ErrNilRequest
	}
	if req.Model == "" {
		return nil, ErrMissingModel
	}

	options := make(map[string]any)
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}
	if req.MaxTokens != nil {
		options["num_predict"] = *req.MaxTokens
	}
	if len(req.Stop) > 0 {
		options["stop"] = req.Stop
	}

	var optsMap map[string]any
	if len(options) > 0 {
		optsMap = options
	}

	wireReq := ollamaGenerateWireRequest{
		Model:    req.Model,
		Prompt:   req.Prompt,
		System:   req.System,
		Template: req.Template,
		Context:  req.Context,
		Stream:   false,
		Options:  optsMap,
	}

	bodyBytes, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal generate request: %w", err)
	}

	endpoint := c.cfg.BaseURL + "/api/generate"
	httpResp, err := ExecuteWithRetry(ctx, c.cfg.RetryPolicy, func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		return c.cfg.HTTPClient.Do(httpReq)
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to read generate response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama: generate error status %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var wireResp ollamaGenerateWireResponse
	if err := json.Unmarshal(respBytes, &wireResp); err != nil {
		return nil, fmt.Errorf("ollama: failed to decode generate response: %w", err)
	}

	if wireResp.Error != "" {
		return nil, fmt.Errorf("ollama: generate returned error: %s", wireResp.Error)
	}

	return &GenerateResponse{
		Model:      wireResp.Model,
		CreatedAt:  wireResp.CreatedAt,
		Response:   wireResp.Response,
		Done:       wireResp.Done,
		DoneReason: wireResp.DoneReason,
		Context:    wireResp.Context,
		Usage: UsageMetrics{
			PromptTokens:     wireResp.PromptEvalCount,
			CompletionTokens: wireResp.EvalCount,
			TotalTokens:      wireResp.PromptEvalCount + wireResp.EvalCount,
		},
	}, nil
}

// StreamGenerate opens a streaming channel to Ollama /api/generate.
func (c *ollamaClientImpl) StreamGenerate(ctx context.Context, req *GenerateRequest) (<-chan ChatChunk, <-chan error) {
	chunkCh := make(chan ChatChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		if ctx.Err() != nil {
			errCh <- ctx.Err()
			return
		}
		if req == nil {
			errCh <- ErrNilRequest
			return
		}
		if req.Model == "" {
			errCh <- ErrMissingModel
			return
		}

		options := make(map[string]any)
		if req.Temperature != nil {
			options["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			options["top_p"] = *req.TopP
		}
		if req.MaxTokens != nil {
			options["num_predict"] = *req.MaxTokens
		}
		if len(req.Stop) > 0 {
			options["stop"] = req.Stop
		}

		var optsMap map[string]any
		if len(options) > 0 {
			optsMap = options
		}

		wireReq := ollamaGenerateWireRequest{
			Model:    req.Model,
			Prompt:   req.Prompt,
			System:   req.System,
			Template: req.Template,
			Context:  req.Context,
			Stream:   true,
			Options:  optsMap,
		}

		bodyBytes, err := json.Marshal(wireReq)
		if err != nil {
			errCh <- fmt.Errorf("ollama: failed to marshal stream generate request: %w", err)
			return
		}

		endpoint := c.cfg.BaseURL + "/api/generate"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}

		httpResp, err := c.cfg.HTTPClient.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			body, _ := io.ReadAll(httpResp.Body)
			errCh <- fmt.Errorf("ollama: stream generate error status %d: %s", httpResp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(httpResp.Body)
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var wireChunk ollamaGenerateWireResponse
			if err := json.Unmarshal([]byte(line), &wireChunk); err != nil {
				continue
			}

			chunk := ChatChunk{
				Model:        wireChunk.Model,
				DeltaContent: wireChunk.Response,
			}

			if wireChunk.Done {
				chunk.FinishReason = wireChunk.DoneReason
				if chunk.FinishReason == "" {
					chunk.FinishReason = "stop"
				}
				chunk.Usage = &UsageMetrics{
					PromptTokens:     wireChunk.PromptEvalCount,
					CompletionTokens: wireChunk.EvalCount,
					TotalTokens:      wireChunk.PromptEvalCount + wireChunk.EvalCount,
				}
			}

			select {
			case chunkCh <- chunk:
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("ollama: stream generate read error: %w", err)
		}
	}()

	return chunkCh, errCh
}
