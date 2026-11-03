---
name: code-quality-audit
description: Architectural review, code quality analysis, and engineering defect detection for codebases, APIs, libraries, services, and applications. Use for code reviews, architectural questions, refactoring triage, technical debt assessment, or code quality audits. Run the complete workflow only for explicit codebase audit requests, full/comprehensive code quality reviews, or requested report artifacts. Make sure to use this skill whenever the user asks for a code review, quality audit, code smell detection, architecture analysis, refactoring assessment, or technical debt evaluation, even if they simply say "review this codebase for bugs and design issues".
---

# Code Quality Audit

Find concrete engineering defects and architectural invariant failures that degrade correctness, performance, maintainability, concurrency, or reliability, then provide owners with source evidence, reproduction/rationale, impact severity, and the smallest effective refactoring.

This is a defensive, source-grounded engineering workflow. **Purely cosmetic nits, styling bikeshedding, or linter-level noise are strictly out of scope.** A candidate without a concrete failure mode, performance cliff, race condition, data corruption, or unmaintainable architectural coupling is not a confirmed finding.

## Operating modes

This skill operates in two distinct modes:

- **Guidance mode (Default)**: For focused code reviews, PR reviews, architectural questions, refactoring triage, or investigating specific modules, use only the relevant parts of this skill. Do not automatically create an output directory or write audit run artifacts. You may launch focused agents when useful; they return results directly to the current conversation.
- **Full audit mode**: Use the complete 6-phase workflow when the user explicitly asks to audit a codebase, asks for a full, comprehensive, or end-to-end code quality review, or requests report artifacts (`REPORT.md`, `findings.json`). Run all six phases and write the structured artifacts defined below.

### Interactive Clarification Protocol (`ask`)

When the user's intent is ambiguous or requires profile calibration, **NEVER copy or reuse questions from `security-audit`** (e.g., do not ask about security vulnerabilities, penetration testing, threat modeling, or exploit proof-of-concepts).

Instead, invoke the platform `ask` tool strictly tailored to the **real 6 phases and quality dimensions** of this skill:

#### 1. Mode Disambiguation (`audit_mode`)
When the user asks generally to "review code", "check quality", or "analyze the architecture" without specifying artifacts:

```json
{
  "questions": [
    {
      "id": "audit_mode",
      "header": "Modalidad de Auditoría",
      "question": "¿Qué modalidad de auditoría de calidad de código y arquitectura deseas ejecutar?",
      "recommended": 0,
      "options": [
        {
          "label": "Guidance Mode (Revisión puntual en chat)",
          "description": "Revisión ágil en la conversación de archivos específicos, PRs o consultas de arquitectura, concurrencia y deuda técnica, sin generar artefactos en disco ni ejecutar las 6 fases."
        },
        {
          "label": "Full Audit Mode (Auditoría completa en 6 fases)",
          "description": "Ejecución sistemática de las 6 fases: 1. Reconocimiento arquitectónico, 2. Búsqueda de invariantes en subagentes limpios, 3. Validación adversarial anti-falsos positivos, 4. Validación de esquema JSON (findings.json), 5. Verificación de diffs quirúrgicos, 6. Reportes (REPORT.md, FINDINGS-DETAIL.md, NEEDS-REFACTOR.md)."
        }
      ]
    }
  ]
}
```

#### 2. Profile Selection for Full Audit (`audit_profile`)
When Full Audit is requested without an explicit profile:

