// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package pdk

// Context represents execution context injected into an Extism WASM plugin node.
type Context struct {
	RunID     string            `json:"run_id"`
	TaskID    string            `json:"task_id"`
	Configs   map[string]string `json:"configs"`
}

// Result holds the return payload sent back to the orchestrator.
type Result struct {
	Success bool              `json:"success"`
	Output  string            `json:"output"`
	Data    map[string]any    `json:"data,omitempty"`
}
