// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import "strings"

// Standard factory task, gate and run lifecycle topics.
const (
	TopicTaskPending          = "ov.task.pending"
	TopicTaskStarted          = "ov.task.started"
	TopicTaskCompleted        = "ov.task.completed"
	TopicTaskFailed           = "ov.task.failed"
	TopicTaskLog              = "ov.task.log"
	TopicTaskSpan             = "ov.task.span"
	TopicRunQueued            = "ov.run.queued"
	TopicRunStarted           = "ov.run.started"
	TopicRunCompleted         = "ov.run.completed"
	TopicRunFailed            = "ov.run.failed"
	TopicRunCancelled         = "ov.run.cancelled"
	TopicFactoryMetrics       = "ov.factory.metrics"
	TopicGateApprovalRequired = "gate.approval_required"
	TopicGateResolved         = "gate.resolved"
	TopicGateAudit            = "gate.audit"
	TopicMergerDivertedToPR   = "merger.diverted_to_pr"
	TopicWorkTasks            = "ov.work.tasks"
	TopicWorkTasksFilter      = "ov.work.>"
	TopicWorkerHeartbeat      = "ov.worker.heartbeat"
	TopicWorkerLogs           = "ov.worker.logs"
	TopicAllTasks             = "ov.task.*"
	TopicAllEvents            = "ov.>"

	// Inbound trigger topics
	TopicTriggerPrefix = "factory.triggers."
	TopicAllTriggers   = "factory.triggers.>"
)

// TaskLogPayload defines the payload for TopicTaskLog ("ov.task.log").
type TaskLogPayload struct {
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// TaskSpanPayload defines the telemetry payload for TopicTaskSpan ("ov.task.span").
type TaskSpanPayload struct {
	DurationMS int64  `json:"duration_ms"`
	TraceID    string `json:"trace_id"`
	SpanID     string `json:"span_id"`
	NodeType   string `json:"node_type"`
	Status     string `json:"status"`
	Tokens     int64  `json:"tokens"`
}

// WorkerLogChunkPayload represents a discrete chunk of real-time worker execution logs.
type WorkerLogChunkPayload struct {
	TaskID    string `json:"task_id"`
	StepID    string `json:"step_id"`
	RunID     string `json:"run_id"`
	WorkerID  string `json:"worker_id,omitempty"`
	Stream    string `json:"stream"` // "stdout" | "stderr"
	Chunk     string `json:"chunk"`
	Sequence  int64  `json:"sequence"`
	Timestamp int64  `json:"timestamp"`
}

// ToMap converts WorkerLogChunkPayload into a map[string]any for event payload embedding.
func (p WorkerLogChunkPayload) ToMap() map[string]any {
	m := map[string]any{
		"task_id":   p.TaskID,
		"step_id":   p.StepID,
		"run_id":    p.RunID,
		"stream":    p.Stream,
		"chunk":     p.Chunk,
		"sequence":  p.Sequence,
		"timestamp": p.Timestamp,
	}
	if p.WorkerID != "" {
		m["worker_id"] = p.WorkerID
	}
	return m
}

// TriggerTopicForSource returns the NATS topic for triggers from a specific provider source (e.g. "factory.triggers.github").
func TriggerTopicForSource(source string) string {
	if source == "" {
		return TopicTriggerPrefix + "unknown"
	}
	return TopicTriggerPrefix + source
}

// Event represents an internal messaging envelope passed over NATS / WebSockets.
type Event struct {
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	TaskID    string         `json:"task_id,omitempty"`
	Payload   map[string]any `json:"payload"`
	Timestamp int64          `json:"timestamp"`
}

// matchesTopic evaluates NATS-style wildcard patterns (* for single token, > for remainder).
func matchesTopic(pattern, topic string) bool {
	if pattern == topic || pattern == ">" {
		return true
	}
	pParts := strings.Split(pattern, ".")
	tParts := strings.Split(topic, ".")

	for i := 0; i < len(pParts); i++ {
		if pParts[i] == ">" {
			return true
		}
		if i >= len(tParts) {
			return false
		}
		if pParts[i] != "*" && pParts[i] != tParts[i] {
			return false
		}
	}
	return len(pParts) == len(tParts)
}
