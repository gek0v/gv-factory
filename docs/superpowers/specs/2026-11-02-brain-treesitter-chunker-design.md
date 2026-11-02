# Spec: Tree-sitter Code Chunker con Extracción de Símbolos AST para Go y TypeScript

- **Fecha:** 2026-11-02
- **Estado:** Aprobado
- **Issue Asociado:** Issue #119 (`feat(brain): implement Tree-sitter code chunker with AST symbol extraction for Go and TypeScript`)
- **Autor:** Spec Writer (gv-factory team)
- **Componente:** `internal/brain`

---

## 1. Resumen Ejecutivo y Alcance

### 1.1. Contexto y Problema
El subsistema de memoria persistente de la plataforma (**El Cerebro** o `internal/brain`) almacena conocimiento semántico del código fuente mediante la tabla `codebase_knowledge` en PostgreSQL con `pgvector`.
Actualmente, no existe un mecanismo formal para segmentar archivos de código fuente en unidades semánticas coherentes. Las segmentaciones basadas en líneas arbitrarias (line chunking) fragmentan funciones a la mitad, pierden contratos de tipos y degradan significativamente la calidad de los embeddings vectoriales y la precisión de los agentes al recuperar contexto de código.

### 1.2. Solución Propuesta
Implementar un módulo de chunking consciente del Árbol de Sintaxis Concreta (AST) en `internal/brain` basado en **Tree-sitter** (`github.com/smacker/go-tree-sitter`) con soporte multilenguaje para **Go** (`.go`) y **TypeScript / TSX** (`.ts`, `.tsx`).

El módulo:
1. Delimita fronteras semánticas exactas: funciones, métodos (con sus receptores o clases calificadas), estructuras (`struct`), interfaces, alias/definiciones de tipos (`type`), clases (`class`) y enumeraciones (`enum`).
2. Extrae para cada fragmento: nombre cualificado del símbolo, tipo canónico de símbolo, ruta relativa del fichero, rangos de línea 1-based (`StartLine`, `EndLine`), contenido íntegro y firma compacta de contrato (`Signature`) sin el cuerpo.
3. Calcula un hash determinista (`ContentHash`) mediante SHA-256 (con soporte para hash rápido XXH64) para posibilitar detección diferencial de cambios e invalidación de caché.
4. Es tolerante a errores sintácticos (*graceful error handling*): archivos con sintaxis rota o en edición activa extraen todos los símbolos válidos reconocibles sin pánico ni interrupción del proceso.
5. Se integra directamente con `internal/brain/knowledge.go`, proveyendo utilidades de conversión a `CodebaseKnowledge` y persistencia en lote en `pgvector`.

---

## 2. Arquitectura y Contratos de Datos

### 2.1. Topología del Módulo

```text
internal/brain/
├── brain.go             # Modelos centrales (Service, CodebaseKnowledge, etc.)
├── knowledge.go         # CRUD y búsqueda vectorial en codebase_knowledge + IngestChunks
├── chunker.go           # Tipos de dominio, constantes de símbolos, interfaz Chunker y utilidades de conversión
├── treesitter.go        # Implementación TreeSitterChunker (gramáticas Go, TS, TSX, parsing AST y signatures)
└── treesitter_test.go   # Suite de pruebas TDD exhaustiva (Go, TS, TSX, syntax errors, hashes, DOD >= 90%)
```

### 2.2. Contratos de Datos y Tipos (`internal/brain/chunker.go`)

