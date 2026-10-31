// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-alpha"

func main() {
	runCmd := flag.NewFlagSet("run", flag.ExitOnError)
	templatePath := runCmd.String("template", "templates/factories/default-tdd-factory.yaml", "Path to factory template")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("ov-cli version %s\n", version)
	case "run":
		_ = runCmd.Parse(os.Args[2:])
		fmt.Printf("Triggering factory run using template: %s\n", *templatePath)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: ov-cli <command> [arguments]")
	fmt.Println("Commands:")
	fmt.Println("  version    Display current CLI version")
	fmt.Println("  run        Execute a software factory pipeline locally")
}
