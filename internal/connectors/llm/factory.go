// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrNoProviderFound is returned when auto provider detection cannot locate any valid provider credentials.
var ErrNoProviderFound = errors.New("llm: no valid LLM provider found in environment")

// NewClient instantiates an LLMClient for the designated provider.
func NewClient(provider ProviderType, opts ...Option) (LLMClient, error) {
	cfg := ClientConfig{
		RetryPolicy: DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	primaryClient, err := createSingleClient(provider, cfg, opts...)
	if err != nil {
		return nil, err
	}

	if cfg.FallbackProvider != "" && cfg.FallbackProvider != provider {
		fallbackCfg := ClientConfig{
			BaseURL:     cfg.FallbackBaseURL,
			APIKey:      cfg.FallbackAPIKey,
			HTTPClient:  cfg.HTTPClient,
			RetryPolicy: cfg.RetryPolicy,
		}
		fallbackOpts := []Option{
			WithBaseURL(fallbackCfg.BaseURL),
			WithAPIKey(fallbackCfg.APIKey),
			WithHTTPClient(fallbackCfg.HTTPClient),
			WithRetryPolicy(fallbackCfg.RetryPolicy),
		}
		fallbackClient, fallbackErr := createSingleClient(cfg.FallbackProvider, fallbackCfg, fallbackOpts...)
		if fallbackErr == nil && fallbackClient != nil {
			return NewFallbackClient(primaryClient, fallbackClient), nil
		}
	}

	return primaryClient, nil
}

func createSingleClient(provider ProviderType, cfg ClientConfig, opts ...Option) (LLMClient, error) {
	prov := ProviderType(strings.ToLower(strings.TrimSpace(string(provider))))

	switch prov {
	case ProviderAnthropic:
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, ErrInvalidAPIKey
		}
		return NewAnthropicClient(apiKey, opts...), nil

	case ProviderOpenAI:
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, ErrInvalidAPIKey
		}
		return NewOpenAIClient(apiKey, opts...), nil

	case ProviderOllama:
		return NewOllamaClient(opts...), nil

	case ProviderOpenAICompatible, ProviderVLLM:
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = os.Getenv("OPENAI_COMPATIBLE_BASE_URL")
			if baseURL == "" {
				baseURL = os.Getenv("VLLM_BASE_URL")
			}
		}
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_COMPATIBLE_API_KEY")
		}
		return NewOpenAICompatibleClient(baseURL, apiKey, opts...), nil

	case ProviderAuto:
		return NewClientFromEnv(opts...)

	default:
		return nil, fmt.Errorf("llm: unsupported provider: %s", provider)
	}
}

// NewClientFromEnv automatically inspects environment variables to configure an LLMClient.
func NewClientFromEnv(opts ...Option) (LLMClient, error) {
	if prov := os.Getenv("LLM_PROVIDER"); prov != "" {
		return NewClient(ProviderType(prov), opts...)
	}

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return NewAnthropicClient(key, opts...), nil
	}

	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return NewOpenAIClient(key, opts...), nil
	}

	if url := os.Getenv("OLLAMA_BASE_URL"); url != "" {
		allOpts := append([]Option{WithBaseURL(url)}, opts...)
		return NewOllamaClient(allOpts...), nil
	}

	if url := os.Getenv("OPENAI_COMPATIBLE_BASE_URL"); url != "" {
		apiKey := os.Getenv("OPENAI_COMPATIBLE_API_KEY")
		return NewOpenAICompatibleClient(url, apiKey, opts...), nil
	}

	if url := os.Getenv("VLLM_BASE_URL"); url != "" {
		return NewVLLMClient(url, opts...), nil
	}

	return nil, ErrNoProviderFound
}

// fallbackClient delegates requests to primary and switches to fallback on failure.
type fallbackClient struct {
	primary  LLMClient
	fallback LLMClient
}

// NewFallbackClient wraps two LLMClients into an active-failover client.
func NewFallbackClient(primary, fallback LLMClient) LLMClient {
	if fallback == nil {
		return primary
	}
	if primary == nil {
		return fallback
	}
	return &fallbackClient{
		primary:  primary,
		fallback: fallback,
	}
}

// ChatCompletion attempts primary completion and falls back if primary fails.
func (f *fallbackClient) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	resp, err := f.primary.ChatCompletion(ctx, req)
	if err == nil {
		return resp, nil
	}

	// Do not attempt fallback if caller canceled the context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return f.fallback.ChatCompletion(ctx, req)
}

// StreamChatCompletion attempts primary streaming and falls back if initialization fails.
func (f *fallbackClient) StreamChatCompletion(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, <-chan error) {
	outChunk := make(chan ChatChunk, 32)
	outErr := make(chan error, 1)

	go func() {
		defer close(outChunk)
		defer close(outErr)

		if ctx.Err() != nil {
			outErr <- ctx.Err()
			return
		}

		primChunk, primErr := f.primary.StreamChatCompletion(ctx, req)

		var firstChunk *ChatChunk
		var firstErr error
		select {
		case c, ok := <-primChunk:
			if ok {
				firstChunk = &c
			}
		case e := <-primErr:
			firstErr = e
		case <-ctx.Done():
			outErr <- ctx.Err()
			return
		}

		if firstErr != nil && ctx.Err() == nil {
			// Primary stream failed at startup; fallback to secondary client
			fbChunk, fbErr := f.fallback.StreamChatCompletion(ctx, req)
			for {
				select {
				case c, ok := <-fbChunk:
					if !ok {
						fbChunk = nil
					} else {
						select {
						case outChunk <- c:
						case <-ctx.Done():
							return
						}
					}
				case e, ok := <-fbErr:
					if !ok {
						fbErr = nil
					} else if e != nil {
						select {
						case outErr <- e:
						case <-ctx.Done():
							return
						}
					}
				case <-ctx.Done():
					return
				}
				if fbChunk == nil && fbErr == nil {
					return
				}
			}
		}

		if firstChunk != nil {
			select {
			case outChunk <- *firstChunk:
			case <-ctx.Done():
				return
			}
		}

		for {
			select {
			case c, ok := <-primChunk:
				if !ok {
					primChunk = nil
				} else {
					select {
					case outChunk <- c:
					case <-ctx.Done():
						return
					}
				}
			case e, ok := <-primErr:
				if !ok {
					primErr = nil
				} else if e != nil {
					select {
					case outErr <- e:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
			if primChunk == nil && primErr == nil {
				return
			}
		}
	}()

	return outChunk, outErr
}
