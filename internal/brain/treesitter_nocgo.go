// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

//go:build !cgo

package brain

import (
	"context"
	"path/filepath"
	"strings"
)

// TreeSitterChunker is a fallback implementation when CGO is disabled.
type TreeSitterChunker struct{}

// NewTreeSitterChunker creates an initialized TreeSitterChunker fallback.
func NewTreeSitterChunker() *TreeSitterChunker {
	return &TreeSitterChunker{}
}

// Supports returns true if the file extension is supported for chunking.
func (c *TreeSitterChunker) Supports(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".go" || ext == ".ts" || ext == ".tsx"
}

// Chunk returns empty chunks when CGO is disabled.
func (c *TreeSitterChunker) Chunk(ctx context.Context, filePath string, content []byte) ([]CodeChunk, error) {
	if len(content) == 0 {
		return nil, ErrEmptyContent
	}
	if !c.Supports(filePath) {
		return nil, ErrUnsupportedLanguage
	}
	return nil, nil
}
