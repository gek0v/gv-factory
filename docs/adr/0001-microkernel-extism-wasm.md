# ADR 0001: Microkernel Extism WASM para Nodos y Agentes

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
En una fábrica de software autónoma multi-agente, diferentes nodos ejecutan tareas heterogéneas (Spec Writer, Builder TDD, Revisores de Seguridad, UI/UX, etc.) que requieren ejecución aislada, portabilidad poliglotta y tiempos de arranque prácticamente instantáneos.

## Decisión
Adoptar **Extism** (basado en Wasmtime / WebAssembly) como microkernel para todos los nodos funcionales del pipeline:
1. Todos los agentes y nodos se compilan como módulos `.wasm`.
2. El anfitrión (`internal/plugins`) expone *Host Functions* seguras con permisos granulares.
3. Permite escribir plugins en Go, Rust, TypeScript o C sin dependencias nativas del sistema anfitrión.

## Consecuencias
* **Positivas:** Aislamiento de memoria por diseño, arranque sub-milisegundo, compatibilidad multiplataforma y portabilidad total de plugins.
* **Negativas:** Limitaciones de memoria inicial WebAssembly (Linear Memory de 4GB máximo, suficiente para tareas de agente) y necesidad de bindings PDK.
