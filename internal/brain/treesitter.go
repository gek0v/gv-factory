// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

//go:build cgo

package brain

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

var whitespaceRegex = regexp.MustCompile(`\s+`)

// TreeSitterChunker segments Go and TypeScript/TSX source code using Tree-sitter AST parsing.
type TreeSitterChunker struct {
	pool sync.Pool
}

// NewTreeSitterChunker creates an initialized TreeSitterChunker with pooled parsers.
func NewTreeSitterChunker() *TreeSitterChunker {
	return &TreeSitterChunker{
		pool: sync.Pool{
			New: func() any {
				return sitter.NewParser()
			},
		},
	}
}

// Supports checks whether a file is supported based on its file extension.
func (c *TreeSitterChunker) Supports(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".go" || ext == ".ts" || ext == ".tsx"
}

// Chunk parses source code and returns semantic code chunks.
func (c *TreeSitterChunker) Chunk(ctx context.Context, filePath string, content []byte) ([]CodeChunk, error) {
	if !c.Supports(filePath) {
		return nil, ErrUnsupportedLanguage
	}

	if len(bytes.TrimSpace(content)) == 0 {
		return []CodeChunk{}, nil
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	var lang *sitter.Language
	switch ext {
	case ".go":
		lang = golang.GetLanguage()
	case ".ts":
		lang = typescript.GetLanguage()
	case ".tsx":
		lang = tsx.GetLanguage()
	default:
		return nil, ErrUnsupportedLanguage
	}

	parser := c.pool.Get().(*sitter.Parser)
	defer c.pool.Put(parser)

	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return []CodeChunk{}, nil
	}

	chunks := make([]CodeChunk, 0)
	if ext == ".go" {
		c.extractGoSymbols(root, content, filePath, &chunks)
	} else {
		c.extractTypeScriptSymbols(root, content, filePath, &chunks, "")
	}

	return chunks, nil
}

func (c *TreeSitterChunker) extractGoSymbols(node *sitter.Node, content []byte, filePath string, chunks *[]CodeChunk) {
	if node == nil {
		return
	}

	nodeType := node.Type()
	switch nodeType {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(content)
			sig := extractGoSignature(node, content)
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  name,
				SymbolType:  SymbolTypeFunction,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})
		}
	case "method_declaration":
		nameNode := node.ChildByFieldName("name")
		receiverNode := node.ChildByFieldName("receiver")
		if nameNode != nil && receiverNode != nil {
			name := nameNode.Content(content)
			receiverTypeName := extractGoReceiverTypeName(receiverNode, content)
			qualifiedName := name
			if receiverTypeName != "" {
				qualifiedName = receiverTypeName + "." + name
			}
			sig := extractGoSignature(node, content)
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  qualifiedName,
				SymbolType:  SymbolTypeMethod,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})
		}
	case "type_declaration":
		// A type declaration may contain one or more type_spec children
		count := int(node.ChildCount())
		for i := 0; i < count; i++ {
			child := node.Child(i)
			if child != nil && child.Type() == "type_spec" {
				c.extractGoTypeSpec(child, node, content, filePath, chunks)
			}
		}
	}

	// Recursively inspect children to tolerate nested declarations or syntax error nodes
	count := int(node.ChildCount())
	for i := 0; i < count; i++ {
		child := node.Child(i)
		// Avoid recursing into function bodies
		if child != nil && nodeType != "function_declaration" && nodeType != "method_declaration" && nodeType != "type_declaration" {
			c.extractGoSymbols(child, content, filePath, chunks)
		}
	}
}

func (c *TreeSitterChunker) extractGoTypeSpec(specNode, declNode *sitter.Node, content []byte, filePath string, chunks *[]CodeChunk) {
	nameNode := specNode.ChildByFieldName("name")
	typeNode := specNode.ChildByFieldName("type")
	if nameNode == nil {
		return
	}

	name := nameNode.Content(content)
	symType := SymbolTypeType
	sig := "type " + name

	if typeNode != nil {
		switch typeNode.Type() {
		case "struct_type":
			symType = SymbolTypeStruct
			sig = "type " + name + " struct"
		case "interface_type":
			symType = SymbolTypeInterface
			sig = "type " + name + " interface"
		default:
			defText := cleanSignature(typeNode.Content(content))
			sig = "type " + name + " " + defText
		}
	}

	fullContent := specNode.Content(content)
	if declNode.ChildCount() == 2 && declNode.Child(0).Type() == "type" {
		fullContent = declNode.Content(content)
	}

	*chunks = append(*chunks, CodeChunk{
		SymbolName:  name,
		SymbolType:  symType,
		FilePath:    filePath,
		StartLine:   int(specNode.StartPoint().Row) + 1,
		EndLine:     int(specNode.EndPoint().Row) + 1,
		Content:     fullContent,
		Signature:   sig,
		ContentHash: ComputeContentHash(fullContent),
	})
}

func extractGoReceiverTypeName(receiverNode *sitter.Node, content []byte) string {
	raw := receiverNode.Content(content)
	raw = strings.TrimPrefix(raw, "(")
	raw = strings.TrimSuffix(raw, ")")
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	typeName := fields[len(fields)-1]
	return strings.TrimPrefix(typeName, "*")
}

