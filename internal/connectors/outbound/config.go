// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"net"
	"net/http"
	"time"
)

// Config configures the outbound webhook dispatcher engine and worker pool.
type Config struct {
	WorkerCount     int
	QueueSize       int
	RetryPolicy     RetryPolicy
	HTTPClient      *http.Client
	SignaturePrefix string
}

// DefaultConfig returns default production settings for the outbound dispatcher.
func DefaultConfig() Config {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return Config{
		WorkerCount: 5,
		QueueSize:   1000,
		RetryPolicy: DefaultRetryPolicy(),
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
		SignaturePrefix: DefaultSignaturePrefix,
	}
}
