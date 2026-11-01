# ADR 0007: Adaptadores Interactivos de Mensajería para Slack y Discord (Compuertas Human-in-the-Loop y Slash Commands)

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
Durante el ciclo de vida de una fábrica de software autónoma, los flujos de trabajo pueden alcanzar compuertas de decisión humanas (`gate.approval_required`) ante cambios de alto riesgo o métricas de calidad por debajo de umbrales. Además, los desarrolladores requieren disparar fábricas y consultar su estado en tiempo real mediante comandos de texto directamente desde sus herramientas de chat (Slack y Discord), sin depender exclusivamente de una interfaz web.

Las plataformas de mensajería exigen:
1. Formatos enriquecidos e interactivos (Block Kit en Slack, Embeds y Components en Discord) con botones de acción (`Approve`, `Reject`, `View Diff`).
2. Validación criptográfica rigurosa de las peticiones entrantes: firmas HMAC-SHA256 con control de tiempo de deriva para Slack (`X-Slack-Signature`), y firmas asimétricas Ed25519 para Discord (`X-Signature-Ed25519`).
3. Tiempos de respuesta estrictos (acuse de recibo en menos de 3 segundos) con ejecución asíncrona desacoplada de la respuesta inicial.
4. Organización de actualizaciones de progreso paso a paso mediante hilos dedicados para minimizar el ruido en canales compartidos.

## Decisión
Implementar el subsistema de mensajería interactiva en `internal/connectors/chat`:

1. **Interfaz Abstracta `ChatNotifier` y Modelo de Dominio (`types.go`):**
   - Definir un contrato uniforme (`SendNotification`, `PromptApproval`, `UpdateStatus`, `Platform`) que aísla al orquestador de las APIs específicas de cada proveedor.
   - Modelar `ApprovalPromptRequest`, `StatusUpdate`, `NotificationMessage` y `GateResolution`.

2. **Tarjetas Interactivas de Decisión y Componentes de Botón (`slack.go`, `discord.go`):**
   - **Slack Block Kit:** Cabeceras con estado, bloques de sección con resumen diagnóstico, cobertura, alertas de seguridad, hashes de commit, y bloque de acciones con botones primarios/peligro (`gate_approve`, `gate_reject`) y botón enlace al diff.
   - **Discord Embeds & Components:** Embed con banner de color según riesgo, campos enriquecidos y fila de acciones con botones (`custom_id: gate_approve:<token>`, `gate_reject:<token>`).
   - Actualización inmediata del mensaje original tras la resolución humana, reemplazando botones interactivos por un cartel definitivo firmado con el usuario decisor.

3. **Verificación Criptográfica Inmune a Ataques de Temporización (`slack_verify.go`, `discord_verify.go`):**
   - **Slack:** Validación HMAC-SHA256 con `crypto/subtle.ConstantTimeCompare` sobre el cuerpo y marca temporal, rechazando peticiones con desfase superior a 5 minutos.
   - **Discord:** Validación de firma Ed25519 mediante clave pública de 32 bytes y par de cabeceras (`X-Signature-Ed25519` y `X-Signature-Timestamp`).

4. **Parser y Despachador de Slash Commands (`commands.go`, `handler.go`):**
   - Soporte para comandos naturales (`/factory run <template> [--issue <n>] [--branch <b>]`, `/factory status <run-id>`).
   - Respuestas efímeras inmediatas (<3s) cumpliendo las exigencias de Slack y Discord, derivando la ejecución intensiva a goroutines asíncronas con reporte posterior vía URLs de respuesta / interacción.

5. **Gestor Centralizado de Notificadores y Puente con Gateway (`manager.go`):**
   - `Manager` unificado para registro múltiple de proveedores, retransmisión masiva (`Broadcast`) o entrega dirigida.
   - Suscripción reactiva al bus de eventos `internal/gateway` sobre `TopicGateApprovalRequired` para emitir tarjetas interactivas de aprobación de forma automática.

## Consecuencias
* **Positivas:**
  - Experiencia de usuario ágil y colaborativa para aprobación y monitoreo desde entornos de chat de equipo.
  - Seguridad reforzada frente a suplantación y repetición gracias al firmado criptográfico constante en tiempo y tokens opacos de aprobación.
  - Desacoplamiento total entre el núcleo del orquestador y los protocolos específicos de mensajería.
* **Negativas:**
  - Se requiere la configuración de tokens de bot y secretos/claves públicas para cada canal de mensajería habilitado.
