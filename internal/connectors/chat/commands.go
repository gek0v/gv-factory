// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package chat

import (
	"strings"
)

// ParseSlashCommand parses a raw command and text into a structured SlashCommand.
func ParseSlashCommand(platform, cmdText, rawText, userID, userName, channelID, responseURL string) (*SlashCommand, error) {
	cmdText = strings.TrimSpace(cmdText)
	if cmdText == "" {
		cmdText = "/factory"
	}

	parts := strings.Fields(rawText)
	if len(parts) == 0 {
		return &SlashCommand{
			Platform:    platform,
			Command:     cmdText,
			Subcommand:  "help",
			UserID:      userID,
			UserName:    userName,
			ChannelID:   channelID,
			ResponseURL: responseURL,
		}, nil
	}

	sub := strings.ToLower(parts[0])
	cmd := &SlashCommand{
		Platform:    platform,
		Command:     cmdText,
		Subcommand:  sub,
		Arguments:   parts[1:],
		Parameters:  make(map[string]string),
		UserID:      userID,
		UserName:    userName,
		ChannelID:   channelID,
		ResponseURL: responseURL,
	}

	switch sub {
	case "run":
		// Syntax: run <template> [--issue <n>] [--branch <b>] [--param k=v]
		var nonFlagArgs []string
		for i := 1; i < len(parts); i++ {
			p := parts[i]
			switch {
			case p == "--issue" || p == "-i":
				if i+1 < len(parts) {
					i++
					cmd.Issue = parts[i]
				}
			case strings.HasPrefix(p, "--issue="):
				cmd.Issue = strings.TrimPrefix(p, "--issue=")
			case p == "--branch" || p == "-b":
				if i+1 < len(parts) {
					i++
					cmd.Branch = parts[i]
				}
			case strings.HasPrefix(p, "--branch="):
				cmd.Branch = strings.TrimPrefix(p, "--branch=")
			case p == "--param" || p == "-p":
				if i+1 < len(parts) {
					i++
					k, v, found := strings.Cut(parts[i], "=")
					if found {
						cmd.Parameters[k] = v
					}
				}
			case strings.HasPrefix(p, "--param="):
				k, v, found := strings.Cut(strings.TrimPrefix(p, "--param="), "=")
				if found {
					cmd.Parameters[k] = v
				}
			case strings.HasPrefix(p, "-"):
				// Unknown flag, keep as argument
				nonFlagArgs = append(nonFlagArgs, p)
			default:
				nonFlagArgs = append(nonFlagArgs, p)
			}
		}
		if len(nonFlagArgs) > 0 {
			cmd.Template = nonFlagArgs[0]
		}
		if cmd.Template == "" {
			cmd.Template = "bugfix-express"
		}

	case "status":
		// Syntax: status <run-id>
		if len(parts) > 1 {
			cmd.RunID = parts[1]
		}

	case "help":
		// default help
	default:
		// treat unknown as sub
	}

	return cmd, nil
}

// HelpMessage returns a formatted help text for slash commands.
func HelpMessage() string {
	return "Available commands:\n" +
		"• `/factory run <template> [--issue <n>] [--branch <b>] [--param k=v]` - Trigger software factory pipeline\n" +
		"• `/factory status <run-id>` - Check real-time status of a factory run\n" +
		"• `/factory help` - Show this usage guide"
}
