// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPTransport implements MCP communication over HTTP and Server-Sent Events (SSE).
// The client connects to an SSE endpoint to receive messages/events and posts requests
// to an endpoint indicated by the server or the default messages endpoint.
type HTTPTransport struct {
	baseURL    string
	postURL    string
	client     *http.Client
	sseResp    *http.Response
	eventsChan chan *Response
	errChan    chan error
	closeOnce  sync.Once
	closed     chan struct{}
	mu         sync.RWMutex
}

// HTTPTransportConfig configures the HTTPTransport.
type HTTPTransportConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	// Optional custom headers
	Headers map[string]string
}

// NewHTTPTransport establishes an SSE connection to baseURL and initializes the transport.
func NewHTTPTransport(ctx context.Context, cfg HTTPTransportConfig) (*HTTPTransport, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("mcp http: baseURL is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0} // No overall client timeout for streaming SSE
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp http: create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp http: connect SSE endpoint: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp http: unexpected status code %d", resp.StatusCode)
	}

	t := &HTTPTransport{
		baseURL:    cfg.BaseURL,
		postURL:    cfg.BaseURL, // Default to baseURL unless updated by endpoint event
		client:     httpClient,
		sseResp:    resp,
		eventsChan: make(chan *Response, 100),
		errChan:    make(chan error, 1),
		closed:     make(chan struct{}),
	}

	go t.listenSSE()

	return t, nil
}

// listenSSE parses standard Server-Sent Events from the response body.
func (t *HTTPTransport) listenSSE() {
	defer close(t.eventsChan)
	defer t.sseResp.Body.Close()

	scanner := bufio.NewScanner(t.sseResp.Body)
	var currentEvent string
	var currentData strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Dispatch event if we accumulated data
			if currentData.Len() > 0 {
				dataStr := strings.TrimSpace(currentData.String())
				currentData.Reset()

				if currentEvent == "endpoint" {
					// SSE endpoint event tells the client where to POST messages
					t.mu.Lock()
					if strings.HasPrefix(dataStr, "http://") || strings.HasPrefix(dataStr, "https://") {
						t.postURL = dataStr
					} else {
						// Relative URL
						base := strings.TrimRight(t.baseURL, "/")
						if !strings.HasPrefix(dataStr, "/") {
							dataStr = "/" + dataStr
						}
						t.postURL = base + dataStr
					}
					t.mu.Unlock()
				} else if currentEvent == "message" || currentEvent == "" {
					var resp Response
					if err := json.Unmarshal([]byte(dataStr), &resp); err == nil {
						select {
						case t.eventsChan <- &resp:
						case <-t.closed:
							return
						}
					}
				}
				currentEvent = ""
			}
			continue
		}

		if strings.HasPrefix(line, ":") {
			// SSE comment, ignore
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			d := strings.TrimPrefix(line, "data:")
			d = strings.TrimPrefix(d, " ")
			if currentData.Len() > 0 {
				currentData.WriteString("\n")
			}
			currentData.WriteString(d)
			continue
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		select {
		case t.errChan <- err:
		case <-t.closed:
		}
	}
}

// Send sends a JSON-RPC request via HTTP POST.
func (t *HTTPTransport) Send(ctx context.Context, req *Request) error {
	select {
	case <-t.closed:
		return ErrTransportClosed
	default:
	}

	if req == nil {
		return ErrNilRequest
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("mcp http: marshal request: %w", err)
	}

	t.mu.RLock()
	postURL := t.postURL
	t.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mcp http: create POST request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("mcp http: post failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mcp http: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Some MCP servers might respond directly with JSON-RPC response body in the POST request
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var r Response
		if err := json.NewDecoder(resp.Body).Decode(&r); err == nil && (r.Result != nil || r.Error != nil || r.ID != nil) {
			select {
			case t.eventsChan <- &r:
			case <-t.closed:
			case <-time.After(500 * time.Millisecond):
			}
		}
	}

	return nil
}

// Receive reads the next response from the SSE stream.
func (t *HTTPTransport) Receive(ctx context.Context) (*Response, error) {
	select {
	case <-t.closed:
		return nil, ErrTransportClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-t.errChan:
		return nil, err
	case resp, ok := <-t.eventsChan:
		if !ok {
			return nil, ErrTransportClosed
		}
		return resp, nil
	}
}

// Close closes the SSE stream connection and halts the transport.
func (t *HTTPTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closed)
		if t.sseResp != nil && t.sseResp.Body != nil {
			_ = t.sseResp.Body.Close()
		}
	})
	return nil
}
