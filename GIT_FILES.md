### 1. Archivos Raíz de Gobernanza, Licenciamiento y Normalización

Estos archivos establecen la identidad legal del proyecto, el cumplimiento de la especificación **REUSE (FSFE)** y la estandarización del entorno antes de escribir la primera línea de código:

| Ruta del Archivo | Propósito y Contenido Conceptual |
| --- | --- |
| `LICENSE` | Texto íntegro de la licencia principal (**Apache-2.0**), otorgando permisos amplios de uso y cláusula de protección de patentes. |
| `LICENSES/Apache-2.0.txt`<br>

<br>`LICENSES/MIT.txt` | Textos de licencias exigidos por el estándar REUSE para que las herramientas automáticas validen las cabeceras SPDX de todos los ficheros del monorepo. |
| `.reuse/dep5` | Archivo declarativo para asignar derechos de autor y licencia en bloque a archivos que no admiten comentarios en cabecera (imágenes, archivos `.json`, esquemas o binarios de prueba). |
| `README.md` | Carta de presentación: diagrama de arquitectura de **OV-Factory**, insignias (*badges*) de estado de CI y OpenSSF Scorecard, requisitos previos y guía de despliegue en 3 pasos. |
| `CONTRIBUTING.md` | Reglas de contribución: flujo basado en *Trunk-Based Development*, gramática obligatoria de **Conventional Commits**, uso del `Makefile` y guía para crear nuevos plugins WASM. |
| `CODE_OF_CONDUCT.md` | Código de conducta comunitario basado en el estándar *Contributor Covenant v2.1* con canales de contacto para moderación. |
| `SECURITY.md` | Política de divulgación responsable: especifica qué versiones reciben soporte de seguridad, el compromiso de tiempo de respuesta y el enlace para reportar vulnerabilidades en privado. |
| `CODEOWNERS` | Mapa de propiedad del repositorio. Exige aprobación obligatoria de los mantenedores principales cuando un PR modifica rutas críticas como `/internal/sandbox/`, `/internal/auth/` o `/.github/`. |
| `CHANGELOG.md` | Registro histórico de versiones gestionado de forma desatendida por *Release-Please* siguiendo el formato *Keep a Changelog*. |
| `.gitignore` | Reglas de exclusión: binarios de Go (`bin/`, `dist/`), artefactos WASM compilados (`*.wasm`), `node_modules/`, volúmenes locales de contenedores (`.data/`), árboles temporales (`.worktrees/`) y archivos `.env`. |
| `.gitattributes` | Normalización de saltos de línea a `LF` en todo el repositorio, marcado de archivos `.wasm` y recursos gráficos como binarios, y reglas de fusión para `go.sum` y `pnpm-lock.yaml`. |
| `.editorconfig` | Unificación del formato entre editores: tabulaciones para archivos `.go` y `Makefile`, y 2 espacios para `.ts`, `.tsx`, `.yaml` y `.json`. |
| `.pre-commit-config.yaml` | Ganchos locales ejecutados antes de cada commit: detección preventiva de secretos (*gitleaks*), validación del mensaje de commit (*commitlint*) y verificación de cabeceras SPDX. |

---

### 2. Automatización, CI/CD y Salud Comunitaria (`.github/`)

Configuración del repositorio para cumplir con las directivas de seguridad de la **OpenSSF** (permisos de mínimo privilegio, acciones fijadas por hash SHA y escaneo continuo):

* **`.github/dependabot.yml`:** Configuración de revisión semanal separada en tres ecosistemas: `gomod` (backend en Go), `npm` (frontend en `/web`) y `github-actions` (actualización de los propios flujos de trabajo).
* **`.github/PULL_REQUEST_TEMPLATE.md`:** Plantilla con lista de verificación para el autor: vinculación con el Issue original, confirmación de pruebas unitarias añadidas, impacto en la memoria del Cerebro (migraciones SQL) y declaración de cambios incompatibles (*breaking changes*).
* **`.github/ISSUE_TEMPLATE/`:**
* `config.yml`: Desactiva issues en blanco y redirige dudas generales a *GitHub Discussions*.
* `bug_report.yml`: Formulario estructurado que solicita versión del binario, plantilla de fábrica utilizada, logs del sandbox y pasos de reproducción.
* `feature_request.yml`: Formulario para proponer mejoras en el motor de grafos o en la interfaz visual.
* `plugin_proposal.yml`: Plantilla específica para proponer nuevos nodos oficiales (Revisores, Builders o conectores de entrada).


* **`.github/workflows/`:**
* `ci-verify.yml`: Se dispara en cada PR. Ejecuta linters (`golangci-lint`, `eslint`), comprueba el cumplimiento REUSE y corre la suite de pruebas en Go y TypeScript con permisos `contents: read`.
* `security-audit.yml`: Ejecuta análisis estático **CodeQL** para Go y TypeScript, escaneo de vulnerabilidades en dependencias (`govulncheck`) y evaluación periódica de **OpenSSF Scorecard**.
* `release-please.yml`: Analiza los commits en `main`, actualiza automáticamente el PR de lanzamiento y, al fusionarse, compila los binarios multiplataforma con **GoReleaser**, construye las imágenes de contenedor y las firma criptográficamente con **Cosign (Sigstore)**.



---

### 3. Documentación Arquitectónica y Semillas del "Cerebro" (`docs/` y `migrations/`)

Dado que el **Cerebro** de OV-Factory lee las decisiones arquitectónicas directamente del repositorio, estos archivos sirven tanto para los desarrolladores humanos como para inicializar la memoria a largo plazo del proyecto:

* **`docs/adr/` (Architecture Decision Records):**
* `0001-microkernel-extism-wasm.md`: Registra la decisión de aislar todos los nodos funcionales en módulos WebAssembly mediante Extism.
* `0002-orquestacion-hibrida-hatchet.md`: Define la separación entre el intérprete de grafos mutables en PostgreSQL y la ejecución duradera en Hatchet.
* `0003-aislamiento-git-worktrees-sandboxes.md`: Establece las reglas de concurrencia, prohibición de `git checkout` directo y fórmula de asignación de puertos.
* `0004-protocolo-aprobacion-humana.md`: Documenta el esquema de tokens opacos y validación criptográfica para las compuertas *Human-in-the-Loop*.


* **`migrations/brain/`:**
* `000001_init_pgvector.up.sql`: Habilita la extensión `vector` y crea las tres tablas fundacionales (`architectural_rules`, `codebase_knowledge` y `task_episodes`) junto con sus índices HNSW para búsqueda semántica rápida.


* **`templates/factories/`:**
* `default-tdd-factory.yaml`: Plantilla oficial de fábrica basada en tu diseño original (Ingesta $\rightarrow$ Spec Writer $\rightarrow$ Builder TDD $\rightarrow$ 4 Revisores en paralelo con contexto limpio $\rightarrow$ Approver $\rightarrow$ Compuerta Humana $\rightarrow$ Producción).



---