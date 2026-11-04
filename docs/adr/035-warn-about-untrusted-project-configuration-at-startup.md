---
status: accepted
date: 2026-11-04
decision-makers: Travis Ennis
---
# Warn about untrusted project configuration at startup

## Context and Problem Statement

cake 0.1.0 introduced explicit trust for authority-bearing project
configuration (cake ADR 053). Settings keys that grant sandbox paths, gates,
hooks, toolbox tools, a project system prompt, or skill paths require a
versioned, per-project trust record. An enforcing command — including a live
prompt — resolves that trust before it applies project authority. When the
snapshot is untrusted, changed since the record was written, or the record is
invalid, cake exits with an input error (exit 3) before emitting any stream-json
record.

cake-repl only discovered this after the first prompt was submitted. It passed
no trust flags and did not inspect trust, and its other cake commands
(`replay <uuid>` and `sessions list --json`) are marked as not using project
configuration by cake, so they succeed in an untrusted checkout. A live prompt
then surfaced only as `cake exited with code 3` followed by cake's raw stderr.
A user had no way to learn before prompting that `cake trust` had to run.

cake exposes read-only trust commands: `cake trust --status` (human output),
`cake trust --status --json` (one versioned diagnostic document, exit 0 in every
trust state), and `cake trust --check` (exit nonzero unless the current snapshot
is trusted). The question is whether cake-repl should widen its engine contract
to use one of them for a startup preflight.

## Decision Drivers

- Give the user a heads-up during startup, normally before the first prompt, not
  after a failed run.
- Keep engine isolation: use cake's versioned machine-readable output, never its
  private files or human-readable text.
- Stay read-only. The preflight must never create, replace, or revoke a trust
  record, and it must not submit a prompt.
- Never block input or make startup fatal. An older cake without the command, a
  non-zero exit, malformed JSON, or a timeout must be silent apart from the
  debug log.
- Use the same executable, working directory, and environment as live prompts,
  with bounded output and the established child-process teardown discipline.
- Decode forward-compatibly so a newer cake diagnostic envelope cannot break the
  REPL.

## Considered Options

- **Run `cake trust --check` at startup (exit status only).** Less decoding, but
  a nonzero exit conflates `untrusted`, `changed`, `invalid`, and a malformed
  project configuration, and it cannot name the state or authority-bearing
  files in the warning.
- **Run `cake trust --status --json` at startup (chosen).** One read-only call
  returns the state, canonical project root, authority-bearing files and keys,
  changes from the trusted snapshot, and an invalid-record reason.
- **Add trust state to a stream-json initialization record and report it when a
  run starts.** Rejected as the primary fix: the failure happens before any
  stream record exists, and it still arrives only after the first prompt.
- **Read `.cake/` and the trust record directly from cake-repl.** Rejected: it
  crosses the engine boundary and duplicates cake's hashing and record
  validation.
- **Do nothing.** Rejected: the first-prompt failure is the exact gap this
  decision closes.

## Decision Outcome

Chosen option: a read-only startup probe through
`cake trust --status --json`, because it reports the decision cake will make in
one forward-compatible machine document without gating or writing anything.

`internal/cake` gains one permitted invocation, `cake trust --status --json`.
It runs with the configured `-cake-bin`, the same `-cwd` and environment as a
live prompt, the debug log, a 1 MiB stdout cap, the existing 8 KiB stderr tail,
process-group isolation, a 3-second `WaitDelay`, and a 10-second context
deadline. The probe registers with the process-level child registry while it
runs so quit and the unobserved-exit path can cancel and reap it.

The client decodes the diagnostic envelope's `data` object: `state`,
`project_root`, `authority[].path`, `authority[].keys`, `changes[]`, and
`invalid_reason`. Unknown fields and a newer `schema_version` are ignored. A
missing or empty `state` is malformed output, not an implicit success.

At startup, when the probe returns:

- `trusted` and `not_required` are silent.
- `untrusted`, `changed`, and `invalid` append one warning to the timeline
  naming the state, the authority-bearing files or changes, the canonical
  project root, and the `cake trust` command that resolves it. The warning
  normally appears before the first prompt, but the probe never gates input, so
  an immediate submission can arrive first.
- An unrecognized state also warns, naming the state, so a future cake trust
  decision is not silently discarded.
- Any probe failure — non-zero exit, older cake without the subcommand,
  malformed JSON, or a timeout — is written only to the debug log when
  configured. It never becomes a timeline item or a startup failure.

The probe is skipped when `CAKE_TRUST_PROJECT=1` or `CAKE_PROJECT_CONFIG=off` is
set in the REPL's environment, because the next live prompt will not depend on
the project's persistent trust record in those modes; `cake trust --status`
itself always inspects the project and would otherwise produce a false warning.
The preflight does not add `--trust-project` or `--no-project-config`
pass-through flags, does not run `cake trust`, and never writes the trust record.

The warning is advisory. Submitting a prompt is still allowed, the first prompt
is still the engine's decision, and no existing run, replay, or session-listing
behavior changes.

### Consequences

- Good: the user sees the trust requirement and the exact fix before sending a
  prompt, including in worktrees where each canonical root has its own record.
- Good: the preflight is read-only and cannot change project authority.
- Good: forward-compatible decoding and silent failure preserve startup with
  older cake binaries and future diagnostic fields.
- Bad: every startup now makes one extra short cake invocation, even when the
  project has no authority-bearing configuration; the command is bounded and
  silent on failure.
- Neutral: the warning is a snapshot. If `.cake/` changes after startup, the
  next prompt's exit 3 remains the authority; a later change can re-probe on
  demand if needed.
- Neutral: the probe is asynchronous and does not gate input. An immediate
  first submission can reach cake before the warning is appended; the warning
  is still useful for the retry and the rest of the session.
- Neutral: the engine contract grows by one read-only diagnostic command, so
  future changes to the trust-status envelope must be decoded compatibly.

## More Information

- cake ADR 053: trust project configuration by capability.
- [ADR 016](016-allow-read-only-cake-sessions-list-for-the-session-browser.md):
  the precedent for widening the contract with one read-only, versioned cake
  command.
- [ADR 019](019-cancel-and-bound-the-read-only-session-listing.md): cancellation,
  output bounds, and request ownership for a read-only helper.
- [ADR 034](034-bound-unobserved-exit-child-teardown.md): the process-level
  registry and bounded child teardown the probe joins.
- cake documentation: `configuration.md`, `security.md`, and `integrations.md`
  describe the trust record and the `trust --status --json` envelope.
