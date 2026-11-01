// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// HeaderSlackSignature is the HTTP header containing the HMAC signature from Slack.
	HeaderSlackSignature = "X-Slack-Signature"

	// HeaderSlackTimestamp is the HTTP header containing the timestamp from Slack.
	HeaderSlackTimestamp = "X-Slack-Request-Timestamp"

	// DefaultSlackMaxDrift is the maximum allowed difference between request time and server time (5 minutes).
	DefaultSlackMaxDrift = 5 * time.Minute
)

// VerifySlackSignature validates an incoming HTTP request using Slack's v0 signing protocol.
func VerifySlackSignature(signingSecret string, header http.Header, body []byte, now time.Time, maxDrift time.Duration) error {
	if signingSecret == "" {
		return fmt.Errorf("%w: missing signing secret", ErrInvalidSignature)
	}

	sig := strings.TrimSpace(header.Get(HeaderSlackSignature))
	if sig == "" {
		return fmt.Errorf("%w: missing %s header", ErrInvalidSignature, HeaderSlackSignature)
	}

	tsStr := strings.TrimSpace(header.Get(HeaderSlackTimestamp))
	if tsStr == "" {
		return fmt.Errorf("%w: missing %s header", ErrInvalidSignature, HeaderSlackTimestamp)
	}

	tsSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: invalid timestamp format %q", ErrInvalidSignature, tsStr)
	}

	if maxDrift <= 0 {
		maxDrift = DefaultSlackMaxDrift
	}

	reqTime := time.Unix(tsSec, 0)
	drift := math.Abs(now.Sub(reqTime).Seconds())
	if drift > maxDrift.Seconds() {
		return fmt.Errorf("%w: drift %.1fs exceeds allowed %.1fs", ErrExpiredTimestamp, drift, maxDrift.Seconds())
	}

	// Slack signing format: v0:{timestamp}:{body}
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(fmt.Sprintf("v0:%s:", tsStr)))
	_, _ = mac.Write(body)
	expectedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return fmt.Errorf("%w: signature mismatch", ErrInvalidSignature)
	}

	return nil
}
