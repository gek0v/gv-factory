// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package version

import (
	"testing"
)

func TestInfoDefault(t *testing.T) {
	expected := "dev (commit: none, date: unknown, built by: unknown)"
	if got := Info(); got != expected {
		t.Fatalf("expected Info() = %q, got %q", expected, got)
	}
}

func TestInfoCustom(t *testing.T) {
	origV, origC, origD, origB := Version, Commit, Date, BuiltBy
	defer func() {
		Version, Commit, Date, BuiltBy = origV, origC, origD, origB
	}()

	Version = "1.0.0"
	Commit = "abcdef123"
	Date = "2026-11-01T12:00:00Z"
	BuiltBy = "goreleaser"

	expected := "1.0.0 (commit: abcdef123, date: 2026-11-01T12:00:00Z, built by: goreleaser)"
	if got := Info(); got != expected {
		t.Fatalf("expected Info() = %q, got %q", expected, got)
	}
}
