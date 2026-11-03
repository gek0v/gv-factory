// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

//go:build !cgo

package brain

import (
	"context"
	"testing"
)

func TestTreeSitter_NoCgoFallback(t *testing.T) {
	chunker := NewTreeSitterChunker()
	if !chunker.Supports("main.go") {
		t.Errorf("expected main.go to be supported")
	}
	chunks, err := chunker.Chunk(context.Background(), "main.go", []byte("package main\nfunc main() {}"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks in nocgo fallback, got %d", len(chunks))
	}
}
