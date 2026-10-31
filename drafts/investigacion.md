# Arquitectura y Diseño de Plataforma para OV-Factory: Ingeniería de Fábricas de Software Autónomas y Modulares de Código Abierto

## Arquitectura de Micro-Kernel y Sistema de Extensiones Políglota

La industrialización del desarrollo de software mediante agentes autónomos exige abandonar la concepción de pipelines lineales y monolíticos en favor de una plataforma desacoplada, observable y resiliente. La plataforma OV-Factory se estructura sobre el patrón arquitectónico de micro-kernel, aislando el ciclo de vida de los datos, el motor de ejecución de tareas y el bus de comunicaciones en un núcleo minimalista. Todas las capacidades funcionales especializadas —la ingesta multimodal, el redactor de especificaciones (*Spec Writer*), el generador de código guiado por pruebas (*Builder TDD*), los auditores concurrentes y el agente de aprobación (*Approver*)— se articulan como extensiones modulares e intercambiables.

La elección del modelo de plugins constituye una decisión estructural determinante. Permitir la ejecución arbitraria de extensiones desarrolladas en lenguajes nativos mediante procesos del sistema operativo introduce vulnerabilidades de seguridad, fugas de memoria y fricción de dependencias en entornos multi-tenant. Para garantizar aislamiento estricto sin penalizar el rendimiento, se adopta WebAssembly (WASM) gobernado por el framework de código abierto Extism. Extism proporciona kits de desarrollo de plugins (PDKs) políglotas que permiten a la comunidad programar extensiones en Rust, TypeScript, Go, C o Python, compilándolas hacia un formato binario ejecutable universal.

Bajo el entorno de ejecución Wasmtime integrado en Extism, cada módulo de OV-Factory opera dentro de una memoria lineal protegida con un modelo de seguridad basado en capacidades. El anfitrión define de manera determinista qué funciones del sistema pueden invocar los plugins, impidiendo el acceso indiscriminado al sistema de archivos, a las variables de entorno o a la red. La comunicación entre el núcleo y los módulos se formaliza mediante interfaces binarias tipadas estructuradas en esquemas compartidos, lo que viabiliza un ecosistema abierto de extensiones auditables y empaquetables en registros descentralizados.

La parametrización de los flujos se formaliza a través de un esquema declarativo universal en formato JSON Schema y YAML. Una plantilla de fábrica define la topología del grafo de ejecución, los criterios de aceptación, los límites de consumo de tokens y cómputo, así como los umbrales de riesgo exigidos para la integración continua. Este esquema estandarizado permite exportar factorías completas hacia repositorios Git, facilitando la gobernanza, la reutilización comunitaria y el versionado exhaustivo de la configuración operativa.

## Motor de Orquestación y Canvas Visual Dinámico

La construcción de flujos de trabajo análogos a los de n8n requiere una disociación nítida entre la interfaz gráfica interactiva y el motor distribuido encargado de la persistencia y ejecución de las tareas.

### Canvas Visual Basado en Grafos

Para la interfaz de usuario, se adopta `@xyflow/react` (el núcleo de desarrollo de React Flow) bajo licencia MIT. Esta biblioteca permite orquestar grafos directos en el cliente mediante componentes nativos de React, ofreciendo libertad total para incrustar controles avanzados, monitores de latencia, consolas de streaming de logs y formularios de parametrización dentro de cada nodo. El estado del canvas visual se sincroniza de forma unidireccional utilizando bibliotecas reactivas como Zustand, desacoplando las transformaciones visuales del renderizado de alta frecuencia.

La validación estructural del grafo se realiza localmente en el cliente antes de la sincronización con el servidor: el sistema comprueba la compatibilidad de contratos de datos entre los conectores de salida y de entrada, e implementa algoritmos de ordenamiento topológico para detectar y bloquear ciclos no permitidos en etapas donde se exige un grafo acíclico estricto. Las especificaciones visuales se serializan en un formato JSON limpio, discriminando entre los atributos de presentación gráfica y los metadatos de ejecución.

### Evaluación de Motores de Orquestación y Ejecución Duradera

