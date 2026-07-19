---
status: accepted
date: 2026-06-27
decision-makers: Travis Ennis
---
# Config file for REPL defaults

## Context and Problem Statement

cake-repl historically accepted all persistent defaults only as startup flags.
That was fine while the flag surface was small, but users who always want the
same cake binary, model, profile, tool-output limit, or timeline cap had to
repeat those options for every invocation.

The application still needs to preserve the cake engine boundary: cake-repl may
pass supported flags through to cake, but it must not read cake session files,
parse cake human output, or import cake internals. Any config file must
configure cake-repl startup behavior only.

## Decision Drivers

- Make repeated local usage less noisy without changing default behavior.
- Keep session selection explicit and non-persistent so config cannot silently
  resume or hijack a conversation.
- Support both user-level defaults and project-local overrides.
- Keep CLI flags authoritative for one-off invocations and scripts.
- Use a format that is easy to read, edit, and test without a real cake binary.

## Considered Options

### Keep CLI flags only

No new files or dependencies, and every invocation stays explicit. Not chosen:
the growing flag set makes persistent defaults useful enough, and repeating
stable defaults such as `-model`, `-profile`, and `-cake-bin` is unnecessary
friction.

### Add a single user-level config file

Load one file from the XDG config location. Simple, but it does not support
repository-specific defaults such as a local cake binary path or a project
profile.

### Add XDG plus project-local TOML config

Load a user-level XDG config file and a project-local `.cake-repl.toml`, with
project-local values overriding user-level values. CLI flags override both.
This gives users broad defaults while allowing individual projects to opt into
different defaults.

## Decision Outcome

Chosen option: **XDG plus project-local TOML config**, read once at startup.

The config file contract is:

- Load `$XDG_CONFIG_HOME/cake-repl/config.toml`, falling back to
  `~/.config/cake-repl/config.toml` when `XDG_CONFIG_HOME` is unset.
- Load `.cake-repl.toml` from the current working directory.
- Merge in this order: hardcoded defaults < XDG config < project-local config <
  CLI flags.
- Support only stable REPL defaults: `cake-bin`, `model`, `profile`,
  `output-limit`, and `max-timeline-items`.
- Exclude session-specific or invocation-specific values such as `cwd`,
  `continue`, `resume`, `debug-log`, `history-file`, `config`, `no-config`, and
  `version`.
- Provide `-config <path>` to load a single explicit config file instead of the
  default paths.
- Provide `-no-config` to skip all config loading.
- Missing config files are ignored. Malformed or unreadable config files are
  startup errors.
- Dynamic reload is out of scope; config is read only at startup.

### Consequences

- Good, because repeated invocations can share durable defaults without changing
  the no-config behavior.
- Good, because CLI flags remain the highest-precedence layer for scripts,
  debugging, and one-off overrides.
- Good, because session selection stays outside the persisted config shape,
  preserving the session-hijack prevention model.
- Neutral, because project-local config is intentionally tied to the process
  current directory rather than repository discovery. Use `-cwd` and shell
  working directory deliberately.
- Bad, because `github.com/BurntSushi/toml` becomes a runtime dependency and
  part of the release surface.

## More Information

- Task: `.ahm/tasks/completed/008.md`.
- Implementation: `internal/config/config.go`.
- Startup merge: `cmd/cake-repl/main.go`.
- User docs: `README.md#config-file`.
- CLI/output guardrail: `docs/guardrails/cli-and-user-output.md`.
