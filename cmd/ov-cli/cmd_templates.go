// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

// ExecuteTemplatesCommand handles `ov-cli templates [subcommand]` (e.g. `ov-cli templates list`).
func ExecuteTemplatesCommand(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	subcommand := "list"
	subArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		subArgs = args[1:]
	}

	switch subcommand {
	case "list":
		return executeTemplatesList(ctx, subArgs, out, color)
	default:
		fmt.Fprintf(out, "%s unknown templates subcommand %q (available: list)\n", color.Red("Error:"), subcommand)
		return 2
	}
}

func executeTemplatesList(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("templates list", flag.ContinueOnError)
	fs.SetOutput(out)

	var jsonFlag = fs.Bool("json", false, "Output templates in JSON format")

	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: ov-cli templates list [flags]")
		fmt.Fprintln(out, "\nList available built-in software factory templates.")
		fmt.Fprintln(out, "\nFlags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	templates := ListTemplates()

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(templates)
		return 0
	}

	fmt.Fprintln(out, "Available Factory Templates:")
	fmt.Fprintf(out, "| %-18s | %-8s | %-22s | %-5s | %-50s |\n", "Template", "Version", "Entrypoint", "Steps", "Description")
	fmt.Fprintf(out, "|%s|%s|%s|%s|%s|\n",
		strings.Repeat("-", 20),
		strings.Repeat("-", 10),
		strings.Repeat("-", 24),
		strings.Repeat("-", 7),
		strings.Repeat("-", 52))

	for _, tpl := range templates {
		desc := tpl.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(out, "| %-18s | %-8s | %-22s | %-5d | %-50s |\n",
			color.Bold(tpl.Name), tpl.Version, tpl.Entrypoint, tpl.StepsCount, desc)
	}
	fmt.Fprintln(out)

	return 0
}
