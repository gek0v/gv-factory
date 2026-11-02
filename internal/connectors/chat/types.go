// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidSignature is returned when an interaction signature fails verification.
	ErrInvalidSignature = errors.New("chat: invalid signature")

	// ErrExpiredTimestamp is returned when a request timestamp is older than the allowed drift threshold.
	ErrExpiredTimestamp = errors.New("chat: request timestamp expired")

	// ErrUnknownCommand is returned when a slash command is not recognized.
	ErrUnknownCommand = errors.New("chat: unknown slash command")

	// ErrUnsupportedInteraction is returned when an interaction payload type is unsupported.
	ErrUnsupportedInteraction = errors.New("chat: unsupported interaction type")

	// ErrInvalidToken is returned when an approval token is missing or malformed.
	ErrInvalidToken = errors.New("chat: invalid approval token")

	// ErrChannelRequired is returned when no destination channel is specified.
	ErrChannelRequired = errors.New("chat: destination channel required")
)

// MessageRef identifies a posted message on a chat platform.
type MessageRef struct {
	Platform  string    `json:"platform"`
	Channel   string    `json:"channel"`
	MessageID string    `json:"message_id"`
	ThreadID  string    `json:"thread_id,omitempty"`
	URL       string    `json:"url,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NotificationMessage encapsulates a notification to be sent to a chat channel or thread.
type NotificationMessage struct {
	Channel  string            `json:"channel,omitempty"`
	ThreadTS string            `json:"thread_ts,omitempty"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Level    string            `json:"level,omitempty"` // info, success, warning, error
	Fields   map[string]string `json:"fields,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ApprovalPromptRequest contains all data needed to render an interactive decision gate card.
type ApprovalPromptRequest struct {
	RunID               string    `json:"run_id"`
	Token               string    `json:"token"`
	Channel             string    `json:"channel,omitempty"`
	ThreadTS            string    `json:"thread_ts,omitempty"`
	Summary             string    `json:"summary"`
	CommitSHA           string    `json:"commit_sha,omitempty"`
	DiffURL             string    `json:"diff_url,omitempty"`
	AffectedFiles       []string  `json:"affected_files,omitempty"`
	CoveragePercent     float64   `json:"coverage_percent"`
	CriticalAlerts      int       `json:"critical_alerts"`
	HighAlerts          int       `json:"high_alerts"`
	ExpiresAt           time.Time `json:"expires_at"`
	HasMigrationChanges bool      `json:"has_migration_changes"`
	HasAuthChanges      bool      `json:"has_auth_changes"`
	HasSchemaChanges    bool      `json:"has_schema_changes"`
	ReviewerScores      []string  `json:"reviewer_scores,omitempty"`
	TaskTitle           string    `json:"task_title,omitempty"`
	GateReason          string    `json:"gate_reason,omitempty"`
	FindingsCount       int       `json:"findings_count,omitempty"`
}

// StatusUpdate represents a step-by-step progress update during factory execution.
type StatusUpdate struct {
	RunID     string    `json:"run_id"`
	TaskID    string    `json:"task_id,omitempty"`
	StepName  string    `json:"step_name"`
	Channel   string    `json:"channel,omitempty"`
	ThreadTS  string    `json:"thread_ts,omitempty"`
	Status    string    `json:"status"` // pending, running, completed, failed
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// GateResolution captures the user's decision (APPROVE or REJECT) from an interactive card.
type GateResolution struct {
	RunID     string    `json:"run_id"`
	Token     string    `json:"token"`
	Decision  string    `json:"decision"` // APPROVE or REJECT
	User      string    `json:"user"`
	UserID    string    `json:"user_id,omitempty"`
	Channel   string    `json:"channel"`
	MessageID string    `json:"message_id"`
	ThreadID  string    `json:"thread_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// GateResolver resolves gate approval tokens.
type GateResolver interface {
	ResolveGate(ctx context.Context, res GateResolution) error
}

// GateResolverFunc allows standard functions to implement GateResolver.
type GateResolverFunc func(ctx context.Context, res GateResolution) error

// ResolveGate calls the wrapped function.
func (f GateResolverFunc) ResolveGate(ctx context.Context, res GateResolution) error {
	return f(ctx, res)
}

// SlashCommand represents a parsed slash command payload.
type SlashCommand struct {
	Platform    string            `json:"platform"`
	Command     string            `json:"command"`
	Subcommand  string            `json:"subcommand"`
	Arguments   []string          `json:"arguments"`
	Template    string            `json:"template,omitempty"`
	Issue       string            `json:"issue,omitempty"`
	Branch      string            `json:"branch,omitempty"`
	RunID       string            `json:"run_id,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	UserID      string            `json:"user_id"`
	UserName    string            `json:"user_name"`
	ChannelID   string            `json:"channel_id"`
	ResponseURL string            `json:"response_url,omitempty"`
}

// CommandExecutor executes a validated slash command asynchronously.
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, cmd SlashCommand) (string, error)
}

// CommandExecutorFunc allows functions to implement CommandExecutor.
type CommandExecutorFunc func(ctx context.Context, cmd SlashCommand) (string, error)

// ExecuteCommand calls the wrapped function.
func (f CommandExecutorFunc) ExecuteCommand(ctx context.Context, cmd SlashCommand) (string, error) {
	return f(ctx, cmd)
}

// ChatNotifier defines the uniform interface for all chat platform adapters.
type ChatNotifier interface {
	Platform() string
	SendNotification(ctx context.Context, msg NotificationMessage) (*MessageRef, error)
	PromptApproval(ctx context.Context, req ApprovalPromptRequest) (*MessageRef, error)
	UpdateStatus(ctx context.Context, update StatusUpdate) (*MessageRef, error)
}

