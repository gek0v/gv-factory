---
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors
description: Resuelve 2 o más issues de GitHub en paralelo delegando un orquestador aislado por cada issue
---

Ejecuta la orquestación concurrente y paralela para los siguientes issues de GitHub: $ARGUMENTS

## Protocolo de Orquestación Paralela

1. **Extracción y Validación de Argumentos:**
   - Parsea los identificadores o números de issue proporcionados en `$ARGUMENTS` (ej. `63 64`, `#63 #64`).
   - Comprueba que se hayan especificado los issues a resolver.

2. **Despacho Concurrente de Orquestadores:**
   - Para **CADA ISSUE**, despacha de forma simultánea e independiente una instancia del subagente `issue-orchestrator` en modo background (`background: true`):
     - Asigna a cada orquestador su propio git worktree aislado (`.worktrees/issue-<id>`).
     - Cada orquestador ejecutará de forma autónoma el ciclo: `spec-writer` -> TDD builder -> `reviewer` -> `pr-creator`, limpieza de worktree y reporte final.
   - **REGLA MANDATORIA DE PARALELISMO:** Invoca las herramientas de despacho en paralelo sin esperar secuencialmente la finalización de uno antes de lanzar los demás.

3. **Monitoreo y Recolección de Resultados:**
   - Supervisa la finalización de los subagentes orquestadores concurrentes.
   - Cada orquestador trabajará sin riesgo de colisión al operar en su worktree aislado.

4. **Consolidación y Reporte Final:**
   - Emite una tabla resumen consolidada con el resultado de todos los issues procesados:
     | Issue | Título / Alcance | Rama | Auditoría (Reviewer) | Pull Request | Estado |
     | :--- | :--- | :--- | :--- | :--- | :--- |
   - Enlaza cada Pull Request creado.