func extractGoSignature(node *sitter.Node, content []byte) string {
	bodyNode := node.ChildByFieldName("body")
	full := node.Content(content)
	if bodyNode != nil {
		start := node.StartByte()
		bodyStart := bodyNode.StartByte()
		if bodyStart >= start && int(bodyStart-start) <= len(full) {
			sig := string(full[:bodyStart-start])
			return cleanSignature(sig)
		}
	}
	if idx := strings.Index(full, "{"); idx != -1 {
		return cleanSignature(full[:idx])
	}
	return cleanSignature(full)
}

func (c *TreeSitterChunker) extractTypeScriptSymbols(node *sitter.Node, content []byte, filePath string, chunks *[]CodeChunk, currentClass string) {
	if node == nil {
		return
	}

	nodeType := node.Type()
	switch nodeType {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(content)
			sig := extractTSSignature(node, content)
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  name,
				SymbolType:  SymbolTypeFunction,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})
		}
	case "class_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			className := nameNode.Content(content)
			sig := extractTSSignature(node, content)
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  className,
				SymbolType:  SymbolTypeClass,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})

			// Recurse into class body to extract methods
			bodyNode := node.ChildByFieldName("body")
			if bodyNode != nil {
				c.extractTypeScriptSymbols(bodyNode, content, filePath, chunks, className)
			}
			return
		}
	case "method_definition":
		if currentClass != "" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				methodName := nameNode.Content(content)
				if methodName != "constructor" {
					qualifiedName := currentClass + "." + methodName
					sig := extractTSSignature(node, content)
					fullContent := node.Content(content)
					*chunks = append(*chunks, CodeChunk{
						SymbolName:  qualifiedName,
						SymbolType:  SymbolTypeMethod,
						FilePath:    filePath,
						StartLine:   int(node.StartPoint().Row) + 1,
						EndLine:     int(node.EndPoint().Row) + 1,
						Content:     fullContent,
						Signature:   sig,
						ContentHash: ComputeContentHash(fullContent),
					})
				}
			}
		}
	case "interface_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(content)
			sig := "interface " + name
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  name,
				SymbolType:  SymbolTypeInterface,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})
		}
	case "type_alias_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(content)
			sig := "type " + name
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  name,
				SymbolType:  SymbolTypeType,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})
		}
	case "enum_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(content)
			sig := "enum " + name
			fullContent := node.Content(content)
			*chunks = append(*chunks, CodeChunk{
				SymbolName:  name,
				SymbolType:  SymbolTypeEnum,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     fullContent,
				Signature:   sig,
				ContentHash: ComputeContentHash(fullContent),
			})
		}
	case "lexical_declaration":
		// Handle const fn = () => ...
		count := int(node.ChildCount())
		for i := 0; i < count; i++ {
			decl := node.Child(i)
			if decl != nil && decl.Type() == "variable_declarator" {
				nameNode := decl.ChildByFieldName("name")
				valueNode := decl.ChildByFieldName("value")
				if nameNode != nil && valueNode != nil {
					valType := valueNode.Type()
					if valType == "arrow_function" || valType == "function_expression" {
						name := nameNode.Content(content)
						sig := extractArrowSignature(name, valueNode, content)
						fullContent := node.Content(content)
						*chunks = append(*chunks, CodeChunk{
							SymbolName:  name,
							SymbolType:  SymbolTypeFunction,
							FilePath:    filePath,
							StartLine:   int(node.StartPoint().Row) + 1,
							EndLine:     int(node.EndPoint().Row) + 1,
							Content:     fullContent,
							Signature:   sig,
							ContentHash: ComputeContentHash(fullContent),
						})
					}
				}
			}
		}
	}

	// Recurse into children (unless inside function body)
	if nodeType != "function_declaration" && nodeType != "interface_declaration" && nodeType != "type_alias_declaration" && nodeType != "enum_declaration" {
		count := int(node.ChildCount())
		for i := 0; i < count; i++ {
			c.extractTypeScriptSymbols(node.Child(i), content, filePath, chunks, currentClass)
		}
	}
}

func extractTSSignature(node *sitter.Node, content []byte) string {
	bodyNode := node.ChildByFieldName("body")
	full := node.Content(content)
	if bodyNode != nil {
		start := node.StartByte()
		bodyStart := bodyNode.StartByte()
		if bodyStart >= start && int(bodyStart-start) <= len(full) {
			sig := string(full[:bodyStart-start])
			return cleanSignature(sig)
		}
	}
	if idx := strings.Index(full, "{"); idx != -1 {
		return cleanSignature(full[:idx])
	}
	return cleanSignature(full)
}

func extractArrowSignature(name string, arrowNode *sitter.Node, content []byte) string {
	full := arrowNode.Content(content)
	if idx := strings.Index(full, "=>"); idx != -1 {
		prefix := strings.TrimSpace(full[:idx])
		return cleanSignature(name + " = " + prefix)
	}
	if idx := strings.Index(full, "{"); idx != -1 {
		return cleanSignature(name + " = " + full[:idx])
	}
	return cleanSignature(name + " = " + full)
}

func cleanSignature(sig string) string {
	sig = strings.TrimSpace(sig)
	sig = whitespaceRegex.ReplaceAllString(sig, " ")
	return strings.TrimSpace(sig)
}
