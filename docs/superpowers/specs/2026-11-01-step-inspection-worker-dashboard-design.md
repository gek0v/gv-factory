# Spec: Inspección Granular de Steps y Dashboard en Tiempo Real del Clúster de Workers (Issue #92)

- **Fecha:** 2026-11-01
- **Estado:** Aprobado
- **Issue Asociado:** Issue #92 (`feat(web): add granular step execution inspection and real-time worker cluster dashboard`)
- **Autor / Arquitecto:** Spec Writer (gv-factory team)
- **Documentos de Referencia:**
  - `AGENTS.md` (Estándares TDD, REUSE, Invariantes)
  - `ESTRUCTURA.md` (Topología `web/`, `cmd/`, `internal/`)
  - `INFRAESTRUCTURA.md` (Aislamiento, Worker Pools, Eventos NATS/SSE)
  - `docs/adr/0005-orquestador-multi-fabrica-concurrente-colas-prioridad.md`
  - `docs/adr/0008-cli-desarrollador-runner-memoria-worker.md`

---

## 1. Resumen Ejecutivo y Alcance

### 1.1. Contexto y Problema Raíz
La interfaz visual de `gv-factory` (`web/`) permite diseñar pipelines DAG, previsualizar logs de ejecución en streaming (`LogStreamViewer`) y visualizar diffs de Git (`GitDiffViewer`). Sin embargo, durante ejecuciones concurrentes y distribuidas:
1. **Falta de visibilidad de pasos intermedios (Step Lifecycle):** Los operadores no pueden auditar el ciclo de vida granular de cada tarea individual (`PENDING`, `RUNNING`, `COMPLETED`, `FAILED`, `SKIPPED`), ni consultar duraciones precisas por paso, payloads de salida estructurados (`output`) ni causas detalladas de fallos en pasos específicos dentro del `NodeInspector`.
2. **Opacidad del Clúster de Workers:** La plataforma despacha tareas hacia workers distribuidos (`internal/worker`), pero el operador en el navegador carece de un monitor en tiempo real que refleje la salud del pool, la concurrencia activa, la utilización por nodo y los heartbeats en vivo emitidos vía SSE (`ov.worker.heartbeat`).

### 1.2. Alcance de la Solución
El Issue #92 introduce dos componentes de observabilidad y control operativo en la aplicación cliente web:
1. **API Client Layer:** Módulos cliente fuertemente tipados (`web/src/api/steps.ts` y `web/src/api/workers.ts`) para interactuar con los endpoints REST `/api/v1/runs/{id}/steps` y `/api/v1/workers`, con manejo exhaustivo de errores HTTP y red.
2. **Step Inspection UI en `NodeInspector`:** Nueva pestaña "Steps" en el cajón lateral (`NodeInspector.tsx`), implementada en `web/src/components/drawer/StepExecutionViewer.tsx`, que presenta el estado de ejecución, duración formateada, destino de cómputo (Distributed Worker vs In-Process), visor JSON interactivo de salidas y diagnóstico de errores por paso.
3. **Worker Cluster Monitor Dashboard:** Componente `WorkerDashboard.tsx` integrado en la barra superior/header con pill de salud (`Workers: X/Y Active`), desplegando un modal/panel interactivo con tarjetas KPI de capacidad, tabla de nodos workers, barras de utilización y sincronización en vivo (polling y eventos SSE).

---

## 2. Arquitectura y Contratos de Datos

### 2.1. Topología de Archivos Frontend (`web/`)

```
web/
├── src/
│   ├── api/
│   │   ├── steps.ts                 # Cliente API GET /api/v1/runs/{id}/steps
│   │   ├── steps.test.ts            # Tests unitarios de steps.ts
│   │   ├── workers.ts               # Cliente API GET /api/v1/workers
│   │   ├── workers.test.ts          # Tests unitarios de workers.ts
│   │   └── dispatcher.ts            # Manejo de eventos SSE ov.worker.heartbeat
│   ├── types/
│   │   ├── worker.ts                # Interfaces de RunStep, WorkerInfo, WorkerPoolResponse
│   │   └── flow.ts                  # Referencias y re-exportación
│   ├── components/
│   │   ├── drawer/
│   │   │   ├── NodeInspector.tsx    # Pestaña 'steps' agregada
│   │   │   ├── StepExecutionViewer.tsx      # Componente de lista y detalle de steps
│   │   │   └── StepExecutionViewer.test.tsx # Tests unitarios de renderizado y estados
│   │   ├── workers/
│   │   │   ├── WorkerDashboard.tsx          # Modal y Health Pill de cluster
│   │   │   └── WorkerDashboard.test.tsx     # Tests unitarios de dashboard de workers
│   │   ├── Toolbar.tsx              # Integración de indicador de cluster
│   │   └── icons/Icons.tsx          # Iconos ServerIcon, RefreshIcon, ChevronIcon
│   ├── store.ts                     # Estado reactivo del dashboard y worker pool
│   └── index.css                    # Estilos CSS específicos (tabs, badges, metrics cards)
```

