// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// Canonical symbol types for AST semantic extraction.
const (
	SymbolTypeFunction  = "function"
	SymbolTypeMethod    = "method"
	SymbolTypeStruct    = "struct"
	SymbolTypeInterface = "interface"
	SymbolTypeType      = "type"
	SymbolTypeClass     = "class"
	SymbolTypeEnum      = "enum"
)

var (
	// ErrUnsupportedLanguage is returned when parsing a file with an unsupported file extension.
	ErrUnsupportedLanguage = errors.New("brain: unsupported language or file extension")

	// ErrEmptyContent is returned when source code content is empty.
	ErrEmptyContent = errors.New("brain: source content is empty")
)

// CodeChunk represents a semantic unit extracted from an AST.
type CodeChunk struct {
	SymbolName  string `json:"symbol_name"`
	SymbolType  string `json:"symbol_type"`
	FilePath    string `json:"file_path"`
	StartLine   int    `json:"start_line"` // 1-based, inclusive
	EndLine     int    `json:"end_line"`   // 1-based, inclusive
	Content     string `json:"content"`
	Signature   string `json:"signature"`
	ContentHash string `json:"content_hash"`
}

// Chunker defines the contract for AST-aware code segmentation.
type Chunker interface {
	Supports(filePath string) bool
	Chunk(ctx context.Context, filePath string, content []byte) ([]CodeChunk, error)
}

// ComputeContentHash computes the SHA-256 hexadecimal hash (64 chars) of trimmed content.
func ComputeContentHash(content string) string {
	trimmed := strings.TrimSpace(content)
	h := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(h[:])
}

// ToCodeSnippet converts a CodeChunk into a CodebaseKnowledge record ready for storage (alias/helper).
func (c CodeChunk) ToCodeSnippet() CodebaseKnowledge {
	return c.ToCodebaseKnowledge()
}

// ChunksToCodeSnippets converts a slice of CodeChunk into a slice of CodebaseKnowledge.
func ChunksToCodeSnippets(chunks []CodeChunk) []CodebaseKnowledge {
	return ChunksToCodebaseKnowledge(chunks)
}

// ToCodebaseKnowledge converts a CodeChunk into a CodebaseKnowledge record ready for storage.
func (c CodeChunk) ToCodebaseKnowledge() CodebaseKnowledge {
	return CodebaseKnowledge{
		FilePath:    c.FilePath,
		SymbolName:  c.SymbolName,
		SymbolType:  c.SymbolType,
		Content:     c.Content,
		ContentHash: c.ContentHash,
	}
}

// ChunksToCodebaseKnowledge converts a slice of CodeChunk into a slice of CodebaseKnowledge.
func ChunksToCodebaseKnowledge(chunks []CodeChunk) []CodebaseKnowledge {
	if len(chunks) == 0 {
		return []CodebaseKnowledge{}
	}
	res := make([]CodebaseKnowledge, len(chunks))
	for i, chunk := range chunks {
		res[i] = chunk.ToCodebaseKnowledge()
	}
	return res
}
