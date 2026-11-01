# ADR 0008: CLI Oficial de Desarrollador con Runner de Fábricas, Consultas Semánticas al Cerebro y Demonio Worker

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
La plataforma de fábricas de software autónomas `gv-factory` requiere una herramienta de consola (`cmd/ov-cli`) de grado de producción que permita a los desarrolladores:
1. Ejecutar pipelines de fábrica de forma local y autónoma, o enviarlos a instancias remotas de `ov-server` (`ov-cli run`).
2. Realizar búsquedas semánticas vectoriales sobre el Cerebro de código (`internal/brain`) y consultar símbolos AST, decisiones de arquitectura y episodios anteriores (`ov-cli memory query`).
3. Desplegar un demonio worker local conectado a la cola de tareas para aportar capacidad de cómputo de sandboxes (`ov-cli worker start`).
4. Inspeccionar el estado en tiempo real de nodos de grafos DAG y registros de ejecución (`ov-cli status`).
5. Descubrir y validar plantillas declarativas de fábrica disponibles (`ov-cli templates list`).
6. Proveer una experiencia de desarrollo ergonómica con códigos de salida estandarizados, formato JSON para automatización y badges de estado colorizados.

## Decisión
Extender e implementar la arquitectura modular de `cmd/ov-cli`:

1. **Abstracción de Aplicación y Desacoplamiento de Flujos I/O (`app.go`, `CLIApp`):**
   - Encapsular los flujos `Stdin`, `Stdout`, `Stderr` y el colorizador en la estructura `CLIApp`, permitiendo pruebas unitarias deterministas y ejecución libre de efectos secundarios.
   - Enrutamiento estandarizado de comandos y subcomandos con códigos de salida deterministas: `0` para éxito, `1` para error de ejecución en tiempo de ejecución, y `2` para parámetros inválidos o sintaxis incorrecta.

2. **Runner de Fábricas y Catálogo Canónico Integrado (`cmd_run.go`, `templates_catalog.go`):**
   - Resolución transparente de plantillas: verificación de ruta en sistema de archivos local, búsqueda relativa en `templates/factories/` y catálogo de respaldo con plantillas integradas (`bugfix-express`, `feature-complete`, `refactor-doc`).
   - Validación estricta estructural, de alcanzabilidad y umbrales de compuerta mediante `dag.ValidateTemplate`.
   - Soporte para ejecución local directa sobre el motor DAG y plugins WASM con streaming de pasos paso a paso (`--wait`), o despacho remoto vía HTTP (`--server`).

3. **Consultas Semánticas al Cerebro con Proyecciones Vectoriales (`cmd_memory.go`):**
   - Generación de vectores de consulta semántica de 1536 dimensiones normalizados a longitud unitaria a partir de texto libre.
   - Consulta sobre índices vectoriales HNSW de `codebase_knowledge`, `architectural_rules` y `task_episodes` en PostgreSQL/pgvector.
   - Modo degradado / desconectado con catálogo de símbolos AST para entornos de desarrollo sin base de datos activa.
   - Salida enriquecida en tablas de texto formateadas o JSON estricto (`--json`).

4. **Demonio Worker Persistente y Apagado Agraciado (`cmd_worker.go`):**
   - Inicio del bucle de ejecución de sandboxes y tareas con concurrencia configurable (`--concurrency`), etiquetas de capacidades (`--tags`) y directorio base de worktrees.
   - Captura de señales del sistema operativo (`SIGINT`, `SIGTERM`) mediante `signal.NotifyContext` para liberación segura de recursos antes de terminar el proceso.

5. **Inspección de Estado y Plantillas (`cmd_status.go`, `cmd_templates.go`):**
   - Inspección de ejecuciones en curso o finalizadas con desglose temporal por paso del grafo DAG.
   - Listado tabular y JSON de plantillas con versiones, puntos de entrada y recuento de pasos.

## Consecuencias
* **Positivas:**
  - Los desarrolladores pueden ejecutar y depurar pipelines completos en local sin depender de la UI web ni de infraestructura en la nube.
  - Capacidades de introspección semántica inmediata sobre decisiones de arquitectura y componentes del código.
  - Integración nativa en scripts y herramientas de CI/CD gracias a la bandera universal `--json` y códigos de error consistentes.
* **Negativas:**
  - El soporte para streaming en vivo en Windows y terminales básicas depende de la compatibilidad con secuencias de escape ANSI (gestionado mediante `Colorizer` y respeto a `NO_COLOR`).
