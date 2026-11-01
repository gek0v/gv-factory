# ADR 0010: Repositorio de Persistencia PostgreSQL y Migraciones de Esquema para Runs, Steps y Auditoría de Gates

* **Estado:** Aceptado
* **Fecha:** 2026-11-01
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
Originalmente, `cmd/ov-server` mantenía el ciclo de vida de ejecuciones de pipelines (`FactoryRun`), pasos y compuertas de decisión humanas (`PendingGateInfo` y `AuditRecord`) de forma volátil en memoria.
Para entornos empresariales y despliegues en clústeres de alta disponibilidad, se requiere:
1. **Persistencia duradera e inmutable:** Registro permanente del historial de ejecuciones, métricas de pasos, duración y fallos.
2. **Auditoría forense de compuertas Human-in-the-Loop:** Trazabilidad inalterable de decisiones de aprobación/rechazo de compuertas de seguridad con hash criptográfico SHA-256 de tokens HMAC, actor, IP, User-Agent y motivo.
3. **Evolución continua del esquema de base de datos:** Migraciones versionadas y declarativas (`001_initial_schema.sql`) embebidas en el binario sin necesidad de herramientas externas de migración.
4. **Desarrollo local y tests desacoplados:** Resiliencia y fallback automático a `MemoryStore` en caso de ausencia de configuración de `DATABASE_URL`, garantizando pruebas unitarias rápidas y ejecución offline sin base de datos obligatoria.

## Decisión
Implementar el paquete de persistencia unificada `internal/store`:

1. **Interfaces Segregadas (`internal/store/repository.go`):**
   - `RunRepository`: `CreateRun`, `GetRun`, `ListRuns`, `UpdateRunStatus`.
   - `StepRepository`: `RecordStep`, `ListStepsByRun`.
   - `GateAuditRepository`: `RecordPendingGate`, `RecordGateAudit`, `ListPendingGates`, `ResolveGate`.
   - `Store`: Interfaz agregadora con soporte transaccional `WithTx(ctx, func(tx Store) error)` y ciclo de vida `Close()`.

2. **Implementación PostgreSQL con `pgxpool` (`internal/store/postgres.go`):**
   - Gestión eficiente de conexiones mediante `github.com/jackc/pgx/v5/pgxpool`.
   - Parámetros de pool configurables (`MaxOpenConns`, `MinIdleConns`, `ConnMaxLifetime`, `ConnMaxIdleTime`).
   - Almacenamiento semi-estructurado con columnas `JSONB` nativas (`metadata`, `output`, `arguments`).
   - Bloqueo pesimista `SELECT ... FOR UPDATE` en resolución de compuertas para prevenir condiciones de carrera o ataques de repetición (*replay attacks*).
   - Sanitización preventiva de credenciales en logs (`SanitizeDatabaseURL`).

3. **Implementación en Memoria Thread-Safe (`internal/store/memory.go`):**
   - Sincronización concurrente con `sync.RWMutex`.
   - Aislamiento transaccional `WithTx` con semántica de rollback basada en copias instantáneas (*snapshots*) ante fallos en closures.
   - Copias defensivas profundas (*deep copy*) para asegurar ausencia de data races (`-race`).

4. **Sistema de Migraciones Embebidas (`internal/store/migrations.go`):**
   - Inclusión de archivos SQL en tiempo de compilación con `//go:embed migrations/*.sql` y `embed.FS`.
   - Registro idempotente en tabla `schema_migrations`.
   - Ejecución automática de migraciones pendientes durante la inicialización de `PostgresStore`.

5. **Integración en `cmd/ov-server`:**
   - Soporte para flag `-database-url` y variable de entorno `DATABASE_URL`.
   - Fallback transparente a `MemoryStore` en caso de no definir `DATABASE_URL` o ante indisponibilidad temporal.
   - Enlace automático con los endpoints REST `/api/v1/runs`, `/api/v1/runs/{id}`, `/api/v1/gates/pending`, `/api/gate/resolve` y `/api/v1/gates/resolve`.

## Consecuencias
* **Positivas:**
  - Tolerancia a reinicios y escalabilidad horizontal de réplicas de `ov-server`.
  - Auditoría inmutable de aprobaciones humanas lista para cumplimiento normativo (SOC2, ISO 27001).
  - Cero dependencias externas requeridas para desarrollo local o suite de CI rápida gracias al fallback `MemoryStore`.
  - Migraciones atómicas y reproducibles sin herramientas CLI externas.
* **Negativas / Compromisos:**
  - Requiere mantenimiento y versionado estricto de scripts SQL en `internal/store/migrations/`.
  - Operaciones multi-tabla en PostgreSQL deben gobernarse a través de `WithTx` para mantener consistencia referencial.
