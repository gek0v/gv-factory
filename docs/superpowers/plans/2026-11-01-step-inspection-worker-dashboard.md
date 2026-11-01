<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Step Execution Inspection and Real-Time Worker Cluster Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement granular step execution inspection in `NodeInspector` and a real-time worker cluster health & utilization dashboard in `web/` for Issue #92.

**Architecture:** Create typed API clients (`api/steps.ts`, `api/workers.ts`), extend Zustand store for worker cluster state and heartbeats, add a dedicated "Steps" tab with collapsible JSON outputs and duration formatters to `NodeInspector.tsx`, and build a modal/slide-over `WorkerDashboard.tsx` with live cluster metrics and SSE updates.

**Tech Stack:** TypeScript 5.7+, React 18+, Zustand 5+, Vitest 5+, standard fetch API, Vite.

**Spec:** `docs/superpowers/specs/2026-11-01-step-inspection-worker-dashboard-design.md`

---

## Global Constraints

- Every new file (`.ts`, `.tsx`, `.css`) must contain the SPDX Apache-2.0 header:
  ```typescript
  // SPDX-License-Identifier: Apache-2.0
  // SPDX-FileCopyrightText: 2026 gv-factory contributors
  ```
- Indentation: 2 spaces for TypeScript/React/CSS.
- Line endings: Unix (`LF`).
- Conventional Commits: `feat(web): ...` or `test(web): ...` referencing Issue #92.
- Test coverage $\ge 90\%$ on new code with 0 lint and 0 TypeScript build warnings.

---

### Task 1: TypeScript Models for Steps and Worker Pool (`web/src/types/worker.ts`)

**Files:**
- Create: `web/src/types/worker.ts`
- Modify: `web/src/types/flow.ts` (re-export worker types)

**Interfaces:**
- Produces: `RunStepStatus`, `RunStep`, `WorkerStatus`, `WorkerInfo`, `WorkerPoolResponse`.