El requerimiento de ejecutar flujos prolongados, tolerar reinicios del sistema y habilitar la modificación de la topología del flujo en cualquier momento (incluso sobre ejecuciones activas) impone restricciones severas sobre el motor de backend. La siguiente tabla evalúa las principales alternativas de orquestación de código abierto:

| Plataforma | Licencia de Software | Paradigma de Persistencia | Soporte de Mutación Dinámica en Caliente | Huella de Infraestructura |
| --- | --- | --- | --- | --- |
| **Temporal** | MIT | Replay determinista basado en historial de eventos | Incompatible: el cambio del grafo en ejecución corrompe el determinismo | Elevada: clúster de coordinación, esquemas específicos y servicios auxiliares |
| **Hatchet** | MIT | Motor de colas reactivas orientado a eventos sobre PostgreSQL | Moderada: permite bifurcaciones dinámicas, reintentos modificados y sub-flujos | Ligera: binario compilado en Go respaldado por una base de datos relacional |
| **Windmill** | AGPLv3 / Comercial | Ejecución de DAGs de scripts con Rust y PostgreSQL | Alta en tiempo de diseño; restringida en mutaciones de estado en vuelo | Media: runtime eficiente pero condicionado por licencia restrictiva |
| **LangGraph** | MIT | Máquina de estados cíclica con persistencia de checkpoints | Extrema: admite inyección y supresión de tareas sobre grafos vivos | Mínima: biblioteca integrable directamente en el backend de la aplicación |

### Arquitectura de Orquestación Híbrida Recomendada

Temporal constituye el estándar industrial en confiabilidad distribuida, pero impone una regla axiomática: la función de flujo debe ser completamente determinista para poder ser reconstruida a partir del historial de eventos. Si un usuario modifica la estructura de una factoría en ejecución mediante el editor visual (por ejemplo, intercalando un revisor de seguridad adicional mientras el constructor procesa una tarea), el mecanismo de replay de Temporal entra en colisión directa, arrojando excepciones irrecuperables de no-determinismo. Por otro lado, la licencia de Windmill limita su libre distribución o integración como solución de código abierto puro.

La arquitectura adoptada para OV-Factory implementa una solución desacoplada en dos niveles:

- **Nivel Superior de Grafo de Estado Vivo (State Graph Interpreter):** El grafo se gestiona como una máquina de estados almacenada en PostgreSQL, inspirada en los patrones de ejecución dinámica de LangGraph y Hivemind. La estructura del grafo no está codificada rígidamente en un ejecutable, sino que se mantiene como un conjunto de nodos y transiciones mutables. La inyección de nuevas directivas o la alteración de ramas se procesa mediante eventos de mutación: las tareas no iniciadas se recalculan dinámicamente, las dependencias pendientes se ajustan en la tabla de estado y las tareas obsoletas se cancelan sin comprometer el historial de los pasos ya consolidados.
- **Nivel Inferior de Ejecución Duradera y Colas (Hatchet):** Las tareas individuales determinadas por el intérprete del grafo se despachan hacia Hatchet. Hatchet actúa como el motor de despacho distribuido, garantizando control de concurrencia, límites de tasa para llamadas a modelos fundacionales, suspensiones de bajo consumo computacional mientras se aguarda intervención externa y reanudación segura ante caídas de los nodos de cómputo. Su diseño apoyado íntegramente en PostgreSQL y su licencia MIT permiten un despliegue homogéneo y accesible.

## Aislamiento Concurrente de Múltiples Factorías en un Mismo Repositorio

La ejecución en paralelo de múltiples instancias operativas sobre un mismo proyecto de software introduce el desafío de evitar colisiones tanto en el sistema de archivos como en los recursos de ejecución del sistema anfitrión.

### La Brecha Entre Aislamiento de Código y Aislamiento de Runtime

Uno de los errores más frecuentes en la ingeniería de agentes consiste en homologar el aislamiento de archivos con la independencia del entorno de ejecución. Dos instancias pueden operar en carpetas físicas completamente disjuntas y, sin embargo, experimentar fallos catastróficos si ambas intentan vincularse al mismo puerto TCP de desarrollo, compartir volúmenes en segundo plano o ejecutar mutaciones simultáneas sobre el mismo motor de base de datos de pruebas.