### 2.2. Modelos y Tipos de Datos (`web/src/types/worker.ts`)

```typescript
// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export type RunStepStatus =
  | 'PENDING'
  | 'RUNNING'
  | 'COMPLETED'
  | 'FAILED'
  | 'SKIPPED';

export interface RunStep {
  id: string;
  run_id: string;
  step_name: string;
  status: RunStepStatus;
  duration_ms: number;
  error_message?: string;
  output?: Record<string, unknown>;
  created_at: string;
}

export type WorkerStatus = 'ready' | 'busy' | 'offline';

export interface WorkerInfo {
  worker_id: string;
  status: WorkerStatus;
  active_tasks: number;
  max_concurrency: number;
  uptime_seconds: number;
  last_heartbeat?: string;
  timestamp?: number;
}

export interface WorkerPoolResponse {
  workers: WorkerInfo[];
  total_workers: number;
  active_workers: number;
  total_capacity: number;
  total_active_tasks: number;
}
```

### 2.3. Contratos de API REST

#### 1. `GET /api/v1/runs/{id}/steps`
- **Request:** Path param `id` (identificador del run, e.g. `run-20261101-abcd`).
- **Respuesta Exitosa (200 OK):** Array de objetos `RunStep[]`.
  ```json
  [
    {
      "id": "builder-1-completed-1730462400000",
      "run_id": "run-20261101-abcd",
      "step_name": "builder-1",
      "status": "COMPLETED",
      "duration_ms": 1420,
      "output": {
        "files_changed": 3,
        "worker_id": "worker-eu-central-1"
      },
      "created_at": "2026-11-01T12:00:00Z"
    }
  ]
  ```
- **Errores:**
  - `404 Not Found`: `{"error": "run \"run-20261101-abcd\" not found"}`
  - `500 Internal Server Error`: `{"error": "database connection failure"}`
- **Firma TypeScript:**
  ```typescript
  export async function fetchRunSteps(
    runId: string,
    options?: { baseUrl?: string; customFetch?: typeof fetch }
  ): Promise<RunStep[]>;
  ```

#### 2. `GET /api/v1/workers`
- **Request:** Sin parámetros obligatorios. Soporta `baseUrl`.
- **Respuesta Exitosa (200 OK):**
  ```json
  {
    "workers": [
      {
        "worker_id": "ov-worker-alpha-01",
        "status": "ready",
        "active_tasks": 1,
        "max_concurrency": 4,
        "uptime_seconds": 3600,
        "timestamp": 1730462400000
      },
      {
        "worker_id": "ov-worker-beta-02",
        "status": "busy",
        "active_tasks": 4,
        "max_concurrency": 4,
        "uptime_seconds": 1800,
        "timestamp": 1730462395000
      }
    ],
    "total_workers": 2,
    "active_workers": 2,
    "total_capacity": 8,
    "total_active_tasks": 5
  }
  ```
- **Firma TypeScript:**
  ```typescript
  export async function fetchWorkerPool(
    options?: { baseUrl?: string; customFetch?: typeof fetch }
  ): Promise<WorkerPoolResponse>;
  ```

### 2.4. Integración en el Store Global Zustand (`web/src/store.ts`)
Ampliación de `FactoryFlowState` para soportar el control del dashboard y heartbeats:
- `showWorkerDashboard: boolean`: Control de visibilidad del modal de workers.
- `setShowWorkerDashboard: (show: boolean) => void`.
- `workerPool: WorkerPoolResponse | null`: Caché del estado del pool de workers.
- `setWorkerPool: (pool: WorkerPoolResponse | null) => void`.
- `updateWorkerHeartbeat: (heartbeat: { worker_id: string; active_tasks: number; max_concurrency: number; uptime_seconds: number; timestamp?: number }) => void`: Actualización incremental inmediata sin necesidad de re-consultar el endpoint HTTP completo.

