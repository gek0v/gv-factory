# ADR 0006: Despachador de Webhooks Salientes con Firmado Criptográfico HMAC-SHA256, Reintentos Exponenciales y Cola de Mensajes Muertos (DLQ)

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
La plataforma de fábricas de software autónomas `gv-factory` requiere notificar hitos de ciclo de vida (ej. `task.completed`, `pr.ready_for_review`, `gate.approval_required`, `factory.failed`) a sistemas externos (pipelines de CI/CD, sistemas de auditoría, dashboards corporativos y plataformas de observabilidad).
Las entregas hacia infraestructura externa están sujetas a condiciones de red adversas, degradación temporal (códigos HTTP 503, 429), fallos permanentes en endpoints de clientes (códigos HTTP 4xx) y riesgos de falsificación de eventos o ataques de repetición.

## Decisión
Implementar el motor de despacho de webhooks salientes en `internal/connectors/outbound`:

1. **Modelo de Suscripciones y Filtrado de Eventos (`Subscription`, `SubscriptionStore`):**
   - Registro de suscripciones con URL de destino, clave secreta (`Secret`), cabeceras personalizadas (`Headers`), categorías de eventos habilitadas (`EventTypes`) y estado activo (`Active`).
   - Filtrado jerárquico y por comodines: soporte de coincidencia exacta (`task.completed`), comodines universales (`*`, `>`, `all`), comodines de categoría (`task.*`, `pr.*`, `gate.*`) y jerarquías (`task.>`).

2. **Sobre Canónico de Carga y Firmado HMAC-SHA256 (`signer.go`, `Payload`):**
   - Envoltorio estándar conteniendo `id`, `event`, `timestamp`, `run_id` y `data`.
   - Cálculo criptográfico de firma HMAC-SHA256 sobre el cuerpo serializado en JSON mediante la clave secreta de la suscripción.
   - Inyección de cabeceras de seguridad y trazabilidad: `X-OV-Signature-256`, `X-OV-Timestamp`, `X-OV-Delivery` y `X-OV-Event`.
   - Verificación en tiempo constante (`subtle.ConstantTimeCompare`) inmune a ataques de temporización de canal lateral.

3. **Pool de Workers Asíncrono y Política de Reintentos con Retroceso Exponencial (`worker.go`, `retry.go`):**
   - Pool configurable de workers consumiendo trabajos de una cola interna con amortiguamiento.
   - Política de reintentos exponenciales con fluctuación (jitter): intervalo inicial, factor de multiplicación, límite máximo y tiempo de espera por petición.
   - Discriminación determinista entre errores transitorios (HTTP 5xx, 429, 408 y errores de red/timeout) frente a errores permanentes de cliente (HTTP 4xx salvo 429/408), abortando inmediatamente reintentos estériles ante errores del cliente.

4. **Cola de Mensajes Muertos (DLQ) y Pista de Auditoría (`deadletter.go`, `audit.go`):**
   - Enrutamiento automático de entregas fallidas permanentemente (tras agotar reintentos o por error permanente) hacia `DeadLetterQueue` con diagnósticos detallados, recuento de intentos y cuerpo de error.
   - Pista de auditoría en memoria delimitada (`DeliveryAuditTrail`) para consultar el historial de entregas recientes por evento o suscripción.
   - Integración nativa con `internal/gateway` vía `AttachToGateway` y mapeo directo de `gateway.Event` y `schema.TriggerEvent`.

## Consecuencias
* **Positivas:**
  - Emisión desacoplada y no bloqueante de eventos del ciclo de vida de la fábrica.
  - Protección de integridad y autenticidad del emisor verificable criptográficamente por los receptores.
  - Resiliencia frente a caídas temporales de receptores sin bloquear los pipelines internos.
  - Aislamiento de fallos y trazabilidad forense completa en caso de fallo definitivo.
* **Negativas:**
  - El buffer en memoria requiere límites de capacidad para prevenir consumo excesivo de memoria ante fallos continuos de endpoints externos.