| Dimensión de Conflicto | Vector de Fallo Típico | Mecanismo de Mitigación en OV-Factory |
| --- | --- | --- |
| **Sistema de Archivos Git** | Sobrescritura de árboles de trabajo, bloqueos en `.git/index.lock`. | Git Worktrees efímeros independientes sobre el almacén común de objetos. |
| **Gestión de Paquetes** | Colisiones en lockfiles (`package-lock.json`, `poetry.lock`). | Serialización de dependencias y aislamiento estricto de directorios vendor. |
| **Red y Puertos** | Conflictos de enlace en puertos estándar de frameworks (ej. 3000, 8080). | Fórmula matemática de puertos dinámicos y proxy inverso con dominios locales. |
| **Persistencia de Tests** | Contaminación cruzada de esquemas o registros durante migraciones. | Aprovisionamiento de bases de datos efímeras aisladas por identificador de factoría. |
| **Caché de Compilación** | Corrupción de artefactos binarios o carpetas `.cache` compartidas. | Redirección de variables `TMPDIR`, `XDG_CACHE_HOME` hacia directorios locales. |

### Gestión de Código Mediante Git Worktrees Efímeros

La duplicación exhaustiva del repositorio mediante clones independientes resulta prohibitiva en entornos de alta concurrencia: clonar un repositorio de varios gigabytes para cada tarea satura rápidamente el almacenamiento secundario y degrada los tiempos de respuesta del sistema.

OV-Factory utiliza la capacidad nativa de Git Worktrees (`git worktree add -b <rama> <directorio> <referencia-base>`). Los worktrees permiten que múltiples ramas coexistan simultáneamente en directorios de trabajo totalmente separados, compartiendo una única estructura central `.git` y unificando el almacenamiento de objetos sin sobrecarga de duplicación.

Para evitar los modos de fallo comunes en la manipulación automatizada de worktrees por agentes, se establecen directivas estrictas de contención:

- Cada worktree se asigna de forma exclusiva a una única tarea o hilo de ejecución, impidiendo cambios de rama locales mediante comandos directos de cambio de contexto (`git checkout` o `git switch`), los cuales son bloqueados a nivel de directivas del agente.
- Las anomalías provocadas por procesos interrumpidos abruptamente, como el bloqueo huérfano de `.git/worktrees/<id>/index.lock`, se resuelven mediante verificaciones de exclusión mutua en el orquestador previas al despacho de operaciones.
- Tras la resolución y fusión de cada requerimiento, los directorios temporales son purgados a través de `git worktree remove --force` seguido de `git worktree prune`, previniendo la acumulación de metadatos obsoletos en el árbol principal.

### Aislamiento de Runtime Mediante Sandboxes y Puertos Dinámicos

La contención de la ejecución se delega a contenedores ligeros sin privilegios de administrador (*rootless*) gestionados mediante Podman o Docker Engine. Cada worktree se monta como el volumen raíz de trabajo dentro de su contenedor respectivo, garantizando que los binarios generados, scripts de instalación y pruebas unitarias se ejecuten en un espacio de nombres estrictamente segregado.

El problema de las colisiones en puertos de red se erradica mediante dos estrategias complementarias:

1. **Asignación Aritmética de Puertos:** Se computa un rango exclusivo de puertos para cada instancia concurrente aplicando la fórmula de dispersión:

$P_{\text{servicio}} = P_{\text{base}} + (\text{Factory\_Index} \times 10) + \text{Offset}_{\text{servicio}}$Pservicio​=Pbase​+(Factory_Index×10)+Offsetservicio​

Donde $P_{\text{base}}$Pbase​ se fija en un bloque de puertos no privilegiados (por ejemplo, $40000$40000), asignando 10 canales contiguos a cada factoría en ejecución para desacoplar bases de datos, servidores de aplicación y daemons auxiliares.
2. **Enrutamiento Basado en Nombres Virtuales:** Alternativamente, en entornos que admiten balanceo local, un proxy inverso de bajo consumo (como Traefik o Caddy) intercepta solicitudes hacia dominios sintéticos tipo `worktree-auth.localhost`, redirigiendo el tráfico directamente a la dirección IP privada del contenedor sin necesidad de exponer puertos en el adaptador de red del host.

