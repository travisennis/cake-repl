# Add opt-in after-turn prompt suggestions through cake-suggest

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must
be kept up to date as work proceeds.

This plan must be maintained in accordance with
[`docs/workflow/exec-plans.md`](../../workflow/exec-plans.md).

## Purpose / Big Picture

A user of cake-repl finishes a turn and then has to decide what to ask next.
This plan adds an optional ghost suggestion to the prompt composer: after a
completed turn, cake-repl runs an external `cake-suggest` binary, and if that
binary returns a suggested next user prompt, the suggestion appears as dim text
inside the composer. Pressing `Ctrl+S` submits the suggestion as an ordinary
prompt. Typing anything else first dismisses the suggestion, and a late result
never overwrites a draft. The feature is off by default because it spends model
quota on every completed turn.

A user can see it working by putting this in
`~/.config/cake-repl/config.toml`, installing a `cake-suggest` that implements
the contract in
[`../../adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md`](../../adr/028-offer-after-turn-prompt-suggestions-from-an-external-suggester.md),
and completing one turn. The composer shows the suggestion in place of the
normal `Type a prompt…` placeholder, and the footer reads
`Ctrl+S use suggestion · type to dismiss`. Pressing `Ctrl+S` produces a normal
user message containing exactly the suggested text.

## Progress

- [x] (2026-10-27 17:45Z) Created task 127 and ADR 028.
- [x] (2026-10-27 17:50Z) Wrote this ExecPlan.
- [ ] Add the `[suggestions]` config table, validation, merge, and provenance
  rules.
- [ ] Add `internal/suggest` with the bounded subprocess and document decoding.
- [ ] Add the app model state, trigger, result application, dismissal, and
  cancellation behavior.
- [ ] Add Go tests for config, subprocess, trigger, application, dismissal, and
  stale results.
- [ ] Update user and architecture docs, guardrails, and AGENTS.md.
- [ ] Run the review and verification ladders, complete task 127, and move this
  plan to `docs/exec-plans/completed/`.

## Surprises & Discoveries

- Observation: `Ctrl+S` is already the only submit binding, so the requested
  accept key needs no new key binding. The suggestion only changes what
  `Ctrl+S` submits when the composer is empty.
  Evidence: `internal/app/keys.go` defines `Submit: ctrl+s`, and
  `internal/app/update.go` routes it to `submit`.
- Observation: `cake-suggest` does not exist in the sibling projects or on
  `PATH` as of 2026-10-27. This plan implements the cake-repl side against the
  documented contract; a fake binary exercises every path without a model call.
  Evidence: `command -v cake-suggest` returns no path.
- Observation: the existing `textarea` placeholder is rendered dim whenever the
  value is empty and is absent as soon as the user types. That is the exact
  "ghost suggestion" behavior the request describes, and it avoids prefilling
  real editable text that a first keystroke would append to.
  Evidence: `github.com/charmbracelet/bubbles/textarea` `placeholderView`.

## Decision Log

- Decision: use `[suggestions]` with `enabled` and `cake-suggest-bin`, default
  off, and let the automatic project-local layer only disable it.
  Rationale: a suggestion spends model quota and reads session content; it has
  the same provenance boundary as compaction.
  Date/Author: 2026-10-27 / Travis Ennis
- Decision: pass the session id, not the visible transcript, to the external
  suggester.
  Rationale: the session id is the stable engine boundary. The timeline is a
  bounded rendering view, not a transcript contract.
  Date/Author: 2026-10-27 / Travis Ennis
- Decision: show the suggestion as placeholder ghost text rather than setting
  the textarea value.
  Rationale: a prefilled value would be indistinguishable from a draft and a
  first keystroke would append to it. A placeholder disappears on the first
  typed character and keeps `Ctrl+S` semantics simple.
  Date/Author: 2026-10-27 / Travis Ennis
- Decision: start the suggestion only after an automatic compaction settles.
  Rationale: the compactor appends to the same session; the suggester must not
  read a partially written transcript. The suggestion is not urgent.
  Date/Author: 2026-10-27 / Travis Ennis
