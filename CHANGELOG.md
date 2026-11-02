# Changelog

Todos los cambios notables en este proyecto serán documentados en este archivo.

El formato está basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
y este proyecto se adhiere a [Semantic Versioning](https://semver.org/lang/es/).
Las actualizaciones de este fichero son gestionadas automáticamente por **Release-Please**.

---

## [1.7.0](https://github.com/gek0v/gv-factory/compare/v1.6.0...v1.7.0) (2026-11-02)


### Features

* **brain:** implement automated ADR Markdown parser and sync to architectural_rules store ([#122](https://github.com/gek0v/gv-factory/issues/122)) ([#145](https://github.com/gek0v/gv-factory/issues/145)) ([f06fb10](https://github.com/gek0v/gv-factory/commit/f06fb101920fa83072f80f37c3896f4aebbfe09d))
* **brain:** implement Tree-sitter code chunker with AST symbol extraction ([#119](https://github.com/gek0v/gv-factory/issues/119)) ([#148](https://github.com/gek0v/gv-factory/issues/148)) ([430a178](https://github.com/gek0v/gv-factory/commit/430a17839554d065d4922b65c6f9e9f45e54b179))
* **server,dag:** trigger automated pipeline runs from inbound GitHub webhooks ([#121](https://github.com/gek0v/gv-factory/issues/121)) ([#147](https://github.com/gek0v/gv-factory/issues/147)) ([1efa7d3](https://github.com/gek0v/gv-factory/commit/1efa7d3f47a201246b4fd4db3506c0756e313959))
* **web:** add interactive DecisionGate node in canvas with live HMAC approval actions ([#120](https://github.com/gek0v/gv-factory/issues/120)) ([#146](https://github.com/gek0v/gv-factory/issues/146)) ([7cec2d9](https://github.com/gek0v/gv-factory/commit/7cec2d98d7cea98c49f31872ae5b3906655367cd))

## [1.6.0](https://github.com/gek0v/gv-factory/compare/v1.5.0...v1.6.0) (2026-11-02)


### Features

* **plugins,server:** support dry-run/mock plugin execution when WASM binaries are missing ([#115](https://github.com/gek0v/gv-factory/issues/115)) ([#117](https://github.com/gek0v/gv-factory/issues/117)) ([beecf4d](https://github.com/gek0v/gv-factory/commit/beecf4d0aa4bf8d487282a886320b51944026c3e))


### Bug Fixes

* **web:** enforce valid extism:// URIs in initial canvas flow and palette presets ([#114](https://github.com/gek0v/gv-factory/issues/114)) ([#116](https://github.com/gek0v/gv-factory/issues/116)) ([6af9701](https://github.com/gek0v/gv-factory/commit/6af97019dc5e0570c2d83a272e08f9b2371cfa28))

## [1.5.0](https://github.com/gek0v/gv-factory/compare/v1.4.1...v1.5.0) (2026-11-02)


### Features

* **llm:** implement multi-provider LLM connector with tool calling and MCP schema mapping ([#107](https://github.com/gek0v/gv-factory/issues/107)) ([8c65b15](https://github.com/gek0v/gv-factory/commit/8c65b15e8e813564d7ced718c235e8e98400d303))
* **web:** add LLM model selection in NodeInspector and live reasoning/tool-call stream viewer ([#108](https://github.com/gek0v/gv-factory/issues/108)) ([0fc9181](https://github.com/gek0v/gv-factory/commit/0fc9181ec1fccd58948f23c86c58931db832a623))
* **worker:** implement cognitive AgentStepExecutor with MCP tool loop and live reasoning streaming ([#109](https://github.com/gek0v/gv-factory/issues/109)) ([#113](https://github.com/gek0v/gv-factory/issues/113)) ([c8d9926](https://github.com/gek0v/gv-factory/commit/c8d992687ac05b3eb9a7897612cfad31dd1e83c3))

## [1.4.1](https://github.com/gek0v/gv-factory/compare/v1.4.0...v1.4.1) (2026-11-02)


### Bug Fixes

* **web:** proxy /events and /api endpoints to backend in vite dev server ([#104](https://github.com/gek0v/gv-factory/issues/104)) ([#105](https://github.com/gek0v/gv-factory/issues/105)) ([d1f5c79](https://github.com/gek0v/gv-factory/commit/d1f5c791049492fc721d687f772172f5b2cac106))

## [1.4.0](https://github.com/gek0v/gv-factory/compare/v1.3.0...v1.4.0) (2026-11-02)


### Features

* **sandbox,worker:** implement OCI container runner with cgroups v2 resource quotas and mount isolation ([#100](https://github.com/gek0v/gv-factory/issues/100)) ([#103](https://github.com/gek0v/gv-factory/issues/103)) ([9dc5281](https://github.com/gek0v/gv-factory/commit/9dc5281d4c3a484c7ceb2e0b05914259a1c8123a))
* **web:** add sandbox isolation policy configuration and execution security telemetry in inspector ([#101](https://github.com/gek0v/gv-factory/issues/101)) ([#102](https://github.com/gek0v/gv-factory/issues/102)) ([fcf0b63](https://github.com/gek0v/gv-factory/commit/fcf0b6348209bb028f4dfc27edcb70b40c8947a8))
* **web:** interactive real-time LogViewer with ANSI formatting, search, and auto-scroll ([#96](https://github.com/gek0v/gv-factory/issues/96)) ([#97](https://github.com/gek0v/gv-factory/issues/97)) ([59ae9fb](https://github.com/gek0v/gv-factory/commit/59ae9fb69e73b4351606fda8615e104f8224b196))
* **worker,server:** stream chunked execution logs via NATS and SSE bridge ([#95](https://github.com/gek0v/gv-factory/issues/95)) ([#98](https://github.com/gek0v/gv-factory/issues/98)) ([91e9aef](https://github.com/gek0v/gv-factory/commit/91e9aefd073da2e36bdc49899d9a2d70967cddab))

## [1.3.0](https://github.com/gek0v/gv-factory/compare/v1.2.0...v1.3.0) (2026-11-01)


### Features

* **dag,server:** implement distributed step dispatcher via NATS JetStream and persist run steps in store ([#87](https://github.com/gek0v/gv-factory/issues/87)) ([#89](https://github.com/gek0v/gv-factory/issues/89)) ([1fa8778](https://github.com/gek0v/gv-factory/commit/1fa87785da11d259ae14921d17828f12e606adcb))
* **server:** implement worker pool monitoring endpoint and heartbeat aggregator ([#91](https://github.com/gek0v/gv-factory/issues/91)) ([#93](https://github.com/gek0v/gv-factory/issues/93)) ([a1eacbc](https://github.com/gek0v/gv-factory/commit/a1eacbc805c804039d645dbc2865c34ecf332d6e))
* **web:** add granular step execution inspection and real-time worker cluster dashboard ([#92](https://github.com/gek0v/gv-factory/issues/92)) ([#94](https://github.com/gek0v/gv-factory/issues/94)) ([b7c7ec6](https://github.com/gek0v/gv-factory/commit/b7c7ec69e6bcf3b09c0d78a9c22490996777dafc))
* **worker:** integrate execution sub-engines with task router ([#86](https://github.com/gek0v/gv-factory/issues/86)) ([#88](https://github.com/gek0v/gv-factory/issues/88)) ([e7421b2](https://github.com/gek0v/gv-factory/commit/e7421b27b8a9227bef31840e86e276d679f99c25))

## [1.2.0](https://github.com/gek0v/gv-factory/compare/v1.1.0...v1.2.0) (2026-11-01)


### Features

* **store:** implement PostgreSQL persistence repository and schema migrations ([#81](https://github.com/gek0v/gv-factory/issues/81)) ([#83](https://github.com/gek0v/gv-factory/issues/83)) ([39df588](https://github.com/gek0v/gv-factory/commit/39df588a35dd109989c024bc8173df70b406e963))
* **worker:** implement distributed task queue consumer and NATS JetStream worker ([#82](https://github.com/gek0v/gv-factory/issues/82)) ([#84](https://github.com/gek0v/gv-factory/issues/84)) ([64fe03a](https://github.com/gek0v/gv-factory/commit/64fe03a8c86413fa89d04614435f85f372372f51))

## [1.1.0](https://github.com/gek0v/gv-factory/compare/v1.0.0...v1.1.0) (2026-11-01)


### Features

* **api:** expose pipeline runs and DAG management endpoints in ov-server ([#69](https://github.com/gek0v/gv-factory/issues/69)) ([243d431](https://github.com/gek0v/gv-factory/commit/243d43164f0848cafba3a55dc6ea4cf8b86af45e))
* **api:** stream real-time task logs, telemetry spans, and run-filtered SSE events ([#72](https://github.com/gek0v/gv-factory/issues/72)) ([#75](https://github.com/gek0v/gv-factory/issues/75)) ([25f0379](https://github.com/gek0v/gv-factory/commit/25f037933d98c25e1677ef3edab206ad07361514))
* **opencode:** add /work-issue slash command for automated multi-agent workflow ([#58](https://github.com/gek0v/gv-factory/issues/58)) ([ad01297](https://github.com/gek0v/gv-factory/commit/ad0129729740fb98f1b297a796b287fda8ea41ae))
* **opencode:** add parallel multi-issue orchestration command and orchestrator agent ([#67](https://github.com/gek0v/gv-factory/issues/67)) ([dbd9ebf](https://github.com/gek0v/gv-factory/commit/dbd9ebf5e36ef3c4297b4ac0ad2d35a16b4acec7))
* **opencode:** define specialized subagents for autonomous workflow ([#55](https://github.com/gek0v/gv-factory/issues/55)) ([4af79ab](https://github.com/gek0v/gv-factory/commit/4af79ab0a58dd56de0bde9d7f76563adb3fe5e73))
* **runtime:** implement Model Context Protocol (MCP) client and tool execution bridge ([#71](https://github.com/gek0v/gv-factory/issues/71)) ([909252e](https://github.com/gek0v/gv-factory/commit/909252eacbb5d071bdf48851a8ec2db7caaa3f62))
* **runtime:** intercept MCP tool calls with HMAC decision gates and approval workflow ([#77](https://github.com/gek0v/gv-factory/issues/77)) ([#80](https://github.com/gek0v/gv-factory/issues/80)) ([d7b8e0d](https://github.com/gek0v/gv-factory/commit/d7b8e0d24ad6484a1173407d9525ed57ea0b743c))
* **security:** enforce HMAC cryptographic verification and audit logging on gate resolution ([#68](https://github.com/gek0v/gv-factory/issues/68)) ([0e7c0e5](https://github.com/gek0v/gv-factory/commit/0e7c0e5bb21fe0e5fedceabdb8a9b0b7d5038250))
* **web:** add live server pipeline execution and DAG export in orchestrator canvas ([#70](https://github.com/gek0v/gv-factory/issues/70)) ([5d09621](https://github.com/gek0v/gv-factory/commit/5d09621524b0611b8f7943c20adfad621d75532e))
* **web:** human-in-the-loop approval modal and inspector integration for MCP agent tool gates ([#78](https://github.com/gek0v/gv-factory/issues/78)) ([#79](https://github.com/gek0v/gv-factory/issues/79)) ([48f0875](https://github.com/gek0v/gv-factory/commit/48f0875ae74b1ce52ea4a8eef09b2d3b1daee28d))
* **web:** live DAG execution tracking, real-time log streaming and run lifecycle controls ([#73](https://github.com/gek0v/gv-factory/issues/73)) ([#74](https://github.com/gek0v/gv-factory/issues/74)) ([87ace85](https://github.com/gek0v/gv-factory/commit/87ace852f9aff8427e238e79af08cb0b794327bc))


### Bug Fixes

* **ci:** fix REUSE compliance parsing and update golang.org/x/text for security audit ([fb42d10](https://github.com/gek0v/gv-factory/commit/fb42d109c9fe2a45486c33c394370b6e07e246ac))
* **dist:** make Homebrew tap publishing resilient in .goreleaser.yaml with skip_upload: auto ([#57](https://github.com/gek0v/gv-factory/issues/57)) ([e06f027](https://github.com/gek0v/gv-factory/commit/e06f027941b10b2c3f6ab077598b786ac7489c01))
* **docker:** align go.mod Go version to 1.24 and whitelist esbuild in web/package.json for container builds ([#56](https://github.com/gek0v/gv-factory/issues/56)) ([4883ecb](https://github.com/gek0v/gv-factory/commit/4883ecb3e4408bad409c98028741d5a378237b06))
* **docker:** pin pnpm v9 in Dockerfile.web and handle attest-build-provenance resilience in release workflow ([23ecdbe](https://github.com/gek0v/gv-factory/commit/23ecdbe0532d1cf36c4376259aa6923e79e7d017))

## [Unreleased]

### Added
- Configuración de gobernanza, licenciamiento Apache-2.0 y cumplimiento de estándar REUSE.
- Normalización de control de versiones con `.gitignore`, `.gitattributes`, `.editorconfig` y `.pre-commit-config.yaml`.
- Automatización de CI/CD en `.github/` con verificación de pruebas, auditoría estática CodeQL, OpenSSF Scorecard y publicación con Release-Please.
- Plantillas estructuradas de Issues y Pull Requests con soporte para plugins WASM.
- Documentos de directrices para agentes autónomos (`AGENTS.md`) y hoja de ruta (`ROADMAP.MD`).
