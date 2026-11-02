<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Plan TDD: Tree-sitter Code Chunker con Extracción de Símbolos AST (#119)

> **Para Desarrolladores y Agentes:** Sigue estrictamente el ciclo Red-Green-Refactor tarea por tarea. Cada tarea contiene pasos de prueba que fallan primero (**Red**), la implementación mínima que los satisface (**Green**) y la optimización limpia (**Refactor**).

**Objetivo:** Implementar el segmentador de código basado en Tree-sitter con extracción de símbolos AST para Go (`.go`) y TypeScript (`.ts`, `.tsx`) en `internal/brain`, extrayendo funciones, métodos, structs, interfaces y tipos con firmas compactas y hashes deterministas, e integrándolo con `CodebaseKnowledge` para persistencia en `pgvector`.

**Especificación:** `docs/superpowers/specs/2026-11-02-brain-treesitter-chunker-design.md`

---

## Restricciones Globales e Invariantes
- Cabecera SPDX Apache-2.0 obligatoria en todo nuevo archivo `.go`:
  ```go
  // SPDX-License-Identifier: Apache-2.0
  // SPDX-FileCopyrightText: 2026 gv-factory contributors
  ```
- Indentación con tabulaciones (`\t`) en código Go y saltos de línea Unix (`LF`).
- Mensajes de commit convencionales con referencia al issue (`#119`).
- Ausencia total de condiciones de carrera con `go test -race ./internal/brain/...`.
- Tolerancia a errores de sintaxis sin generar `panic`.
- Cobertura de pruebas unitarias $\ge 90\%$.

---

## Tareas del Plan TDD

### Tarea 1: Dependencias de Tree-sitter en `go.mod`

**Archivos:**
- Modificar: `go.mod`
- Modificar: `go.sum`

- [ ] **Paso 1: Agregar dependencias requeridas**
  Descargar e incluir:
  - `github.com/smacker/go-tree-sitter`
  - `github.com/cespare/xxhash/v2` (ya presente como indirecta, promover a directa si se usa explícitamente)
  Ejecutar:
  ```bash
  go get github.com/smacker/go-tree-sitter@v0.0.0-20240827094217-dd81d9e9be82
  go mod tidy
  ```
- [ ] **Paso 2: Verificar compilación limpia**
  Ejecutar:
  ```bash
  go test ./internal/brain/...
  ```
  Resultado esperado: PASS sin advertencias ni conflictos de dependencias.
- [ ] **Paso 3: Commit**
  ```bash
  git add go.mod go.sum
  git commit -m "build(brain): add go-tree-sitter dependencies (#119)"
  ```

---

### Tarea 2: Contratos de Dominio y Tipos (`internal/brain/chunker.go`)

**Archivos:**
- Crear: `internal/brain/chunker.go`
- Crear: `internal/brain/chunker_test.go`

**Interfaces y Tipos:**
- Constantes: `SymbolTypeFunction`, `SymbolTypeMethod`, `SymbolTypeStruct`, `SymbolTypeInterface`, `SymbolTypeType`, `SymbolTypeClass`, `SymbolTypeEnum`.
- Errores: `ErrUnsupportedLanguage`, `ErrEmptyContent`.
- Estructura: `CodeChunk` con `SymbolName`, `SymbolType`, `FilePath`, `StartLine`, `EndLine`, `Content`, `Signature`, `ContentHash`.
- Interfaz: `Chunker` (`Supports(filePath string) bool`, `Chunk(ctx context.Context, filePath string, content []byte) ([]CodeChunk, error)`).
- Helpers: `ComputeContentHash(content string) string`, `(c CodeChunk) ToCodebaseKnowledge() CodebaseKnowledge`, `ChunksToCodebaseKnowledge(chunks []CodeChunk) []CodebaseKnowledge`.

- [ ] **Paso 1 (RED): Escribir pruebas unitarias de contratos y hashing**
  En `internal/brain/chunker_test.go`:
  - `TestCodeChunk_ToCodebaseKnowledge`: Validar que los campos de `CodeChunk` se transfieren fielmente a `CodebaseKnowledge` (`FilePath`, `SymbolName`, `SymbolType`, `Content`, `ContentHash`).
  - `TestChunksToCodebaseKnowledge_Batch`: Validar conversión de slices vacíos y slices con múltiples elementos.
  - `TestComputeContentHash_Deterministic`: Validar que el hash SHA-256 es determinista, insensible a espacios en blanco iniciales/finales, y produce 64 caracteres hexadecimales consistentes con `CodebaseKnowledge.Validate()`.
- [ ] **Paso 2: Ejecutar prueba para verificar el fallo**
  ```bash
  go test -v ./internal/brain -run TestCodeChunk
  ```
  Resultado esperado: FAIL (tipos no definidos).
- [ ] **Paso 3 (GREEN): Implementar `internal/brain/chunker.go`**
  Escribir `chunker.go` con cabecera SPDX, estructuras, constantes, cálculo de SHA-256 y métodos de conversión.
- [ ] **Paso 4: Ejecutar prueba para verificar que pasa**
  ```bash
  go test -v ./internal/brain -run TestCodeChunk
  ```
  Resultado esperado: PASS.
- [ ] **Paso 5 (REFACTOR): Validar legibilidad y documentación de GoDoc**
  Asegurar comentarios en cada tipo público exportado.
- [ ] **Paso 6: Commit**
  ```bash
  git add internal/brain/chunker.go internal/brain/chunker_test.go
  git commit -m "feat(brain): define CodeChunk contracts and knowledge conversion helpers (#119)"
  ```

---

### Tarea 3: Segmentador Go con Tree-sitter (`internal/brain/treesitter.go`)

**Archivos:**
- Crear: `internal/brain/treesitter.go`
- Crear: `internal/brain/treesitter_test.go`

**Comportamiento a probar:**
- Extracción de funciones Go estándar (`func FuncName(args) Ret`).
- Extracción de métodos Go con receptor por valor y por puntero (`func (s *Service) MethodName(args) Ret` -> `SymbolName: "Service.MethodName"`).
- Extracción de structs Go (`type MyStruct struct { ... }` -> `SymbolName: "MyStruct"`, `SymbolType: "struct"`).
- Extracción de interfaces Go (`type DBPool interface { ... }` -> `SymbolName: "DBPool"`, `SymbolType: "interface"`).
- Extracción de tipos derivados y alias (`type CustomID string` -> `SymbolName: "CustomID"`, `SymbolType: "type"`).
- Extracción correcta de `StartLine` y `EndLine` (1-based, inclusivos).
- Extracción de firmas limpias sin el cuerpo `{ ... }`.

- [ ] **Paso 1 (RED): Escribir pruebas unitarias para Go en `treesitter_test.go`**
  Crear casos de prueba con fragmentos de código Go representativos:
  ```go
  const goSample = `
  package sample
  
  type User struct {
      ID   string
      Name string
  }
  
  type Greeter interface {
      Greet(name string) string
  }
  
  func NewUser(name string) *User {
      return &User{Name: name}
  }
  
  func (u *User) Greet(greeting string) string {
      return greeting + " " + u.Name
  }
  `
  ```
  Aserciones:
  - 4 chunks extraídos.
  - Símbolos: `User` (struct), `Greeter` (interface), `NewUser` (function), `User.Greet` (method).
  - Verificación de `Signature`:
    - `type User struct`
    - `type Greeter interface`
    - `func NewUser(name string) *User`
    - `func (u *User) Greet(greeting string) string`
  - Verificación de `StartLine` y `EndLine` correspondientes a cada bloque.
  - Verificación de que `ContentHash` coincide con el cálculo esperado.
- [ ] **Paso 2: Ejecutar prueba para verificar el fallo**
  ```bash
  go test -v ./internal/brain -run TestTreeSitter_ChunkGo
  ```
  Resultado esperado: FAIL (`NewTreeSitterChunker` no existe).
- [ ] **Paso 3 (GREEN): Implementar soporte para Go en `internal/brain/treesitter.go`**
  - Implementar `TreeSitterChunker` con gramática `golang.GetLanguage()`.
  - Recorrido del AST con `sitter.NewParser()` y extracción de nodos `function_declaration`, `method_declaration` y `type_declaration`.
  - Formateo de firmas compactas antes del delimitador `{`.
- [ ] **Paso 4: Ejecutar prueba para verificar que pasa**
  ```bash
  go test -v ./internal/brain -run TestTreeSitter_ChunkGo
  ```
  Resultado esperado: PASS.
- [ ] **Paso 5: Commit**
  ```bash
  git add internal/brain/treesitter.go internal/brain/treesitter_test.go
  git commit -m "feat(brain): implement Go AST symbol extraction with tree-sitter (#119)"
  ```

---

### Tarea 4: Segmentador TypeScript y TSX (`internal/brain/treesitter.go`)

**Archivos:**
- Modificar: `internal/brain/treesitter.go`
- Modificar: `internal/brain/treesitter_test.go`

**Comportamiento a probar:**
- Extracción de funciones declaradas (`export function parseConfig(raw: string): Config`).
- Extracción de funciones flecha en constantes (`export const processItem = (item: Item): Result =>`).
- Extracción de interfaces TypeScript (`export interface UserProfile { id: string; }`).
- Extracción de alias de tipo (`export type State = "idle" | "busy";`).
- Extracción de clases y sus métodos internos (`export class WorkerService { async start(): Promise<void> }` -> `WorkerService` [class] y `WorkerService.start` [method]).
- Extracción de enums (`export enum LogLevel { DEBUG, INFO, ERROR }`).
- Soporte para extensiones `.ts` y componentes `.tsx` (con sintaxis JSX/TSX).

- [ ] **Paso 1 (RED): Escribir pruebas unitarias para TypeScript y TSX en `treesitter_test.go`**
  Añadir casos de prueba:
  ```typescript
  const tsSample = `
  export interface Config {
      port: number;
  }
  
  export type Status = "pending" | "ready";
  
  export enum Priority {
      Low = 1,
      High = 2,
  }
  
  export function createServer(cfg: Config): Server {
      return new Server(cfg);
  }
  
  export const helper = (x: number): number => {
      return x * 2;
  };
  
  export class Engine {
      private running: boolean = false;
      public async run(): Promise<void> {
          this.running = true;
      }
  }
  `;
  ```
  Y una muestra TSX con componentes React:
  ```tsx
  export const Dashboard: React.FC<Props> = ({ title }) => {
      return <div>{title}</div>;
  };
  ```
  Aserciones:
  - Detección precisa de todos los símbolos y sus categorías.
  - Calificación de método de clase como `Engine.run`.
  - Extracción de firmas limpias sin cuerpos.
- [ ] **Paso 2: Ejecutar prueba para verificar el fallo**
  ```bash
  go test -v ./internal/brain -run TestTreeSitter_ChunkTypeScript
  ```
  Resultado esperado: FAIL.
- [ ] **Paso 3 (GREEN): Implementar gramáticas TypeScript y TSX en `treesitter.go`**
  - Configurar `typescript.GetLanguage()` para `.ts` y `tsx.GetLanguage()` para `.tsx`.
  - Recorrer `function_declaration`, `lexical_declaration` (arrow functions), `interface_declaration`, `type_alias_declaration`, `enum_declaration`, `class_declaration` y `method_definition`.
- [ ] **Paso 4: Ejecutar prueba para verificar que pasa**
  ```bash
  go test -v ./internal/brain -run TestTreeSitter_ChunkTypeScript
  ```
  Resultado esperado: PASS.
- [ ] **Paso 5: Commit**
  ```bash
  git add internal/brain/treesitter.go internal/brain/treesitter_test.go
  git commit -m "feat(brain): implement TypeScript and TSX AST extraction (#119)"
  ```

---

### Tarea 5: Tolerancia a Errores Sintácticos y Casos de Borde

**Archivos:**
- Modificar: `internal/brain/treesitter.go`
- Modificar: `internal/brain/treesitter_test.go`

**Casos a cubrir:**
1. Archivo con sintaxis rota antes de una función válida (ej. llaves sin cerrar o tokens inválidos).
2. Archivo vacío o compuesto únicamente por espacios en blanco / comentarios (debe retornar `[]CodeChunk{}`, `nil`).
3. Extensión de archivo desconocida / no soportada (ej. `.py`, `.md`), debe retornar `ErrUnsupportedLanguage`.
4. Archivo con sintaxis 100% rota sin ningún símbolo reconocible (debe retornar slice vacío sin entrar en pánico).
5. Invocación concurrente en múltiples goroutines (`sync.Pool` o parsers aislados) sin condiciones de carrera.

- [ ] **Paso 1 (RED): Escribir pruebas de resiliencia y concurrencia**
  - `TestTreeSitter_SyntaxErrorsGraceful`: Código con fragmento roto arriba y funciones válidas abajo. Comprobar que las funciones válidas son extraídas y no ocurre panic.
  - `TestTreeSitter_EmptyAndWhitespace`: Probar con `""`, `"   \n\t  "`, `// solo comentarios`.
  - `TestTreeSitter_UnsupportedFile`: Probar con `file.py`, `readme.txt`.
  - `TestTreeSitter_ConcurrentSafety`: Ejecutar 50 goroutines concurrentes parseando archivos simultáneamente con `-race`.
- [ ] **Paso 2: Ejecutar pruebas para verificar posibles fallos o carreras**
  ```bash
  go test -v -race ./internal/brain -run TestTreeSitter_
  ```
- [ ] **Paso 3 (GREEN/REFACTOR): Blindar la lógica de traversal y manejo de errores**
  - Validar comprobaciones de punteros nulos en `node.ChildByFieldName` y llamadas a `node.Content`.
  - Usar asignación de parser protegida por goroutine o pool para thread-safety.
- [ ] **Paso 4: Verificar ejecución limpia**
  ```bash
  go test -v -race ./internal/brain -run TestTreeSitter_
  ```
  Resultado esperado: PASS.
- [ ] **Paso 5: Commit**
  ```bash
  git add internal/brain/treesitter.go internal/brain/treesitter_test.go
  git commit -m "fix(brain): ensure syntax resilience and concurrent thread-safety in chunker (#119)"
  ```

---

### Tarea 6: Integración con `internal/brain/knowledge.go`

**Archivos:**
- Modificar: `internal/brain/knowledge.go`
- Modificar: `internal/brain/knowledge_test.go`

**Objetivo:** Proveer método de ingesta directa de chunks en `Service` (`IngestChunks`).

- [ ] **Paso 1 (RED): Escribir prueba de ingesta de chunks en `knowledge_test.go`**
  - `TestService_IngestChunks`: Validar que una lista de `CodeChunk` puede ser transformada e insertada/actualizada en `codebase_knowledge` a través de mock o servicio DB.
- [ ] **Paso 2: Implementar método `IngestChunks` en `knowledge.go`**
  ```go
  // IngestChunks almacena en lote fragmentos semánticos extraídos por el chunker en la base de conocimientos.
  func (s *Service) IngestChunks(ctx context.Context, chunks []CodeChunk) ([]*CodebaseKnowledge, error)
  ```
- [ ] **Paso 3: Ejecutar pruebas para verificar que pasa**
  ```bash
  go test -v ./internal/brain -run TestService_IngestChunks
  ```
  Resultado esperado: PASS.
- [ ] **Paso 4: Commit**
  ```bash
  git add internal/brain/knowledge.go internal/brain/knowledge_test.go
  git commit -m "feat(brain): integrate AST chunk ingestion into knowledge service (#119)"
  ```

---

### Tarea 7: Verificación de Cobertura, REUSE y Definition of Done

- [ ] **Paso 1: Medir cobertura de pruebas unitarias en `internal/brain`**
  ```bash
  go test -coverprofile=coverage.out ./internal/brain/...
  go tool cover -func=coverage.out
  ```
  Verificar que la cobertura en `chunker.go` y `treesitter.go` supera el 90%.
- [ ] **Paso 2: Comprobar ausencia de carreras de datos**
  ```bash
  go test -v -race ./internal/brain/...
  ```
- [ ] **Paso 3: Validar cumplimiento REUSE y licencias**
  Verificar que todo archivo nuevo cuente con cabecera SPDX Apache-2.0.
- [ ] **Paso 4: Commit final del hito**
  ```bash
  git commit --allow-empty -m "chore(brain): complete tree-sitter chunker DoD verification (#119)"
  ```
