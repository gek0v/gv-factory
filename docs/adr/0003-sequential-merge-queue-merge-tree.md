<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# ADR 0003: Merge Queue secuencial con validación previa 'git merge-tree' antes de rebase y fast-forward

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
En `gv-factory`, múltiples fábricas de software autónomas ejecutan tareas en paralelo sobre un mismo repositorio base. Cada fábrica opera sobre una rama de ejecución dedicada (`ov-run/<run_id>`) dentro de un Git Worktree efímero asignado.

La integración concurrente de los cambios producidos por estas ramas hacia la rama principal (`main`) plantea graves desafíos de concurrencia y consistencia:
1. **Carreras críticas de integración:** Si dos fábricas intentan fusionar sus ramas casi simultáneamente basándose en un mismo commit ancestro, la segunda fusión puede invalidar silenciosamente el código o romper la suite de pruebas en `main`, aun cuando Git no reporte un conflicto sintáctico a nivel de texto (*semantic breakages*).
2. **Contención y corrupción de locks de Git:** Ejecutar operaciones de rebase o merge directas concurrentemente sobre el repositorio base genera contención en `.git/index.lock` y `.git/refs/heads/main.lock`, pudiendo corromper el árbol de Git o abortar pipelines en vuelo.
3. **Sobrecarga de I/O en chequeos especulativos:** Aprovisionar un worktree completo en disco y ejecutar `git checkout` o `git merge` solo para verificar si una rama presenta conflictos genera una sobrecarga excesiva de disco y tiempo.
4. **Historial no lineal:** Las fusiones mediante merges de tres vías tradicionales (*merge commits*) ensucian el grafo de versiones con bifurcaciones innecesarias, dificultando auditorías forenses, bisectados automáticos y reversiones de emergencia.

## Decisión
Implementar una **Merge Queue Secuencial** en `internal/sandbox` y `internal/dag`, respaldada por validación especulativa previa en memoria con **`git merge-tree`**:

1. **Validación Especulativa en Memoria con `git merge-tree`:**
   - Antes de encolar una rama para integración o requerir validación humana, el sistema invoca `git merge-tree --write-tree <main> <rama_run>` directamente a través de Git plomería.
   - Esta operación evalúa la fusión de 3 vías íntegramente en la memoria de Git, sin tocar el directorio de trabajo, sin modificar el índice en disco y sin alterar las referencias de `main`.
   - Si `git merge-tree` reporta conflictos de código o metadatos (código de retorno $\ne 0$ o presencia de marcadores de conflicto), el intento de fusión se rechaza de inmediato (*Fail Fast*) y se redirige a un nodo de resolución de conflictos o a intervención humana.
2. **Cola de Fusión Serializada (FIFO) con Prioridades:**
   - Las ejecuciones validadas sin conflictos ingresan a una cola secuencial gobernada por un mutex por repositorio (`repoLock`).
   - Solo una rama es procesada para integración a `main` a la vez, garantizando serializabilidad estricta.
3. **Rebase Dinámico y Compuerta de Verificación (Smoke Test):**
   - Cuando le corresponde el turno a la rama a la cabeza de la cola, el sistema actualiza la rama sobre el commit más reciente de `main` mediante `git rebase main`.
   - Se ejecuta una compuerta final de validación automatizada (suite de pruebas unitarias y linters) sobre el worktree aislado de integración.
4. **Fusión Atómica Fast-Forward:**
   - Una vez aprobada la compuerta de verificación, la integración hacia `main` se realiza de manera atómica con avance rápido exclusivo (`git merge --ff-only`), preservando un historial lineal, limpio y determinista.
5. **Prohibición de Modificación Directa en `main`:**
   - La rama `main` queda estrictamente protegida; ningún agente ni proceso tiene autorización para realizar commits o pushes directos fuera de la Merge Queue.

## Consecuencias
* **Positivas:**
  - Garantía absoluta de que la rama `main` nunca contiene compilaciones rotas o tests en rojo (*green-build invariant*).
  - Cero escrituras espurias en disco o contención de locks durante la detección de conflictos gracias a `git merge-tree`.
  - Historial Git completamente lineal, simplificando diagnósticos con `git bisect` y rollback atómico con `git revert`.
* **Negativas:**
  - La serialización estricta de la cola de fusión limita el rendimiento máximo de merges al tiempo acumulado de ejecución de las compuertas de prueba.
  - Si una rama al principio de la cola tarda demasiado en sus pruebas, las ramas posteriores deben esperar su turno en la cola antes de ser integradas.
