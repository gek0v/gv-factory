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

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/dag"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

func main() {
	worktreeDir := flag.String("worktree-dir", "", "Base directory for sandbox git worktrees (defaults to OS temp directory or OV_WORKTREE_DIR)")
	flag.Parse()

	fmt.Println("Starting ov-server (Orchestrator & API Server)...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	brainSvc, err := brain.NewService(ctx, brain.ConfigFromEnv())
	if err != nil {
		log.Printf("Warning: brain service failed to configure: %v", err)
	} else {
		defer brainSvc.Close()
		if pingErr := brainSvc.Ping(ctx); pingErr != nil {
			log.Printf("Warning: PostgreSQL (brain) unreachable at startup (degraded): %v", pingErr)
		} else {
			log.Println("PostgreSQL (brain) connected and healthy.")
		}
	}

	_ = dag.NewEngine()
	_ = sandbox.NewManager(sandbox.Config{BaseWorktreeDir: *worktreeDir})
	_ = plugins.NewHost()
	gw := gateway.NewGateway()
	_ = auth.NewService("secret-key")

	webhookIngestor := inbound.NewIngestor(inbound.Config{
		GitHubSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		GitLabSecret: os.Getenv("GITLAB_WEBHOOK_SECRET"),
		SentrySecret: os.Getenv("SENTRY_WEBHOOK_SECRET"),
	}, inbound.NewGatewayDispatcher(gw))
	_ = webhookIngestor

	log.Println("ov-server initialized successfully. Listening for orchestrations...")

	<-ctx.Done()
	log.Println("Shutting down ov-server gracefully...")
}