### Protocolo de Integración y Resolución de Conflictos

La confluencia de múltiples factorías que finalizan su ciclo de desarrollo de manera asíncrona sobre el tronco común (`main`) exige un mecanismo de serialización riguroso. OV-Factory incorpora un Agente Integrador respaldado por una cola de integración secuencial (*Merge Queue*).

Antes de fusionar una rama en producción, el sistema efectúa una evaluación previa en memoria mediante `git merge-tree --write-tree $(git merge-base main feat/x) main feat/x`. Este procedimiento determina la existencia de conflictos textuales o estructurales sin alterar el estado del árbol de trabajo. Si no se detectan inconsistencias, la rama se rebasea sobre la cabeza actualizada de `main`, se someten las pruebas unitarias a una ejecución final de validación y se ejecuta la integración definitiva.

En situaciones de conflicto semántico, el sistema desvía la integración hacia una factoría especializada de conciliación o solicita una apertura de solicitud de extracción formal para dictamen humano.

## Infraestructura de Ingesta de Eventos, Webhooks y Mensajería Asíncrona

La integración con orígenes de datos heterogéneos —tales como repositorios de GitHub, registros de auditoría de servidores o plataformas colaborativas como Slack— impone desacoplar de forma inmediata la recepción de las cargas útiles respecto a la computación analítica subyacente.

### Puerta de Enlace de Webhooks: Svix

El procesamiento directo de webhooks entrantes en el núcleo de la aplicación introduce vulnerabilidades de denegación de servicio provocadas por ráfagas de tráfico, timeouts forzados por los proveedores de eventos y fallos en la verificación de autenticidad. La arquitectura incorpora el servidor de código abierto **Svix** (desarrollado en Rust y licenciado bajo MIT) como gateway de entrada.

Svix asume de forma nativa la verificación de firmas criptográficas basadas en HMAC (soportando los formatos habituales de GitHub, GitLab y Stripe), garantizando la legitimidad del emisor antes de enrutar el contenido.

El gateway almacena temporalmente los eventos entrantes, mitiga ataques de repetición mediante marcas temporales estrictas, y responde con estados `202 Accepted` dentro de los márgenes de tiempo requeridos por los clientes externos, orquestando políticas de reintento automático con retroceso exponencial si los consumidores internos presentan demoras.

### Bus de Mensajería: NATS con JetStream

Para el intercambio de mensajes entre los servicios de backend, las factorías activas y los clientes visuales conectados, se selecciona **NATS JetStream**. A diferencia de plataformas más complejas operativamente como Apache Kafka (que demanda la gestión de clústeres voluminosos en Java) o RabbitMQ, NATS proporciona un binario único compilado en Go de consumo de memoria residual y rendimiento extremo.

| Requerimiento del Sistema | NATS JetStream | Apache Kafka | RabbitMQ |
| --- | --- | --- | --- |
| **Modelo de Mensajería** | Publish-Subscribe y Workqueues unificadas | Log distribuido particionado | Enrutamiento por intercambios AMQP |
| **Persistencia Duradera** | Motor JetStream nativo (disco y memoria) | Basada en log persistente en disco | Persistencia en colas configuradas |
| **Facilidad de Autoalojamiento** | Binario estático ligero en Go | Compleja (Requiere JVM, Zookeeper o KRaft) | Media (Erlang OTP y dependencias) |
| **Licencia de Código Abierto** | Apache 2.0 | Apache 2.0 | Mozilla Public License 2.0 |

NATS JetStream ofrece soporte directo para colas de distribución equitativa de carga (*worker queues*), permitiendo que múltiples instancias de ejecutores consuman eventos concurrentemente garantizando una entrega fiable. La retención configurable de mensajes permite reproducir historiales de auditoría y enviar el estado reactivo hacia la interfaz de usuario mediante una pasarela WebSocket integrada.

## Arquitectura del Cerebro: Memoria Persistente y Análisis de Código

El subsistema denominado "Cerebro" trasciende los esquemas convencionales de Generación Aumentada por Recuperación (RAG) no contextual, conformando la base estructurada de verdad histórica y técnica del repositorio.

### Almacenamiento Centralizado: PostgreSQL 16 con `pgvector`