- Decision: do not interpret a suggestion that begins with `/` as a command.
  Rationale: the suggestion is model output derived from session content, not a
  user-issued command. The accepted text is sent as a prompt.
  Date/Author: 2026-10-27 / Travis Ennis
- Decision: warn at most once per REPL process when suggestions cannot run.
  Rationale: suggestions run after every turn; a missing binary or broken
  document must be discoverable without adding a warning row after every turn.
  Date/Author: 2026-10-27 / Travis Ennis

## Outcomes & Retrospective

Implementation is in progress. The target outcome is an opt-in, non-blocking
suggestion that is visibly a suggestion, accepts with `Ctrl+S`, dismisses on
typing, and cannot cross a session or draft boundary. The main gap at
completion will be live quality: `cake-suggest` is external, so this plan
verifies the contract with fake binaries and does not claim that suggested
prompts are useful until a real suggester is available.

## Context and Orientation

cake-repl is a Go Bubble Tea terminal REPL. Each submitted prompt spawns one
`cake --output-format stream-json` subprocess and renders its NDJSON events.
The REPL never reads cake session files. It already runs one optional external
sibling binary after turns: `cake-compact`, through
`internal/compact/compact.go`. That integration is the template for this one.

The relevant existing files and what they own:

- `internal/app/model.go`: the `Model` struct, `Config`, `New`, placeholder
  setup, and `layout`.
- `internal/app/update.go`: `Update`, key handling, `submit`, `startRun`, and
  `finishRun`.
- `internal/app/compaction.go`: the post-turn trigger, external process
  request/result messages, and the finish path. The suggestion follows the same
  shape but without the gate.
- `internal/app/view.go`: composer state and footer hint.
- `internal/config/config.go`: TOML load, merge, validation, and project-local
  provenance.
- `cmd/cake-repl/main.go`: merges config into `app.Config` and applies
  `-no-session`.
- `internal/compact/`: the external-process pattern (bounded stdout/stderr,
  SIGTERM cancellation, `WaitDelay`, `childproc.Isolate`, debug-log line).
- `internal/ui/`: pure rendering; the application must sanitize suggestion text
  before it reaches the textarea.

Terms used in this plan:

- **Ghost suggestion**: dim placeholder text in the composer shown only while
  the textarea value is empty. It is never part of the textarea value until
  accepted.
- **Pinned session**: the cake session UUID in `sessionState.ResumeID` that the
  next prompt will resume. Suggestions are only requested for a pinned session.
- **Suggester**: the external `cake-suggest` executable.
- **Trusted config**: XDG config or an explicit `-config` file. The
  automatically loaded `.cake-repl.toml` is untrusted provenance.

## Plan of Work

The work is additive. It keeps every existing default and only activates when
`[suggestions] enabled = true` is supplied by trusted config.

First, extend `internal/config/config.go`. Add a `Suggestions` struct with
`Enabled`, `EnabledSet`, and `CakeSuggestBin`. Add
`DefaultCakeSuggestBin = "cake-suggest"`, `DefaultSuggestions`, a
`Suggestions.Validate` that requires a non-empty binary only when enabled, and
`MergeSuggestions`. Extend `Config` with `Suggestions Suggestions`, extend
`Merge`, and extend `LoadProject` so `suggestions.cake-suggest-bin` is always
cleared and `suggestions.enabled = true` is cleared, while `enabled = false`
survives. Add matching cases to `projectConfigWarning` in
`cmd/cake-repl/main.go`.

Second, add `internal/suggest/suggest.go`. It defines `Options`, `Args`,
`Document`, `Result`, and `Suggest`, mirroring `internal/compact` but simpler.
The argument shape is
`suggest -json [-cake-bin <path>] [-model <name>] [-profile <name>] -- <session-id>`.
It uses `childproc.Isolate`, a `capWriter` for bounded stdout, a `tailBuffer`
for bounded stderr, SIGTERM cancellation with a `WaitDelay`, and a content-free
`DebugLog` line. It decodes one JSON document only after exit 0, ignores unknown
fields, and reports malformed output as `Result.Err`.

