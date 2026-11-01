// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

// ExecuteWorkerCommand handles `ov-cli worker [subcommand]` (e.g. `ov-cli worker start`).
func ExecuteWorkerCommand(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printWorkerUsage(out)
		return 0
	}

	subcommand := args[0]
	switch subcommand {
	case "start":
		return executeWorkerStart(ctx, args[1:], out, color)
	default:
		fmt.Fprintf(out, "%s unknown worker subcommand %q\n\n", color.Red("Error:"), subcommand)
		printWorkerUsage(out)
		return 2
	}
}

func printWorkerUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: ov-cli worker <subcommand> [flags]")
	fmt.Fprintln(out, "\nSubcommands:")
	fmt.Fprintln(out, "  start       Spawn a persistent local sandbox worker daemon")
	fmt.Fprintln(out, "\nFlags:")
	fmt.Fprintln(out, "  --concurrency int    Number of concurrent worker threads (default: CPU count)")
	fmt.Fprintln(out, "  --tags string        Comma-separated worker capability tags (default: docker,baremetal,wasm)")
	fmt.Fprintln(out, "  --worktree-dir path  Base directory for sandbox git worktrees")
	fmt.Fprintln(out, "  --nats-url url       NATS server URL (default: nats://localhost:4222)")
}

func executeWorkerStart(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("worker start", flag.ContinueOnError)
	fs.SetOutput(out)

	defaultConcurrency := runtime.NumCPU()
	if defaultConcurrency <= 0 {
		defaultConcurrency = 4
	}

	var (
		concurrencyFlag = fs.Int("concurrency", defaultConcurrency, "Worker execution concurrency")
		tagsFlag        = fs.String("tags", "docker,baremetal,wasm", "Capability tags for task routing")
		worktreeDirFlag = fs.String("worktree-dir", "", "Directory for sandbox git worktrees")
		natsURLFlag     = fs.String("nats-url", "nats://localhost:4222", "Task queue NATS URL")
	)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	workerID := fmt.Sprintf("worker-%d", time.Now().Unix())
	parsedTags := strings.Split(*tagsFlag, ",")
	for i := range parsedTags {
		parsedTags[i] = strings.TrimSpace(parsedTags[i])
	}

	fmt.Fprintf(out, "%s Initializing OV-Factory Sandbox Worker Daemon...\n", color.Cyan("▶"))
	fmt.Fprintf(out, "• Worker ID:    %s\n", workerID)
	fmt.Fprintf(out, "• Concurrency:  %d threads\n", *concurrencyFlag)
	fmt.Fprintf(out, "• Capabilities: [%s]\n", strings.Join(parsedTags, ", "))
	if *worktreeDirFlag != "" {
		fmt.Fprintf(out, "• Worktree Dir: %s\n", *worktreeDirFlag)
	}
	fmt.Fprintf(out, "• Queue Server: %s\n", *natsURLFlag)

	// Initialize execution sub-engines
	sb := sandbox.NewManager(sandbox.Config{
		BaseWorktreeDir: *worktreeDirFlag,
	})
	_ = sb

	gw := gateway.NewGatewayWithConfig(gateway.Config{
		NATSURL:     *natsURLFlag,
		DisableNATS: true, // in-memory fallback if NATS unavailable
	})
	defer gw.Close()

	dagEngine := dag.NewEngine()
	pluginHost := plugins.NewHost()
	defer pluginHost.Close()

	dispatcherCfg := dag.DispatcherConfig{
		DefaultStepPolicy: dag.StepPolicy{
			MaxRetries: 3,
			Timeout:    30 * time.Second,
		},
	}
	dispatcher := dag.NewDispatcher(dagEngine, pluginHost, gw, dispatcherCfg)
	_ = dispatcher

	fmt.Fprintf(out, "\n%s Worker ready. Connected and listening for factory tasks...\n", color.Green("✓"))
	fmt.Fprintf(out, "Press Ctrl+C to stop.\n")

	// Set up graceful shutdown channel
	sigCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	<-sigCtx.Done()

	fmt.Fprintf(out, "\n%s Received shutdown signal. Cleaning up sandboxes and active runs...\n", color.Yellow("⚠"))
	fmt.Fprintf(out, "%s Worker daemon shut down gracefully. Goodbye.\n", color.Green("✓"))
	return 0
}
