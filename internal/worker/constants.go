// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import "github.com/gv-factory/gv-factory/internal/gateway"

const (
	StreamTasks          = "OV_TASKS"
	ConsumerGroup        = "ov-worker-group"
	TopicWorkTasks       = gateway.TopicWorkTasks
	TopicWorkTasksFilter = gateway.TopicWorkTasksFilter
	TopicWorkerHeartbeat = gateway.TopicWorkerHeartbeat

	TaskTypeWasmPlugin = "wasm_plugin"
	TaskTypeAgentStep  = "agent_step"
	TaskTypeGitSandbox = "git_sandbox"
)