Third, extend `internal/app`. Add `Config.Suggestions` and a
`SuggestionConfig` value. Add model fields for one in-flight request
(`suggesting`, `suggestionSeq`, `suggestionReq`, `suggestionCancel`,
`suggestionText`, `suggestionWarned`). Add `internal/app/suggestion.go` with
the request/result message, `canSuggest`, `maybeStartSuggestion`,
`startSuggestion`, `finishSuggestion`, `clearSuggestion`, `cancelSuggestion`,
`normalizeSuggestion`, and a failure text helper. Use a fixed 60-second
context timeout. The request command returns a `suggestionDoneMsg`; the Update
loop handles it without blocking.

Fourth, wire the behavior:

- `New` uses a `defaultInputPlaceholder` constant.
- `finishRun` calls `maybeStartSuggestion` only for a clean, completed turn
  (not canceled, no start/wait error, exit 0, `sawComplete`, and a completion
  record). If an automatic compaction starts, the suggestion waits.
- `finishCompaction` calls `maybeStartSuggestion` after the outcome row, unless
  the REPL is exiting or the session changed.
- `finishSuggestion` applies the suggestion only when the request identity,
  session id, idle state, and empty composer all still match. It sets
  `input.Placeholder`, marks `contentChanged`, and adds no timeline row.
- `submit` uses the active suggestion when the textarea value is empty and
  bypasses slash-command parsing for that text.
- `handleKey` dismisses the ghost before any composer edit, history recall, or
  tab completion. `Ctrl+U`, `Ctrl+N`, `/new`, `/resume`, `/sessions`, quit, and
  `CancelRunning` clear or cancel the request.
- `composerHint` advertises `Ctrl+S use suggestion · type to dismiss` while the
  ghost is showing.

Fifth, add tests that require no real cake or model call. Config tests cover
load, merge, validation, and project-local stripping. `internal/suggest` tests
cover argument shape, success, no-op, malformed output, non-zero exit, missing
binary, cancellation, and bounded output. App tests cover the trigger, the
compaction ordering, applying a result, ignoring stale/wrong-session results,
dismissing on typing, accepting on submit, hint text, cancellation on new
session, and the one-warning rule.

Finally, update the durable docs: `README.md` (requirements, key binding note,
a prompt-suggestions section, config shape, precedence, and merge order),
`ARCHITECTURE.md` (the second non-cake executable, module map, and invariant),
`docs/guardrails/cake-integration-and-stream-json.md` (the suggester contract
and tests), `docs/guardrails/cli-and-user-output.md` (config and rendering),
`docs/guardrails/session-and-security.md` (read-only background subprocess and
secret boundary), and `AGENTS.md` (compatibility surfaces and routing). ADR 028
is already created and indexed.

## Concrete Steps

Run every command from the repository root,
`/Users/travisennis/Projects/cake/cake-repl`, on branch
`feat/cake-suggest-integration`.

1. Confirm the starting state:

```bash
git status --short
git branch --show-current
ahm adr show 028
ahm task show 127
```

Expect a clean worktree, the feature branch, ADR 028 accepted, and task 127
`In Progress`.

2. Implement the config changes and run:

```bash
go test ./internal/config
```

3. Implement `internal/suggest` and run:

```bash
go test ./internal/suggest
go test -race ./internal/suggest
```

4. Implement the app behavior and run:

```bash
go test ./internal/app
go test -race ./internal/app
```

5. Format and run the broad gates:

```bash
just fmt-check
just vet
just test-race
just lint
just deadcode
just vuln
just build
```

`just ci` is the final gate; it includes the release snapshot check.

6. Exercise the user-visible path with fake binaries. Create a fake cake that
   emits a minimal successful stream and a fake suggester that returns one
   document, point a temporary config at both, and run the TUI. The expected
   observation after the turn is a dim suggestion in the composer and the
   footer `Ctrl+S use suggestion · type to dismiss`; `Ctrl+S` then submits the
   suggested text as a user message.

7. Run the preflight skill in a subagent, apply its worthwhile findings, rerun
   the affected checks, complete task 127 with documentation notes, and move
   this plan to `docs/exec-plans/completed/`.

## Validation and Acceptance

The feature is accepted when all of these are observable:

