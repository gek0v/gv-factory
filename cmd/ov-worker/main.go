// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gv-factory/gv-factory/internal/sandbox"
)

func main() {
	fmt.Println("Starting ov-worker (Hatchet Execution Agent & Sandbox Runner)...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sbManager := sandbox.NewManager(sandbox.Config{
		BaseWorktreeDir: "/tmp/ov-worktrees",
	})
	_ = sbManager

	log.Println("ov-worker ready to process tasks from queue...")

	<-ctx.Done()
	log.Println("Shutting down ov-worker gracefully...")
}
