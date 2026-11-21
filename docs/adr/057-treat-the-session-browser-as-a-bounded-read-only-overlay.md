---
status: accepted
date: 2026-11-20
---
# Treat the session browser as a bounded read-only overlay

## Context and Problem Statement

The `/sessions` browser and the bare `-resume` startup picker list sessions in
the current directory through `cake sessions list --json`. A listing can be slow,
missing, malformed, or replaced while it is in flight. The browser must not
resume a row the user cannot see, and a failed helper must not block input or
leave stale rows.

## Decision

- The browser has one presentation phase: hidden, loading, ready, empty, or
  failed. Selectability is derived from that phase, not from a second row-count
  check, so a ready body and an actionable cursor cannot disagree.
- Listing request state is separate from the presentation phase. The model owns
  one active request identity, its cancel function, and a set of draining done
  channels. Closing the browser, starting a replacement listing, or quitting
  cancels and invalidates the active request; a late result may close its drain
  but can never touch the new body.
- Opening `/sessions` and the startup picker share one path, so both present the
  same title, loading state, and reload. The previous rows are dropped before a
  reload starts.
- A failed listing keeps the browser open with a capped error body and clears
  the old rows. The error is non-fatal: the REPL keeps accepting input.
- An empty listing is its own phase, not a ready list with zero rows.
- `cake sessions list --json` is bounded to 8 MiB. A pathological listing fails
  and degrades to a warning. Cancellation kills the helper's process group
  outright; `WaitDelay` bounds a helper that ignores termination or a descendant
  that inherited stdout.
- Only `data.sessions` is read from the envelope, and only sessions in
  `opts.Cwd` are shown. A newer `schema_version` and unknown fields are ignored.
  An entry without a session id is skipped because it could never be resumed;
  an unparseable timestamp keeps the zero time so one odd row cannot hide the
  rest.
- A row's title is rendered when the list is built: short id, relative age, and
  a sanitized, single-line first prompt. Untrusted session fields are sanitized
  and flattened before they reach the list.
- The browser replaces the timeline and reserves one row for its key hint. It
  occupies the timeline's height so the composer and status line do not move.

## Consequences

- Good: Enter can only resume a row visible in the current successful listing.
- Good: a slow or missing helper cannot freeze key handling.
- Good: a malformed row does not hide the rest of the listing.
- Bad: the browser has both presentation state and request state, and they must
  be updated together.
- Neutral: the listing is read-only; the REPL never reads or writes a cake
  session file.

## More Information

- [`internal/app/sessions.go`](../../internal/app/sessions.go)
- [`internal/cake/sessions.go`](../../internal/cake/sessions.go)
- Related: [ADR 041](041-isolate-and-bound-every-external-child-process.md),
  [ADR 053](053-decode-stream-json-forward-compatibly.md)