Con el fin de evitar la sobrecarga arquitectónica de desplegar y sincronizar bases de datos vectoriales independientes (como Milvus o Qdrant) en conjunción con almacenes relacionales, OV-Factory consolida toda su persistencia en **PostgreSQL 16 optimizado con la extensión `pgvector`**.

El modelo de datos se estructura en tres dimensiones complementarias:

- **Memoria Arquitectónica y Normativa:** Almacena los Registros de Decisiones Arquitectónicas (ADRs), restricciones estructurales, guías de estilo obligatorias y listas de librerías homologadas.
- **Memoria de Código y Simbología:** Contiene el desglose estructural del código fuente, asociando tipos de entidades sintácticas, dependencias importadas, rutas de archivos y vectores numéricos calculados a partir de los cuerpos semánticos.
- **Memoria Episódica y de Rendimiento:** Conserva el registro cronológico de todas las tareas ejecutadas, las especificaciones técnicas generadas, los motivos de rechazo en auditorías y las intervenciones correctivas realizadas por seres humanos, vectorizadas para permitir comparaciones por similitud frente a nuevos incidentes.

### Segmentación de Código Basada en Sintaxis: Tree-sitter y `ast-grep`

La práctica tradicional de segmentar archivos de código fuente a partir de fragmentos de texto planos o recuentos arbitrarios de caracteres induce a la pérdida del alcance de variables, rompe la relación entre firmas e implementaciones y contamina los índices semánticos con fragmentos incompletos.

El pipeline de ingesta de código de OV-Factory opera a nivel sintáctico:

- **Árboles de Sintaxis Concreta con Tree-sitter:** Se utiliza Tree-sitter para compilar el código en representaciones arbóreas estructuradas. Esta aproximación es altamente tolerante a errores gramaticales o archivos en fase de edición activa. Tree-sitter delimita de manera precisa las fronteras semánticas: métodos, funciones completas, clases, interfaces y esquemas de tipos. De este modo, la unidad mínima indexada representa una entidad ejecutable coherente, preservando la información exacta de archivo y rango de líneas para trazabilidad.
- **Búsqueda Estructural con `ast-grep`:** Para la consulta de dependencias y la auditoría de conformidad sintáctica, se recurre a `ast-grep`. A diferencia de los modelos probabilísticos de lenguaje, `ast-grep` ejecuta búsquedas estructurales basadas en patrones de árboles sintácticos, garantizando cero falsos positivos en la localización de llamadas a métodos depreciados, firmas de seguridad comprometidas o usos indebidos de variables globales.

### Mecanismo de Sincronización Incremental y Bucle de Aprendizaje

El mantenimiento de los índices vectoriales ante la evolución constante del repositorio se articula mediante sincronizaciones diferenciales. Ante un nuevo commit consolidado en `main`, un worker ejecuta `git diff --name-only HEAD~1` para aislar exclusivamente los archivos alterados.

Tree-sitter analiza los módulos modificados y calcula firmas criptográficas (mediante hashes rápidos XXH64) para cada bloque de código. Si el cuerpo funcional de una entidad no ha experimentado alteraciones estructurales, su representación vectorial existente en PostgreSQL se conserva intacta. Únicamente los bloques con discrepancias en sus firmas son recalculados mediante modelos de embeddings de código (como `bge-base` o alternativas locales de código abierto), eliminando simultáneamente los nodos huérfanos de la base de datos.

En el sentido inverso, una vez superada la compuerta de aprobación, el resultado validado (incluyendo los diffs aprobados y los comentarios explicativos del revisor humano) alimenta la memoria episódica. Este ciclo confiere a OV-Factory una capacidad de aprendizaje continuo, garantizando que el redactor de especificaciones (*Spec Writer*) consulte patrones de resolución previos ante incidentes análogos, inhibiendo la reaparición de defectos ya mitigados.

## Pipeline de Revisión Paralela y Protocolo Criptográfico Human-in-the-Loop

La fase de verificación representa la barrera crítica de calidad que salvaguarda la integridad de la base de código antes de autorizar cualquier cambio hacia ramas productivas.

### Auditorías Concurrentes en Contexto Limpio