```go
// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
)

// Constantes de tipos de símbolos semánticos reconocidos en el AST.
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
	// ErrUnsupportedLanguage se retorna cuando la extensión del fichero no cuenta con gramática configurada.
	ErrUnsupportedLanguage = errors.New("brain: unsupported language or file extension")
	// ErrEmptyContent se retorna cuando el contenido del código está vacío o es únicamente espacio en blanco.
	ErrEmptyContent = errors.New("brain: source content is empty")
)

// CodeChunk representa un bloque semántico ejecutable o declarativo extraído del AST.
type CodeChunk struct {
	// SymbolName es el identificador único o cualificado del símbolo (ej. "Add", "Service.Upsert", "UserProfile").
	SymbolName string `json:"symbol_name"`

	// SymbolType es la categoría semántica (function, method, struct, interface, type, class, enum).
	SymbolType string `json:"symbol_type"`

	// FilePath es la ruta relativa del archivo en el repositorio (ej. "internal/brain/knowledge.go").
	FilePath string `json:"file_path"`

	// StartLine es la línea inicial (1-based, inclusiva) donde comienza la declaración.
	StartLine int `json:"start_line"`

	// EndLine es la línea final (1-based, inclusiva) donde concluye la declaración.
	EndLine int `json:"end_line"`

	// Content es el código fuente completo del fragmento (incluyendo cuerpo).
	Content string `json:"content"`

	// Signature es el contrato compacto del símbolo (encabezado/firma sin el cuerpo de la función o método).
	Signature string `json:"signature"`

	// ContentHash es el hash criptográfico SHA-256 en hexadecimal (64 caracteres) del contenido normalizado.
	ContentHash string `json:"content_hash"`
}

// Chunker define el contrato de segmentación de código fuente basado en AST.
type Chunker interface {
	// Supports indica si el parser cuenta con gramática para el fichero especificado.
	Supports(filePath string) bool

	// Chunk analiza el contenido del archivo y extrae los fragmentos semánticos correspondientes.
	Chunk(ctx context.Context, filePath string, content []byte) ([]CodeChunk, error)
}
```

### 2.3. Conversión a `CodebaseKnowledge` y Utilidades

```go
// ToCodebaseKnowledge convierte un CodeChunk en un registro CodebaseKnowledge listo para pgvector.
func (c CodeChunk) ToCodebaseKnowledge() CodebaseKnowledge {
	return CodebaseKnowledge{
		FilePath:    c.FilePath,
		SymbolName:  c.SymbolName,
		SymbolType:  c.SymbolType,
		Content:     c.Content,
		ContentHash: c.ContentHash,
	}
}

// ChunksToCodebaseKnowledge transforma una colección de CodeChunk en una colección de CodebaseKnowledge.
func ChunksToCodebaseKnowledge(chunks []CodeChunk) []CodebaseKnowledge {
	if len(chunks) == 0 {
		return []CodebaseKnowledge{}
	}
	result := make([]CodebaseKnowledge, len(chunks))
	for i, chunk := range chunks {
		result[i] = chunk.ToCodebaseKnowledge()
	}
	return result
}
```

### 2.4. Motor Tree-sitter y Gramáticas (`internal/brain/treesitter.go`)

```go
// TreeSitterChunker implementa la interfaz Chunker utilizando Tree-sitter.
type TreeSitterChunker struct {
	parserPool sync.Pool
}

// NewTreeSitterChunker inicializa una nueva instancia del segmentador Tree-sitter.
func NewTreeSitterChunker() *TreeSitterChunker
```

#### Reglas de Extracción para Go (`.go`):
1. **`function_declaration`**:
   - `SymbolName`: nombre del identificador de la función (ej. `NewService`).
   - `SymbolType`: `SymbolTypeFunction`.
   - `Signature`: texto desde el inicio del nodo hasta el inicio del bloque `{` (ej. `func NewService(cfg Config) (*Service, error)`).
   - `Content`: texto completo del nodo.
   - `StartLine` / `EndLine`: `node.StartPoint().Row + 1` y `node.EndPoint().Row + 1`.
2. **`method_declaration`**:
   - `SymbolName`: `<ReceiverType>.<MethodName>` (ej. `Service.UpsertCodebaseKnowledge`). Se normaliza extrayendo el identificador base del tipo del receptor sin punteros `*`.
   - `SymbolType`: `SymbolTypeMethod`.
   - `Signature`: texto desde el inicio del nodo hasta el inicio del bloque `{` (ej. `func (s *Service) UpsertCodebaseKnowledge(ctx context.Context, k CodebaseKnowledge) (*CodebaseKnowledge, error)`).
3. **`type_declaration` con `type_spec`**:
   - Si `type_spec` contiene `struct_type`:
     - `SymbolName`: nombre del tipo struct.
     - `SymbolType`: `SymbolTypeStruct`.
     - `Signature`: primera línea o encabezado `type <Name> struct`.
   - Si `type_spec` contiene `interface_type`:
     - `SymbolName`: nombre del tipo interface.
     - `SymbolType`: `SymbolTypeInterface`.
     - `Signature`: primera línea o encabezado `type <Name> interface`.
   - En otros casos (type alias o tipos derivados):
     - `SymbolName`: nombre del tipo.
     - `SymbolType`: `SymbolTypeType`.
     - `Signature`: declaración completa (ej. `type SymbolType string`).

