// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCLI_GeneralAndHelp(t *testing.T) {
	ctx := context.Background()

	t.Run("version command returns 0", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"version"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !strings.Contains(out.String(), "ov-cli version") {
			t.Errorf("expected version string, got %q", out.String())
		}
	})

	t.Run("help command returns 0", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"help"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !strings.Contains(out.String(), "Autonomous Software Factory Platform") {
			t.Errorf("expected manual banner, got %q", out.String())
		}
	})

	t.Run("empty args returns exit code 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
	})

	t.Run("unknown command returns exit code 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"unknown-cmd"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
		if !strings.Contains(errBuf.String(), "unknown command") {
			t.Errorf("expected error message in stderr, got %q", errBuf.String())
		}
	})
}

func TestCLI_TemplatesCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("templates list table format", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"templates", "list"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		output := out.String()
		if !strings.Contains(output, "bugfix-express") || !strings.Contains(output, "feature-complete") {
			t.Errorf("expected built-in template names in table, got %q", output)
		}
	})

	t.Run("templates list JSON format", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"templates", "list", "--json"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}

		var list []TemplateSummary
		if err := json.Unmarshal(out.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode JSON template summaries: %v", err)
		}
		if len(list) < 3 {
			t.Errorf("expected at least 3 templates, got %d", len(list))
		}
	})
}
func TestCLI_StatusCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("status with runID returns 0", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"status", "run-sample-101"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		output := out.String()
		if !strings.Contains(output, "run-sample-101") || !strings.Contains(output, "DAG Execution Steps:") {
			t.Errorf("expected run status output, got %q", output)
		}
	})

	t.Run("status with runID JSON format", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"status", "--json", "run-sample-101"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}

		var details RunStatusDetails
		if err := json.Unmarshal(out.Bytes(), &details); err != nil {
			t.Fatalf("failed to decode JSON status: %v", err)
		}
		if details.RunID != "run-sample-101" {
			t.Errorf("expected runID run-sample-101, got %s", details.RunID)
		}
		if len(details.Nodes) == 0 {
			t.Error("expected non-empty DAG nodes list")
		}
	})

	t.Run("status missing runID returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"status"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
	})
}

func TestCLI_MemoryCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("memory query table format", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "auth", "token"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		output := out.String()
		if !strings.Contains(output, "Semantic query:") || !strings.Contains(output, "File Path") {
			t.Errorf("expected table header in memory output, got %q", output)
		}
	})

	t.Run("memory query JSON format with limit", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query", "--limit", "2", "--json", "gate"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}

		var results []MemoryQueryResult
		if err := json.Unmarshal(out.Bytes(), &results); err != nil {
			t.Fatalf("failed to parse memory JSON output: %v", err)
		}
		if len(results) > 2 {
			t.Errorf("expected at most 2 results due to limit, got %d", len(results))
		}
	})

	t.Run("memory query missing query text returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"memory", "query"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
	})
}


func TestCLI_RunCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("run with builtin template and wait stream", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"run", "--template", "bugfix-express", "--wait", "--issue", "42"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. Output: %s", code, out.String())
		}
		output := out.String()
		if !strings.Contains(output, "Factory Pipeline Completed Successfully!") {
			t.Errorf("expected success banner, got %q", output)
		}
		if !strings.Contains(output, "Step 1/") {
			t.Errorf("expected streamed step progress, got %q", output)
		}
	})

	t.Run("run with remote server URL", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"run", "--template", "bugfix-express", "--server", "http://localhost:8080"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		output := out.String()
		if !strings.Contains(output, "Run submitted to http://localhost:8080 successfully") {
			t.Errorf("expected remote confirmation, got %q", output)
		}
	})

	t.Run("run with non-existent template returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(ctx, []string{"run", "--template", "does-not-exist-at-all"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
	})
}

func TestCLI_WorkerCommand(t *testing.T) {
	t.Run("worker start handles graceful shutdown via context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)

		// Trigger shutdown after 100ms
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		code := app.Run(ctx, []string{"worker", "start", "--concurrency", "2", "--tags", "wasm,docker"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		output := out.String()
		if !strings.Contains(output, "Worker daemon shut down gracefully") {
			t.Errorf("expected graceful shutdown message, got %q", output)
		}
	})

	t.Run("worker unknown subcommand returns 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		app := NewCLIApp(&out, &errBuf, nil)
		code := app.Run(context.Background(), []string{"worker", "unknown-action"})
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d", code)
		}
	})
}

func TestColorizer(t *testing.T) {
	c := &Colorizer{disabled: false}
	if !strings.Contains(c.Green("ok"), "ok") {
		t.Error("expected colorized text")
	}

	badge := c.BadgeStatus("completed")
	if !strings.Contains(badge, "COMPLETED") {
		t.Errorf("expected badge to contain COMPLETED, got %s", badge)
	}

	cDisabled := &Colorizer{disabled: true}
	if cDisabled.Green("plain") != "plain" {
		t.Errorf("expected plain text when disabled, got %s", cDisabled.Green("plain"))
	}
}

