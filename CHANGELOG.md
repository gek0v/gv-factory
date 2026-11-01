# Changelog

Todos los cambios notables en este proyecto serán documentados en este archivo.

El formato está basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
y este proyecto se adhiere a [Semantic Versioning](https://semver.org/lang/es/).
Las actualizaciones de este fichero son gestionadas automáticamente por **Release-Please**.

---

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
