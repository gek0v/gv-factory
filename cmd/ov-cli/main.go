// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"os"
)

func main() {
	ctx := context.Background()
	app := NewCLIApp(os.Stdout, os.Stderr, os.Stdin)
	exitCode := app.Run(ctx, os.Args[1:])
	os.Exit(exitCode)
}

