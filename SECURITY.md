# Política de Seguridad y Divulgación Responsable

En **OV-Factory** la seguridad es una prioridad crítica, especialmente dado que la plataforma orquesta ejecución de código en sandboxes, gestión de secretos y automatizaciones con acceso a repositorios.

---

## Versiones con Soporte de Seguridad

Brindamos parches de seguridad para las siguientes versiones activas de OV-Factory:

| Versión | Recibe Parches de Seguridad |
| :--- | :--- |
| `0.x.x` (Desarrollo activo en `main`) | :white_check_mark: Sí |
| Versiones archivadas / descontinuadas | :x: No |

---

## Reporte de Vulnerabilidades

**Por favor, no abras issues públicos para reportar fallos de seguridad.**

Para reportar una vulnerabilidad de forma confidencial y privada:

1. **Avisos de Seguridad de GitHub (Método Preferido):**
   Ve a la pestaña [Security Advisories](https://github.com/ov-factory/ov-factory/security/advisories/new) y selecciona **"Report a vulnerability"**.

2. **Correo Electrónico Cifrado:**
   Si no dispones de cuenta en GitHub, envía un correo a:
   - **Email:** `security@ov-factory.org`
   - Si es necesario cifrar la comunicación, solicita nuestra clave PGP en el primer contacto.

### Información Requerida en el Reporte:
- Descripción clara del fallo de seguridad (ej. escape de sandbox en `internal/sandbox`, inyección SQL en `internal/brain`, falsificación de token en `internal/auth`).
- Pasos detallados para reproducir el problema o prueba de concepto (PoC).
- Impacto potencial estimado y escenarios de explotación.

---

## Compromiso de Respuesta (SLA)

- **Confirmación inicial:** Responderemos a tu reporte en un plazo máximo de **48 horas laborables**.
- **Evaluación y triage:** Determinaremos la severidad y alcance en un plazo de **5 días hábiles**.
- **Parche y divulgación:** Emitiremos la corrección de seguridad y la publicaremos junto con el CVE correspondiente según la política de divulgación coordinada (normalmente dentro de los 90 días posteriores al reporte o antes si hay riesgo inminente).
