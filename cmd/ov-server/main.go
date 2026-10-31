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

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

func main() {
	fmt.Println("Starting ov-server (Orchestrator & API Server)...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	_ = brain.NewService()
	_ = dag.NewEngine()
	_ = sandbox.NewManager(sandbox.Config{BaseWorktreeDir: "/tmp/ov-worktrees"})
	_ = plugins.NewHost()
	_ = gateway.NewGateway()
	_ = auth.NewService("secret-key")

	log.Println("ov-server initialized successfully. Listening for orchestrations...")

	<-ctx.Done()
	log.Println("Shutting down ov-server gracefully...")
}
