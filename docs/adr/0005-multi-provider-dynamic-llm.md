<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# ADR 0005: Arquitectura de conectores LLM con detección dinámica, fallback transparente y soporte multi-proveedor

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
Las fábricas de software autónomas multi-agente en `gv-factory` dependen intensivamente de Modelos de Lenguaje de Gran Escala (LLM) para tareas cognitivas clave: interpretación de requerimientos, especificación técnica de contratos, síntesis de código TDD, análisis diferencial de parches y revisión de seguridad.

La dependencia exclusiva de un único proveedor comercial (por ejemplo, OpenAI o Anthropic) introduce graves riesgos operativos y limitaciones arquitectónicas:
1. **Puntos únicos de fallo y falta de disponibilidad:** Caídas de servicio, mantenimiento programado o degradación de latencia del proveedor bloquean por completo la operación de las fábricas.
2. **Límites de tasa de peticiones (Rate Limiting y HTTP 429):** Picos de actividad de agentes concurrentes superan con facilidad los límites de cuota por minuto (TPM/RPM), interrumpiendo pipelines en curso.
3. **Restricciones de soberanía de datos y entornos aislados (Air-Gapped):** Ciertas organizaciones y entornos de desarrollo exigen que el código fuente no salga de la infraestructura local o privada, requiriendo ejecución mediante modelos abiertos auto-hospedados (Ollama, vLLM).
4. **Optimización de costes y balance de razonamiento:** Asignar todas las tareas a modelos frontera de alto coste (Claude 3.5 Sonnet, GPT-4o, o1) para operaciones sencillas como chunking o generación de resúmenes resulta financieramente ineficiente. Se requiere dirigir tareas masivas a modelos locales rápidos (Qwen2.5-Coder, DeepSeek-R1) y reservar los modelos frontera para razonamiento complejo y auditorías de seguridad.

## Decisión
Implementar un subsistema modular de conectores LLM políglota y resiliente en `internal/llm` (y conectores asociados):

1. **Interfaz Común de Conector (`LLMConnector`):**
   - Definir un contrato unificado e independiente del proveedor:
     ```go
     type LLMConnector interface {
         Generate(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
         Stream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)
         CountTokens(prompt string) (int, error)
         HealthCheck(ctx context.Context) error
         ProviderName() string
     }
     ```
2. **Soporte Oficial Multi-Proveedor:**
   - **OpenAI:** Conector para modelos GPT-4o, o1, o3-mini mediante endpoints REST estandarizados con soporte de streaming SSE y llamadas a herramientas (*tool calling*).
   - **Anthropic:** Conector para Claude 3.5 Sonnet y Claude 3.5 Haiku utilizando el protocolo Messages API y bloques de pensamiento/razonamiento estructurado.
   - **Ollama:** Conector HTTP hacia instancias locales (`http://localhost:11434`) para ejecución offline y privada de modelos de código abiertos (`qwen2.5-coder`, `llama3.1`, `codellama`, `deepseek-r1`).
   - **vLLM:** Conector compatible con la API de OpenAI optimizado para inferencia local de alto rendimiento con procesamiento por lotes continuo (*PagedAttention / Continuous Batching*).
3. **Detección Dinámica de Salud y Registro de Modelos:**
   - Monitoreo periódico de disponibilidad mediante sondeos ligeros (`/health`, `/api/tags`, `/v1/models`).
   - Descubrimiento dinámico de modelos locales disponibles en runtime sin requerir reinicio del servidor.
4. **Cadena de Respaldo Transparente (Fallback Chain):**
   - Configuración declarativa de cadenas de respaldo (ej. `OpenAI GPT-4o -> Anthropic Claude 3.5 Sonnet -> Ollama Qwen2.5-Coder`).
   - En caso de errores no recuperables de red, timeouts o códigos de respuesta HTTP 429 (Rate Limit) y 5xx (Server Error), el enrutador conmuta de forma transparente al siguiente conector disponible, preservando los mensajes y contexto de la conversación.
5. **Guardarraíles de Tokens y Telemetría Financiera:**
   - Contabilidad en tiempo real de tokens de entrada (*prompt*), tokens de salida (*completion*) y tokens de caché (*cache hit*).
   - Aplicación de topes presupuestarios por ejecución de fábrica con emisión de eventos de telemetría OpenTelemetry / Prometheus para análisis de costes.

## Consecuencias
* **Positivas:**
  - Alta resiliencia operativa: tolerancia a caídas de proveedores mediante conmutación automática por fallback.
  - Cero atadura a proveedores (*no vendor lock-in*): capacidad de intercambiar o alternar modelos según disponibilidad o preferencia del cliente.
  - Soporte total para despliegues locales y desconectados con Ollama y vLLM sin enviar código a la nube.
  - Eficiencia económica mediante asignación dinámica del modelo idóneo según la complejidad cognitiva del paso.
* **Negativas:**
  - Diferencias sutiles en las respuestas y en el formateo de salida (por ejemplo, llamadas a funciones vs bloques markdown JSON) entre familias de modelos, requiriendo filtros de normalización en el SDK.
  - Mayor complejidad de configuración y pruebas de integración para validar el comportamiento homogéneo en todos los proveedores soportados.
