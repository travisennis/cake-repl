# Interactive session browser

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. It must be maintained in accordance with
`docs/workflow/exec-plans.md`.

## Purpose / Big Picture

Today a user who has run several sessions in one directory cannot see or choose
them from inside the REPL: `/resume` requires a UUID they already know, and the
README records "No session browser" as a known limitation. After this change, a
user types `/sessions` and gets a navigable list of that directory's past
sessions — a short id, a relative timestamp, and the first prompt — with arrow
keys to move, `Enter` to select one, and `Esc` or `q` to close. Selecting a
session behaves exactly like running `/resume <uuid>`: the next prompt resumes
that session. The user can see this working by starting cake-repl in a directory
that has prior sessions, running `/sessions`, moving with the arrow keys,
pressing `Enter`, and observing that the status line's next-run value and the
timeline notice name the chosen session.

The data comes only from cake's supported, read-only `cake sessions list --json`
command. This plan depends on the accepted contract expansion in
`docs/adr/016-allow-read-only-cake-sessions-list-for-the-session-browser.md`;
do not read cake's session files or parse its human text output.

## Progress

No implementation has started; this plan was written when task 011 was accepted.
Every step below is unstarted.

- [ ] (2026-10-07T11:25Z) Write this ExecPlan and record the accepted contract
  decision (ADR 016). Docs and task record only; no code yet.
- [ ] Milestone 1: add `internal/cake/sessions.go` with `ListSessions` and its
  fake-cake tests.
- [ ] Milestone 2: add the `CmdSessions` slash command, model state, and the
  asynchronous load command.
- [ ] Milestone 3: render the list and handle navigation, selection, and close
  keys.
- [ ] Milestone 4: empty and failure states, README and `HelpText` updates, and
  a real-cake round trip.
- [ ] Run `just ci` and confirm it passes.

## Surprises & Discoveries

- Observation: `cake sessions` exposes only a `list` subcommand — there is no
  `delete` or per-session `show`. The browser therefore can only list and select;
  it cannot delete or display session details without crossing the engine
  boundary.
  Evidence: `cake sessions --help` on cake 0.1.0 prints `Commands: list, help`.
- Observation: the `sessions list --json` envelope carries `schema_version: 1`,
  which gives a forward-compatibility hook (ignore a newer version rather than
  failing).
  Evidence: a real `cake sessions list --json` run emitted `"schema_version": 1`
  with a `data.sessions[]` array.

## Decision Log

- Decision: widen the cake invocation contract to the single read-only,
  informational command `cake sessions list --json`.
  Rationale: it is versioned and machine-readable, and it avoids reading cake's
  session files; recorded in ADR 016 and approved by Travis Ennis.
  Date/Author: 2026-10-07, Travis Ennis.
- Decision: `Enter` on a selected session delegates to the existing `/resume
  <uuid>` path rather than replaying history.
  Rationale: the `/resume` slash command pins the next prompt only; startup
  `-resume` is what replays. Matching `/resume` keeps the browser's behavior
  consistent with the command it mirrors and avoids a second hydration path in
  this task. A future task may add replay-on-select.
  Date/Author: 2026-10-07, Travis Ennis.
- Decision: the browser is idle-only, like `/new` and `/resume`.
  Rationale: `Enter` sets the resume pin, and setting a session target mid-run is
  already rejected for those commands; reusing that gate keeps behavior
  consistent and avoids racing a running subprocess.
  Date/Author: 2026-10-07, Travis Ennis.
- Decision: the browser is selection-only — no delete or details actions.
  Rationale: cake exposes neither, and engine isolation forbids reaching into its
  storage (see Surprises & Discoveries).
  Date/Author: 2026-10-07, Travis Ennis.
- Decision: the list UI uses `github.com/charmbracelet/bubbles/list`, which is
  already a direct dependency (`bubbles v1.0.0` in `go.mod`), owned by
  `internal/app`.
  Rationale: avoids a new dependency and keeps `internal/ui` side-effect-free;
  `app` composes components and `ui` sanitizes strings.
  Date/Author: 2026-10-07, Travis Ennis.

## Outcomes & Retrospective

To be filled in at each milestone and at completion. At plan time the outcome is
the artifact you are reading: an accepted contract decision, a self-contained
plan, and a task moved to Pending. Nothing user-visible has changed yet.

## Context and Orientation

