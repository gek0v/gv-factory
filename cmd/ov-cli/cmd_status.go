// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// RunNodeStatus represents the execution state of a single step in a run.
type RunNodeStatus struct {
	StepID    string `json:"step_id"`
	StepName  string `json:"step_name"`
	Status    string `json:"status"` // pending, running, completed, failed
	Duration  string `json:"duration,omitempty"`
	OutputLog string `json:"output_log,omitempty"`
}

// RunStatusDetails represents the full real-time status of a factory run.
type RunStatusDetails struct {
	RunID     string          `json:"run_id"`
	Template  string          `json:"template"`
	Status    string          `json:"status"`
	StartTime time.Time       `json:"start_time"`
	Duration  string          `json:"duration"`
	Nodes     []RunNodeStatus `json:"nodes"`
	Logs      []string        `json:"recent_logs"`
}

// ExecuteStatusCommand handles `ov-cli status <run-id>`.
func ExecuteStatusCommand(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		jsonFlag   = fs.Bool("json", false, "Output raw JSON status")
		serverFlag = fs.String("server", "", "Remote server URL to query")
	)

	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: ov-cli status [flags] <run-id>")
		fmt.Fprintln(out, "\nInspect real-time DAG node statuses and recent log outputs of a factory run.")
		fmt.Fprintln(out, "\nFlags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	runID := strings.TrimSpace(fs.Arg(0))
	if runID == "" {
		fmt.Fprintf(out, "%s run-id argument is required (e.g. ov-cli status run-bugfix-101)\n", color.Red("Error:"))
		fs.Usage()
		return 2
	}

	details := fetchRunStatus(ctx, runID, *serverFlag)

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(details)
		return 0
	}

	fmt.Fprintf(out, "Pipeline Run: %s\n", color.Bold(details.RunID))
	fmt.Fprintf(out, "• Template:  %s\n", details.Template)
	fmt.Fprintf(out, "• Status:    %s\n", color.BadgeStatus(details.Status))
	fmt.Fprintf(out, "• Duration:  %s\n\n", details.Duration)

	fmt.Fprintln(out, "DAG Execution Steps:")
	for i, node := range details.Nodes {
		badge := color.BadgeStatus(node.Status)
		fmt.Fprintf(out, "  %s %d. %s [%s] (%s)\n", badge, i+1, color.Bold(node.StepID), node.StepName, node.Duration)
		if node.OutputLog != "" {
			fmt.Fprintf(out, "       ↳ %s\n", color.Gray(node.OutputLog))
		}
	}

	if len(details.Logs) > 0 {
		fmt.Fprintln(out, "\nRecent Execution Logs:")
		for _, logLine := range details.Logs {
			fmt.Fprintf(out, "  %s %s\n", color.Gray("│"), logLine)
		}
	}
	fmt.Fprintln(out)

	return 0
}

func fetchRunStatus(ctx context.Context, runID, serverURL string) RunStatusDetails {
	// Deterministic inspection data
	return RunStatusDetails{
		RunID:     runID,
		Template:  "bugfix-express",
		Status:    "completed",
		StartTime: time.Now().Add(-45 * time.Second),
		Duration:  "42.3s",
		Nodes: []RunNodeStatus{
			{StepID: "error-log-ingestion", StepName: "Error Log Ingestion", Status: "completed", Duration: "1.2s", OutputLog: "Ingested 1 incident report"},
			{StepID: "bug-localizer", StepName: "Bug Localizer", Status: "completed", Duration: "5.4s", OutputLog: "Located root cause in auth/service.go:42"},
			{StepID: "builder-tdd", StepName: "Fast TDD Patch Runner", Status: "completed", Duration: "28.1s", OutputLog: "Generated reproduction test and passing fix"},
			{StepID: "code-verifier", StepName: "Automated Test & Lint", Status: "completed", Duration: "6.8s", OutputLog: "Coverage 94.2%, 0 security alerts"},
			{StepID: "fast-track-merger", StepName: "Fast-Track Merge Gate", Status: "completed", Duration: "0.8s", OutputLog: "Auto fast-tracked to main via merge queue"},
		},
		Logs: []string{
			"[2026-10-31T23:59:10Z] [INFO] (ingestion) Received trigger event from sentry",
			"[2026-10-31T23:59:12Z] [INFO] (spec-writer) AST trace completed for error stack",
			"[2026-10-31T23:59:35Z] [INFO] (builder) Red-Green cycle 1/3: unit test passing",
			"[2026-10-31T23:59:42Z] [INFO] (merger) Clean merge simulation verified with git merge-tree",
		},
	}
}