- [ ] **Step 1: Write type definitions in `web/src/types/worker.ts`**
  Define `RunStepStatus` ('PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'SKIPPED'), `RunStep`, `WorkerStatus` ('ready' | 'busy' | 'offline'), `WorkerInfo`, and `WorkerPoolResponse`.
- [ ] **Step 2: Re-export in `web/src/types/flow.ts`**
  Ensure backwards-compatible access from existing imports.
- [ ] **Step 3: Commit**
  ```bash
  git add web/src/types/worker.ts web/src/types/flow.ts
  git commit -m "feat(web): define RunStep and WorkerCluster type contracts (#92)"
  ```

---

### Task 2: Steps API Client with TDD (`web/src/api/steps.ts`)

**Files:**
- Create: `web/src/api/steps.ts`
- Test: `web/src/api/steps.test.ts`

**Interfaces:**
- `fetchRunSteps(runId: string, options?: { baseUrl?: string; customFetch?: typeof fetch }): Promise<RunStep[]>`

- [ ] **Step 1: Write failing test in `web/src/api/steps.test.ts`**
  Cover 200 OK array return, 404 Not Found error handling, 500 error handling, network error, and custom `baseUrl`.
- [ ] **Step 2: Run test to verify failure**
  Run: `pnpm --prefix web test web/src/api/steps.test.ts`
  Expected: FAIL (module not found).
- [ ] **Step 3: Implement `web/src/api/steps.ts`**
  Implement `fetchRunSteps` with defensive JSON parsing and descriptive error throwing.
- [ ] **Step 4: Run test to verify it passes**
  Run: `pnpm --prefix web test web/src/api/steps.test.ts`
  Expected: PASS.
- [ ] **Step 5: Commit**
  ```bash
  git add web/src/api/steps.ts web/src/api/steps.test.ts
  git commit -m "feat(web): implement fetchRunSteps API client with full error handling (#92)"
  ```

---

### Task 3: Workers API Client with TDD (`web/src/api/workers.ts`)

**Files:**
- Create: `web/src/api/workers.ts`
- Test: `web/src/api/workers.test.ts`

**Interfaces:**
- `fetchWorkerPool(options?: { baseUrl?: string; customFetch?: typeof fetch }): Promise<WorkerPoolResponse>`

- [ ] **Step 1: Write failing test in `web/src/api/workers.test.ts`**
  Cover successful fetching of `/api/v1/workers`, calculation of aggregated metrics (`total_workers`, `active_workers`, `total_capacity`, `total_active_tasks`), 500 server error, and network exceptions.
- [ ] **Step 2: Run test to verify failure**
  Run: `pnpm --prefix web test web/src/api/workers.test.ts`
  Expected: FAIL (module not found).
- [ ] **Step 3: Implement `web/src/api/workers.ts`**
  Implement `fetchWorkerPool` with normalization of worker statuses and aggregated defaults.
- [ ] **Step 4: Run test to verify it passes**
  Run: `pnpm --prefix web test web/src/api/workers.test.ts`
  Expected: PASS.
- [ ] **Step 5: Commit**
  ```bash
  git add web/src/api/workers.ts web/src/api/workers.test.ts
  git commit -m "feat(web): implement fetchWorkerPool API client (#92)"
  ```

---

### Task 4: Store State & Real-Time SSE Dispatcher Integration

**Files:**
- Modify: `web/src/store.ts`
- Modify: `web/src/store.test.ts`
- Modify: `web/src/api/dispatcher.ts`
- Modify: `web/src/api/dispatcher.test.ts`

**Interfaces:**
- `showWorkerDashboard: boolean`, `setShowWorkerDashboard(show: boolean)`
- `workerPool: WorkerPoolResponse | null`, `setWorkerPool(pool: WorkerPoolResponse | null)`
- `updateWorkerHeartbeat(heartbeat: HeartbeatPayload)`
- Dispatcher handles `ov.worker.heartbeat` event type.

- [ ] **Step 1: Write failing test in `web/src/store.test.ts` & `web/src/api/dispatcher.test.ts`**
  Test dashboard toggle, worker pool caching, and heartbeat event consumption.
- [ ] **Step 2: Implement store state and dispatcher action**
  Update `web/src/store.ts` and `web/src/api/dispatcher.ts`.
- [ ] **Step 3: Run tests to verify they pass**
  Run: `pnpm --prefix web test web/src/store.test.ts web/src/api/dispatcher.test.ts`
  Expected: PASS.
- [ ] **Step 4: Commit**
  ```bash
  git add web/src/store.ts web/src/store.test.ts web/src/api/dispatcher.ts web/src/api/dispatcher.test.ts
  git commit -m "feat(web): add worker pool store state and heartbeat SSE dispatching (#92)"
  ```

---

### Task 5: Step Execution Inspection UI in NodeInspector

**Files:**
- Create: `web/src/components/drawer/StepExecutionViewer.tsx`
- Test: `web/src/components/drawer/StepExecutionViewer.test.ts` (or `.test.tsx`)
- Modify: `web/src/components/drawer/NodeInspector.tsx`
- Modify: `web/src/index.css`

**Interfaces:**
- New tab "Steps" in `NodeInspector.tsx`.
- Renders:
  - Empty state when no run / steps.
  - Step cards with status badges (`PENDING`, `RUNNING`, `COMPLETED`, `FAILED`, `SKIPPED`).
  - Duration formatter (`85ms`, `1.4s`, `2m 10s`).
  - Execution target badge (Distributed Worker vs In-Process).
  - Error banner on failure with message.
  - Collapsible JSON output tree / pre.
  - Refresh button.

- [ ] **Step 1: Write failing tests for StepExecutionViewer**
  Test empty state, step rendering, duration formatting, error banner, and refresh action.
- [ ] **Step 2: Implement `StepExecutionViewer.tsx`**
  Include duration formatting utility and collapsible JSON viewer.
- [ ] **Step 3: Wire into `NodeInspector.tsx`**
  Add "Steps" tab to `NodeInspector` tabs header and body.
- [ ] **Step 4: Add CSS styles in `web/src/index.css`**
- [ ] **Step 5: Run tests to verify they pass**
  Run: `pnpm --prefix web test`
  Expected: PASS.
- [ ] **Step 6: Commit**
  ```bash
  git add web/src/components/drawer/StepExecutionViewer.tsx web/src/components/drawer/NodeInspector.tsx web/src/index.css
  git commit -m "feat(web): add step execution viewer tab to NodeInspector (#92)"
  ```

---

### Task 6: Worker Cluster Monitor Dashboard Component

**Files:**
- Create: `web/src/components/workers/WorkerDashboard.tsx`
- Test: `web/src/components/workers/WorkerDashboard.test.ts` (or `.test.tsx`)
- Modify: `web/src/components/Toolbar.tsx` or `web/src/App.tsx`
- Modify: `web/src/index.css`

**Interfaces:**
- Cluster health pill in header/toolbar: "Workers: X/Y Active".
- Slide-over/modal panel:
  - 4 KPI metric cards (Total Workers, Active Workers, Total Capacity, Total Active Tasks).
  - Worker cluster table (Worker ID, Status, Utilization gauge, Uptime, Last Heartbeat).
  - Auto-polling (every 5 seconds) and SSE real-time sync.

- [ ] **Step 1: Write failing tests for WorkerDashboard**
  Test cluster health indicator, modal open/close, KPI cards, worker table rendering, utilization math, and polling cleanup.
- [ ] **Step 2: Implement `WorkerDashboard.tsx`**
  Implement health button and modal dashboard with auto-polling and cleanup.
- [ ] **Step 3: Integrate into `Toolbar.tsx` or `App.tsx`**
  Place the Worker Health Pill in the top header bar next to gateway status and canvas tools.
- [ ] **Step 4: Add CSS styles in `web/src/index.css`**
- [ ] **Step 5: Run tests to verify they pass**
  Run: `pnpm --prefix web test`
  Expected: PASS.
- [ ] **Step 6: Commit**
  ```bash
  git add web/src/components/workers/WorkerDashboard.tsx web/src/components/Toolbar.tsx web/src/App.tsx web/src/index.css
  git commit -m "feat(web): add real-time worker cluster dashboard and header health pill (#92)"
  ```

---

### Task 7: Architecture Decision Record (ADR-0011)

**Files:**
- Create: `docs/adr/0011-step-inspection-worker-cluster-dashboard.md`

- [ ] **Step 1: Document ADR-0011**
  Record context, decision, consequences, and architecture choices.
- [ ] **Step 2: Commit**
  ```bash
  git add docs/adr/0011-step-inspection-worker-cluster-dashboard.md
  git commit -m "docs(adr): record ADR-0011 for step inspection and worker cluster dashboard (#92)"
  ```

---

### Task 8: End-to-End Validation & Production Build Verification

- [ ] **Step 1: Full unit test run**
  Run: `pnpm --prefix web test`
  Expected: All tests PASS with coverage $\ge 90\%$.
- [ ] **Step 2: TypeScript & Vite Production Build**
  Run: `pnpm --prefix web build`
  Expected: Clean build with 0 TypeScript errors.
- [ ] **Step 3: REUSE compliance check**
  Run: `reuse lint` (or verify SPDX headers across all new files).
