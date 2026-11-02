// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

// Package e2e provides end-to-end integration test suites verifying the
// complete lifecycle of the autonomous software factory:
//  1. Inbound webhook triggering from GitHub (ready-for-agent / @factory build).
//  2. Spec synthesis, TDD Builder in isolated worktree, Security & Architecture review, Approver decision.
//  3. Sequential Merge Queue validation (git merge-tree) & Pull Request creation on GitHub API.
//  4. Human-in-the-Loop Gate card dispatch to Slack & Discord with HMAC verification and auto-resolution.
//  5. CLI interaction via ov-cli factory run --sync and ov-cli factory status.
package e2e
