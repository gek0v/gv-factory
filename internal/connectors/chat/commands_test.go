// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"strings"
	"testing"
)

func TestCommands_ParseSlashCommand(t *testing.T) {
	t.Run("Empty rawText defaults to help", func(t *testing.T) {
		cmd, err := ParseSlashCommand("slack", "", "", "U1", "user1", "C1", "https://resp.local")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Command != "/factory" {
			t.Errorf("expected default command /factory, got %s", cmd.Command)
		}
		if cmd.Subcommand != "help" {
			t.Errorf("expected subcommand help, got %s", cmd.Subcommand)
		}
	})

	t.Run("Parse run with flags", func(t *testing.T) {
		raw := "run feature-custom --issue 10 --branch feat/x -p key1=val1 --param=key2=val2 -i 20 -b fix/y"
		cmd, err := ParseSlashCommand("slack", "/factory", raw, "U1", "user1", "C1", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Subcommand != "run" {
			t.Errorf("expected subcommand run, got %s", cmd.Subcommand)
		}
		if cmd.Template != "feature-custom" {
			t.Errorf("expected template feature-custom, got %s", cmd.Template)
		}
		if cmd.Issue != "20" {
			t.Errorf("expected issue 20, got %s", cmd.Issue)
		}
		if cmd.Branch != "fix/y" {
			t.Errorf("expected branch fix/y, got %s", cmd.Branch)
		}
		if cmd.Parameters["key1"] != "val1" || cmd.Parameters["key2"] != "val2" {
			t.Errorf("expected parameters, got %v", cmd.Parameters)
		}
	})

	t.Run("Parse run with equals flags and default template", func(t *testing.T) {
		raw := "run --issue=99 --branch=main --unknown-flag"
		cmd, err := ParseSlashCommand("discord", "/factory", raw, "U2", "user2", "C2", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Template != "bugfix-express" {
			t.Errorf("expected default template bugfix-express, got %s", cmd.Template)
		}
		if cmd.Issue != "99" {
			t.Errorf("expected issue 99, got %s", cmd.Issue)
		}
		if cmd.Branch != "main" {
			t.Errorf("expected branch main, got %s", cmd.Branch)
		}
	})

	t.Run("Parse status with runID", func(t *testing.T) {
		cmd, err := ParseSlashCommand("slack", "/factory", "status run-12345", "U3", "user3", "C3", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Subcommand != "status" {
			t.Errorf("expected subcommand status, got %s", cmd.Subcommand)
		}
		if cmd.RunID != "run-12345" {
			t.Errorf("expected runID run-12345, got %s", cmd.RunID)
		}
	})

	t.Run("Parse unknown command", func(t *testing.T) {
		cmd, err := ParseSlashCommand("slack", "/factory", "inspect something", "U4", "user4", "C4", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Subcommand != "inspect" {
			t.Errorf("expected subcommand inspect, got %s", cmd.Subcommand)
		}
	})

	t.Run("HelpMessage contains commands", func(t *testing.T) {
		msg := HelpMessage()
		if !strings.Contains(msg, "/factory run") || !strings.Contains(msg, "/factory status") {
			t.Errorf("unexpected help message: %s", msg)
		}
	})
}