### 2.5. Lógica del Despachador de Eventos SSE (`web/src/api/dispatcher.ts`)
Procesamiento de eventos `ov.worker.heartbeat`:
- Evento recibido: `type: "ov.worker.heartbeat"`
- Payload contiene: `worker_id`, `active_tasks`, `max_concurrency`, `uptime_seconds`, `timestamp`.
- Invoca `store.updateWorkerHeartbeat(...)`.

---

## 3. Plan TDD Paso a Paso (Red-Green-Refactor)

### Fase 1: Capa de Tipos y Clientes API (Red -> Green -> Refactor)

- [ ] **1.1 (Red):** Crear archivo de pruebas unitarias `web/src/api/steps.test.ts` con tests para `fetchRunSteps`:
  - Test 1: Retorna lista de pasos (`RunStep[]`) al recibir respuesta 200 OK.
  - Test 2: Normaliza y valida pasos vacíos retornando `[]`.
  - Test 3: Lanza `Error("Run not found: <runId>")` cuando el servidor responde 404.
  - Test 4: Lanza `Error` con el mensaje devuelto por el backend cuando el servidor responde 500.
  - Test 5: Maneja fallo de red (`fetch` rejected) arrojando excepción descriptiva.
  - Test 6: Respeta la opción `baseUrl`.
- [ ] **1.2 (Green):** Crear `web/src/types/worker.ts` e implementar `web/src/api/steps.ts` con la función `fetchRunSteps`.
- [ ] **1.3 (Refactor):** Simplificar el parsing de respuestas y validación defensiva en `steps.ts`.
- [ ] **1.4 (Red):** Crear archivo de pruebas unitarias `web/src/api/workers.test.ts` con tests para `fetchWorkerPool`:
  - Test 1: Retorna `WorkerPoolResponse` con workers y métricas agregadas correctas al recibir 200 OK.
  - Test 2: Calcula o preserva métricas agregadas (`total_workers`, `active_workers`, `total_capacity`, `total_active_tasks`) si vienen del backend o si necesitan fallback.
  - Test 3: Lanza `Error` cuando el endpoint responde con status HTTP de error (400, 500).
  - Test 4: Maneja rechazo de conexión o timeout de red.
  - Test 5: Respeta la opción `baseUrl`.
- [ ] **1.5 (Green):** Implementar `web/src/api/workers.ts` con la función `fetchWorkerPool`.
- [ ] **1.6 (Refactor):** Unificar manejo de respuestas JSON y extracción de mensajes de error HTTP en una utilidad compartida.

### Fase 2: Gestión de Estado y Eventos en Tiempo Real (Red -> Green -> Refactor)

- [ ] **2.1 (Red):** Extender `web/src/store.test.ts` para verificar:
  - Manejo de `showWorkerDashboard` y acción `setShowWorkerDashboard`.
  - Almacenamiento de `workerPool` mediante `setWorkerPool`.
  - Acción `updateWorkerHeartbeat` que actualiza un worker existente o agrega uno nuevo, recalculando `total_active_tasks`, `total_capacity` y `active_workers`.
- [ ] **2.2 (Green):** Modificar `web/src/store.ts` incorporando los nuevos campos y métodos a `FactoryFlowState`.
- [ ] **2.3 (Red):** Añadir test en `web/src/api/dispatcher.test.ts` para el evento `ov.worker.heartbeat`.
- [ ] **2.4 (Green):** Actualizar `web/src/api/dispatcher.ts` para capturar `ov.worker.heartbeat` y despachar a `store.updateWorkerHeartbeat`.
- [ ] **2.5 (Refactor):** Optimizar la mutación inmutable del arreglo de workers para evitar renders redundantes.

### Fase 3: Componente Step Execution Viewer en NodeInspector (Red -> Green -> Refactor)