Para erradicar el sesgo cognitivo acumulativo propio de conversaciones lineales de agentes, los cuatro revisores previstos en el flujo operan de forma simultánea e independiente en **contextos limpios**. Cada revisor recibe de forma aislada un paquete que incluye la especificación formal del cambio, el parche unificado (`git diff`) y el reporte de ejecución de las suites de prueba generadas por el *Builder TDD*:

- **Revisor de Seguridad:** Ejecuta herramientas de escaneo estático (SAST), detección de secretos mediante patrones basados en entropía y análisis de vulnerabilidades conocidas en dependencias externas.
- **Revisor de UI:** Comprueba la adecuación del código visual frente a los componentes y tokens estandarizados del sistema de diseño corporativo.
- **Revisor de UX:** Audita el cumplimiento de normativas de accesibilidad (WCAG 2.1 / a11y), la completitud de los estados visuales (carga, vacío y fallo) y la inteligibilidad de las interacciones generadas.
- **Revisor de Calidad de Código:** Evalúa la cobertura efectiva de pruebas unitarias, la complejidad ciclomática de las funciones y el apego irrestricto a los estándares arquitectónicos del proyecto.

### El Agente Aprobador y la Mitigación de Bucles Finitos

El agente **Approver** recibe los cuatro dictámenes estructurados. Si surgen observaciones menores no críticas (como el formato de un mensaje de error o la falta de un test de borde), el Aprobador sintetiza las observaciones y despacha una directiva de corrección hacia el *Builder TDD*.

Para prevenir bucles infinitos de retroalimentación que consuman recursos y tokens de forma estéril, la plataforma impone un contador estricto de reintentos (configurado por defecto en tres ciclos). Si tras agotar las iteraciones permitidas las auditorías continúan emitiendo objeciones, el flujo cancela la vía automatizada y desvía la tarea directamente a la compuerta de revisión humana.

### Protocolo de Pausa Duradera y Aprobación Segura

En sistemas colaborativos con agentes de software, tratar la interacción en plataformas de mensajería (Slack, Discord) como una orden ejecutiva directa es un fallo de seguridad grave. Los mensajes expuestos en clientes de chat pueden ser interceptados, manipulados o reenviados en ataques de repetición.

OV-Factory implementa un protocolo de autorización desacoplado basado en estados duraderos y comprobación de permisos:

- Al determinarse la necesidad de revisión humana, el orquestador congela la ejecución del flujo y persiste una instantánea de estado (*snapshot*) en PostgreSQL.
- El sistema emite un registro de aprobación que asocia el identificador de la corrida, el hash criptográfico del parche de código generado, los roles con privilegios suficientes y un periodo de caducidad temporal determinado.
- El mensaje enviado a los canales de interacción (Slack Bolt o webhook hacia GitHub Checks) contiene únicamente un identificador unívoco y opaco (`approval_id`), desprovisto de privilegios intrínsecos.
- Al accionarse el botón de aprobación o rechazo en la interfaz externa, el backend valida la autenticidad de la firma HMAC de la plataforma emisora, corrobora que la identidad del usuario posea el rol de aprobación requerido, valida que el token no haya expirado y comprueba que el hash del código se mantenga inalterado.
- Únicamente tras la satisfacción de estas premisas, se emite una señal que reactiva la factoría en Hatchet para procesar el desenlace.

### Bifurcación de Salida: Fast-Track Directo frente a Pull Request

El desenlace de la tarea se bifurca en función del perfil de riesgo intrínseco del cambio:

- **Vía Rápida Directa (*Fast-Track a main*):** Aplicable únicamente si la tarea ha sido clasificada previamente como de bajo riesgo (corrección de documentación, actualización de tipos auxiliares o adición de casos de prueba), cuenta con una cobertura de pruebas del 100%, supera de manera impecable todas las auditorías automáticas y la plantilla de la factoría habilita explícitamente el despliegue desatendido.
- **Solicitud de Extracción (*Pull Request formal*):** Obligatoria siempre que el cambio involucre modificaciones en esquemas de persistencia, lógica crítica de negocio, componentes de autenticación o cuando la compuerta humana haya sido activada por discrepancias en las revisiones. La plataforma utiliza la API del repositorio para redactar un PR exhaustivo, enlazando la especificación original, el informe consolidado de auditorías y el registro de la decisión humana.