```json
{
  "questions": [
    {
      "id": "audit_profile",
      "header": "Perfil de Cobertura",
      "question": "¿Qué perfil de cobertura deseas aplicar a las 6 fases de la auditoría de calidad?",
      "recommended": 1,
      "options": [
        {
          "label": "standard (Auditoría sistemática en 6 fases)",
          "description": "Flujo completo de las 6 fases en todos los subsistemas y boundaries con verificación adversarial separada y validación de esquema determinista. (Recomendado)"
        },
        {
          "label": "quick (Paso rápido y acotado)",
          "description": "Fase 1 rápida + 1 oleada de búsqueda (Fase 2) centrada en cuellos de botella críticos (concurrencia, N+1, fugas) + verificación unificada."
        },
        {
          "label": "deep (Auditoría profunda multi-oleada)",
          "description": "Múltiples oleadas de búsqueda de subagentes, análisis de ciclos de dependencias entre módulos, contención bajo concurrencia y doble verificación independiente."
        },
        {
          "label": "scoped (Alcance acotado a módulos o PR)",
          "description": "Audita únicamente rutas, submódulos específicos o el diff de un Pull Request seleccionado."
        }
      ]
    }
  ]
}
```

#### 3. Quality Focus Calibration (`quality_focus`)
When the user wishes to prioritize specific engineering dimensions:

```json
{
  "questions": [
    {
      "id": "quality_focus",
      "header": "Dimensiones Prioritarias",
      "question": "¿Deseas priorizar alguna dimensión técnica de calidad en las oleadas de búsqueda?",
      "multi": true,
      "options": [
        {
          "label": "Concurrencia y Sincronización",
          "description": "Condiciones de carrera, deadlocks, TOCTOU, fugas de goroutines/handles y pérdida de actualizaciones en estado compartido."
        },
        {
          "label": "Rendimiento y Complejidad Algorítmica",
          "description": "Explosiones algorítmicas O(n²), cascadas de consultas N+1 en bases de datos, asignaciones excesivas y contención."
        },
        {
          "label": "Arquitectura y Acoplamiento Modular",
          "description": "Violaciones de límites de capas, dependencias circulares, acoplamiento de dominio con persistencia o transporte."
        },
        {
          "label": "Gestión de Recursos y Fugas de Memoria",
          "description": "Manejo no seguro de conexiones, streams o archivos no liberados en rutas de error y listeners sin desregistrar."
        },
        {
          "label": "Resiliencia y Manejo de Errores",
          "description": "Captura silenciosa de excepciones, transacciones huérfanas sin rollback y fallos no recuperables en procesos background."
        }
      ]
    }
  ]
}
```

## Platform terminology

This skill is agent-neutral:

- **Parent** is the coordinating agent that owns shared state, metadata, and final synthesis.
- **Task tool** is the platform's delegation or sub-agent mechanism.
- **`research` agent** is a delegated agent for focused source exploration and factual verification.
- **`general` agent** is a delegated agent for broad investigation and bounded local execution.
- **`subagent_type:`** in headings indicates which delegated role runs that task.

### Mandatory Clean-Context Subagent Delegation

All audit execution—both for substantive reviews in Guidance mode and across all six phases of Full Audit mode—**MUST execute inside dedicated subagents spawned with a clean context**:

1. **Zero Conversational Pollution**: Audit tasks must never inherit general conversational history, speculative user prompts, or unrelated prior agent chatter. Every subagent begins with a pristine context window.
2. **Strictly Explicit Inputs**: Subagents are supplied only with verified repository context: the absolute repository path, `architecture.md`, assigned coverage ledger units, selected quality classes from `QUALITY-CLASSES.md` / companion files, and the structured JSON output schema.
3. **Adversarial Context Isolation**: Verifier subagents in Phase 3 receive only the candidate defect's claimed invariant, source trace, and raw target files. They MUST NOT receive the hunter's intermediate scratch notes or subjective musings, ensuring zero confirmation bias.
4. **No Inline Parent Auditing**: The parent agent acts exclusively as coordinator, ledger manager, schema validator, and report synthesist. It never performs manual ad-hoc audits inline in the main chat context.
## Universal execution safety

Source inspection is read-only. Never modify target source during an audit. Run target-controlled tests, benchmarks, type-checkers, linters, or profiling scripts only inside an isolated environment:

- No external network requests; use mock fixtures or isolated loopback if testing local APIs.
- Target source is read-only. Writable scratch files must reside in an assigned temporary scratch directory.
- Low CPU, memory, and timeout limits.
- Never execute arbitrary untrusted build scripts or post-install hooks without sandboxing.

