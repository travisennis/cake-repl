---
status: accepted
date: 2026-10-08
decision-makers: Travis Ennis
---
# Default the cake sandbox to workspace-write-interactive

## Context and Problem Statement

The cake CLI sandboxes model-generated shell commands with the policy selected by
`--sandbox`, defaulting to `workspace-write`. `cake-repl` currently passes
`--sandbox` only when the user supplies `-sandbox`, so an ordinary REPL run
inherits cake's `workspace-write` default.

A REPL is a conversational, interactive harness: the user is present at a
terminal and frequently asks cake to run commands that would benefit from the
extra macOS capabilities `workspace-write-interactive` grants (letting a command
launch and automate other applications). The profile is better suited to that
use than the non-interactive `workspace-write` that cake applies when it runs
unattended.

## Decision Drivers

- Match the REPL's interactive nature with the interactive sandbox profile by
  default, without the user having to remember a flag.
- Preserve the pass-through contract: `-sandbox` still selects any cake policy
  (`read-only`, `workspace-write`, `workspace-write-interactive`,
  `danger-full-access`), and an explicit empty value still lets cake resolve its
  own policy.
- Keep session invocation policy out of the persisted config-file shape
  ([ADR 002](002-config-file-for-repl-defaults.md),
  [ADR 012](012-remove-continue-and-expose-cake-session-controls.md)).
- Make the changed default visible in the docs and in the exit resume hint.

## Considered Options

### Keep cake's `workspace-write` default

Rejected. It is the right default for unattended cake runs, but it withholds the
interactive capabilities the REPL use case is most likely to need.

### Add a config-file key for the sandbox policy

Rejected. The sandbox policy shapes every cake invocation, so it belongs with the
other startup pass-through controls that stay out of the TOML config shape
(ADR 012). A config default would also be invisible to the resume hint's
invocation provenance.

### Default the `-sandbox` flag to `workspace-write-interactive`

Chosen. `cmd/cake-repl/main.go` sets the flag's hardcoded default to
`workspace-write-interactive`. The runner is unchanged and keeps its
pass-through behavior.

## Decision Outcome

- The `-sandbox` flag default is `workspace-write-interactive`; an ordinary run
  passes `--sandbox workspace-write-interactive` to cake.
- `-sandbox <policy>` still overrides the default with any cake policy.
- `-sandbox ""` omits `--sandbox`, letting cake resolve its own policy from
  `CAKE_SANDBOX` or its settings. This is the escape hatch for users who rely on
  cake's own sandbox resolution.
- The runner (`internal/cake/runner.go`) keeps emitting `--sandbox` only when the
  option is non-empty, so the engine contract is unchanged.
- The exit resume hint continues to include the effective `-sandbox` value, so a
  pasted resume keeps the profile the original run used.

### Consequences

- Good, because ordinary REPL runs can launch and automate applications on macOS
  without a flag.
- Good, because the sandbox policy stays a startup pass-through control with the
  same override semantics; only the hardcoded default changed.
- Bad, because the REPL now overrides cake's `workspace-write` default, and
  therefore `CAKE_SANDBOX` and cake settings, for every run that does not pass an
  explicit `-sandbox`. This is a deliberate permission escalation over cake's
  default and is documented in `README.md` and the CLI/output and
  session/security guardrails.
- Neutral, because the runner, the stream-json contract, and the config-file
  shape are untouched.
- Bad, because a run started with `-sandbox ""` is not yet reproduced by the exit
  hint, which omits empty values, so a pasted resume falls back to the default
  policy. The limitation is documented in `README.md` and the CLI/output
  guardrail, and the provenance fix is owned by task 104.

## More Information

- Implementation: `cmd/cake-repl/main.go` (flag default), `README.md`, and
  `docs/guardrails/cli-and-user-output.md` /
  `docs/guardrails/session-and-security.md`.
- Related: [ADR 012](012-remove-continue-and-expose-cake-session-controls.md)
  introduced the `-sandbox` pass-through control; this ADR changes only its
  default. [ADR 002](002-config-file-for-repl-defaults.md) keeps session
  invocation policy out of the config file.
- cake `--sandbox` policy list and the `workspace-write-interactive` capabilities
  are described by `cake --help`.
