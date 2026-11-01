// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"fmt"
	"os"
)

// ANSI color escape codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
	colorBold    = "\033[1m"
)

// Colorizer formats text with ANSI color codes unless NO_COLOR is set.
type Colorizer struct {
	disabled bool
}

// NewColorizer creates a Colorizer respecting the NO_COLOR standard.
func NewColorizer() *Colorizer {
	_, noColor := os.LookupEnv("NO_COLOR")
	return &Colorizer{disabled: noColor}
}

// Colorize wraps text with the given ANSI code if colors are enabled.
func (c *Colorizer) Colorize(code, text string) string {
	if c.disabled {
		return text
	}
	return code + text + colorReset
}

func (c *Colorizer) Red(text string) string     { return c.Colorize(colorRed, text) }
func (c *Colorizer) Green(text string) string   { return c.Colorize(colorGreen, text) }
func (c *Colorizer) Yellow(text string) string  { return c.Colorize(colorYellow, text) }
func (c *Colorizer) Blue(text string) string    { return c.Colorize(colorBlue, text) }
func (c *Colorizer) Cyan(text string) string    { return c.Colorize(colorCyan, text) }
func (c *Colorizer) Gray(text string) string    { return c.Colorize(colorGray, text) }
func (c *Colorizer) Bold(text string) string    { return c.Colorize(colorBold, text) }

// BadgeStatus returns a stylized, colorized status badge.
func (c *Colorizer) BadgeStatus(status string) string {
	switch status {
	case "pending":
		return c.Yellow("[ PENDING   ]")
	case "running":
		return c.Cyan("[ RUNNING   ]")
	case "completed", "success":
		return c.Green("[ COMPLETED ]")
	case "failed", "error":
		return c.Red("[ FAILED    ]")
	default:
		return fmt.Sprintf("[%s]", status)
	}
}
