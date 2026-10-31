// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package pdk

import "testing"

func TestPDK_Smoke(t *testing.T) {
	ctx := Context{
		RunID: "run-1",
	}
	if ctx.RunID != "run-1" {
		t.Fatalf("unexpected run ID: %s", ctx.RunID)
	}
}
