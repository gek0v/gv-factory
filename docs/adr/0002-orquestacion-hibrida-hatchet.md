# ADR 0002: Orquestación Híbrida (Grafo Mutable en PostgreSQL + Hatchet)

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
Los flujos de trabajo de desarrollo de software autónomo son dinámicos: un Revisor de Seguridad o un fallo en el ciclo TDD puede mutar el grafo en caliente (añadiendo nodos de corrección o reintentos). Sin embargo, se requiere persistencia duradera, tolerancia a fallos y reanudación tras caídas del servidor.

## Decisión
Separar las responsabilidades de orquestación en dos capas:
1. **Intérprete de Grafos Mutables (`internal/dag`):** Mantiene la definición y estado de los nodos en PostgreSQL, permitiendo mutaciones de grafo en tiempo de ejecución.
2. **Motor de Ejecución Duradera (Hatchet):** Gestiona la cola distribuida de tareas, reintentos exponenciales, timeouts, concurrencia de workers (`cmd/ov-worker`) y pausas asíncronas.

## Consecuencias
* **Positivas:** Capacidad de mutación dinámica de pipelines en caliente combinada con ejecución duradera de grado empresarial.
* **Negativas:** Necesidad de coordinar el estado de sincronización entre PostgreSQL y los eventos de Hatchet.
