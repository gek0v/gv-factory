// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import "net/http"

// ProviderType identifies supported LLM backend providers.
type ProviderType string

const (
	ProviderAnthropic        ProviderType = "anthropic"
	ProviderOpenAI           ProviderType = "openai"
	ProviderOllama           ProviderType = "ollama"
	ProviderOpenAICompatible ProviderType = "openai_compatible"
	ProviderVLLM             ProviderType = "vllm"
	ProviderAuto             ProviderType = "auto"
)

// ClientConfig holds configuration settings for LLM providers.
type ClientConfig struct {
	BaseURL          string
	APIKey           string
	Model            string
	FallbackProvider ProviderType
	FallbackBaseURL  string
	FallbackAPIKey   string
	HTTPClient       *http.Client
	RetryPolicy      RetryPolicy
}

// Option configures a ClientConfig.
type Option func(*ClientConfig)

// WithBaseURL sets a custom base endpoint URL.
func WithBaseURL(url string) Option {
	return func(c *ClientConfig) {
		c.BaseURL = url
	}
}

// WithAPIKey sets the API authentication key or token.
func WithAPIKey(apiKey string) Option {
	return func(c *ClientConfig) {
		c.APIKey = apiKey
	}
}

// WithModel sets the target model identifier.
func WithModel(model string) Option {
	return func(c *ClientConfig) {
		c.Model = model
	}
}

// WithFallbackProvider configures fallback parameters when primary fails.
func WithFallbackProvider(provider ProviderType, baseURL, apiKey string) Option {
	return func(c *ClientConfig) {
		c.FallbackProvider = provider
		c.FallbackBaseURL = baseURL
		c.FallbackAPIKey = apiKey
	}
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *ClientConfig) {
		if client != nil {
			c.HTTPClient = client
		}
	}
}

// WithRetryPolicy sets a custom retry policy.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *ClientConfig) {
		c.RetryPolicy = policy
	}
}
