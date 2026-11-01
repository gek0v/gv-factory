---
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors
description: Resuelve uno o más issues de GitHub de principio a fin mediante flujo TDD (un issue o delegación paralela si son varios)
---

Ejecuta el ciclo de desarrollo autónomo para los siguientes issues de GitHub: $ARGUMENTS

## Protocolo de Decisión y Ejecución

1. **Evaluación de Argumentos ($ARGUMENTS):**
   - Parsea los números de issue proporcionados.
   - **Si se especifican 2 o más issues (ej. `63 64`):**
     - Despacha inmediatamente en paralelo un subagente `issue-orchestrator` (`background: true`) para cada issue.
     - Cada orquestador operará en su propio worktree aislado (`.worktrees/issue-<id>`).
     - Al completar todos, consolida los resultados y los enlaces a los Pull Requests.
   - **Si se especifica 1 único issue (ej. `63`):**
     - Puedes delegar en `issue-orchestrator` o ejecutar directamente el ciclo local:
       1. **Fase 1 (spec-writer):** Inspecciona el issue con `gh issue view $1` y genera SDD y plan TDD.
       2. **Fase 2 (TDD):** Crea la rama `feat/issue-$1-...` o `fix/...`, implementa en ciclo Red-Green-Refactor y corre tests.
       3. **Fase 3 (reviewer):** Audita el diff en contexto limpio (solo lectura).
       4. **Fase 4 (pr-creator):** Valida DoD, commit convencional y crea PR (`Closes #$1`).