- [ ] **3.1 (Red):** Crear test `web/src/components/drawer/StepExecutionViewer.test.tsx` (o test de lógica de UI):
  - Test 1: Muestra estado vacío ("No active run or no steps executed yet") cuando `activeRunId` es nulo o la lista está vacía.
  - Test 2: Muestra indicador de carga ("Loading execution steps...") mientras se ejecuta la petición.
  - Test 3: Renderiza tarjetas de pasos con status badge (`PENDING`, `RUNNING`, `COMPLETED`, `FAILED`, `SKIPPED`).
  - Test 4: Formatea la duración correctamente (`85ms`, `1.4s`, `2m 15s`).
  - Test 5: Renderiza target badge ("Distributed Worker: <id>" o "In-Process").
  - Test 6: Muestra banner de error visible con `error_message` cuando un step tiene status `FAILED`.
  - Test 7: Renderiza salida estructurada (`output`) en visor colapsable formateado.
  - Test 8: Provee botón "Refresh" que re-ejecuta `fetchRunSteps`.
- [ ] **3.2 (Green):** Implementar `web/src/components/drawer/StepExecutionViewer.tsx`.
- [ ] **3.3 (Red):** Modificar `web/src/components/drawer/NodeInspector.tsx` y testear la presencia de la pestaña "Steps" junto a "Config & Meta", "Logs", "Git Diff".
- [ ] **3.4 (Green):** Integrar la pestaña "Steps" en `NodeInspector.tsx` invocando `StepExecutionViewer`.
- [ ] **3.5 (Refactor):** Extraer la función `formatDuration(ms: number)` a una utilidad pura reutilizable.

### Fase 4: Dashboard y Health Indicator de Workers (Red -> Green -> Refactor)

- [ ] **4.1 (Red):** Crear test `web/src/components/workers/WorkerDashboard.test.tsx`:
  - Test 1: Renderiza el indicador de salud del clúster ("Workers: X/Y Active") con la clase de color adecuada.
  - Test 2: Al hacer click en el indicador o botón de toggle, abre el panel/modal del dashboard.
  - Test 3: Muestra las 4 tarjetas KPI: Total Workers, Active Workers, Total Capacity, Total Active Tasks.
  - Test 4: Renderiza la tabla de workers con Worker ID, Status badge (Ready, Busy, Offline), barra de utilización (`active_tasks / max_concurrency`) y Uptime formateado.
  - Test 5: Botón de refresco manual invoca `fetchWorkerPool`.
  - Test 6: Ejecuta polling automático periódico cuando el dashboard está abierto.
  - Test 7: Permite cerrar el modal mediante el botón cerrar (X) o backdrop.
- [ ] **4.2 (Green):** Implementar `web/src/components/workers/WorkerDashboard.tsx`.
- [ ] **4.3 (Green):** Integrar el pill de salud y trigger del dashboard en `web/src/components/Toolbar.tsx` o `web/src/App.tsx`.
- [ ] **4.4 (Refactor):** Estilizar el dashboard en `web/src/index.css` siguiendo el sistema de diseño oscuro y minimalista de la plataforma.

### Fase 5: Validación Integral, Build y Cumplimiento

- [ ] **5.1:** Ejecutar suite completa de tests de frontend (`vitest run`).
- [ ] **5.2:** Validar compilación de producción de TypeScript y Vite (`pnpm --prefix web build`).
- [ ] **5.3:** Validar conformidad legal REUSE (cabeceras SPDX Apache-2.0 en cada archivo creado).

---

## 4. Criterios de Aceptación (Definition of Done)

1. **Cobertura de Pruebas:** $\ge 90\%$ en los nuevos módulos creados (`web/src/api/steps.ts`, `web/src/api/workers.ts`, componentes UI correspondientes).
2. **Cero Secretos y Resiliencia HTTP:** Manejo seguro de códigos 404, 500 y desconexión de red sin excepciones no capturadas en consola.
3. **Observabilidad en Tiempo Real:** El panel de workers reacciona tanto a peticiones REST periódicas como a eventos de heartbeat vía SSE (`ov.worker.heartbeat`).
4. **Inspección Granular de Pasos:** El operador puede examinar de forma independiente los pasos ejecutados en cualquier nodo del DAG durante o después de una ejecución de fábrica.
5. **Cumplimiento REUSE:** Todos los archivos creados contienen la cabecera SPDX Apache-2.0 oficial.
6. **Compilación Limpia:** `pnpm --prefix web build` finaliza con código 0 sin errores de tipos de TypeScript.
