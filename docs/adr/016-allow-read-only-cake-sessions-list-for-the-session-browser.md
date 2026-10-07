---
status: accepted
date: 2026-10-07
decision-makers: Travis Ennis
---
# Allow read-only cake sessions list for the session browser

## Context and Problem Statement

The README records "No session browser; `/resume` needs a UUID you already know"
as a known limitation. After running several sessions in one directory, a user
has no way to see, choose, or recall past sessions from inside the REPL; they
must remember a UUID or inspect cake's session storage by hand. The `/session`
slash command reports the current session, not a list of past ones.

cake 0.1.0 exposes `cake sessions list --json`, a read-only introspection
subcommand that prints a versioned JSON envelope for the sessions belonging to
the current working directory: `schema_version`, `command`, `status`, a
`summary.count`, and `data.sessions[]` entries carrying `session_id`,
`first_prompt`, and `timestamp`. This is a stable, machine-readable contract.

cake-repl's accepted contract currently permits only prompt-running
`cake --output-format stream-json` invocations (live and `--resume`) plus the
read-only `replay <uuid>` command; listing cake's session directory or files
directly is prohibited by engine isolation. This ADR decides whether to widen
the contract to include `cake sessions list --json` so the session browser can
be built on a supported source.

## Decision Drivers

- Close the documented session-browser limitation without reading cake's private
  storage or parsing human-readable output.
- Prefer a versioned, machine-readable command over filesystem inspection or
  text scraping.
- Keep the widened contract minimal: one read-only, informational command.
- Preserve engine isolation: all cake interaction stays in `internal/cake`, and
  `app` never shells out to cake.
- Keep failures non-fatal so browsing can never block a prompt.
- Follow the precedent set for `replay` in
  [ADR 011](011-replay-resumed-sessions-through-cake-stream-json.md).

## Considered Options

- List or read cake's session directory and JSONL files directly: rejected
  because it crosses the engine boundary and couples cake-repl to cake's private
  storage layout.
- Parse the human-readable `cake sessions` output: rejected because it is not a
  stable machine contract and loses structured fields.
- Invoke `cake sessions list --json` as a read-only introspection command:
  chosen because it is the supported, versioned, read-only contract.
- Do nothing and keep the limitation: rejected because it leaves a top
  user-visible gap that cake already makes solvable.

## Decision Outcome

cake-repl may invoke exactly one additional cake command, read-only and
informational: `cake sessions list --json`. It is a peer of `replay <uuid>` and
is subject to the same rules.

It runs only through `internal/cake`, with the same working directory as a live
prompt so the listed sessions correspond to the directory the REPL runs in.
cake-repl decodes the documented JSON envelope and reads `data.sessions[]`; it
does not read cake's session files, does not parse cake's human text output, and
does not mutate any session. Unknown fields and a newer `schema_version` are
ignored rather than treated as a hard error.

Any failure — non-zero exit, malformed JSON, an unsupported envelope, or an
older cake binary without the subcommand — is non-fatal. It surfaces as an empty
or warning state, and it never prevents submitting a prompt. The browser lists
and selects sessions; it does not delete or edit them, because cake exposes no
such subcommand and engine isolation forbids reaching into storage.

### Consequences

- Good, because the session browser is buildable from a supported, versioned
  machine contract, closing the README limitation.
- Good, because the boundary stays explicit and mirrors the accepted `replay`
  decision instead of inventing a new pattern.
- Bad, because the invocation contract now includes a second command shape, so
  future changes to the `sessions list` envelope must be handled
  forward-compatibly.
- Neutral, because `sessions list` returns only id, timestamp, and first prompt;
  the browser is therefore read-only and selection-only, with no per-session
  detail or deletion.

## More Information

- Task 011: interactive session browser.
- [ADR 011](011-replay-resumed-sessions-through-cake-stream-json.md): the
  precedent for adding a read-only cake command to the contract.
- [`../guardrails/cake-integration-and-stream-json.md`](../guardrails/cake-integration-and-stream-json.md):
  the invocation contract and decoding rules.
- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md): the system boundary.
- cake command: `cake sessions list --json` (envelope `schema_version` 1).