- With `[suggestions] enabled = true` and `cake-suggest-bin` pointing at a fake
  script that prints
  `{"suggested":true,"suggestion":"Add a test for the new path.","session_id":"<pinned-id>"}`,
  a completed turn fills the composer with that suggestion as placeholder text.
- `Ctrl+S` with the empty composer starts a run whose user message is exactly
  `Add a test for the new path.`, and the ghost is gone.
- Typing `x` first dismisses the ghost, leaves the input value `x`, and the
  suggestion is never submitted.
- A result with a superseded request identity or a different session id does
  not change the composer.
- A result that arrives while the user has a draft, or while a run/hydration/
  compaction/browser is active, is dropped.
- A `suggested:false` document and a canceled request add no timeline row; a
  missing binary or malformed document adds at most one warning per process.
- `/new`, `/resume`, `/sessions`, `Ctrl+N`, `Ctrl+C`, and quit cancel an
  in-flight request, and quitting does not wait on it.
- `-no-session` disables suggestions with a startup warning. The automatic
  project-local `.cake-repl.toml` cannot enable suggestions or set
  `cake-suggest-bin`; it may set `enabled = false`.
- `just test-race`, `just lint`, `just vet`, `just deadcode`, `just vuln`, and
  `just ci` pass.

Tests that fail before the change and pass after it include
`TestStartupSuggestionsConfig`, `TestSuggestAppliesDocument`,
`TestSuggestionDismissedByTyping`, `TestSubmitUsesSuggestionAsPrompt`, and
`TestSuggestionStaleResultIgnored`.

## Idempotence and Recovery

All edits are additive and can be repeated. The config defaults keep the feature
off, so a partially implemented branch does not change existing users. A fake
suggester used for manual verification lives in a temporary directory and is
never committed. If a test leaves a subprocess running, the test context and
`WaitDelay` bound it; rerunning the test is safe. If the implementation must be
rolled back, remove the `[suggestions]` wiring and the package; no persisted
state is created by this feature.

## Artifacts and Notes

The expected document shapes:

```json
{"suggested":true,"suggestion":"Add a test for the new path.","session_id":"11111111-2222-3333-4444-555555555555","reason":""}
```

```json
{"suggested":false,"suggestion":"","session_id":"11111111-2222-3333-4444-555555555555","reason":"no completed turn"}
```

The expected composer states:

```text
prompt · ready
╭─ ... ─╮
│ Add a test for the new path.                                  │
│                                                                │
╰─ Ctrl+S use suggestion · type to dismiss · 1 line ─╯
```

After the first typed character:

```text
prompt · ready
╭─ ... ─╮
│ x                                                              │
│                                                                │
╰─ Ctrl+S submit · Enter newline · /help · 1 line ─╯
```

## Interfaces and Dependencies

No new third-party dependency is introduced.

`internal/config` must expose:

```go
type Suggestions struct {
    Enabled        bool   `toml:"enabled"`
    EnabledSet     bool   `toml:"-"`
    CakeSuggestBin string `toml:"cake-suggest-bin"`
}

const DefaultCakeSuggestBin = "cake-suggest"

func DefaultSuggestions() Suggestions
func (s Suggestions) Validate() error
func MergeSuggestions(base Suggestions, file *Config) Suggestions
```

`internal/suggest` must expose:

```go
type Options struct {
    Bin       string
    Cwd       string
    SessionID string
    CakeBin   string
    Model     string
    Profile   string
    DebugLog  io.Writer
}

func (o Options) Args() []string

type Document struct {
    Suggested  bool   `json:"suggested"`
    Suggestion string `json:"suggestion"`
    SessionID  string `json:"session_id"`
    Reason     string `json:"reason"`
}

type Result struct {
    Document *Document
    Stderr   string
    ExitCode int
    Canceled bool
    Err      error
}

func Suggest(ctx context.Context, opts Options) Result
```

`internal/app` must expose to `cmd/cake-repl/main.go`:

```go
type SuggestionConfig struct {
    Enabled bool
    Bin     string
}
```

and `Config` must gain `Suggestions SuggestionConfig`. The rest of the
suggestion state and helpers stay unexported in `internal/app`.
