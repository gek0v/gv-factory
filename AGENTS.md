# AGENTS.md — Directrices para Agentes de IA en gv-factory

Instrucciones operativas para agentes de IA que analizan, diseñan e implementan código en el repositorio **gv-factory** (plataforma modular de fábricas de software autónomas multi-agente).

---

## 1. Context Pointers (Fuentes de Verdad)

Consulta los documentos de referencia según la tarea específica:

* **Topología del repositorio y límites de módulos:** [ESTRUCTURA.md](ESTRUCTURA.md) — Árbol del monorepo (Go workspaces, `cmd/`, `internal/`, `pkg/`, `web/`, `plugins-sdk/`).
* **Diseño del sandbox, contenedores y merge queue:** [INFRAESTRUCTURA.md](INFRAESTRUCTURA.md) — Gestión de Git Worktrees efímeros, asignación de puertos por fórmula, contenedores rootless y `git merge-tree`.
* **Fases, hitos y criterios de aceptación:** [ROADMAP.MD](ROADMAP.MD) — Hoja de ruta incremental desde Fase 0 (Setup) hasta Fase 6 (Producción).
* **Gobernanza, licencias y CI/CD:** [GIT_FILES.md](GIT_FILES.md) — Especificaciones de archivos raíz, REUSE, workflows de GitHub Actions y templates de issues/PR.
* **Investigación técnica profunda y microkernel:** [drafts/investigacion.md](drafts/investigacion.md) — Microkernel Extism (WASM), orquestación híbrida PostgreSQL + Hatchet, memoria vectorial con `pgvector` y segmentación Tree-sitter.

---

## 2. Pila Tecnológica y Arquitectura

* **Backend y Core:** Go 1.22+ (`cmd/ov-server`, `cmd/ov-worker`, `cmd/ov-cli`).
* **Frontend y Canvas Visual:** TypeScript, React 18+, `@xyflow/react` (React Flow), Zustand, Vite en el directorio `web/`.
* **Sistema de Plugins:** WebAssembly (WASM) gobernado por **Extism** y Wasmtime (`plugins-sdk/`, `internal/plugins/`).
* **Memoria Persistente ("El Cerebro"):** PostgreSQL 16 con extensión `pgvector` (`migrations/brain/`, `internal/brain/`).
* **Indexación de Código:** Tree-sitter (AST parsing) y `ast-grep` para análisis estructural de código y cálculo diferencial por hashes XXH64.
* **Orquestación de Procesos:** Intérprete de grafo de estados mutables sobre PostgreSQL + motor de ejecución duradera con **Hatchet**.
* **Aislamiento:** Git Worktrees efímeros (`/tmp/ov-worktrees/<run_id>`) + contenedores rootless (Docker/Podman SDK) en `internal/sandbox`.
* **Eventos y Mensajería:** NATS JetStream, Svix (webhooks) y WebSockets.

---

## 3. Reglas Críticas de Implementación (Invariantes)

### A. Cumplimiento Legal y Licenciamiento REUSE
* Licencia base: **Apache-2.0**.
* **Cabecera SPDX obligatoria:** Cada archivo nuevo de código (`.go`, `.ts`, `.tsx`, `.sql`, `.yaml`, `.sh`) debe incluir la cabecera SPDX:
  ```text
  // SPDX-License-Identifier: Apache-2.0
  // SPDX-FileCopyrightText: 2026 gv-factory contributors
  ```
* En archivos de datos (`.json`, imágenes, esquemas), registrar la atribución en `.reuse/dep5`.

### B. Formato, Convenciones y Commits
* **Saltos de línea:** `LF` en todo el repositorio (`.gitattributes`).
* **Indentación:**
  * Tabulaciones (`\t`) para código Go (`.go`) y `Makefile`.
  * 2 espacios para TypeScript/React (`.ts`, `.tsx`), YAML (`.yaml`, `.yml`) y JSON (`.json`).