#### Reglas de Extracción para TypeScript (`.ts`, `.tsx`):
1. **`function_declaration`**:
   - `SymbolName`: identificador de la función.
   - `SymbolType`: `SymbolTypeFunction`.
   - `Signature`: texto antes de `{` (ej. `export async function fetchUserData(id: string): Promise<User>`).
2. **`method_definition`** (dentro de `class_declaration`):
   - `SymbolName`: `<ClassName>.<MethodName>` (ej. `BrainClient.queryKnowledge`).
   - `SymbolType`: `SymbolTypeMethod`.
   - `Signature`: texto antes de `{` (ej. `public async queryKnowledge(query: string): Promise<Result[]>`).
3. **`class_declaration`**:
   - `SymbolName`: identificador de la clase.
   - `SymbolType`: `SymbolTypeClass`.
   - `Signature`: texto desde el inicio hasta `{` (ej. `export class BrainClient extends BaseClient implements IBrain`).
4. **`interface_declaration`**:
   - `SymbolName`: identificador de la interfaz.
   - `SymbolType`: `SymbolTypeInterface`.
   - `Signature`: encabezado `interface <Name>` o `export interface <Name>`.
5. **`type_alias_declaration`**:
   - `SymbolName`: identificador del tipo.
   - `SymbolType`: `SymbolTypeType`.
   - `Signature`: declaración completa (ej. `export type Status = "pending" | "running" | "completed";`).
6. **`enum_declaration`**:
   - `SymbolName`: identificador del enum.
   - `SymbolType`: `SymbolTypeEnum`.
   - `Signature`: encabezado `export enum <Name>`.
7. **`lexical_declaration` con arrow function o function expression**:
   - Si contiene una variable asignada a una función flecha `arrow_function`:
     - `SymbolName`: nombre de la constante (ej. `calculateScore`).
     - `SymbolType`: `SymbolTypeFunction`.
     - `Signature`: declaración hasta `=>` (ej. `export const calculateScore = (a: number, b: number): number =>`).

---

## 3. Resiliencia, CGO y Compatibilidad Multiplataforma

1. **Compilación CGO y Dependencias:**
   - La librería `github.com/smacker/go-tree-sitter` compila código C ANSI compatible con GCC/Clang y MinGW/TDM-GCC en Windows.
   - Se validó empíricamente la compilación de `smacker/go-tree-sitter`, `smacker/go-tree-sitter/golang` y `smacker/go-tree-sitter/typescript` en el entorno local (Windows x86_64 con GCC) y en entornos Linux CI estándar.
   - Dado que el proyecto ya requiere CGO para tests con detección de carreras (`go test -race ./...`), la dependencia en CGO es 100% compatible con los estándares existentes del monorepo.
2. **Seguridad en Concurrencia (Thread Safety):**
   - Los parsers de Tree-sitter (`sitter.Parser`) no son reentrantes.
   - `TreeSitterChunker` implementa un `sync.Pool` de parsers o asigna instancias efímeras para garantizar invocaciones simultáneas seguras bajo `go test -race`.
3. **Manejo Tolerante de Errores Gramaticales:**
   - Si un archivo contiene bloques corruptos o errores sintácticos (`ERROR` nodes en Tree-sitter), el árbol AST aísla la sección dañada.
   - El visitor continúa recorriendo el árbol, ignorando nodos `ERROR` y extrayendo sin interrupción todas las funciones y tipos sintácticamente válidos del resto del archivo.

---

## 4. Criterios de Aceptación (Definition of Done)

- [ ] Soporte multilenguaje: análisis y extracción exitosa para ficheros `.go`, `.ts` y `.tsx`.
- [ ] Detección determinista de símbolos: cada chunk incluye `SymbolName`, `SymbolType`, `FilePath`, `StartLine`, `EndLine`, `Content`, `Signature` y `ContentHash`.
- [ ] Tolerancia a sintaxis rota: ficheros con errores sintácticos o incompletos extraen los símbolos sanos sin provocar panic.
- [ ] Integración con el Cerebro: funciones `ToCodebaseKnowledge()` y `ChunksToCodebaseKnowledge()` integradas con `CodebaseKnowledge` y persistencia en `internal/brain`.
- [ ] Licenciamiento REUSE: cabecera SPDX Apache-2.0 en todos los ficheros nuevos.
- [ ] Calidad y Cobertura: cobertura de pruebas unitarias en `internal/brain/treesitter_test.go` $\ge 90\%$.
- [ ] Ejecución en verde: `go test -v -race ./internal/brain/...` pasa al 100%.
