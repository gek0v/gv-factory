# Guía de Contribución a OV-Factory

¡Gracias por tu interés en contribuir a OV-Factory! Como proyecto de código abierto para fábricas de software autónomas, exigimos altos estándares de calidad, seguridad y reproducibilidad.

---

## 1. Flujo de Trabajo (Trunk-Based Development)

Operamos bajo el modelo **Trunk-Based Development**:

1. **Ramas de Vida Corta:** Crea ramas descriptivas derivadas de `main` siguiendo la convención:
   - `feat/nombre-funcionalidad`
   - `fix/descripcion-bug`
   - `docs/tema-documentacion`
2. **Pull Requests Pequeños:** Los cambios deben ser acotados y enfocados en un único objetivo para agilizar la revisión y minimizar conflictos en la cola de integración.
3. **Validación Previa:** Antes de enviar un PR, asegúrate de que todos los tests pasen y los linters estén limpios localmente.

---

## 2. Convención Obligatoria de Commits (Conventional Commits)

Todos los mensajes de commit deben adherirse a la especificación [Conventional Commits v1.0.0](https://www.conventionalcommits.org/):

```text
<tipo>(<alcance opcional>): <descripción concisa e imperativa>

[cuerpo explicativo opcional]

[pie con referencias a issues o breaking changes]
```

### Tipos Permitidos:
- `feat`: Nuevas características o módulos (eleva versión MINOR).
- `fix`: Corrección de errores en código de producción (eleva versión PATCH).
- `docs`: Cambios exclusivos en la documentación.
- `perf`: Mejoras de rendimiento sin cambios funcionales.
- `refactor`: Cambios de código que ni corrigen bugs ni agregan funcionalidades.
- `test`: Adición o corrección de pruebas unitarias o de integración.
- `ci`: Modificaciones en flujos de GitHub Actions o scripts de integración.
- `chore`: Tareas rutinarias de dependencias, formateo o mantenimiento.

### Breaking Changes:
Indica cambios incompatibles añadiendo un signo de exclamación `!` después del tipo o mediante `BREAKING CHANGE:` en el pie del mensaje:
```text
feat(dag)!: change state mutation signature for dynamic nodes
```

---

## 3. Uso del `Makefile`

Disponemos de comandos estandarizados para el ciclo de vida del proyecto:

```bash
make help       # Muestra la lista de comandos disponibles
make dev        # Inicia dependencias locales con docker-compose y modo hot-reload
make build      # Compila los binarios principales (ov-server, ov-worker, ov-cli)
make test       # Ejecuta la suite de pruebas unitarias con detección de condiciones de carrera (-race)
make lint       # Ejecuta golangci-lint en Go y eslint en frontend
make reuse      # Valida el cumplimiento de licencias SPDX con el linter de REUSE
make clean      # Limpia artefactos binarios temporales y cachés
```

---

## 4. Guía para Crear Nuevos Plugins WebAssembly (WASM)

Todos los nodos especializados (Revisores, Builders, Conectores) se ejecutan de manera aislada como módulos WASM mediante **Extism**:

1. **Selecciona un PDK oficial:** Puedes implementar plugins en Rust, Go (TinyGo) o TypeScript usando los Kits de Desarrollo de Plugins de Extism.
2. **Consulta las plantillas:** Encontrarás esqueletos de partida en el directorio `plugins-sdk/`.
3. **Define el contrato:** Cada plugin debe consumir y producir cargas útiles serializadas en JSON de acuerdo a los esquemas en `pkg/schema/`.
4. **Respeta las Host Functions:** Los plugins no tienen acceso arbitrario a la red ni al disco. Toda operación externa debe realizarse a través de las funciones de anfitrión registradas por el motor de OV-Factory.
5. **Compilación y Entrega:** Compila tu plugin a un archivo `.wasm` autónomo y acompáñalo de un manifiesto descriptivo con sus requerimientos de memoria y permisos.

---

## 5. Licenciamiento y Cumplimiento REUSE

Cada archivo creado en el repositorio debe incluir la cabecera correspondiente de identificación de licencia SPDX:

```text
// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 OV-Factory Contributors
```

Para archivos sin soporte para comentarios, declara la atribución en `.reuse/dep5`.
