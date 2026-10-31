// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"errors"
	"net/http"
)

var (
	// ErrInvalidSignature indicates HMAC or secret token mismatch.
	ErrInvalidSignature = errors.New("invalid webhook signature")

	// ErrMissingSignature indicates the required signature or token header is absent.
	ErrMissingSignature = errors.New("missing webhook signature or token")

	// ErrPayloadTooLarge indicates request body exceeded maximum allowed bytes.
	ErrPayloadTooLarge = errors.New("payload exceeds maximum allowed body size")

	// ErrInvalidPayload indicates unparseable or malformed request body JSON.
	ErrInvalidPayload = errors.New("malformed or unparseable webhook payload")

	// ErrUnsupportedProvider indicates the requested provider is unknown or not registered.
	ErrUnsupportedProvider = errors.New("unsupported webhook provider")

	// ErrTimestampExpired indicates the webhook timestamp is too old (replay attack defense).
	ErrTimestampExpired = errors.New("webhook timestamp expired (replay attack defense)")

	// ErrTimestampInFuture indicates the webhook timestamp is too far in the future.
	ErrTimestampInFuture = errors.New("webhook timestamp too far in future (clock skew limit)")

	// ErrMissingEventHeader indicates the required provider event header is absent.
	ErrMissingEventHeader = errors.New("missing provider event header")

	// ErrMethodNotAllowed indicates a non-POST HTTP request.
	ErrMethodNotAllowed = errors.New("method not allowed; webhooks require HTTP POST")
)

// HTTPStatusCode maps an error to the corresponding HTTP status code.
func HTTPStatusCode(err error) int {
	switch {
	case errors.Is(err, ErrInvalidSignature), errors.Is(err, ErrMissingSignature):
		return http.StatusUnauthorized // 401
	case errors.Is(err, ErrTimestampExpired), errors.Is(err, ErrTimestampInFuture):
		return http.StatusUnauthorized // 401: rejected due to freshness / replay defense
	case errors.Is(err, ErrPayloadTooLarge):
		return http.StatusRequestEntityTooLarge // 413
	case errors.Is(err, ErrInvalidPayload), errors.Is(err, ErrMissingEventHeader):
		return http.StatusBadRequest // 400
	case errors.Is(err, ErrUnsupportedProvider):
		return http.StatusNotFound // 404
	case errors.Is(err, ErrMethodNotAllowed):
		return http.StatusMethodNotAllowed // 405
	default:
		return http.StatusInternalServerError // 500
	}
}
