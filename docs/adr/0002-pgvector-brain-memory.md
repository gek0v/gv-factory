<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# ADR 0002: PostgreSQL 16 con extensión pgvector para almacenamiento relacional y memoria vectorial episódica

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
El subsistema de memoria a largo plazo ("El Cerebro", `internal/brain`) de `gv-factory` requiere almacenar, indexar y recuperar eficientemente tres categorías fundamentales de conocimiento:
1. **Reglas arquitectónicas normativas (ADRs):** Decisiones de diseño con severidad (`ERROR`, `WARN`, `INFO`), estado y reglas de gobernanza que los agentes de revisión y construcción deben respetar obligatoriamente.
2. **Conocimiento estructural del código fuente:** Fragmentos sintácticos (AST) generados por Tree-sitter (estructuras, interfaces, funciones, firmas), asociados con rutas de archivo y hashes criptográficos de contenido (XXH64).
3. **Memoria episódica de tareas pasadas:** Historial de ejecuciones (`task_episodes`), entradas diagnósticas, salidas generadas, fallos de compilación resueltos y patrones de refactorización exitosos para retroalimentar el aprendizaje contextual del pipeline.

El uso de bases de datos vectoriales dedicadas externas (tales como Pinecone, Weaviate o Qdrant) en conjunto con una base de datos relacional convencional introduce problemas severos:
- Doble escritura y pérdida de consistencia transaccional (anomalías ACID entre metadatos y vectores).
- Complejidad operacional añadida (múltiples servicios de almacenamiento en clúster, replicación desincronizada, copias de seguridad fragmentadas).
- Imposibilidad de ejecutar consultas mixtas híbridas (por ejemplo, filtrar por severidad `ERROR` y estado `Accepted` mientras se calcula la distancia coseno de similitud semántica).

## Decisión
Estandarizar **PostgreSQL 16** con la extensión oficial **`pgvector`** (`migrations/brain/000001_init_pgvector.up.sql`) como repositorio persistente unificado para datos relacionales y vectores semánticos:

1. **Esquema Unificado de Memoria Cognitiva:**
   - Tabla `architectural_rules`: Almacena el código de regla (`rule_code`), título, descripción normalizada, estado (`Accepted`, `Proposed`, etc.), severidad y un vector `embedding vector(1536)` (o `vector(768)`) para consultas de compatibilidad de arquitectura.
   - Tabla `codebase_knowledge`: Almacena nodos AST parseados con Tree-sitter, lenguaje, ruta de archivo, rango de líneas, tipo de símbolo y vector de incrustación, con índice único sobre hash XXH64 para deduplicación incremental.
   - Tabla `task_episodes`: Almacena episodios históricos de ejecución con vectores de similitud de error y solución para recuperación RAG contextual durante el ciclo TDD.
2. **Indexación de Alto Rendimiento con HNSW:**
   - Emplear índices **HNSW** (*Hierarchical Navigable Small World*) con el operador de distancia coseno `vector_cosine_ops`:
     ```sql
     CREATE INDEX IF NOT EXISTS idx_architectural_rules_embedding_hnsw 
     ON architectural_rules USING hnsw (embedding vector_cosine_ops) 
     WITH (m = 16, ef_construction = 64);
     ```
   - Esta configuración garantiza una tasa de recuperación (*recall*) superior al 99% con latencias de consulta inferiores a 5 ms para decenas de miles de fragmentos y reglas.
3. **Búsqueda Híbrida Integrada (FTS + Coseno):**
   - Implementar en `internal/brain/hybrid_search.go` consultas combinadas que ponderan relevancia de búsqueda léxica en texto completo (`tsvector` / `tsquery`) y proximidad semántica vectorial (`<=>` distancia coseno), permitiendo a los agentes encontrar contexto tanto por nombres exactos de funciones como por conceptos arquitectónicos abstractos.
4. **Respaldo y Operación Atómica:**
   - Respaldos completos y atómicos del cerebro del sistema mediante herramientas estándar de PostgreSQL (`pg_dump` y `pg_restore`), eliminando cualquier riesgo de desincronización de memoria.

## Consecuencias
* **Positivas:**
  - Garantías ACID completas en transacciones que involucran metadatos y embeddings vectoriales.
  - Reducción drástica de la superficie operativa y de infraestructura: una única instancia o clúster PostgreSQL gestiona toda la persistencia.
  - Consultas híbridas nativas eficientes sin sobrecarga de red entre servicios de almacenamiento disjuntos.
  - Compatibilidad total con herramientas de migración declarativa (`golang-migrate`).
* **Negativas:**
  - Requiere que la imagen de contenedor o la instancia de PostgreSQL disponga de la extensión compilada `pgvector` instalada.
  - Exige afinamiento de memoria en PostgreSQL (`maintenance_work_mem`, `shared_buffers`) durante la construcción y mantenimiento de índices HNSW extensos.
