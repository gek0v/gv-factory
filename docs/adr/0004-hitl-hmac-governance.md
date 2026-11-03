<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# ADR 0004: Gobernanza Human-in-the-Loop con tokens HMAC criptográficos para compuertas de decisión

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
En un entorno de fábricas de software donde agentes autónomos de IA generan código, ejecutan refactorizaciones y proponen cambios a repositorios de producción, existen operaciones de alto impacto que no deben aplicarse sin supervisión y autorización humana explícita (*Human-in-the-Loop* o HITL):
1. Modificaciones en esquemas relacionales de bases de datos (`migrations/`).
2. Alteraciones en la lógica de autenticación, autorización o criptografía (`internal/auth`).
3. Disminución de la cobertura de pruebas unitarias por debajo del umbral mínimo mandatorio ($< 90\%$).
4. Fusión de código hacia la rama de producción `main` cuando los revisores automáticos de seguridad detectan alertas no categorizadas.

Cuando el pipeline alcanza una compuerta de decisión (*Decision Gate*), el orquestador debe pausar la ejecución de manera segura y notificar a los operadores humanos a través de múltiples canales externos: interfaces web (Web SPA), adaptadores de mensajería (Slack Block Kit, Discord Components) o herramientas de línea de comandos (`ov-cli`).

Esta interacción expone vectores críticos de vulnerabilidad:
- **Ataques de falsificación y suplantación:** Un usuario no autorizado en un canal de chat compartido podría intentar aprobar una compuerta enviando un comando o haciendo clic en una acción interactiva.
- **Ataques de repetición (Replay Attacks):** Reutilización de solicitudes de aprobación pasadas para autorizar cambios posteriores no relacionados.
- **Manipulación de parámetros:** Alteración del identificador de ejecución (`run_id`), identificador de compuerta (`gate_id`) o decisión tomada (`approve` vs `reject`).

## Decisión
Implementar un protocolo criptográfico robusto de gobernanza Human-in-the-Loop en `internal/auth` y `internal/dag`:

1. **Tokens Opacos Firmados Criptográficamente (HMAC-SHA256):**
   - Cuando se requiere una decisión humana, el sistema genera un token opaco efímero estructurado:
     $$\text{Payload} = \text{run\_id} \mathbin{\Vert} \text{gate\_id} \mathbin{\Vert} \text{action} \mathbin{\Vert} \text{timestamp}$$
     $$\text{Signature} = \text{HMAC-SHA256}(K_{\text{secret}}, \text{Payload})$$
     $$\text{ApprovalToken} = \text{base64url}(\text{Payload} \mathbin{\Vert} \text{Signature})$$
   - El token viaja incrustado de manera transparente en los botones de acción de Slack/Discord (`custom_id`), en los enlaces de la Web SPA y en los comandos emitidos por el CLI (`ov-cli gate resolve`).
2. **Ventana Temporal de Expiración Determinista (TTL):**
   - Cada token cuenta con una marca de tiempo inmutable y un periodo de validez configurable (por defecto 15 minutos).
   - Cualquier solicitud recibida tras la expiración del TTL es rechazada automáticamente, forzando la reemisión de la compuerta si procede.
3. **Verificación en Tiempo Constante:**
   - La comprobación de la firma criptográfica se ejecuta obligatoriamente utilizando `crypto/subtle.ConstantTimeCompare`, neutralizando cualquier vector de ataque por análisis de temporización de canal lateral (*timing attacks*).
4. **Registro Inmutable y Auditoría Forense (`internal/store`):**
   - Cada resolución de compuerta (Aprobada, Rechazada, Expirada) se registra permanentemente en la tabla `gate_audit_records`.
   - Se almacena el hash SHA-256 del token, la identidad del decisor, dirección IP, User-Agent, comentario justificativo y marca de tiempo exacta para cumplimiento normativo y trazabilidad forense.
5. **Reglas Deterministas de Disparo de Compuerta:**
   - El evaluador de compuertas (`internal/dag/gate.go`) detiene el flujo y exige resolución HMAC si se cumple cualquiera de las siguientes condiciones:
     - Cobertura de tests unitarios inferior al 90%.
     - Alertas de linters o escáneres de seguridad con severidad `HIGH` o `CRITICAL`.
     - Archivos modificados en rutas de seguridad o migraciones de base de datos.

## Consecuencias
* **Positivas:**
  - Garantía criptográfica absoluta contra manipulación de decisiones y suplantación de aprobaciones en canales de chat y web.
  - Trazabilidad y auditoría inmutable de todas las decisiones humanas tomadas sobre el código generado por IA.
  - Protección de ramas productivas frente a despliegues no autorizados o cambios de alto riesgo sin supervisión.
* **Negativas:**
  - Si un operador humano no atiende la compuerta dentro de la ventana de validez del token (15 min), el pipeline entra en estado de expiración y requiere renotificación manual.
  - Requiere la gestión y custodia segura de la clave secreta `HMAC_SECRET` en el entorno de despliegue del servidor.
