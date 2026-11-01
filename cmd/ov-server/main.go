// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/connectors/outbound"
	"github.com/gv-factory/gv-factory/internal/dag"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	"github.com/gv-factory/gv-factory/internal/telemetry"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func main() {
	portFlag := flag.String("port", "", "HTTP listen port (defaults to PORT env var or 8080)")
	worktreeDir := flag.String("worktree-dir", "", "Base directory for sandbox git worktrees (defaults to OS temp directory or OV_WORKTREE_DIR)")
	flag.Parse()

	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting ov-server version %s (commit: %s, date: %s, built by: %s)...", version, commit, date, builtBy)
	fmt.Printf("Starting ov-server (Orchestrator & API Server) on port %s...\n", port)

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

	outboundDispatcher := outbound.NewDefaultDispatcher()
	outboundDispatcher.Start(ctx)
	defer func() {
		_ = outboundDispatcher.Stop()
	}()
	_, _ = outboundDispatcher.AttachToGateway(ctx, gw)
	_ = outboundDispatcher

	chatMgr := chat.NewManager()
	slackAdapter := chat.NewSlackAdapter(chat.SlackConfig{
		BotToken:       os.Getenv("SLACK_BOT_TOKEN"),
		SigningSecret:  os.Getenv("SLACK_SIGNING_SECRET"),
		DefaultChannel: os.Getenv("SLACK_DEFAULT_CHANNEL"),
	})
	chatMgr.Register(slackAdapter)

	discordAdapter := chat.NewDiscordAdapter(chat.DiscordConfig{
		BotToken:         os.Getenv("DISCORD_BOT_TOKEN"),
		PublicKey:        os.Getenv("DISCORD_PUBLIC_KEY"),
		DefaultChannelID: os.Getenv("DISCORD_DEFAULT_CHANNEL"),
	})
	chatMgr.Register(discordAdapter)

	chatHandler := chat.NewChatHandler(chat.HandlerConfig{
		SlackSigningSecret: os.Getenv("SLACK_SIGNING_SECRET"),
		DiscordPublicKey:   os.Getenv("DISCORD_PUBLIC_KEY"),
		SlackAdapter:       slackAdapter,
		DiscordAdapter:     discordAdapter,
	})
	_ = chatHandler

	unsubChat, _ := chatMgr.AttachToGateway(ctx, gw, os.Getenv("SLACK_DEFAULT_CHANNEL"))
	if unsubChat != nil {
		defer unsubChat()
	}

	tel, err := telemetry.New(ctx, telemetry.ConfigFromEnv())
	if err != nil {
		log.Printf("Warning: telemetry provider failed to configure: %v", err)
	} else {
		log.Printf("Telemetry subsystem initialized (status: %s)", tel.Status())
	}

	mux := NewServerMux(brainSvc, gw, webhookIngestor, chatHandler, tel)

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("ov-server HTTP server listening on :%s", port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("ov-server HTTP server error: %v", err)
		}
	}()

	log.Printf("ov-server initialized successfully on port %s. Listening for orchestrations...", port)

	<-ctx.Done()
	log.Println("Shutting down ov-server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during ov-server HTTP shutdown: %v", err)
	}
	if tel != nil {
		if err := tel.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error during telemetry shutdown: %v", err)
		}
	}
}
