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
* `0001-wasm-extism-plugins.md`: Registra la decisión de aislar todos los nodos funcionales y extensiones en módulos WebAssembly mediante Extism.
* `0002-pgvector-brain-memory.md`: Define el almacenamiento relacional y memoria vectorial episódica unificada en PostgreSQL 16 con pgvector.
* `0003-sequential-merge-queue-merge-tree.md`: Establece la Merge Queue secuencial con validación previa 'git merge-tree' antes de rebase/fast-forward.
* `0004-hitl-hmac-governance.md`: Documenta el esquema de gobernanza Human-in-the-Loop con tokens HMAC criptográficos para compuertas de decisión.
* `0005-multi-provider-dynamic-llm.md`: Define la arquitectura de conectores LLM con detección dinámica, fallback transparente y soporte multi-proveedor.


* **`migrations/brain/`:**
* `000001_init_pgvector.up.sql`: Habilita la extensión `vector` y crea las tres tablas fundacionales (`architectural_rules`, `codebase_knowledge` y `task_episodes`) junto con sus índices HNSW para búsqueda semántica rápida.


* **`templates/factories/`:**
* `default-tdd-factory.yaml`: Plantilla oficial de fábrica basada en tu diseño original (Ingesta $\rightarrow$ Spec Writer $\rightarrow$ Builder TDD $\rightarrow$ 4 Revisores en paralelo con contexto limpio $\rightarrow$ Approver $\rightarrow$ Compuerta Humana $\rightarrow$ Producción).



---

### 3. Arquitectura del Módulo de Aislamiento (`internal/sandbox`) y Cola de Integración

El paquete `internal/sandbox` en Go actúa como el controlador de infraestructura local para que múltiples instancias del *Builder* operen en paralelo sobre el mismo repositorio sin pisarse entre sí ni saturar el disco.

---

#### A. Gestión del Ciclo de Vida de Git Worktrees

Para evitar clonar el repositorio completo por cada fábrica activa, el controlador gestiona árboles de trabajo efímeros vinculados al almacén `.git` principal:

1. **Verificación Pre-vuelo (Exclusión Mutua):**
Antes de crear o reutilizar un entorno, el controlador inspecciona la existencia de archivos `.git/index.lock` o `.git/worktrees/<id>/index.lock` huérfanos provocados por caídas previas y los libera de forma segura.
2. **Aprovisionamiento Atómico:**
Asigna un identificador único a la corrida (`run_id`) y aprovisiona una rama aislada en un directorio temporal dedicado mediante `git worktree add -b ov-run/<run_id> /tmp/ov-worktrees/<run_id> main`.
3. **Bloqueo de Comandos de Contexto:**
Las funciones del anfitrión (*Host Functions*) expuestas al plugin WASM del *Builder* restringen la ejecución de `git checkout` o `git switch` dentro del sandbox para evitar que el agente altere la rama asignada al worktree.
4. **Destrucción y Recolección de Basura:**
Una vez que la tarea finaliza (ya sea fusionada en `main` o enviada a un Pull Request), el módulo desmonta el contenedor y ejecuta `git worktree remove --force` seguido de `git worktree prune` para limpiar los metadatos.

---

#### B. Aislamiento de Runtime, Red y Recursos (Contenedores Rootless)

El directorio del worktree se monta dentro de un contenedor efímero sin privilegios (*rootless*) gobernado a través del SDK de Docker/Podman para Go:

| Recurso | Estrategia de Aislamiento en `internal/sandbox` |
| --- | --- |
| **Sistema de Archivos y Cachés** | El worktree se monta como volumen de trabajo. Las variables `TMPDIR`, `XDG_CACHE_HOME` y las cachés del lenguaje (`GOPATH`, `npm_cache`) se redirigen a volúmenes efímeros propios de la instancia para evitar corrupción de artefactos binarios. |
| **Asignación de Puertos** | El gestor mantiene un registro en memoria de índices activos (`Factory_Index`) y calcula un bloque de 10 puertos por instancia con la fórmula <br>$$P_{\text{servicio}} = 40000 + (\text{Factory\_Index} \times 10) + \text{Offset}_{\text{servicio}}$$

<br>. |
| **Enrutamiento Virtual** | Registra dinámicamente en la API de Caddy/Traefik el subdominio `run-<id>.ov-factory.localhost` apuntando a la IP interna del contenedor, facilitando que Playwright (para los revisores de UI/UX) acceda al servidor sin colisiones de puertos. |
| **Base de Datos de Tests** | Si la suite de pruebas requiere base de datos, el controlador crea un esquema o base de datos efímera (`test_db_<run_id>`) en PostgreSQL e inyecta la cadena de conexión en el contenedor. |
| **Inyección de Secretos** | Descifra en memoria las variables requeridas por el proyecto objetivo y las pasa directamente al entorno del contenedor, aplicando un filtro de ofuscación (`***`) sobre los flujos `stdout`/`stderr` antes de devolver los logs al LLM. |

---

#### C. Cola de Integración Secuencial (*Merge Queue*)

Cuando múltiples fábricas terminan sus tareas de manera asíncrona y el *Approver* autoriza el pase a producción (*Fast-Track a `main*`), no pueden hacer `git push` de forma simultánea. El sub-paquete `internal/sandbox/merger` serializa las entregas mediante una cola FIFO en NATS JetStream:

1. **Simulación en Memoria (Sin tocar el árbol de trabajo):**
El trabajador de integración toma la rama aprobada y ejecuta `git merge-tree --write-tree $(git merge-base main ov-run/<id>) main ov-run/<id>`. Esto comprueba en milisegundos si los cambios chocan con commits que otra fábrica haya integrado segundos antes.
2. **Camino Feliz (Sin Conflictos):**
Si la fusión es limpia, el integrador hace un `git rebase main` dentro del sandbox, ejecuta por última vez los tests unitarios afectados para descartar regresiones semánticas cruzadas y realiza el *fast-forward merge* hacia `main`.
3. **Desvío por Conflicto:**
Si `git merge-tree` detecta colisiones de código o las pruebas post-rebase fallan, se aborta la integración automática. El flujo cambia automáticamente de vía: empuja la rama al repositorio remoto, abre un Pull Request detallando el conflicto detectado y notifica a través del protocolo *Human-in-the-Loop*.

---