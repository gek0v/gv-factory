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
	TopicRunQueued            = "ov.run.queued"
	TopicRunStarted           = "ov.run.started"
	TopicRunCompleted         = "ov.run.completed"
	TopicRunFailed            = "ov.run.failed"
	TopicRunCancelled         = "ov.run.cancelled"
	TopicFactoryMetrics       = "ov.factory.metrics"
	TopicGateApprovalRequired = "gate.approval_required"
	TopicMergerDivertedToPR   = "merger.diverted_to_pr"
	TopicAllTasks             = "ov.task.*"
	TopicAllEvents            = "ov.>"

	// Inbound trigger topics
	TopicTriggerPrefix = "factory.triggers."
	TopicAllTriggers   = "factory.triggers.>"
)

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
	if pattern == topic || pattern == ">" || pattern == "ov.>" {
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
