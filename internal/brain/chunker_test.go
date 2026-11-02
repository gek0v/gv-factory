// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCodeChunk_ToCodebaseKnowledge(t *testing.T) {
	chunk := CodeChunk{
		SymbolName:  "Service.Upsert",
		SymbolType:  SymbolTypeMethod,
		FilePath:    "internal/brain/knowledge.go",
		StartLine:   66,
		EndLine:     110,
		Content:     "func (s *Service) Upsert() error { return nil }",
		Signature:   "func (s *Service) Upsert() error",
		ContentHash: ComputeContentHash("func (s *Service) Upsert() error { return nil }"),
	}

	k := chunk.ToCodebaseKnowledge()
	kSnippet := chunk.ToCodeSnippet()
	if kSnippet.SymbolName != k.SymbolName || kSnippet.FilePath != k.FilePath {
		t.Errorf("ToCodeSnippet should match ToCodebaseKnowledge: %+v vs %+v", kSnippet, k)
	}

	if k.FilePath != chunk.FilePath {
		t.Errorf("expected FilePath %s, got %s", chunk.FilePath, k.FilePath)
	}
	if k.SymbolName != chunk.SymbolName {
		t.Errorf("expected SymbolName %s, got %s", chunk.SymbolName, k.SymbolName)
	}
	if k.SymbolType != chunk.SymbolType {
		t.Errorf("expected SymbolType %s, got %s", chunk.SymbolType, k.SymbolType)
	}
	if k.Content != chunk.Content {
		t.Errorf("expected Content %s, got %s", chunk.Content, k.Content)
	}
	if k.ContentHash != chunk.ContentHash {
		t.Errorf("expected ContentHash %s, got %s", chunk.ContentHash, k.ContentHash)
	}
}

func TestChunksToCodebaseKnowledge_Batch(t *testing.T) {
	t.Run("nil or empty slice", func(t *testing.T) {
		res := ChunksToCodebaseKnowledge(nil)
		if len(res) != 0 {
			t.Fatalf("expected empty slice, got %d items", len(res))
		}

		resEmpty := ChunksToCodebaseKnowledge([]CodeChunk{})
		if len(resEmpty) != 0 {
			t.Fatalf("expected empty slice, got %d items", len(resEmpty))
		}
	})

	t.Run("multiple chunks converted", func(t *testing.T) {
		chunks := []CodeChunk{
			{
				SymbolName:  "SymbolA",
				SymbolType:  SymbolTypeFunction,
				FilePath:    "pkg/a.go",
				Content:     "func SymbolA() {}",
				ContentHash: ComputeContentHash("func SymbolA() {}"),
			},
			{
				SymbolName:  "SymbolB",
				SymbolType:  SymbolTypeStruct,
				FilePath:    "pkg/b.go",
				Content:     "type SymbolB struct{}",
				ContentHash: ComputeContentHash("type SymbolB struct{}"),
			},
		}

		knowledgeList := ChunksToCodebaseKnowledge(chunks)
		if len(knowledgeList) != 2 {
			t.Fatalf("expected 2 items, got %d", len(knowledgeList))
		}
		if knowledgeList[0].SymbolName != "SymbolA" || knowledgeList[1].SymbolName != "SymbolB" {
			t.Errorf("unexpected symbol names in mapped slice")
		}

		snippets := ChunksToCodeSnippets(chunks)
		if len(snippets) != 2 || snippets[0].SymbolName != "SymbolA" {
			t.Errorf("unexpected ChunksToCodeSnippets results")
		}
	})
}

func TestComputeContentHash_Deterministic(t *testing.T) {
	content := "  func Greet() string { return \"hello\" } \n"
	hash1 := ComputeContentHash(content)
	hash2 := ComputeContentHash(content)

	if hash1 != hash2 {
		t.Fatalf("hashes should be deterministic, got %s and %s", hash1, hash2)
	}

	if len(hash1) != 64 {
		t.Fatalf("expected 64 char hex string (sha256), got len %d", len(hash1))
	}

	// Validate consistency with CodebaseKnowledge.Validate auto-hashing
	k := CodebaseKnowledge{
		FilePath:   "test.go",
		SymbolName: "Greet",
		SymbolType: SymbolTypeFunction,
		Content:    content,
	}
	if err := k.Validate(); err != nil {
		t.Fatalf("k.Validate failed: %v", err)
	}

	expectedHash := sha256.Sum256([]byte(k.Content))
	if hash1 != hex.EncodeToString(expectedHash[:]) {
		t.Errorf("ComputeContentHash does not match CodebaseKnowledge.Validate trimmed hash: got %s vs %s", hash1, hex.EncodeToString(expectedHash[:]))
	}
}
