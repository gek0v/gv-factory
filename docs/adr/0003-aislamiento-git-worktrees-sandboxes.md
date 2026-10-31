# ADR 0003: Aislamiento con Git Worktrees Efímeros y Contenedores Rootless

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
Múltiples fábricas autónomas operan concurrentemente sobre el mismo repositorio base. Clonar el repositorio completo por cada tarea satura el ancho de banda y almacenamiento, mientras que mutar ramas concurrentemente en un único directorio de trabajo genera colisiones destructivas en Git.

## Decisión
1. **Git Worktrees Dedicados (`internal/sandbox`):** Cada ejecución (`run_id`) aprovisiona un worktree atómico en `/tmp/ov-worktrees/<run_id>` bajo una rama aislada `ov-run/<run_id>`.
2. **Prohibición de `git checkout`:** Las host functions bloquean cambios de rama dentro del sandbox.
3. **Contenedores Rootless:** El worktree se monta en un contenedor Docker/Podman efímero sin privilegios.
4. **Fórmula Determinista de Puertos:** Cada instancia recibe un bloque de 10 puertos calculado como:
   $$P_{\text{servicio}} = 40000 + (\text{Factory\_Index} \times 10) + \text{Offset}_{\text{servicio}}$$

## Consecuencias
* **Positivas:** Cero colisiones entre ejecuciones paralelas, uso eficiente de espacio en disco compartiendo el `.git` base y aislamiento estricto de red.
* **Negativas:** Requiere limpieza activa de worktrees huérfanos y locks de índice (`git worktree prune`).