## Full audit setup

In full audit mode, resolve these values before reconnaissance:

- **Skill directory**: the directory containing this `SKILL.md` (e.g. `~/.agents/skills/code-quality-audit`).
- **Target**: the absolute repository root under review.
- **Repo name**: a stable repository identifier from the directory name or git remote.
- **Output directory**: a new writable directory outside the target, defaulting to `~/code-quality-audit/<repo-name>/run-<N>`, where `<N>` is the next unused integer. Only write inside the target repository if explicitly requested by the user and verified to be ignored by `.gitignore`.
- **Source ref**: the reviewed commit hash and whether the worktree has uncommitted changes.

### Write isolation

The parent coordinates the run and is the sole writer of shared artifacts:

- `run-metadata.json`
- `architecture.md`
- `coverage-ledger.json`
- `findings.json`
- `REPORT.md`
- `FINDINGS-DETAIL.md`
- `NEEDS-REFACTOR.md`

Each hunter or verifier receives a unique root under `<output-dir>/agents/<agent-id>/scratch/`. Subagents return structured JSON results via the platform Task tool. Subagents never modify shared artifacts or target source directly.

## Full audit planning

### Coverage and prior runs

No single pass catches every defect. Build a deterministic coverage plan in Phase 1 and track it in `coverage-ledger.json`:

1. Ledger units are defined by `surface × boundary × subsystem × quality_class`.
2. Status transitions follow: `planned` -> `in_progress` -> `covered` | `candidate` | `blocked` | `deferred`.
3. If prior audit runs exist in `<output-dir>`, inspect prior `coverage-ledger.json` and `findings.json`:
   - Carry forward `confirmed` findings only if relevant source paths are unchanged.
   - If relevant source has changed, create a revalidation unit.
   - Re-evaluate prior `needs_validation` or `deferred` units.

### Run profiles and scope

During audit setup, select or propose a profile based on codebase size and user request (invoke `audit_profile` question if not explicitly stated by user):

- **`quick`**: Bounded pass. Evaluates critical surfaces, core architectural boundaries, and common high-impact classes (concurrency, performance cliffs, error handling). Single hunter wave followed by one post-wave critic.
- **`standard`**: Full systematic workflow as written across all subsystems and relevant quality classes.
- **`deep`**: Comprehensive audit for high-stakes core systems. Multi-wave hunting with adversarial verification, boundary-crossing tracing, and dependency-graph cycle analysis.
- **`scoped`**: Targets named paths, a specific PR/diff, or a single subsystem. Units outside the scope are marked `out_of_scope`.

## Core principles

### 1. Require an invariant and concrete degradation (The Anti-Nitpicking Rule)
Every candidate finding MUST name:
- The broken software engineering invariant or design contract.
- The exact source path and location.
- The concrete failure mode or measurable degradation (e.g. memory leak, race condition, $O(n^2)$ algorithmic explosion, unhandled crash, circular dependency blocking compilation or modular testing).
- **Cosmetic styling, arbitrary naming preferences, formatting choices, and minor linter warnings that do not impact reliability, performance, or architecture are STRICTLY FORBIDDEN.**

### 2. Adversarial validation
The agent that validates a defect is **NEVER** the agent that discovered it. A fresh, skeptical verifier subagent attempts to disprove the candidate:
- Is this alleged N+1 query actually cached or deduplicated upstream?
- Is this alleged race condition actually protected by an outer mutex, single-threaded event loop, or transactional isolation?
- Is this alleged code smell actually an intentional zero-allocation performance optimization?
If the verifier disproves the candidate or finds it lacks real engineering impact, it is recorded as `rejected` with the refutation rationale.

