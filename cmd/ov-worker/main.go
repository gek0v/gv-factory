// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	worktreeDir := flag.String("worktree-dir", "", "Base directory for sandbox git worktrees (defaults to OS temp directory or OV_WORKTREE_DIR)")
	natsURL := flag.String("nats-url", "", "NATS server URL (defaults to nats://localhost:4222 or in-memory fallback)")
	flag.Parse()

	fmt.Println("Starting ov-worker (Hatchet Execution Agent & Sandbox Runner)...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	worker := NewWorker(WorkerConfig{
		BaseWorktreeDir: *worktreeDir,
		NATSURL:         *natsURL,
	})
	defer worker.Close()

	log.Println("ov-worker ready to process tasks from queue...")

	<-ctx.Done()
	log.Println("Shutting down ov-worker gracefully...")
}

