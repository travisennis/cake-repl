package app

import (
	"fmt"
	"regexp"
	"strings"
)

// CommandKind identifies a slash command.
type CommandKind int

const (
	CmdHelp CommandKind = iota
	CmdExit
	CmdNew
	CmdContinue
	CmdResume
	CmdSession
	CmdClear
)

// Command is one parsed slash command.
type Command struct {
	Kind CommandKind
	Arg  string
}

// commandTable is the single source of truth for slash command names.
// Canonical names first; aliases follow so completion prefers the canonical form.
// Both ParseCommand and completeSlash reference this table.
var commandTable = []struct {
	name string
	kind CommandKind
}{
	{"/help", CmdHelp},
	{"/exit", CmdExit},
	{"/new", CmdNew},
	{"/continue", CmdContinue},
	{"/resume", CmdResume},
	{"/session", CmdSession},
	{"/clear", CmdClear},
	{"/quit", CmdExit},
	{"/q", CmdExit},
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsSessionID reports whether s looks like a cake session uuid. It backs both
// the /resume command and main's -resume flag validation.
func IsSessionID(s string) bool {
	return uuidRe.MatchString(s)
}

// ParseCommand parses input as a slash command. ok is false when the input is
// not a slash command at all (a normal prompt). An error means the input
// looked like a command but was invalid.
func ParseCommand(input string) (cmd Command, ok bool, err error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return Command{}, false, nil
	}
	fields := strings.Fields(trimmed)
	name := strings.ToLower(fields[0])
	args := fields[1:]

	for _, entry := range commandTable {
		if entry.name == name {
			if entry.kind == CmdResume {
				if len(args) != 1 {
					return Command{}, true, fmt.Errorf("usage: /resume <uuid>")
				}
				if !IsSessionID(args[0]) {
					return Command{}, true, fmt.Errorf("invalid session uuid: %s", args[0])
				}
				return Command{Kind: CmdResume, Arg: strings.ToLower(args[0])}, true, nil
			}
			return Command{Kind: entry.kind}, true, nil
		}
	}

	return Command{}, true, fmt.Errorf("unknown command: %s (try /help)", name)
}

// HelpText lists commands and keybindings for /help.
const HelpText = `commands
  /help            show this help
  /exit /quit /q   exit (cancels a running task first)
  /new             start a fresh cake session on the next prompt
  /continue        continue cake's latest session on the next prompt
  /resume <uuid>   resume a specific cake session on the next prompt
  /session         show session id, task id, cwd, run mode, last result
  /clear           clear the timeline (session state is kept)

keybindings
  enter            insert newline
  ctrl+s           submit prompt
  ctrl+c           cancel running task; quit when idle
  ctrl+u           clear input
  tab              complete slash commands
  up/down          recall prompt history (at the input's first/last line)
  pgup/pgdn        scroll timeline`
