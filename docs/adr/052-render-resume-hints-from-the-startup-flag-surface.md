---
status: accepted
date: 2026-11-20
---
# Render resume hints from the startup flag surface

## Context and Problem Statement

On exit, the REPL prints a shell-ready `cake -resume <session-id>` command. Cake
session controls such as `-sandbox`, `-model`, and `-cwd` shape every cake
invocation but are not recorded anywhere cake exposes for replay. A hint that
omitted them would silently reproduce a different run: for example, it could
revert an explicitly selected read-only sandbox to the workspace-write
interactive default, or let a config file re-supply a model the original run
suppressed.

## Decision

- One `startupFlags` holder backs both the process command line and the exit
  hint, so the two read the same flag surface.
- `explicitFlags` records which flags the user set. Config-file values apply
  only where the flag was omitted.
- The merge order is hardcoded defaults, then config file, then CLI flags.
- The hint re-emits an explicit override whose effective value is empty (for
  example `-model ""` or `-sandbox ""`) or equal to the default (for example
  `-cake-bin cake`), because reloading the config on resume would otherwise
  re-supply a value the original run suppressed.
- A value-taking control is emitted when it was explicit or when its effective
  value is non-empty. A bare default stays out so an ordinary resume stays
  short.
- `-no-config` is part of the run: omitting it would reload a config file the
  original run skipped.
- Values are single-quoted when the shell would otherwise interpret them, and
  an explicit empty value survives as `''`.
- Optional-value flags (`-fork`, `-resume`) are normalized before the standard
  `flag` parse. A bare `-resume` opens the session picker; it names no session
  and hydrates nothing. A bare `-fork` and a bare `-resume` are mutually
  exclusive in every form.
- The reconstruction duplicates the flag surface and is tracked for replacement
  by #13, which would source the controls from cake's session metadata.

## Consequences

- Good: a pasted hint reproduces the original run's controls.
- Good: explicit empty overrides survive the paste.
- Bad: the REPL maintains a second copy of flag merge and quoting rules.
- Bad: adding a new cake control requires updating both the flag binding and the
  hint path.

## More Information

- [`cmd/cake-repl/main.go`](../../cmd/cake-repl/main.go)
- Related: [ADR 046](046-keep-project-local-config-disable-only.md)