### 3. Separate priority from certainty
- **`confirmed`**: The invariant failure and its concrete degradation are verified deterministically in source code. Severity is calibrated by demonstrated impact (`critical`, `high`, `medium`, `low`, `informational`).
- **`needs_validation`**: A credible defect hypothesis that cannot be conclusively confirmed without runtime load profiling, production metrics, or missing environment data. Has NO severity assigned; includes an actionable validation/benchmark plan.
- **`rejected`**: Disproved or purely cosmetic candidate.

### 4. Precision & Grounding Contract
- **Verified Source Grounding**: Every line number and symbol must be verified by directly reading target files. Speculative or hallucinated line numbers are strictly forbidden.
- **Call-Graph Reachability**: An invariant failure must be reachable from an active entrypoint or public API. Dead or unreachable code cannot produce a `critical` or `high` operational defect.
- **The Grounding Discriminator**: If a candidate defect requires external assumptions not provable from source alone (e.g. database table exceeding $10^5$ rows, network latency $> 500$ms, unobserved cache hit rates), it **MUST be classified as `needs_validation`**, never prematurely confirmed.

### 5. Token Efficiency & Progressive Disclosure
- **Selective Companion Injection**: Subagents must NEVER receive all companion files simultaneously. The parent assigns at most ONE relevant domain companion file (`WEB-AND-APIS.md`, `DATA-AND-STORAGE.md`, etc.) per hunter based on the subsystem under review.
- **Targeted Line-Range Reads**: Agents must inspect targeted line ranges (e.g. `path:40-90`) instead of reading entire files into context.
- **Bounded Output Diffs**: Remediations must be concise, unified diffs ($\le 25$ lines) targeting the exact invariant site. Never dump full replacement files.
- **No Redundant Echoing**: Parent synthesis must provide concise status overviews and point to generated files, rather than echoing entire report files into conversational responses.
### 6. Recommend the smallest effective refactoring
For every confirmed finding, prescribe:
- The invariant the code must guarantee.
- The minimal, surgical refactoring diff at the proper layer of abstraction.
- A regression test plan verifying the fix.

## Full audit workflow (6 Phases)

1. **Phase 1: Reconnaissance (`RECONNAISSANCE.md`)**:
   Map system architecture, subsystem boundaries, entry points, state management, dependencies, and initialize the deterministic `coverage-ledger.json`. Validate with `validate-coverage-ledger.cjs`.
2. **Phase 2: Coverage-Led Hunting Waves (`HUNTING.md`)**:
   Dispatch isolated hunters across assigned ledger units using `QUALITY-CLASSES.md` and domain companions. Run coverage critics between waves to catch blind spots.
3. **Phase 3: Candidate Validation (`VALIDATION-AND-REPORTING.md`)**:
   Send each unique candidate finding to a fresh adversarial verifier to confirm, refute, or mark as needing runtime validation.
4. **Phase 4: Structured Output (`findings.json`)**:
   Consolidate verdicts into `findings.json`. Validate strictly with `report-schema.json` via `validate-findings.cjs` and re-validate ledger via `validate-coverage-ledger.cjs`.
5. **Phase 5: Independent Record Verification**:
   Independent review of final confirmed claims and proposed refactoring diffs for accuracy against the latest source.
6. **Phase 6: Reporting (`VALIDATION-AND-REPORTING.md`)**:
   Synthesize final reports: `REPORT.md` (executive summary & metrics), `FINDINGS-DETAIL.md` (exhaustive technical evidence & diffs), and `NEEDS-REFACTOR.md` (prioritized refactoring roadmap).

## Anti-patterns to avoid

1. Reporting linter, formatting, or stylistic nits as quality defects.
2. Flagging architectural patterns without demonstrating concrete negative consequences.
3. Recommending massive rewrites where a surgical modular fix resolves the defect.
4. Guessing framework behavior or assuming missing features that are standard in the runtime.
5. Emitting unstructured or unverifiable claims lacking line numbers and source traces.
6. Allowing the discovering hunter to grade its own findings.
7. Assigning severity ratings to unconfirmed `needs_validation` leads.
8. Modifying target source code during an audit.
