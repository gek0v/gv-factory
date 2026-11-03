<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# ADR 0001: Microkernel Extism WASM para extensiones y Host Functions de seguridad

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
En una plataforma de fábricas de software autónomas multi-agente (`gv-factory`), los distintos nodos de un grafo de ejecución (DAG) desempeñan tareas heterogéneas y extensibles por el usuario: redacción de especificaciones (*Spec Writer*), generación de código guiado por pruebas (*TDD Builder*), análisis estático, revisores semánticos de seguridad, inspectores AST y adaptadores de mensajería.

Ejecutar el código de estos nodos y extensiones como binarios nativos del sistema anfitrión o procesos no confinados plantea riesgos críticos:
1. **Seguridad y escape de sandbox:** Un agente defectuoso o un plugin malicioso podría acceder al sistema de archivos del anfitrión, variables de entorno con secretos de infraestructura o sockets de red no autorizados.
2. **Portabilidad y dependencias:** La necesidad de compilar y distribuir dependencias nativas en múltiples arquitecturas (Linux, macOS, Windows) genera fricción de despliegue y fragilidad.
3. **Sobrecarga de inicialización:** Levantar contenedores Docker o máquinas virtuales completas para tareas atómicas de pipeline introduce latencias de arranque del orden de segundos, incompatibles con la reactividad requerida por la fábrica.
4. **Ecosistema políglota:** Los desarrolladores y creadores de plugins requieren poder implementar extensiones en diversos lenguajes (Go, Rust, TypeScript, C) bajo un contrato de ejecución homogéneo.

## Decisión
Adoptar **Extism** (motor de plugins basado en WebAssembly / Wasmtime) como microkernel estándar para la ejecución de extensiones, agentes y nodos funcionales del pipeline:

1. **Aislamiento de memoria WebAssembly:** Cada plugin se compila a un módulo WebAssembly (`.wasm`) independiente. La memoria lineal de la instancia está estrictamente aislada por diseño, con límites de memoria física configurables (hasta 4 GB por sandbox) y terminación determinista ante agotamiento de recursos o bucles infinitos.
2. **Host Functions seguras con permisos basados en capacidades (`internal/plugins`):**
   - El runtime en Go expone un conjunto explícito y restringido de funciones anfitrión (*Host Functions*) importables por el módulo WASM.
   - Las operaciones de I/O de disco están confinadas exclusivamente a la ruta relativa del Git Worktree efímero asignado a la ejecución actual (`/tmp/ov-worktrees/<run_id>`).
   - Bloqueo estricto de llamadas de bajo nivel al sistema operativo: prohibición de invocaciones arbitrarias a sockets de red, llamadas a subprocesos o modificación de variables de entorno globales.
   - Ofuscación y enmascaramiento automático de secretos en flujos `stdout`/`stderr` antes de transferir datos fuera del sandbox.
3. **Kits de desarrollo de plugins (PDK) políglotas (`plugins-sdk/`):**
   - Soporte oficial para autoría de extensiones en Rust (`extism-pdk`), Go (`go-pdk` con TinyGo), TypeScript/JavaScript y C.
   - Esquemas de serialización binaria estándar mediante JSON y MessagePack sobre buffers de memoria compartida gestionados por Extism.
4. **Tiempos de arranque sub-milisegundo:**
   - Carga estática de módulos WASM precompilados en memoria y reutilización de contextos Wasmtime (*instance pooling*), permitiendo inicializaciones y ejecuciones en tiempos menores a 1 ms.

## Consecuencias
* **Positivas:**
  - Aislamiento riguroso de memoria y privilegios mínimos garantizados por la especificación WebAssembly.
  - Tiempos de arranque prácticamente instantáneos frente a virtualización o contenedores tradicionales.
  - Compatibilidad multiplataforma y binarios compilados una sola vez ejecutables en cualquier sistema operativo.
  - Interfaz unificada de extensión para agentes de IA y herramientas de desarrollador.
* **Negativas:**
  - Límite de memoria lineal de 4 GB inherente a la arquitectura WASM de 32 bits (`wasm32-wasi`), suficiente para tareas individuales de agente pero requiriendo streaming para artefactos muy grandes.
  - Sobrecarga de serialización/deserialización en el cruce de fronteras de memoria entre el anfitrión Go y el módulo WASM.
  - Necesidad de que los desarrolladores de plugins compilen sus implementaciones contra los toolchains y PDKs correspondientes.
