// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

const (
	defaultOllamaBaseURL = "http://localhost:11434/v1"
)

// NewOllamaClient creates an LLMClient for local Ollama deployments.
// By default connects to http://localhost:11434/v1 with no API key requirement.
func NewOllamaClient(opts ...Option) LLMClient {
	cfg := ClientConfig{
		BaseURL:     defaultOllamaBaseURL,
		RetryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return NewOpenAIClient("",
		WithBaseURL(cfg.BaseURL),
		WithHTTPClient(cfg.HTTPClient),
		WithRetryPolicy(cfg.RetryPolicy),
	)
}

// NewOpenAICompatibleClient creates an LLMClient for any OpenAI-compatible provider (vLLM, LocalAI, TGI, OpenRouter).
func NewOpenAICompatibleClient(baseURL, apiKey string, opts ...Option) LLMClient {
	opts = append([]Option{WithBaseURL(baseURL)}, opts...)
	return NewOpenAIClient(apiKey, opts...)
}
