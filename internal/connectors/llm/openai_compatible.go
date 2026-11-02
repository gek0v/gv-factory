// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import (
	"strings"
)

const (
	defaultVLLMBaseURL    = "http://localhost:8000/v1"
	defaultLocalAIBaseURL = "http://localhost:8080/v1"
)

// normalizeOpenAIBaseURL ensures standard OpenAI endpoint compatibility.
// If the URL does not end with /v1, it appends /v1 so that subsequent requests hit /v1/chat/completions.
func normalizeOpenAIBaseURL(baseURL string) string {
	clean := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if clean == "" {
		return defaultVLLMBaseURL
	}
	if !strings.HasSuffix(clean, "/v1") && !strings.Contains(clean, "/v1") {
		clean += "/v1"
	}
	return clean
}

// NewOpenAICompatibleClient creates an LLMClient for any OpenAI-compatible provider
// (such as vLLM, LM Studio, LocalAI, LiteLLM, or OpenRouter).
// It normalizes the BaseURL and supports optional authentication tokens.
func NewOpenAICompatibleClient(baseURL, apiKey string, opts ...Option) LLMClient {
	normalizedURL := normalizeOpenAIBaseURL(baseURL)
	allOpts := make([]Option, 0, len(opts)+1)
	allOpts = append(allOpts, WithBaseURL(normalizedURL))
	allOpts = append(allOpts, opts...)

	return NewOpenAIClient(apiKey, allOpts...)
}

// NewVLLMClient creates an LLMClient specifically configured for local or private vLLM deployments.
// If baseURL is empty, defaultVLLMBaseURL (http://localhost:8000/v1) is used.
func NewVLLMClient(baseURL string, opts ...Option) LLMClient {
	if baseURL == "" {
		baseURL = defaultVLLMBaseURL
	}
	return NewOpenAICompatibleClient(baseURL, "", opts...)
}

// NewLocalAIClient creates an LLMClient for local LocalAI instances.
// If baseURL is empty, defaultLocalAIBaseURL (http://localhost:8080/v1) is used.
func NewLocalAIClient(baseURL string, opts ...Option) LLMClient {
	if baseURL == "" {
		baseURL = defaultLocalAIBaseURL
	}
	return NewOpenAICompatibleClient(baseURL, "", opts...)
}
