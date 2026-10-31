## Descripción del Cambio

<!-- Resumen conciso de los cambios introducidos y el problema que resuelven. -->

Fixes #(issue)

---

## Tipo de Cambio

- [ ] `feat`: Nueva funcionalidad o módulo
- [ ] `fix`: Corrección de errores
- [ ] `perf`: Optimización de rendimiento
- [ ] `refactor`: Refactorización de código sin cambios funcionales
- [ ] `docs`: Modificación o añadido en la documentación
- [ ] `chore`: Mantenimiento, dependencias o tareas internas
- [ ] `ci`: Cambios en la integración continua o automatizaciones

---

## Lista de Verificación (Checklist)

Por favor, confirma el cumplimiento de los siguientes requisitos antes de solicitar revisión:

- [ ] **Vinculación con el Issue:** Este PR está enlazado a un issue abierto y discutido previamente.
- [ ] **Pruebas Automatizadas:** Se han añadido o actualizado pruebas unitarias/integración que cubren los cambios realizados.
- [ ] **Cumplimiento REUSE / SPDX:** Todos los nuevos ficheros contienen la cabecera correspondiente de licencia y autoría (Apache-2.0).
- [ ] **Impacto en el Cerebro (Migraciones SQL):**
  - [ ] No aplica (no modifica estructuras de base de datos ni esquemas vectoriales).
  - [ ] Aplica: Se incluye migración idempotente bajo `migrations/brain/` y se han validado índices HNSW.
- [ ] **Aislamiento de Sandboxes y Worktrees:** Los cambios en `internal/sandbox` no interfieren con la política de asignación de puertos ni con el ciclo de vida de los worktrees efímeros.
- [ ] **Compatibilidad y Breaking Changes:**
  - [ ] No introduce cambios incompatibles con versiones anteriores.
  - [ ] **Introduce Breaking Changes** (detallados a continuación).

---

## Declaración de Cambios Incompatibles (*Breaking Changes*)

<!-- Si aplica, detalla la justificación técnica y la guía de migración requerida para usuarios y plugins existentes. -->
