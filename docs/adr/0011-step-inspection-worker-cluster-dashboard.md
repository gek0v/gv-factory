# 11. Inspección Granular de Steps y Dashboard de Clúster de Workers en Canvas Web

- **Estado:** Aceptado
- **Fecha:** 2026-11-01
- **Decisores:** Equipo Core gv-factory, Agente Spec-Writer
- **Consulta:** Issue #92 (`feat(web): add granular step execution inspection and real-time worker cluster dashboard`)

---

## Contexto y Planteamiento del Problema

La plataforma `gv-factory` coordina la ejecución de fábricas de software mediante grafos DAG distribuidos. Aunque el backend en Go registra cada paso en `store.Store` y despacha tareas hacia daemon workers (`internal/worker`), el frontend carecía de:
1. Una vista granular dentro del drawer de inspección de nodos (`NodeInspector`) para auditar la traza paso a paso (estado, duración, errores específicos y datos de salida estructurados).
2. Un panel en tiempo real para visualizar el estado operativo del clúster de workers, su capacidad de concurrencia total y la recepción de heartbeats sin recurrir a herramientas CLI o consultas directas a la base de datos.

## Opciones Consideradas

1. **Inspección exclusiva por logs en texto plano:** Depender exclusivamente de la pestaña "Logs" existente para inferir el progreso de los pasos y el estado de los workers.
   - *Desventaja:* Difícil diagnóstico cuando fallan pasos específicos en paralelo; sin visibilidad del estado de los workers.
2. **Tableros externos (Grafana / Prometheus):** Redirigir al operador a dashboards externos de Grafana.
   - *Desventaja:* Rompe la experiencia unificada del canvas interactivo y añade fricción operativa para desarrolladores.
3. **Inspección Granular Nativa + Dashboard Modal en Canvas (Opción Elegida):**
   - Incorporar una pestaña dedicada "Steps" en `NodeInspector` conectada a `GET /api/v1/runs/{id}/steps`.
   - Incorporar un pill de salud del clúster en el header conectado a `GET /api/v1/workers` y reactivo ante eventos SSE `ov.worker.heartbeat`.

## Decisión de Diseño

Se aprueba la implementación nativa en `web/` con los siguientes pilares:
1. **Contratos Tipados Fuertes:** Interfaces TypeScript unificadas en `web/src/types/worker.ts` (`RunStep`, `WorkerInfo`, `WorkerPoolResponse`).
2. **Clientes API con Manejo Resiliente de Errores:** `web/src/api/steps.ts` y `web/src/api/workers.ts` con tipado estricto y manejo defensivo de códigos de error HTTP 404, 500 y cortes de red.
3. **Sincronización Híbrida (REST + SSE):** El panel de workers soporta auto-polling periódico (cada 5s) complementado con actualización reactiva inmediata ante eventos SSE `ov.worker.heartbeat` canalizados por el despachador `web/src/api/dispatcher.ts`.
4. **Visor de Salidas Estructuradas:** Formateo legible de duraciones (`formatDuration`) y visores colapsables para payloads JSON complejos en cada step.

## Consecuencias

### Positivas
- Trazabilidad y depuración inmediata de ejecuciones DAG complejas sin abandonar el canvas web.
- Control en tiempo real de la utilización y saturación de los daemon workers.
- Cobertura de pruebas unitarias $\ge 90\%$ bajo Vitest.

### Negativas / Mitigaciones
- *Sobrecarga de red por polling:* Mitigada limitando el auto-polling únicamente mientras el modal de workers esté visible, pausándolo al cerrarse y apoyándose en eventos SSE.
