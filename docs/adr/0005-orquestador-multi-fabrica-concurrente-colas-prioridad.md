# ADR 0005: Orquestador Multi-Fábrica Concurrente con Aislamiento de Ramas y Colas de Prioridad

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
La ejecución simultánea de múltiples fábricas de software sobre ramas distintas de un mismo repositorio presentaba desafíos de coordinación: contención y fallos en `.git/index.lock` al crear/eliminar worktrees en paralelo, potenciales colisiones entre ramas idénticas, y saturación de recursos sin discriminación de urgencia entre pipelines críticos (e.g. `bugfix-express`) y pipelines estándar o de documentación.

## Decisión
1. **Registro Concurrente y Control de Colisiones (`internal/dag.MultiFactoryOrchestrator`):**
   - Registro centralizado de ejecuciones activas con control de unicidad de rama (`ErrBranchConflict`).
   - Límites de concurrencia globales (`MaxConcurrentRuns`) y por repositorio (`MaxConcurrentPerRepo`).
2. **Multitenencia y Protección de Locks (`internal/sandbox`):**
   - Rutas aisladas por ejecución y rama bajo el patrón `ov-factory/runs/<run_id>/<branch>`.
   - Serialización de operaciones administrativas de Git mediante mutex por repositorio (`getRepoLock`), reintentos exponenciales y limpieza proactiva de locks huérfanos (`CleanStaleLocks`).
   - Limpieza determinista (`Destroy` / `RecycleWorktree`) al finalizar, fallar o cancelar una ejecución.
3. **Planificación con Cola de Prioridad y Prevención de Inanición (`internal/dag.PriorityQueue`):**
   - Niveles de prioridad base: `PriorityHigh` (bugfix/express), `PriorityNormal` (feature), `PriorityLow` (refactor/docs).
   - Algoritmo de envejecimiento y bonificación por saltos (`DispatchesSkipped` + `AgingBoost`), garantizando que tareas de menor prioridad avancen y no sufran inanición frente a ráfagas de alta prioridad.
4. **Telemetría Global en Tiempo Real:**
   - Emisión de métricas agregadas (`ov.factory.metrics`) sobre el bus de eventos/Gateway: conteo de fábricas activas/encoladas, sandboxes, porcentaje de progreso en tiempo real y consumo de tokens.

## Consecuencias
* **Positivas:** Ejecución paralela confiable sin errores de Git lock, priorización de flujos de alta urgencia sin inanición, trazabilidad en tiempo real y reciclado atómico del espacio en disco.
* **Negativas:** La serialización breve a nivel de repositorio durante comandos `git worktree` añade latencia despreciable durante el aprovisionamiento.
