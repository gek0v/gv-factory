// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
)

// FactoryRunRequest represents the payload accepted by POST /api/v1/factory/run.
type FactoryRunRequest struct {
	TaskID      string         `json:"task_id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Model       string         `json:"model,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Constraints []string       `json:"constraints,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// FactoryRunResponse represents the status and outcome returned by factory endpoints.
type FactoryRunResponse struct {
	RunID     string                 `json:"run_id"`
	Status    string                 `json:"status"` // "running", "completed", "failed", "gate_waiting"
	Outcome   *agent.PipelineOutcome `json:"outcome,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	UpdatedAt time.Time              `json:"updated_at,omitempty"`
}

// ExecuteFactoryCommand handles `ov-cli factory <subcommand>`.
func ExecuteFactoryCommand(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printFactoryUsage(out)
		return 0
	}

	subcommand := args[0]
	cmdArgs := args[1:]

	switch subcommand {
	case "run":
		return executeFactoryRun(ctx, cmdArgs, out, color)
	case "status":
		return executeFactoryStatus(ctx, cmdArgs, out, color)
	case "list":
		return executeFactoryList(ctx, cmdArgs, out, color)
	case "gates":
		return executeFactoryGates(ctx, cmdArgs, out, color)
	case "approve":
		return executeFactoryApprove(ctx, cmdArgs, out, color)
	case "reject":
		return executeFactoryReject(ctx, cmdArgs, out, color)
	default:
		fmt.Fprintf(out, "%s unknown factory subcommand %q\n\n", color.Red("Error:"), subcommand)
		printFactoryUsage(out)
		return 2
	}
}

func printFactoryUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: ov-cli factory <subcommand> [flags] [arguments]")
	fmt.Fprintln(out, "\nManage and monitor autonomous software factory pipelines.")
	fmt.Fprintln(out, "\nSubcommands:")
	fmt.Fprintln(out, "  run <title>          Dispatch an autonomous software factory pipeline")
	fmt.Fprintln(out, "  status <run-id>      Inspect the real-time execution status and outcome of a run")
	fmt.Fprintln(out, "  list                 List active and recent factory executions")
	fmt.Fprintln(out, "  gates                List active pending decision gates awaiting human approval")
	fmt.Fprintln(out, "  approve <run-id>     Approve a pending human gate and enqueue to merge queue")
	fmt.Fprintln(out, "  reject <run-id>      Reject a pending human gate with optional reason")
	fmt.Fprintln(out, "\nGlobal Flags:")
	fmt.Fprintln(out, "  --server <url>       Remote ov-server URL (default: $OV_SERVER_URL or http://localhost:8080)")
	fmt.Fprintln(out, "  --json               Output raw JSON response")
	fmt.Fprintln(out, "\nUse 'ov-cli factory <subcommand> --help' for flags of a specific subcommand.")
}

func resolveServerURL(serverFlag string) string {
	url := strings.TrimSpace(serverFlag)
	if url == "" {
		url = strings.TrimSpace(os.Getenv("OV_SERVER_URL"))
	}
	if url == "" {
		url = "http://localhost:8080"
	}
	return strings.TrimRight(url, "/")
}

func generateFactoryTaskID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("task-%d-%s", time.Now().Unix(), hex.EncodeToString(b[:]))
}

func parseCommaSeparated(val string) []string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func executeFactoryRun(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("factory run", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		descFlag        = fs.String("description", "", "Description of the task to execute")
		modelFlag       = fs.String("model", "", "Target LLM model for spec generation and code building (e.g. 'claude-3-5-sonnet', 'gpt-4o')")
		labelsFlag      = fs.String("labels", "", "Comma-separated labels (e.g. 'security,auth')")
		constraintsFlag = fs.String("constraints", "", "Comma-separated constraints (e.g. 'no external deps,coverage >= 90')")
		syncFlag        = fs.Bool("sync", false, "Wait for completion and display outcome status table")
		asyncFlag       = fs.Bool("async", false, "Dispatch asynchronously (default mode)")
		serverFlag      = fs.String("server", "", "Remote ov-server URL")
		jsonFlag        = fs.Bool("json", false, "Output in JSON format")
		taskIDFlag      = fs.String("task-id", "", "Explicit task ID (auto-generated if omitted)")
	)

	valFlags := map[string]bool{
		"description": true,
		"model":       true,
		"labels":      true,
		"constraints": true,
		"server":      true,
		"task-id":     true,
	}

	reordered := reorderArgs(args, valFlags)
	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	title := strings.TrimSpace(fs.Arg(0))
	if title == "" {
		fmt.Fprintf(out, "%s title is required (e.g. ov-cli factory run \"Fix authentication token\")\n", color.Red("Error:"))
		return 2
	}

	taskID := strings.TrimSpace(*taskIDFlag)
	if taskID == "" {
		taskID = generateFactoryTaskID()
	}

	reqPayload := FactoryRunRequest{
		TaskID:      taskID,
		Title:       title,
		Description: strings.TrimSpace(*descFlag),
		Model:       strings.TrimSpace(*modelFlag),
		Labels:      parseCommaSeparated(*labelsFlag),
		Constraints: parseCommaSeparated(*constraintsFlag),
	}

	serverURL := resolveServerURL(*serverFlag)
	client := &http.Client{Timeout: 10 * time.Minute}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		fmt.Fprintf(out, "%s failed to serialize request payload: %v\n", color.Red("Error:"), err)
		return 1
	}

	isSync := *syncFlag && !*asyncFlag
	url := fmt.Sprintf("%s/api/v1/factory/run", serverURL)
	if isSync {
		url += "?async=false"
	} else {
		url += "?async=true"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Fprintf(out, "%s failed to create HTTP request: %v\n", color.Red("Error:"), err)
		return 1
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(out, "%s remote server connection failed: %v\n", color.Red("Error:"), err)
		return 1
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(out, "%s failed to read server response: %v\n", color.Red("Error:"), err)
		return 1
	}

	if resp.StatusCode >= 400 {
		var errData map[string]any
		errMsg := string(respBody)
		if json.Unmarshal(respBody, &errData) == nil {
			if e, ok := errData["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		fmt.Fprintf(out, "%s remote server returned HTTP %d: %s\n", color.Red("Error:"), resp.StatusCode, errMsg)
		return 1
	}

	if isSync {
		var outcome agent.PipelineOutcome
		if err := json.Unmarshal(respBody, &outcome); err != nil {
			fmt.Fprintf(out, "%s failed to parse outcome response: %v\n", color.Red("Error:"), err)
			return 1
		}

		if *jsonFlag {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			_ = enc.Encode(outcome)
			if !outcome.Success {
				return 1
			}
			return 0
		}

		printSyncOutcomeTable(out, color, &outcome)
		if !outcome.Success && outcome.Decision != agent.DecisionHumanGate {
			return 1
		}
		return 0
	}

	// Async mode
	var asyncResp map[string]any
	if err := json.Unmarshal(respBody, &asyncResp); err != nil {
		fmt.Fprintf(out, "%s failed to parse server response: %v\n", color.Red("Error:"), err)
		return 1
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(asyncResp)
		return 0
	}

	runIDVal := taskID
	if rid, ok := asyncResp["run_id"].(string); ok && rid != "" {
		runIDVal = rid
	}
	statusVal := "running"
	if s, ok := asyncResp["status"].(string); ok && s != "" {
		statusVal = s
	}

	fmt.Fprintf(out, "%s Pipeline dispatched successfully\n\n", color.BadgeStatus(statusVal))
	fmt.Fprintf(out, "• Run ID:  %s\n", color.Bold(runIDVal))
	fmt.Fprintf(out, "• Title:   %s\n", title)
	if *descFlag != "" {
		fmt.Fprintf(out, "• Desc:    %s\n", *descFlag)
	}
	if len(reqPayload.Labels) > 0 {
		fmt.Fprintf(out, "• Labels:  %s\n", strings.Join(reqPayload.Labels, ", "))
	}
	fmt.Fprintf(out, "\n%s Track progress with: %s\n", color.Cyan("Tip:"), color.Bold(fmt.Sprintf("ov-cli factory status %s", runIDVal)))
	return 0
}

func printSyncOutcomeTable(out io.Writer, color *Colorizer, outcome *agent.PipelineOutcome) {
	fmt.Fprintln(out, color.Bold("═════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintln(out, color.Bold("             AUTONOMOUS FACTORY PIPELINE EXECUTION"))
	fmt.Fprintln(out, color.Bold("═════════════════════════════════════════════════════════════════════════"))

	statusBadge := color.BadgeStatus("completed")
	if outcome.Decision == agent.DecisionHumanGate {
		statusBadge = color.BadgeStatus("gate_waiting")
	} else if !outcome.Success || outcome.Decision == "FAILED" {
		statusBadge = color.BadgeStatus("failed")
	}

	fmt.Fprintf(out, "  Status:           %s\n", statusBadge)
	fmt.Fprintf(out, "  Task ID:          %s\n", color.Bold(outcome.TaskID))
	fmt.Fprintf(out, "  Decision:         %s\n", formatDecisionColor(color, string(outcome.Decision)))
	fmt.Fprintf(out, "  Total Iterations: %d\n", outcome.TotalIterations)
	fmt.Fprintf(out, "  Duration:         %s\n", formatDuration(outcome.Duration))

	// Diff summary
	if outcome.BuildResult != nil {
		diffSummary := formatDiffSummary(outcome.BuildResult)
		fmt.Fprintf(out, "  Diff Summary:     %s\n", diffSummary)
		if len(outcome.BuildResult.FilesModified) > 0 {
			fmt.Fprintln(out, "\n  Files Modified:")
			for _, file := range outcome.BuildResult.FilesModified {
				fmt.Fprintf(out, "    %s %s\n", color.Green("+"), file)
			}
		}
	} else {
		fmt.Fprintf(out, "  Diff Summary:     %s\n", color.Gray("none"))
	}

	if outcome.ErrorMessage != "" {
		fmt.Fprintf(out, "\n  %s %s\n", color.Red("Error:"), outcome.ErrorMessage)
	}

	fmt.Fprintln(out, color.Bold("─────────────────────────────────────────────────────────────────────────"))
}

func formatDiffSummary(br *agent.BuildResult) string {
	if br == nil {
		return "none"
	}
	var parts []string
	if len(br.FilesModified) > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) modified", len(br.FilesModified)))
	}
	if br.GitDiff != "" {
		lines := strings.Split(br.GitDiff, "\n")
		additions, deletions := 0, 0
		for _, line := range lines {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				additions++
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				deletions++
			}
		}
		if additions > 0 || deletions > 0 {
			parts = append(parts, fmt.Sprintf("+%d -%d diff lines", additions, deletions))
		}
	}
	if len(parts) == 0 {
		return "no diff detected"
	}
	return strings.Join(parts, ", ")
}

func formatDecisionColor(color *Colorizer, decision string) string {
	switch strings.ToUpper(strings.TrimSpace(decision)) {
	case "APPROVED":
		return color.Green(decision)
	case "HUMAN_GATE", "GATE_WAITING":
		return color.Yellow(decision)
	case "RETRY":
		return color.Cyan(decision)
	case "FAILED":
		return color.Red(decision)
	default:
		return color.Bold(decision)
	}
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

func executeFactoryStatus(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("factory status", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		jsonFlag   = fs.Bool("json", false, "Output in JSON format")
	)

	valFlags := map[string]bool{
		"server": true,
	}

	reordered := reorderArgs(args, valFlags)
	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	runID := strings.TrimSpace(fs.Arg(0))
	if runID == "" {
		fmt.Fprintf(out, "%s run-id argument is required (e.g. ov-cli factory status run-123)\n", color.Red("Error:"))
		return 2
	}

	serverURL := resolveServerURL(*serverFlag)
	url := fmt.Sprintf("%s/api/v1/factory/runs/%s", serverURL, runID)

	client := &http.Client{Timeout: 15 * time.Second}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(out, "%s failed to create HTTP request: %v\n", color.Red("Error:"), err)
		return 1
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(out, "%s remote server connection failed: %v\n", color.Red("Error:"), err)
		return 1
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(out, "%s failed to read server response: %v\n", color.Red("Error:"), err)
		return 1
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]any
		errMsg := string(bodyBytes)
		if json.Unmarshal(bodyBytes, &errData) == nil {
			if e, ok := errData["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		fmt.Fprintf(out, "%s remote server returned HTTP %d: %s\n", color.Red("Error:"), resp.StatusCode, errMsg)
		return 1
	}

	var runResp FactoryRunResponse
	if err := json.Unmarshal(bodyBytes, &runResp); err != nil {
		fmt.Fprintf(out, "%s failed to parse status response: %v\n", color.Red("Error:"), err)
		return 1
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(runResp)
		return 0
	}

	printStatusTable(out, color, &runResp)
	return 0
}

func printStatusTable(out io.Writer, color *Colorizer, resp *FactoryRunResponse) {
	fmt.Fprintln(out, color.Bold("═════════════════════════════════════════════════════════════════════════"))
	fmt.Fprintln(out, color.Bold("                 FACTORY RUN STATUS & TELEMETRY"))
	fmt.Fprintln(out, color.Bold("═════════════════════════════════════════════════════════════════════════"))

	status := resp.Status
	if resp.Outcome != nil && resp.Outcome.Decision == agent.DecisionHumanGate {
		status = "gate_waiting"
	}
	statusBadge := color.BadgeStatus(status)
	fmt.Fprintf(out, "  Run ID:           %s\n", color.Bold(resp.RunID))
	fmt.Fprintf(out, "  Status:           %s\n", statusBadge)

	durationStr := computeDurationStr(resp)
	fmt.Fprintf(out, "  Duration:         %s\n", durationStr)

	if !resp.CreatedAt.IsZero() {
		fmt.Fprintf(out, "  Created At:       %s\n", resp.CreatedAt.Format(time.RFC3339))
	}
	if !resp.UpdatedAt.IsZero() {
		fmt.Fprintf(out, "  Updated At:       %s\n", resp.UpdatedAt.Format(time.RFC3339))
	}

	if resp.Outcome != nil {
		fmt.Fprintf(out, "  Decision:         %s\n", formatDecisionColor(color, string(resp.Outcome.Decision)))
		fmt.Fprintf(out, "  Total Iterations: %d\n", resp.Outcome.TotalIterations)
		if resp.Outcome.BuildResult != nil {
			fmt.Fprintf(out, "  Diff Summary:     %s\n", formatDiffSummary(resp.Outcome.BuildResult))
			if len(resp.Outcome.BuildResult.FilesModified) > 0 {
				fmt.Fprintln(out, "\n  Files Modified:")
				for _, f := range resp.Outcome.BuildResult.FilesModified {
					fmt.Fprintf(out, "    • %s\n", color.Green(f))
				}
			}
		}
	}

	if resp.Error != "" {
		fmt.Fprintf(out, "\n  %s %s\n", color.Red("Error:"), resp.Error)
	}

	fmt.Fprintln(out, color.Bold("─────────────────────────────────────────────────────────────────────────"))
}

func computeDurationStr(resp *FactoryRunResponse) string {
	if resp.Outcome != nil && resp.Outcome.Duration > 0 {
		return formatDuration(resp.Outcome.Duration)
	}
	if !resp.CreatedAt.IsZero() && !resp.UpdatedAt.IsZero() && resp.UpdatedAt.After(resp.CreatedAt) {
		return formatDuration(resp.UpdatedAt.Sub(resp.CreatedAt))
	}
	return "n/a"
}

func executeFactoryList(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("factory list", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		limitFlag  = fs.Int("limit", 20, "Maximum number of executions to list")
		jsonFlag   = fs.Bool("json", false, "Output in JSON format")
	)

	valFlags := map[string]bool{
		"server": true,
		"limit":  true,
	}

	reordered := reorderArgs(args, valFlags)
	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	serverURL := resolveServerURL(*serverFlag)
	client := &http.Client{Timeout: 15 * time.Second}

	// Try /api/v1/factory/runs first, fallback to /api/v1/runs
	fetchRuns := func(endpoint string) ([]*FactoryRunResponse, int, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, 0, err
		}
		httpReq.Header.Set("Accept", "application/json")
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, resp.StatusCode, nil
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
		}

		var runs []*FactoryRunResponse
		if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
			return nil, resp.StatusCode, err
		}
		return runs, resp.StatusCode, nil
	}

	runs, status, err := fetchRuns(fmt.Sprintf("%s/api/v1/factory/runs", serverURL))
	if err != nil && status != http.StatusNotFound {
		fmt.Fprintf(out, "%s failed to query factory runs: %v\n", color.Red("Error:"), err)
		return 1
	}

	if status == http.StatusNotFound || runs == nil {
		// Try fallback
		runs, _, err = fetchRuns(fmt.Sprintf("%s/api/v1/runs", serverURL))
		if err != nil {
			fmt.Fprintf(out, "%s failed to list runs from server: %v\n", color.Red("Error:"), err)
			return 1
		}
	}

	if *limitFlag > 0 && len(runs) > *limitFlag {
		runs = runs[:*limitFlag]
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if runs == nil {
			runs = []*FactoryRunResponse{}
		}
		_ = enc.Encode(runs)
		return 0
	}

	if len(runs) == 0 {
		fmt.Fprintln(out, "No factory runs found.")
		return 0
	}

	fmt.Fprintln(out, color.Bold("ACTIVE AND RECENT FACTORY RUNS"))
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(out, "%-22s %-16s %-14s %-12s %s\n", "RUN ID", "STATUS", "DECISION", "DURATION", "UPDATED")
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────────")

	for _, r := range runs {
		statusBadge := color.BadgeStatus(r.Status)
		decision := "-"
		if r.Outcome != nil && r.Outcome.Decision != "" {
			decision = string(r.Outcome.Decision)
		} else if r.Error != "" {
			decision = "ERROR"
		}
		durationStr := computeDurationStr(r)
		updatedStr := "-"
		if !r.UpdatedAt.IsZero() {
			updatedStr = r.UpdatedAt.Format("15:04:05")
		} else if !r.CreatedAt.IsZero() {
			updatedStr = r.CreatedAt.Format("15:04:05")
		}

		fmt.Fprintf(out, "%-22s %-16s %-14s %-12s %s\n",
			r.RunID,
			statusBadge,
			formatDecisionColor(color, decision),
			durationStr,
			updatedStr,
		)
	}
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(out, "Total: %d run(s)\n", len(runs))

	return 0
}

// PendingGateResponse represents a pending gate item returned by ov-server.
type PendingGateResponse struct {
	RunID           string         `json:"run_id"`
	ToolName        string         `json:"tool_name,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	Token           string         `json:"token,omitempty"`
	TokenHMACPrefix string         `json:"token_hmac_prefix,omitempty"`
	Diff            string         `json:"diff,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	ExpiresAt       time.Time      `json:"expires_at,omitempty"`
}

// PendingGatesListResponse matches GET /api/v1/gates/pending.
type PendingGatesListResponse struct {
	Gates []PendingGateResponse `json:"gates"`
	Total int                   `json:"total"`
}

func fetchPendingGates(ctx context.Context, client *http.Client, serverURL string) (*PendingGatesListResponse, error) {
	url := fmt.Sprintf("%s/api/v1/gates/pending", serverURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("server connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var res PendingGatesListResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &res, nil
}

func findPendingGateToken(ctx context.Context, client *http.Client, serverURL, runID string) (string, error) {
	list, err := fetchPendingGates(ctx, client, serverURL)
	if err != nil {
		return "", err
	}
	for _, g := range list.Gates {
		if g.RunID == runID && g.Token != "" {
			return g.Token, nil
		}
	}
	return "", fmt.Errorf("no pending gate found for run %q", runID)
}

func executeFactoryGates(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("factory gates", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		jsonFlag   = fs.Bool("json", false, "Output in JSON format")
	)

	valFlags := map[string]bool{
		"server": true,
	}

	reordered := reorderArgs(args, valFlags)
	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	serverURL := resolveServerURL(*serverFlag)
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := fetchPendingGates(ctx, client, serverURL)
	if err != nil {
		fmt.Fprintf(out, "%s failed to query pending gates: %v\n", color.Red("Error:"), err)
		return 1
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
		return 0
	}

	if len(resp.Gates) == 0 {
		fmt.Fprintln(out, "No pending gates awaiting human approval.")
		return 0
	}

	fmt.Fprintln(out, color.Bold("PENDING HUMAN DECISION GATES"))
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(out, "%-22s %-16s %-26s %s\n", "RUN ID", "TOKEN PREFIX", "REASON", "EXPIRES")
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────────")

	for _, g := range resp.Gates {
		tokenPrefix := g.TokenHMACPrefix
		if tokenPrefix == "" && g.Token != "" {
			if len(g.Token) > 12 {
				tokenPrefix = g.Token[:8] + "..."
			} else {
				tokenPrefix = g.Token
			}
		}
		if tokenPrefix == "" {
			tokenPrefix = "—"
		}

		reason := g.Reason
		if reason == "" && g.Arguments != nil {
			if r, ok := g.Arguments["reason"].(string); ok {
				reason = r
			}
		}
		if reason == "" {
			reason = "Awaiting human decision"
		}
		if len(reason) > 25 {
			reason = reason[:22] + "..."
		}

		expiresStr := "—"
		if !g.ExpiresAt.IsZero() {
			expiresStr = g.ExpiresAt.Format("15:04:05")
		}

		fmt.Fprintf(out, "%-22s %-16s %-26s %s\n",
			g.RunID,
			color.Cyan(tokenPrefix),
			reason,
			expiresStr,
		)
	}
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(out, "Total: %d gate(s) awaiting approval\n", len(resp.Gates))
	fmt.Fprintln(out, "Use 'ov-cli factory approve <run_id>' or 'ov-cli factory reject <run_id>'")

	return 0
}

func executeFactoryApprove(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("factory approve", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		tokenFlag  = fs.String("token", "", "Cryptographic HMAC approval token (optional, auto-resolved if omitted)")
		jsonFlag   = fs.Bool("json", false, "Output in JSON format")
	)

	valFlags := map[string]bool{
		"server": true,
		"token":  true,
	}

	reordered := reorderArgs(args, valFlags)
	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	runID := strings.TrimSpace(fs.Arg(0))
	if runID == "" {
		fmt.Fprintf(out, "%s run-id argument is required (e.g. ov-cli factory approve <run-id>)\n", color.Red("Error:"))
		return 2
	}

	serverURL := resolveServerURL(*serverFlag)
	client := &http.Client{Timeout: 15 * time.Second}

	token := strings.TrimSpace(*tokenFlag)
	if token == "" {
		var err error
		token, err = findPendingGateToken(ctx, client, serverURL, runID)
		if err != nil {
			fmt.Fprintf(out, "%s could not locate approval token for run %q: %v\n", color.Red("Error:"), runID, err)
			return 1
		}
	}

	payload := map[string]string{
		"run_id":   runID,
		"token":    token,
		"decision": "approved",
	}
	payloadBytes, _ := json.Marshal(payload)

	resolveURL := fmt.Sprintf("%s/api/v1/gates/resolve", serverURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveURL, bytes.NewReader(payloadBytes))
	if err != nil {
		fmt.Fprintf(out, "%s failed to create request: %v\n", color.Red("Error:"), err)
		return 1
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(out, "%s failed to communicate with server: %v\n", color.Red("Error:"), err)
		return 1
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errMap map[string]any
		errMsg := string(bodyBytes)
		if json.Unmarshal(bodyBytes, &errMap) == nil {
			if e, ok := errMap["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		fmt.Fprintf(out, "%s server rejected approval (HTTP %d): %s\n", color.Red("Error:"), resp.StatusCode, errMsg)
		return 1
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"success":  true,
			"run_id":   runID,
			"decision": "approved",
			"message":  "Gate decision recorded and enqueued to sequential merge queue",
		})
		return 0
	}

	fmt.Fprintf(out, "%s Gate for run %s approved! Enqueued to sequential merge queue.\n", color.Green("✓"), color.Bold(runID))
	return 0
}

func executeFactoryReject(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("factory reject", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		tokenFlag  = fs.String("token", "", "Cryptographic HMAC approval token (optional, auto-resolved if omitted)")
		reasonFlag = fs.String("reason", "", "Optional rejection reason or feedback")
		jsonFlag   = fs.Bool("json", false, "Output in JSON format")
	)

	valFlags := map[string]bool{
		"server": true,
		"token":  true,
		"reason": true,
	}

	reordered := reorderArgs(args, valFlags)
	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	runID := strings.TrimSpace(fs.Arg(0))
	if runID == "" {
		fmt.Fprintf(out, "%s run-id argument is required (e.g. ov-cli factory reject <run-id> [--reason \"...\"])\n", color.Red("Error:"))
		return 2
	}

	serverURL := resolveServerURL(*serverFlag)
	client := &http.Client{Timeout: 15 * time.Second}

	token := strings.TrimSpace(*tokenFlag)
	if token == "" {
		var err error
		token, err = findPendingGateToken(ctx, client, serverURL, runID)
		if err != nil {
			fmt.Fprintf(out, "%s could not locate approval token for run %q: %v\n", color.Red("Error:"), runID, err)
			return 1
		}
	}

	payload := map[string]string{
		"run_id":   runID,
		"token":    token,
		"decision": "rejected",
	}
	if *reasonFlag != "" {
		payload["reason"] = *reasonFlag
	}
	payloadBytes, _ := json.Marshal(payload)

	resolveURL := fmt.Sprintf("%s/api/v1/gates/resolve", serverURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveURL, bytes.NewReader(payloadBytes))
	if err != nil {
		fmt.Fprintf(out, "%s failed to create request: %v\n", color.Red("Error:"), err)
		return 1
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(out, "%s failed to communicate with server: %v\n", color.Red("Error:"), err)
		return 1
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errMap map[string]any
		errMsg := string(bodyBytes)
		if json.Unmarshal(bodyBytes, &errMap) == nil {
			if e, ok := errMap["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		fmt.Fprintf(out, "%s server rejected gate resolution (HTTP %d): %s\n", color.Red("Error:"), resp.StatusCode, errMsg)
		return 1
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"success":  true,
			"run_id":   runID,
			"decision": "rejected",
			"reason":   *reasonFlag,
			"message":  "Gate decision recorded as rejected",
		})
		return 0
	}

	reasonSuffix := ""
	if *reasonFlag != "" {
		reasonSuffix = fmt.Sprintf(" (Reason: %s)", *reasonFlag)
	}
	fmt.Fprintf(out, "%s Gate for run %s rejected.%s\n", color.Red("✕"), color.Bold(runID), reasonSuffix)
	return 0
}