* **Conventional Commits obligatorio:** Estructura `tipo(alcance): descripción imperativa` (ej. `feat(sandbox): add worktree prune on task cleanup`, `fix(brain): handle tree-sitter parse errors`). Tipos válidos: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`.

### C. Aislamiento y Trabajo en Git
* **Prohibición de mutación directa en `main`:** Todo cambio automatizado o tarea de fábrica corre en una rama dedicada `ov-run/<run_id>` o rama de feature.
* **No usar `git checkout` ni `git switch` dentro de sandboxes:** Operar exclusivamente sobre el worktree aislado asignado.
* **Limpieza de locks:** Verificar y liberar archivos `.git/index.lock` o `.git/worktrees/<id>/index.lock` huérfanos antes de aprovisionar un worktree.
* **Merge Queue:** Integrar ramas mediante validación previa con `git merge-tree` antes de aplicar rebase y fast-forward a `main`. Desviar a Pull Request si hay conflictos.

### D. Desarrollo Dirigido por Pruebas (TDD)
* Diseñar interfaces y pruebas unitarias primero (**Red**).
* Implementar la lógica mínima necesaria para satisfacer las pruebas (**Green**).
* Optimizar preservando la suite en verde (**Refactor**).
* Cobertura mínima requerida para Fast-Track a producción: $\ge 90\%$ de cobertura de pruebas unitarias y 0 alertas de seguridad.

### E. Seguridad y Manejo de Secretos
* **Cero secretos en el código:** Prohibido commitear archivos `.env`, tokens o credenciales.
* **Ofuscación de logs:** Aplicar máscara (`***`) a variables de entorno sensibles en los flujos `stdout`/`stderr` antes de exponerlos al usuario o al LLM.
* **Protocolo Human-in-the-Loop:** Usar tokens opacos criptográficos (`approval_id`) con expiración temporal y verificación HMAC. No procesar comandos no autenticados desde chats externos.

---

## 4. Estructura de Paquetes y Responsabilidades

```text
cmd/              -> Puntos de entrada ejecutables (ov-server, ov-worker, ov-cli). Lógica de arranque y wiring de dependencias.
internal/         -> Código privado no exportable fuera del módulo gv-factory.
  brain/          -> pgvector, RAG, Tree-sitter AST, embeddings semánticos y memoria episódica.
  dag/            -> Intérprete del grafo de estados en PostgreSQL y despacho de tareas hacia Hatchet.
  sandbox/        -> Gestión del ciclo de vida de Git Worktrees, contenedores rootless, puertos y merger.
  plugins/        -> Runtime de Extism (WASM) y definición de Host Functions seguras.
  gateway/        -> NATS JetStream, Svix webhooks y WebSockets.
  auth/           -> OAuth2, RBAC, tokens opacos y validación criptográfica de aprobaciones.
pkg/              -> Paquetes reutilizables y contratos públicos (schemas JSON/YAML, SDK de tipos compartidos).
web/              -> Aplicación cliente SPA (Vite + React + @xyflow/react).
plugins-sdk/      -> Plantillas y PDK para compilar plugins en WASM.
deployments/      -> docker-compose.yml y manifiestos de infraestructura local y cloud.
docs/             -> Documentación de usuario y Architecture Decision Records (ADR).
```

---

## 5. Criterios de Completitud (Definition of Done)

Para considerar una tarea o PR finalizado:

1. **Compilación limpia:** Backend Go compila sin advertencias (`go build ./...`) y el frontend pasa el build de producción (`pnpm build`).
2. **Suite de tests aprobada:** Todas las pruebas pasan (`go test -race ./...` y `pnpm test`).
3. **Linters en verde:** `golangci-lint run` y `eslint` finalizan con código de salida 0.
4. **Cumplimiento REUSE:** `reuse lint` verifica que todos los ficheros nuevos cuentan con licencia y copyright válidos.
5. **Documentación sincronizada:** Cualquier cambio en la arquitectura incluye su correspondiente ADR en `docs/adr/`.
