### Estructura Definitiva del Monorepo (Go + React/TS)

Para mantener el código limpio y modular desde el `git init`, esta es la estructura ideal combinando Go Workspaces con el frontend visual y el SDK de plugins:

```text
ov-factory/
├── .github/                   # Workflows CI/CD, plantillas de Issues/PR y Dependabot
├── cmd/
│   ├── ov-server/             # Punto de entrada del servidor principal y orquestador
│   ├── ov-worker/             # Agente de ejecución para Hatchet y gestión de Sandboxes
│   └── ov-cli/                # CLI para desarrolladores (ejecución local y gestión)
├── internal/
│   ├── brain/                 # Conexión a pgvector, ingesta con Tree-sitter y RAG
│   ├── dag/                   # Intérprete de grafos dinámicos y mutación en caliente
│   ├── sandbox/               # Controlador de Git Worktrees y contenedores Docker/Podman
│   ├── plugins/               # Host de Extism (WASM) y definición de Host Functions
│   ├── gateway/               # Integración con Svix, NATS JetStream y WebSockets
│   └── auth/                  # OAuth2, RBAC y verificación criptográfica (Approver)
├── pkg/
│   └── schema/                # Contratos JSON Schema / YAML de flujos (exportables a TS)
├── web/                       # Frontend SPA con @xyflow/react + Zustand + Vite
├── plugins-sdk/               # Plantillas PDK (Go, Rust, TS) para crear nodos custom
├── deployments/
│   └── docker-compose.yml     # Stack completo: Postgres (pgvector), NATS, Hatchet, Svix
├── docs/
│   └── adr/                   # Registros de Decisiones Arquitectónicas (leídos por el Cerebro)
├── go.mod
├── Makefile                   # Comandos unificados (build, test, lint, dev)
├── LICENSE                    # Apache-2.0
└── README.md

```

---
