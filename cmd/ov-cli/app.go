// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

var (
	version = "0.2.0-beta"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

// CLIApp encapsulates CLI I/O streams and command routing.
type CLIApp struct {
	Out   io.Writer
	Err   io.Writer
	In    io.Reader
	Color *Colorizer
}

// NewCLIApp constructs an initialized CLIApp.
func NewCLIApp(out, err io.Writer, in io.Reader) *CLIApp {
	if out == nil {
		out = os.Stdout
	}
	if err == nil {
		err = os.Stderr
	}
	if in == nil {
		in = os.Stdin
	}
	return &CLIApp{
		Out:   out,
		Err:   err,
		In:    in,
		Color: NewColorizer(),
	}
}

// Run executes the command line application and returns an exit code.
func (app *CLIApp) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		app.PrintUsage()
		return 2
	}

	command := args[0]
	cmdArgs := args[1:]

	switch command {
	case "version", "--version", "-v":
		fmt.Fprintf(app.Out, "ov-cli version %s (commit: %s, date: %s, built by: %s)\n", version, commit, date, builtBy)
		return 0

	case "help", "--help", "-h":
		app.PrintUsage()
		return 0

	case "run":
		return ExecuteRunCommand(ctx, cmdArgs, app.Out, app.Color)

	case "memory":
		return ExecuteMemoryCommand(ctx, cmdArgs, app.Out, app.Color)

	case "worker":
		return ExecuteWorkerCommand(ctx, cmdArgs, app.Out, app.Color)

	case "status":
		return ExecuteStatusCommand(ctx, cmdArgs, app.Out, app.Color)

	case "templates":
		return ExecuteTemplatesCommand(ctx, cmdArgs, app.Out, app.Color)

	default:
		fmt.Fprintf(app.Err, "%s unknown command %q\n\n", app.Color.Red("Error:"), command)
		app.PrintUsage()
		return 2
	}
}

// PrintUsage prints the root help manual.
func (app *CLIApp) PrintUsage() {
	fmt.Fprintln(app.Out, "OV-Factory Developer CLI - Autonomous Software Factory Platform")
	fmt.Fprintln(app.Out, "\nUsage:")
	fmt.Fprintln(app.Out, "  ov-cli <command> [subcommand] [flags] [arguments]")
	fmt.Fprintln(app.Out, "\nCommands:")
	fmt.Fprintln(app.Out, "  run          Execute a software factory pipeline locally or remotely")
	fmt.Fprintln(app.Out, "  memory       Query Codebase Brain semantic memory and AST symbols")
	fmt.Fprintln(app.Out, "  worker       Start a persistent local sandbox worker daemon")
	fmt.Fprintln(app.Out, "  status       Inspect real-time DAG node status and logs for a run")
	fmt.Fprintln(app.Out, "  templates    List available built-in software factory templates")
	fmt.Fprintln(app.Out, "  version      Display current CLI version")
	fmt.Fprintln(app.Out, "  help         Show this usage documentation")
	fmt.Fprintln(app.Out, "\nUse 'ov-cli <command> --help' for details on a specific command.")
}
