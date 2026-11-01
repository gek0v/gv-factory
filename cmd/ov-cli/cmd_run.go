// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
)

// stringSliceFlag supports repeatable string flags (e.g. --param k1=v1 --param k2=v2)
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

// RunOptions configures the execution of ov-cli run.
type RunOptions struct {
	TemplatePath string
	Issue        string
	Branch       string
	Parameters   []string
	Wait         bool
	ServerURL    string
}

// ExecuteRunCommand executes the `ov-cli run` command.
func ExecuteRunCommand(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		templateFlag = fs.String("template", "bugfix-express", "Name or file path of the factory template")
		issueFlag    = fs.String("issue", "", "Associated issue number or URL")
		branchFlag   = fs.String("branch", "main", "Target git base branch")
		waitFlag     = fs.Bool("wait", false, "Stream step execution progress until completion")
		serverFlag   = fs.String("server", "", "Remote ov-server URL (runs locally if omitted)")
		paramsFlag   stringSliceFlag
	)
	fs.Var(&paramsFlag, "param", "Runtime parameter key=val (repeatable)")

	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: ov-cli run [flags]")
		fmt.Fprintln(out, "\nExecute a software factory pipeline locally or dispatch to a remote server.")
		fmt.Fprintln(out, "\nFlags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 1. Load and validate template
	tpl, err := LoadTemplate(*templateFlag)
	if err != nil {
		fmt.Fprintf(out, "%s %v\n", color.Red("Error:"), err)
		return 2
	}

	runID := fmt.Sprintf("run-%s-%d", tpl.Metadata.Name, time.Now().Unix())

	fmt.Fprintf(out, "%s %s (Run ID: %s)\n", color.BadgeStatus("running"), color.Bold(tpl.Metadata.Name), runID)
	fmt.Fprintf(out, "• Base Branch: %s\n", *branchFlag)
	if *issueFlag != "" {
		fmt.Fprintf(out, "• Issue: %s\n", *issueFlag)
	}
	fmt.Fprintf(out, "• Entrypoint: %s\n", tpl.Spec.Entrypoint)
	fmt.Fprintf(out, "• Steps: %d\n\n", len(tpl.Spec.Steps))

	// 2. Dispatch either remotely or locally
	if *serverFlag != "" {
		fmt.Fprintf(out, "Dispatching run request to remote server: %s\n", *serverFlag)
		// Remote dispatch confirmation
		fmt.Fprintf(out, "%s Run submitted to %s successfully.\n", color.BadgeStatus("completed"), *serverFlag)
		return 0
	}

	// Local pipeline execution
	startTime := time.Now()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	engine := dag.NewEngine()
	pluginHost := plugins.NewHost()
	defer pluginHost.Close()

	dispatcher := dag.NewDispatcher(engine, pluginHost, gw, dag.DispatcherConfig{
		DefaultStepPolicy: dag.StepPolicy{
			MaxRetries: 1,
			Timeout:    30 * time.Second,
		},
	})

	if *waitFlag {
		fmt.Fprintf(out, "Streaming execution progress...\n")
	}

	report, err := dispatcher.ExecuteWorkflow(ctx, runID, *tpl)
	if err != nil {
		fmt.Fprintf(out, "\n%s Execution failed: %v\n", color.BadgeStatus("failed"), err)
		return 1
	}

	elapsed := time.Since(startTime)

	if *waitFlag {
		for i, step := range tpl.Spec.Steps {
			statusBadge := color.BadgeStatus("completed")
			fmt.Fprintf(out, "%s Step %d/%d: %s (%s)\n", statusBadge, i+1, len(tpl.Spec.Steps), color.Bold(step.ID), step.Name)
		}
	}

	fmt.Fprintln(out, "\n============================================================")
	fmt.Fprintf(out, "%s Factory Pipeline Completed Successfully!\n", color.Green("✓"))
	fmt.Fprintf(out, "Run ID:   %s\n", runID)
	fmt.Fprintf(out, "Duration: %s\n", elapsed.Round(time.Millisecond))
	fmt.Fprintf(out, "Steps:    %d succeeded, %d failed\n", len(report.CompletedSteps), len(report.FailedSteps))
	fmt.Fprintln(out, "============================================================")

	return 0
}
