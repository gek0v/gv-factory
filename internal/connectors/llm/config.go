// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package llm

import "net/http"

// ClientConfig holds configuration settings for LLM providers.
type ClientConfig struct {
	BaseURL     string
	HTTPClient  *http.Client
	RetryPolicy RetryPolicy
}

// Option configures a ClientConfig.
type Option func(*ClientConfig)

// WithBaseURL sets a custom base endpoint URL.
func WithBaseURL(url string) Option {
	return func(c *ClientConfig) {
		c.BaseURL = url
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
