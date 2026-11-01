// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

const (
	// HeaderDiscordSignature is the HTTP header containing the Ed25519 signature from Discord.
	HeaderDiscordSignature = "X-Signature-Ed25519"

	// HeaderDiscordTimestamp is the HTTP header containing the timestamp from Discord.
	HeaderDiscordTimestamp = "X-Signature-Timestamp"
)

// VerifyDiscordSignature validates an incoming interaction request using Discord's Ed25519 signature.
func VerifyDiscordSignature(publicKeyHex string, header http.Header, body []byte) error {
	publicKeyHex = strings.TrimSpace(publicKeyHex)
	if publicKeyHex == "" {
		return fmt.Errorf("%w: missing discord public key", ErrInvalidSignature)
	}

	pubKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: invalid discord public key hex", ErrInvalidSignature)
	}

	sigHex := strings.TrimSpace(header.Get(HeaderDiscordSignature))
	if sigHex == "" {
		return fmt.Errorf("%w: missing %s header", ErrInvalidSignature, HeaderDiscordSignature)
	}

	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("%w: invalid %s hex", ErrInvalidSignature, HeaderDiscordSignature)
	}

	timestamp := header.Get(HeaderDiscordTimestamp)
	if timestamp == "" {
		return fmt.Errorf("%w: missing %s header", ErrInvalidSignature, HeaderDiscordTimestamp)
	}

	msg := append([]byte(timestamp), body...)
	if !ed25519.Verify(pubKeyBytes, msg, sigBytes) {
		return fmt.Errorf("%w: ed25519 signature verification failed", ErrInvalidSignature)
	}

	return nil
}
