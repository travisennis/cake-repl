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
	CmdResume
	CmdSession
	CmdModel
	CmdProfile
	CmdClear
	CmdSessions
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
	{"/resume", CmdResume},
	{"/sessions", CmdSessions},
	{"/session", CmdSession},
	{"/model", CmdModel},
	{"/profile", CmdProfile},
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
			switch entry.kind {
			case CmdResume:
				if len(args) != 1 {
					return Command{}, true, fmt.Errorf("usage: /resume <uuid>")
				}
				if !IsSessionID(args[0]) {
					return Command{}, true, fmt.Errorf("invalid session uuid: %s", args[0])
				}
				return Command{Kind: CmdResume, Arg: strings.ToLower(args[0])}, true, nil
			case CmdSession:
				if len(args) == 0 {
					return Command{Kind: CmdSession}, true, nil
				}
				if len(args) == 1 && strings.EqualFold(args[0], "copy") {
					return Command{Kind: CmdSession, Arg: "copy"}, true, nil
				}
				return Command{}, true, fmt.Errorf("usage: /session [copy]")
			case CmdModel, CmdProfile:
				arg, valid := parseTurnSelectionArg(strings.TrimSpace(trimmed[len(name):]))
				if !valid {
					return Command{}, true, fmt.Errorf("usage: %s [<name>|default]", name)
				}
				if strings.EqualFold(arg, "default") {
					arg = "default"
				}
				return Command{Kind: entry.kind, Arg: arg}, true, nil
			default:
				return Command{Kind: entry.kind}, true, nil
			}
		}
	}

	return Command{}, true, fmt.Errorf("unknown command: %s (try /help)", name)
}

// parseTurnSelectionArg parses the optional argument for /model and /profile.
// An empty rest is the status form; otherwise exactly one name is accepted,
// either bare or quoted. An empty quoted argument and any extra input are not
// part of the grammar. See ADR 007.
func parseTurnSelectionArg(rest string) (string, bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", true
	}
	if rest[0] == '"' || rest[0] == '\'' {
		if len(rest) < 2 || rest[len(rest)-1] != rest[0] || strings.Count(rest, string(rest[0])) != 2 {
			return "", false
		}
		value := rest[1 : len(rest)-1]
		if strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	}
	if strings.ContainsAny(rest, `"'`) {
		return "", false
	}
	fields := strings.Fields(rest)
	if len(fields) != 1 {
		return "", false
	}
	return fields[0], true
}

// HelpText lists commands and keybindings for /help.
const HelpText = `commands
  /help            show this help
  /exit /quit /q   exit (cancels a running task, hydration, or compaction first)
  /new             start a fresh cake session on the next prompt
  /resume <uuid>   resume a specific cake session on the next prompt
                   (startup -resume <uuid> also reloads visible history;
                   startup -resume with no uuid picks a session instead)
  /session         show session id, task id, cwd, run mode, last result
  /session copy    copy the full session id to the clipboard
  /sessions        browse past sessions in this directory and pick one to resume
  /model [name]    show or set the model for the next prompt
                   (default clears to cake's default)
  /profile [name]  show or set the profile for the next prompt
                   (default clears to cake's default)
  /clear           clear the timeline (session state is kept)

  /new, /resume, /sessions, /model, and /profile require an idle REPL

keybindings
  enter            insert newline
  ctrl+s           submit prompt (or insert a shown suggestion when empty)
  ctrl+c           cancel running task or compaction; cancel hydration and exit; quit when idle
  ctrl+n           start a new session (cancels a running task, hydration, or compaction)
  ctrl+u           clear input
  ctrl+o           cycle all tool output (starts hidden): truncated / full / hidden
  ctrl+y           copy last assistant response (markdown)
  tab              complete slash commands
  up/down          recall prompt history (at the input's first/last line)
  pgup/pgdn        scroll timeline
  mouse wheel      scroll timeline`
