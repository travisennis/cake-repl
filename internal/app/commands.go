package app

import (
	"fmt"
	"regexp"
	"strings"
)

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

type Command struct {
	Kind CommandKind
	Arg  string
}

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

func IsSessionID(s string) bool {
	return uuidRe.MatchString(s)
}

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
  /model [name]    show or set the model override for the next prompt
                   (default clears the override; a resumed session keeps its stored model)
  /profile [name]  show or set the profile override for the next prompt
                   (default clears the override; a resumed session keeps its stored profile)
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
