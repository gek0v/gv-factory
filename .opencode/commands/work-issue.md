---
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors
description: Resuelve un issue de GitHub de principio a fin mediante el flujo TDD multi-agente
---

Ejecuta el ciclo de desarrollo autónomo completo para resolver el issue de GitHub #$1:

## Fase 1: Especificación y Plan TDD (Subagente `spec-writer`)
1. Inspecciona el issue ejecutando `gh issue view $1`.
2. Invoca al subagente `spec-writer` con la descripción y requisitos del issue para generar la especificación de diseño de software (SDD) y el plan de pruebas Red-Green-Refactor con contratos de test.

## Fase 2: Implementación TDD en Rama Aislada
1. Asegúrate de partir de `main` actualizado y crea una rama dedicada: `git checkout -b <tipo>/issue-$1-<alcance>`.
2. **Fase Red:** Escribe primero las pruebas unitarias especificadas por el spec-writer (`*_test.go` o `*.test.ts`) y confirma su fallo previo.
3. **Fase Green:** Implementa la lógica mínima indispensable para satisfacer las pruebas.
4. **Fase Refactor:** Optimiza la solución asegurando que el 100% de los tests pasen.
5. Ejecuta las verificaciones locales completas:
   - Backend Go: `go test -race ./...` y `go vet ./...`
   - Frontend (si aplica): `pnpm --dir web test` y `pnpm --dir web build`

## Fase 3: Auditoría en Contexto Limpio (Subagente `reviewer`)
1. Invoca al subagente `reviewer` en un contexto limpio para auditar el `git diff` de la rama.
2. Si el dictamen es `REVISE` o `FAILED`, corrige las observaciones y repite la auditoría hasta obtener el visto bueno.

## Fase 4: Definition of Done y Pull Request (Subagente `pr-creator`)
1. Con el dictamen `PASSED` confirmado, invoca al subagente `pr-creator`.
2. El subagente validará las compuertas de la DoD (compilación limpia, cero secretos, cabeceras SPDX Apache-2.0), realizará el commit con Conventional Commits, enviará la rama al remoto y abrirá el Pull Request vinculando el cierre del issue (`Closes #$1`).