`cake-repl` is a single Go binary that renders a full-screen Bubble Tea terminal
REPL. It treats the external `cake` CLI as an engine: each submitted prompt
spawns one `cake --output-format stream-json` subprocess whose NDJSON (newline
delimited JSON) records are decoded and rendered as a timeline. A second
invocation, `cake --output-format stream-json replay <uuid>`, hydrates history at
startup. The project's core rule is **engine isolation**: all cake interaction
lives in `internal/cake`; `internal/app` consumes typed results and never shells
out to cake itself; `internal/ui` is pure rendering with no side effects. Layers
depend one way only: `app` -> `cake` and `ui`; `cake` and `ui` depend on neither
`app` nor each other.

Terms used in this plan:

- **Envelope** — the top-level JSON object `cake sessions list --json` prints:
  `{"schema_version":1,"command":"sessions list","status":"ok","summary":
  {"count":N},"checks":[],"data":{"sessions":[...]}}`.
- **Session summary** — one element of `data.sessions[]`, with `session_id`
  (a UUID string), `first_prompt` (the session's first user prompt), and
  `timestamp` (an RFC 3339 UTC string).
- **Run mode** — the state machine in `internal/app/session.go` deciding whether
  the next prompt starts fresh or passes `--resume <uuid>`. `/resume <uuid>` sets
  the resume pin through `sessionState.UseResume`.
- **Overlay** — an alternate body rendered in place of the timeline while a
  modal view is open. This project has no overlay infrastructure yet; the
  browser introduces the first one.

Key files:

- `internal/cake/runner.go` — subprocess lifecycle; `Start`, `Replay`,
  `Options.Args`. Add the new listing invocation beside `Replay`.
- `internal/cake/events.go`, `internal/cake/parser.go` — the stream-json schema
  and forward-compatible line parser (not used for the listing envelope, which
  is a single JSON object, not NDJSON).
- `internal/app/commands.go` — the slash-command table, `ParseCommand`, and
  `HelpText`.
- `internal/app/model.go` — the `Model` struct, layout constants, and the
  timeline render cache.
- `internal/app/update.go` — `Update` and `execCommand`; where commands and keys
  are handled.
- `internal/app/view.go` — `View`, which joins the timeline, composer, and status
  line.
- `internal/app/session.go` — `sessionState` and `UseResume`.
- `internal/ui/sanitize.go` — `Sanitize`, the render-boundary function that makes
  untrusted text safe (ADR 005).
- `internal/cake/testdata/fixtures/` and the `*_test.go` files beside the code —
  where fake-cake shell scripts drive subprocess tests.

## Plan of Work

The work proceeds in four milestones. Each is independently verifiable and
leaves the tree building and tested.

### Milestone 1 — Decode `cake sessions list --json` in `internal/cake`

Create `internal/cake/sessions.go`. Define the public types and one function:

    // SessionSummary is one session from `cake sessions list --json`.
    type SessionSummary struct {
        SessionID   string
        FirstPrompt string
        Timestamp   time.Time
    }

    // SessionsOptions configures one listing invocation.
    type SessionsOptions struct {
        Bin      string
        Cwd      string
        DebugLog io.Writer
    }

    // ListSessions runs `cake sessions list --json` and returns the parsed
    // sessions. It never mutates cake state. A non-zero exit, malformed JSON, or
    // an unusable envelope returns an error the caller treats as non-fatal.
    func ListSessions(ctx context.Context, opts SessionsOptions) ([]SessionSummary, error)

Implement it with `exec.CommandContext(ctx, opts.Bin, "sessions", "list",
"--json")`, setting `cmd.Dir = opts.Cwd` so the listed sessions belong to the
same directory as live prompts. Capture stdout with a bounded buffer, capture
stderr as a bounded tail (reuse the existing `tailBuffer` in `runner.go`),
and decode the envelope with `encoding/json`. Decode only the fields this plan
needs from `data.sessions[]`; ignore unknown fields and a `schema_version`
greater than the known value 1 (do not fail on a newer schema). Write the raw
stdout to `opts.DebugLog` when non-nil, matching how live runs treat raw stream
content as debug-log-only. Keep the prompt-free, read-only character: no
prompt argument, no `--output-format`.

Add `internal/cake/sessions_test.go` with fake-cake shell scripts that print:
a valid envelope with two sessions; an empty `data.sessions`; malformed JSON; a
non-zero exit; and an envelope with `schema_version: 2`. Assert the first two
parse, and that the last three return an error rather than panicking.

### Milestone 2 — The `/sessions` command, model state, and async load

In `internal/app/commands.go`, add `CmdSessions` to the `CommandKind` constants
and `{"/sessions", CmdSessions}` to `commandTable` (canonical name, no
aliases). `ParseCommand` needs no argument handling: return
`Command{Kind: CmdSessions}, true, nil`. Extend `HelpText` with:

      /sessions        browse past sessions in this directory and pick one to resume

In `internal/app/model.go`, add browser state to `Model`:

    browserOpen    bool
    browserLoading bool
    browserErr     string
    sessionList    list.Model

Add the `github.com/charmbracelet/bubbles/list` import. In `New` (or wherever
the model is initialized), construct the list with
`list.New(nil, sessionDelegate{}, width, height)`, `SetShowHelp(false)`,
`SetShowStatusBar(false)`, and `SetFilteringEnabled(false)`. Define a small
`sessionDelegate` (implementing `list.DefaultDelegate`-like `Render`/`Height`)
that prints `ShortID(session.SessionID)`, a relative time, and the sanitized
first prompt. Sanitize every string with `ui.Sanitize` before it reaches the
list, because session fields are untrusted (ADR 005).

In `internal/app/update.go`, add a `listSessionsCmd` that calls
`cake.ListSessions` with `m.cfg.CakeBin` and `m.cfg.Cwd` and returns a
`sessionsLoadedMsg{sessions []cake.SessionSummary, err error}`. Handle
`CmdSessions` in `execCommand`:

- Gate it like `/new` and `/resume` by extending the guard at the top of
  `execCommand` so its command predicate also includes `CmdSessions`: when
  hydrating/replay is pending or a task is running, append the existing warning
  items and return.
- Otherwise set `m.browserOpen = true`, `m.browserLoading = true`, clear
  `browserErr`, and return `m, m.listSessionsCmd()`.

Handle `sessionsLoadedMsg` in `Update`: clear `browserLoading`; on error set
`browserErr` and leave the browser open with an empty list; on success convert
summaries to `list.Item`s (a `sessionItem` type carrying the raw
`SessionSummary`) and call `m.sessionList.SetItems(...)` with a title like
`sessions in <basename of cwd>`.

### Milestone 3 — Render the overlay and handle keys

In `internal/app/update.go`, at the top of `Update`'s key handling, when
`m.browserOpen` is true route keys to the browser before the normal handling:

- `up`/`down`/`pgup`/`pgdn`/`home`/`end` -> forward to `m.sessionList.Update`.
- `enter` -> read the selected `sessionItem`; if present, set
  `m.browserOpen = false` and call the existing `/resume` behavior
  (`m.session.UseResume(id)` plus the same timeline notice that `CmdResume`
  emits). Reuse that code rather than duplicating it — factor the `CmdResume`
  body into a helper both paths call.
- `esc`, `q`, `ctrl+c` -> `m.browserOpen = false` with no action.

Because the composer would otherwise swallow these keys, ensure the browser
branch returns before the composer receives the message.

In `internal/app/view.go`, when `m.browserOpen` is true, render the list in place
of the timeline: `m.sessionList.View()` sized to the terminal width and the
height otherwise used by the timeline, followed by a one-line hint
(`↑/↓ move · enter resume · esc close`). Keep the status line. When
`browserLoading` is true, show a "loading sessions…" body instead; when
`browserErr` is non-empty, show the error as a warning body with the hint. Do not
call `ui` for anything stateful.

### Milestone 4 — Empty/failure states, docs, and a real round trip

Ensure the zero-session case renders a clear empty state ("no sessions in
<dir>") rather than an empty box. Confirm the failure path shows a non-fatal
warning and never blocks input.

Update user docs per the documentation impact matrix:

- `README.md`: add a `/sessions` row to the Slash commands table, describe the
  behavior in the Session behavior section (selecting pins the next prompt, like
  `/resume`), add the read-only `cake sessions list --json` command to the
  intro's contract sentence, and remove the "No session browser" bullet from
  Known limitations.
- `internal/app/commands.go` `HelpText`: already updated in Milestone 2; verify.
- `docs/guardrails/cake-integration-and-stream-json.md` and
  `ARCHITECTURE.md`: already updated with the accepted contract in ADR 016;
  verify they still match the shipped behavior.
- `docs/guardrails/cli-and-user-output.md` and
  `docs/guardrails/session-and-security.md`: check whether the new idle-only,
  session-affecting `/sessions` command needs a note; add one only if the
  existing rules do not already cover it.

Run a real-cake round trip (cake 0.1.0 is installed) as the end-to-end check.

## Concrete Steps

All commands run from the repository root, `/Users/travisennis/Projects/cake/cake-repl`.

1. Create the task branch before the first code edit:

    just branch feat/interactive-session-browser

2. After Milestone 1, run the package tests:

    just test ./internal/cake/...

   Expect the new `sessions_test.go` cases to pass.

3. After Milestones 2 and 3, run the app tests and the race detector on the
   runner:

    just test ./internal/app/...
    just test-race

4. Do a real-cake round trip before handoff. In a directory that already has
   sessions, build and run:

    go run ./cmd/cake-repl

   then type `/sessions`. Confirm the list appears, arrow keys move the cursor,
   `Enter` prints a notice naming the chosen session id and updates the status
   line's next-run value, and `Esc` closes the list. Then submit the next prompt
   and confirm the transcript continues that session.

5. Before committing, run the full gate:

    just ci

   Expect every stage to pass.

## Validation and Acceptance

Acceptance is behavior a human can verify:

- Typing `/sessions` in a directory with past sessions opens a list. Each row
  shows a shortened session id, a relative timestamp, and the first prompt.
- `Up`/`Down` (and `PgUp`/`PgDn`) move the selection; the highlighted row is
  visible.
- `Enter` closes the list, appends a timeline notice naming the session, and the
  status line's next-run shows `resume <short-id>`; the following prompt
  continues that cake session.
- `Esc` and `q` close the list with no state change.
- In a directory with no sessions, the browser shows an explicit empty state.
- If `cake` is missing or old, or the output is malformed, the browser shows a
  non-fatal warning and the REPL keeps accepting input.
- `-no-color` renders the list without color.
- Automated: `just test ./internal/cake/...` passes, including the fake-cake
  listing cases that fail before Milestone 1; `just ci` passes.

## Idempotence and Recovery

The change is additive: a new file, new model fields, a new command, and new
tests. Re-running the build, tests, or the REPL is safe. Opening and closing the
browser repeatedly must not mutate session state; `Enter` only sets the resume
pin, which `/new`, `/resume`, and `Ctrl+N` already overwrite. If a listing
fails, the error is confined to the browser body and no session files are
touched. No migration or destructive step exists, so there is nothing to roll
back beyond `git switch` away from the branch.

## Artifacts and Notes

Real `cake sessions list --json` output shape (cake 0.1.0), used to design the
decoder:

    {
      "schema_version": 1,
      "command": "sessions list",
      "status": "ok",
      "summary": { "count": 42 },
      "checks": [],
      "data": {
        "sessions": [
          {
            "first_prompt": "let's groom task 011 and get it ready to work on",
            "session_id": "00775bbe-e314-47fe-83ec-791d15e4ab4d",
            "timestamp": "2026-10-07T11:20:44.141622Z"
          }
        ]
      }
    }

Expected cake arguments for the listing invocation:

    cake sessions list --json

with `cmd.Dir` set to the REPL's working directory.

## Interfaces and Dependencies

Prescriptive interfaces that must exist at the end of the milestones:

    // internal/cake/sessions.go
    type SessionSummary struct {
        SessionID   string
        FirstPrompt string
        Timestamp   time.Time
    }

    type SessionsOptions struct {
        Bin      string
        Cwd      string
        DebugLog io.Writer
    }

    func ListSessions(ctx context.Context, opts SessionsOptions) ([]SessionSummary, error)

    // internal/app (message + command plumbing; names may adjust to fit existing style)
    type sessionsLoadedMsg struct {
        sessions []cake.SessionSummary
        err      error
    }

    func (m Model) listSessionsCmd() tea.Cmd

Dependencies: `github.com/charmbracelet/bubbles/list` (already present via
`bubbles v1.0.0`), `encoding/json`, `os/exec`, `time`, and the existing
`internal/ui.Sanitize`. Do not add a new module dependency. All cake interaction
stays in `internal/cake`; `internal/app` and `internal/ui` do not shell out.
