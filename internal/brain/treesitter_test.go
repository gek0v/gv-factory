// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

//go:build cgo

package brain

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestTreeSitter_ChunkGo(t *testing.T) {
	sourceGo := `// SPDX-License-Identifier: Apache-2.0
package example

// User represents a user model.
type User struct {
	ID   string
	Name string
}

// Greeter describes greeting capability.
type Greeter interface {
	Greet(name string) string
}

// UserID is a custom type.
type UserID string

// NewUser creates a new user.
func NewUser(name string) *User {
	return &User{Name: name}
}

// Greet greets someone.
func (u *User) Greet(greeting string) string {
	return greeting + ", " + u.Name
}

// ByValue method on User.
func (u User) ByValue() string {
	return u.Name
}
`

	chunker := NewTreeSitterChunker()
	if !chunker.Supports("internal/user.go") {
		t.Fatalf("expected Supports internal/user.go to be true")
	}

	chunks, err := chunker.Chunk(context.Background(), "internal/user.go", []byte(sourceGo))
	if err != nil {
		t.Fatalf("chunker.Chunk failed: %v", err)
	}

	expectedSymbols := map[string]struct {
		symbolType string
		hasSig     string
	}{
		"User": {
			symbolType: SymbolTypeStruct,
			hasSig:     "type User struct",
		},
		"Greeter": {
			symbolType: SymbolTypeInterface,
			hasSig:     "type Greeter interface",
		},
		"UserID": {
			symbolType: SymbolTypeType,
			hasSig:     "type UserID string",
		},
		"NewUser": {
			symbolType: SymbolTypeFunction,
			hasSig:     "func NewUser(name string) *User",
		},
		"User.Greet": {
			symbolType: SymbolTypeMethod,
			hasSig:     "func (u *User) Greet(greeting string) string",
		},
		"User.ByValue": {
			symbolType: SymbolTypeMethod,
			hasSig:     "func (u User) ByValue() string",
		},
	}

	if len(chunks) != len(expectedSymbols) {
		t.Fatalf("expected %d chunks, got %d", len(expectedSymbols), len(chunks))
	}

	for _, chunk := range chunks {
		expected, exists := expectedSymbols[chunk.SymbolName]
		if !exists {
			t.Errorf("unexpected symbol extracted: %s", chunk.SymbolName)
			continue
		}
		if chunk.SymbolType != expected.symbolType {
			t.Errorf("symbol %s expected type %s, got %s", chunk.SymbolName, expected.symbolType, chunk.SymbolType)
		}
		if !strings.Contains(chunk.Signature, expected.hasSig) {
			t.Errorf("symbol %s signature expected to contain %q, got %q", chunk.SymbolName, expected.hasSig, chunk.Signature)
		}
		if chunk.FilePath != "internal/user.go" {
			t.Errorf("expected FilePath internal/user.go, got %s", chunk.FilePath)
		}
		if chunk.StartLine <= 0 || chunk.EndLine < chunk.StartLine {
			t.Errorf("invalid line range %d-%d for %s", chunk.StartLine, chunk.EndLine, chunk.SymbolName)
		}
		if chunk.ContentHash == "" || len(chunk.ContentHash) != 64 {
			t.Errorf("invalid content hash %s for %s", chunk.ContentHash, chunk.SymbolName)
		}
	}
}

func TestTreeSitter_ChunkTypeScript(t *testing.T) {
	sourceTS := `
export interface UserConfig {
	timeout: number;
}

export type Status = "active" | "inactive";

export enum Priority {
	Low = 1,
	High = 2,
}

export function initialize(cfg: UserConfig): boolean {
	return cfg.timeout > 0;
}

export const calculateTotal = (a: number, b: number): number => {
	return a + b;
};

export class SessionManager {
	private id: string;

	constructor(id: string) {
		this.id = id;
	}

	public async terminate(): Promise<void> {
		this.id = "";
	}
}
`

	chunker := NewTreeSitterChunker()
	if !chunker.Supports("src/session.ts") {
		t.Fatalf("expected Supports src/session.ts to be true")
	}

	chunks, err := chunker.Chunk(context.Background(), "src/session.ts", []byte(sourceTS))
	if err != nil {
		t.Fatalf("chunker.Chunk failed on TS: %v", err)
	}

	expectedSymbols := map[string]struct {
		symbolType string
		hasSig     string
	}{
		"UserConfig": {
			symbolType: SymbolTypeInterface,
			hasSig:     "interface UserConfig",
		},
		"Status": {
			symbolType: SymbolTypeType,
			hasSig:     "type Status",
		},
		"Priority": {
			symbolType: SymbolTypeEnum,
			hasSig:     "enum Priority",
		},
		"initialize": {
			symbolType: SymbolTypeFunction,
			hasSig:     "function initialize(cfg: UserConfig): boolean",
		},
		"calculateTotal": {
			symbolType: SymbolTypeFunction,
			hasSig:     "calculateTotal = (a: number, b: number): number",
		},
		"SessionManager": {
			symbolType: SymbolTypeClass,
			hasSig:     "class SessionManager",
		},
		"SessionManager.terminate": {
			symbolType: SymbolTypeMethod,
			hasSig:     "public async terminate(): Promise<void>",
		},
	}

	if len(chunks) < len(expectedSymbols) {
		t.Fatalf("expected at least %d chunks, got %d", len(expectedSymbols), len(chunks))
	}

	chunkMap := make(map[string]CodeChunk)
	for _, c := range chunks {
		chunkMap[c.SymbolName] = c
	}

	for name, exp := range expectedSymbols {
		c, found := chunkMap[name]
		if !found {
			t.Errorf("missing expected symbol: %s", name)
			continue
		}
		if c.SymbolType != exp.symbolType {
			t.Errorf("symbol %s expected type %s, got %s", name, exp.symbolType, c.SymbolType)
		}
		if !strings.Contains(c.Signature, exp.hasSig) {
			t.Errorf("symbol %s signature expected to contain %q, got %q", name, exp.hasSig, c.Signature)
		}
	}
}

