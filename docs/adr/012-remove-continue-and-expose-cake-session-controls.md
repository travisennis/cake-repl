---
status: accepted
date: 2026-09-17
decision-makers: Travis Ennis
---
# Remove continue and expose cake session controls

## Context and Problem Statement

`cake-repl` still exposes the obsolete `-continue` startup flag and `/continue` slash command, and its session state machine can fall back to cake's `--continue` mode after a successful task without a reported session ID. The installed cake CLI no longer supports `--continue`, so these paths produce an invalid invocation and are inconsistent with the session-pinning safety boundary.

The cake CLI also provides session, sandbox, tool, skill, and prompt controls that the REPL needs to expose while preserving its stream-json-only engine boundary.

## Decision Drivers

- Keep every live invocation compatible with the current cake CLI.
- Preserve session-hijack prevention by using explicit `--resume <id>` once an ID is known.
- Make the requested cake controls available without importing cake internals or parsing human output.
- Keep stable REPL-only settings out of the persisted config-file shape.

## Considered Options

### Retain `--continue` as a compatibility fallback

Rejected because current cake rejects the flag and selecting the latest session is the hijack vector documented by ADR 001.

### Add a second engine or a cake-version probe

Rejected because it expands the external-process contract and makes behavior depend on runtime probing rather than the documented invocation.

### Remove continue paths and pass through the requested options

Chosen. The REPL starts fresh unless explicitly resumed or forked, pins future prompts to the reported session UUID, and passes the requested controls to each applicable live cake invocation. `--fork` is applied to the initial fresh invocation only; subsequent prompts use the pinned `--resume` session instead of forking again.

## Decision Outcome

- Remove the `-continue` startup flag, `/continue` slash command, `RunContinue` mode, `UseContinue` transition, and success-without-session fallback.
- Keep `RunFresh` as the no-session-selection mode and `RunResume` as the only explicit continuation mode. A successful or failed task without a session ID leaves the current mode unchanged.
- Add startup pass-through controls for `--fork [<uuid>]`, `--no-session`, repeatable `--toolbox <dir>`, `--sandbox <policy>`, `--no-tools`, `--no-skills`, `--skills <names>`, and `--system-prompt <path>`.
- These new controls are CLI startup settings only. They are not added to the TOML config shape because they affect a session's invocation policy rather than stable REPL presentation defaults.
- Continue forcing `--output-format stream-json` and `--` before every live prompt. Replay remains the separate read-only `replay <uuid>` invocation.

### Consequences

- Good, because cake-repl no longer emits the unsupported `--continue` flag.
- Good, because no-session-ID completion cannot select an unrelated latest session.
- Good, because users can configure cake's fork, persistence, sandbox, tool, skill, toolbox, and system-prompt behavior from cake-repl.
- Neutral, because `-fork` without an ID means fork cake's latest session, matching cake; after the first invocation, session pinning prevents repeated forks.
- Neutral, because the new invocation controls are not persisted in `.cake-repl.toml` or XDG config.

## More Information

- Partially supersedes ADR 001: its explicit `--resume` pinning and hijack-prevention rationale remain; only the `--continue` fallback and manual continue mode are removed.
- Implementation: `internal/cake/runner.go`, `internal/app/session.go`, and `cmd/cake-repl/main.go`.
