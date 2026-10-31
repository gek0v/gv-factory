# ADR 0004: Protocolo de Aprobación Humana (Human-in-the-Loop) y Tokens Opacos

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
Cuando las compuertas de calidad detectan cambios de alto riesgo (modificaciones en esquemas de base de datos, lógica de autenticación o caídas por debajo del 90% de cobertura), el pipeline debe detenerse de forma segura y solicitar validación humana a través de canales externos (Slack, Discord, Dashboard Web).

## Decisión
Implementar un protocolo criptográfico en `internal/auth`:
1. Generación de tokens opacos firmados con HMAC-SHA256 (`<run_id>:<timestamp>:<firma>`).
2. Expiración determinista temporal de las autorizaciones.
3. Validación estricta antes de reanudar el flujo en Hatchet o promover cambios a `main`.

## Consecuencias
* **Positivas:** Seguridad frente a suplantación o reejecución de comandos desde integraciones de chat, auditoría completa e inmutabilidad de decisiones.
* **Negativas:** Si el token expira antes de la respuesta del revisor humano, la tarea debe solicitar una revalidación formal.