func TestTreeSitter_ChunkTSX(t *testing.T) {
	sourceTSX := `import React from 'react';

export interface CardProps {
	title: string;
}

export const CardComponent: React.FC<CardProps> = ({ title }) => {
	return <div className="card"><h1>{title}</h1></div>;
};

export function RenderFooter(): JSX.Element {
	return <footer>Footer Content</footer>;
}
`

	chunker := NewTreeSitterChunker()
	if !chunker.Supports("web/src/Card.tsx") {
		t.Fatalf("expected Supports web/src/Card.tsx to be true")
	}

	chunks, err := chunker.Chunk(context.Background(), "web/src/Card.tsx", []byte(sourceTSX))
	if err != nil {
		t.Fatalf("chunker.Chunk failed on TSX: %v", err)
	}

	chunkMap := make(map[string]CodeChunk)
	for _, c := range chunks {
		chunkMap[c.SymbolName] = c
	}

	if _, ok := chunkMap["CardProps"]; !ok {
		t.Errorf("missing CardProps interface")
	}
	if _, ok := chunkMap["CardComponent"]; !ok {
		t.Errorf("missing CardComponent arrow function")
	}
	if _, ok := chunkMap["RenderFooter"]; !ok {
		t.Errorf("missing RenderFooter function")
	}
}

func TestTreeSitter_SyntaxErrorsGraceful(t *testing.T) {
	// Source with syntax errors followed by valid function
	brokenSource := `package broken

var = %! incomplete junk

func ValidFunction(x int) int {
	return x * 2
}
`
	chunker := NewTreeSitterChunker()
	chunks, err := chunker.Chunk(context.Background(), "pkg/broken.go", []byte(brokenSource))
	if err != nil {
		t.Fatalf("chunker should gracefully handle syntax errors without error, got: %v", err)
	}

	found := false
	for _, c := range chunks {
		if c.SymbolName == "ValidFunction" {
			found = true
			if c.SymbolType != SymbolTypeFunction {
				t.Errorf("expected function, got %s", c.SymbolType)
			}
		}
	}
	if !found {
		t.Fatalf("expected to extract ValidFunction despite syntax errors, got %d chunks", len(chunks))
	}
}

func TestTreeSitter_EdgeCases(t *testing.T) {
	chunker := NewTreeSitterChunker()

	t.Run("unsupported language", func(t *testing.T) {
		if chunker.Supports("script.py") {
			t.Errorf("expected python not supported")
		}
		_, err := chunker.Chunk(context.Background(), "script.py", []byte("def hello(): pass"))
		if err != ErrUnsupportedLanguage {
			t.Errorf("expected ErrUnsupportedLanguage, got %v", err)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		chunks, err := chunker.Chunk(context.Background(), "empty.go", []byte(""))
		if err != nil {
			t.Fatalf("unexpected error on empty content: %v", err)
		}
		if len(chunks) != 0 {
			t.Fatalf("expected 0 chunks for empty content, got %d", len(chunks))
		}
	})

	t.Run("whitespace and comments only", func(t *testing.T) {
		chunks, err := chunker.Chunk(context.Background(), "comment.go", []byte("   \n// only comments\n   "))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunks) != 0 {
			t.Fatalf("expected 0 chunks, got %d", len(chunks))
		}
	})
}

func TestTreeSitter_ConcurrentSafety(t *testing.T) {
	chunker := NewTreeSitterChunker()
	sourceGo := `package conc
func Worker(id int) int { return id * 2 }
type Config struct { Name string }
`
	sourceTS := `
export function runner(id: number): number { return id * 2; }
export interface Task { id: string; }
`

	var wg sync.WaitGroup
	goroutines := 30
	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			chunks, err := chunker.Chunk(context.Background(), "conc.go", []byte(sourceGo))
			if err != nil || len(chunks) < 2 {
				t.Errorf("Go chunking failed in concurrency: %v, chunks=%d", err, len(chunks))
			}
		}()
		go func() {
			defer wg.Done()
			chunks, err := chunker.Chunk(context.Background(), "conc.ts", []byte(sourceTS))
			if err != nil || len(chunks) < 2 {
				t.Errorf("TS chunking failed in concurrency: %v, chunks=%d", err, len(chunks))
			}
		}()
	}
	wg.Wait()
}

func TestTreeSitter_SignaturesAndEdgeCases(t *testing.T) {
	chunker := NewTreeSitterChunker()

	t.Run("TS arrow function variants and single line", func(t *testing.T) {
		tsCode := `
export const inlineAdd = (x: number, y: number) => x + y;
export const noArrow = function(x: number) { return x; };
`
		chunks, err := chunker.Chunk(context.Background(), "inline.ts", []byte(tsCode))
		if err != nil {
			t.Fatalf("Chunk failed: %v", err)
		}
		if len(chunks) < 2 {
			t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
		}
	})

	t.Run("Go function without body or single line", func(t *testing.T) {
		goCode := `package test
type CustomType int
func EmptyFunc() {}
`
		chunks, err := chunker.Chunk(context.Background(), "single.go", []byte(goCode))
		if err != nil {
			t.Fatalf("Chunk failed: %v", err)
		}
		if len(chunks) != 2 {
			t.Fatalf("expected 2 chunks, got %d", len(chunks))
		}
	})
}