## Matriz de Selección Tecnológica y Consideraciones Operativas

La selección del ecosistema tecnológico prioriza la resiliencia operativa, la homogeneidad del stack y la preservación irrestricta de licencias de software libre (rechazando dependencias bajo esquemas restrictivos como BSL o SSPL):

| Subsistema de la Plataforma | Componente Tecnológico | Licencia de Código Abierto | Justificación de Ingeniería |
| --- | --- | --- | --- |
| **Canvas Visual Interactivo** | `@xyflow/react` + Zustand | MIT | Flexibilidad absoluta en el DOM para nodos interactivos; desacoplamiento de la reactividad del cliente. |
| **Motor de Orquestación** | Hatchet | MIT | Ejecución duradera en Go con bajo consumo de recursos; gestión eficiente de colas y paralelismo sobre PostgreSQL. |
| **Intérprete de Flujo Dinámico** | Intérprete DAG Dinámico propio | MIT | Gestión del grafo de ejecución como máquina de estados mutable en PostgreSQL, admitiendo mutaciones en caliente. |
| **Aislamiento de Código** | Git Worktrees efímeros | GPLv2 (Core Git) | Creación instantánea de árboles de trabajo concurrentes sin duplicación de la base de datos de objetos `.git`. |
| **Aislamiento de Runtime** | Docker Rootless / Podman | Apache 2.0 | Contención segura de procesos de construcción con mapeo de puertos dinámicos para impedir colisiones. |
| **Arquitectura de Extensiones** | Extism (WebAssembly Runtime) | BSD-3-Clause | Sandboxing seguro basado en capacidades para plugins de usuario políglotas (Rust, Go, TypeScript). |
| **Cerebro y Persistencia** | PostgreSQL 16 con `pgvector` | PostgreSQL Open Source | Unificación de la persistencia relacional, eventos, memoria episódica e indexación vectorial en una única base de datos. |
| **Indexación y Búsqueda de Código** | Tree-sitter + `ast-grep` | MIT | Segmentación sintáctica precisa de clases/funciones y comprobación de patrones AST con cero falsos positivos. |
| **Gateway de Webhooks** | Svix Server | MIT | Ingesta de alta capacidad con verificación de firmas, desacoplamiento de carga y reintentos automáticos. |
| **Bus de Mensajes Distribuido** | NATS con JetStream | Apache 2.0 | Transporte reactivo y de bajísima latencia para eventos de orquestación, colas de trabajadores y WebSockets. |
| **Capa de Notificación e Interacción** | Slack Bolt SDK / Discord.js | MIT / Apache 2.0 | Integraciones ChatOps desacopladas para el protocolo de verificación criptográfica de aprobación humana. |

## Conclusiones y Hoja de Ruta de Implementación

El desarrollo exitoso de una factoría de software autónoma depende de una premisa de diseño fundamental: la inteligencia de los agentes debe estar subordinada a un marco riguroso de ingeniería de sistemas distribuidos. La separación conceptual entre el estado del grafo, el motor de ejecución duradera y el aislamiento del sistema de archivos resuelve los dilemas operacionales que habitualmente frustran las iniciativas de automatización de código.

La adopción de Git Worktrees junto con contenedores efímeros resuelve de raíz el cuello de botella de la concurrencia en repositorios corporativos masivos, confiriendo a cada instancia una superficie de trabajo independiente sin penalizar los recursos de almacenamiento.

De forma simultánea, la integración de WebAssembly a través de Extism proporciona el nivel de modularidad exigido para una solución abierta y comunitaria, permitiendo que terceras partes aporten validadores, herramientas y revisores con garantías formales de contención en memoria y capacidades.

Por último, consolidar el Cerebro en PostgreSQL y `pgvector` mediante un pipeline de fragmentación sintáctica gobernado por Tree-sitter y `ast-grep` trasciende las limitaciones del RAG elemental, convirtiendo el conocimiento técnico acumulado del repositorio en una ventaja sistemática que optimiza los costes computacionales y erradica las regresiones arquitectónicas a lo largo del tiempo.
