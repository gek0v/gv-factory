// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// NewHealthMux creates the HTTP multiplexer for ov-worker health checks.
func NewHealthMux() http.Handler {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"service": "ov-worker",
		})
	}
	mux.HandleFunc("/healthz", handler)
	mux.HandleFunc("/health", handler)
	return mux
}

func main() {
	worktreeDir := flag.String("worktree-dir", "", "Base directory for sandbox git worktrees (defaults to OS temp directory or OV_WORKTREE_DIR)")
	natsURL := flag.String("nats-url", "", "NATS server URL (defaults to nats://localhost:4222 or in-memory fallback)")
	healthPortFlag := flag.String("health-port", "", "Health check HTTP listen port (defaults to HEALTH_PORT env var or 8081)")
	flag.Parse()

	healthPort := *healthPortFlag
	if healthPort == "" {
		healthPort = os.Getenv("HEALTH_PORT")
	}
	if healthPort == "" {
		healthPort = "8081"
	}

	actualWorktreeDir := *worktreeDir
	if actualWorktreeDir == "" {
		actualWorktreeDir = os.Getenv("OV_WORKTREE_DIR")
	}

	actualNatsURL := *natsURL
	if actualNatsURL == "" {
		actualNatsURL = os.Getenv("NATS_URL")
	}

	fmt.Println("Starting ov-worker (Hatchet Execution Agent & Sandbox Runner)...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	worker := NewWorker(WorkerConfig{
		BaseWorktreeDir: actualWorktreeDir,
		NATSURL:         actualNatsURL,
	})
	defer worker.Close()

	// Launch health check server for container orchestration
	healthServer := &http.Server{
		Addr:              ":" + healthPort,
		Handler:           NewHealthMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("ov-worker health endpoint listening on :%s", healthPort)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("ov-worker health server error: %v", err)
		}
	}()

	log.Println("ov-worker ready to process tasks from queue...")

	<-ctx.Done()
	log.Println("Shutting down ov-worker gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during ov-worker health server shutdown: %v", err)
	}
}


