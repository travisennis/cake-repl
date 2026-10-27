---
status: accepted
date: 2026-10-27
decision-makers: Travis Ennis
---
# Offer after-turn prompt suggestions from an external suggester

## Context and Problem Statement

A REPL turn ends with cake's completion record, but the next prompt is still the
user's to compose. In a long session the useful next step is often obvious from
the transcript, and retyping it costs attention. cake-repl already has a
post-turn integration pattern for one external sibling binary: `cake-compact`
reads the pinned session, decides for itself, and returns one JSON document
(ADR 023). Prompt suggestion can use the same boundary shape.

The integration needs a suggester that does not exist in this repository yet:
`cake-suggest`. This decision defines the REPL side of that contract so the
suggester can be built independently without cake-repl learning to read session
files or duplicate cake's transcript projection.

A suggestion is a metered model call, so it must be opt-in and must never run
without a trusted enablement. It must also never block the user: the REPL's
single Update loop stays free, the composer remains editable, and a late result
cannot overwrite a draft or target a session the user has left.

## Decision Drivers

- The user should be able to accept a suggested next prompt with one existing
  key (`Ctrl+S`) instead of retyping it.
- A suggestion must be visibly a suggestion, not user-authored text, and any
  typing must discard it rather than silently appending to it.
- The feature spends model quota after every completed turn; it is off by
  default and only trusted config may enable it or choose its executable.
- The REPL must not read cake session files, parse human text, or duplicate
  cake's agent loop. The suggester is an external process with a documented
  one-document contract, like the compactor.
- Input responsiveness is non-negotiable: an in-flight suggestion is a
  background read-only request, not a gate like compaction.
- A suggestion must never become a slash command by accident.

## Considered Options

- **Generate the suggestion inside cake-repl through a bare cake invocation.**
  Rejected: it makes the REPL own prompt engineering and a second model-call
  path, spends quota on every turn without a separable external contract, and
  cannot be iterated or replaced independently of the REPL.
- **Pass the visible transcript to the suggester on stdin.** Rejected: the
  timeline is a rendering view, not the session. Sending it would create a
  second transcript contract, omit session records the timeline does not keep,
  and make the payload grow with the UI's retention policy. The session id is
  the stable handle, and the external suggester can read it through cake.
- **Prefill the textarea with real editable text.** Rejected: it is
  indistinguishable from a draft, a first keystroke would append to the
  suggestion instead of replacing it, and an accidental Ctrl+S would send text
  the user never wrote. Dim placeholder ghost text is unambiguous.
- **Block prompt submission while the suggester runs.** Rejected: suggestions
  are optional and read-only; gating the composer would make every turn wait on
  a model call the user did not ask for.
- **External `cake-suggest` subprocess with a ghost placeholder (chosen).** It
  reuses the compactor's boundary, keeps the REPL free of session-file access,
  and makes acceptance and dismissal explicit.

## Decision Outcome

Chosen option: add an opt-in `[suggestions]` table and one external
`cake-suggest` request after a completed turn, with the result shown as placeholder
ghost text in the composer.

```toml
[suggestions]
enabled = false
cake-suggest-bin = "cake-suggest"
```

- **Invocation.** The REPL runs only
  `cake-suggest suggest -json [-cake-bin <path>] [-model <name>] [-profile <name>] -- <session-id>`.
  Empty optional values are omitted so the suggester applies its own defaults.
  The session argument follows `--` so it can never be parsed as a flag. The
  process starts on the same POSIX session boundary as a cake run
  (`internal/childproc`), inherits the REPL environment and `-cwd`, and is
  bounded by a model-owned context and a fixed timeout. It runs only from
  `internal/suggest`; the REPL never reads or writes the session file.
- **Result document.** Exit 0 emits one JSON object. `suggested` is a boolean,
  `suggestion` is the prompt text, `session_id` identifies the session the
  suggester read, and `reason` is an optional explanation for a no-op. Unknown
  fields are ignored. A document with `suggested:false` is a silent no-op. A
  document whose `session_id` is not the request's session is rejected. The
  suggestion is sanitized, whitespace-collapsed to one line, and bounded before
  it reaches the composer.
- **Trigger.** One request may run at a time. It starts after a turn that
  delivered a completion record and ended cleanly, once the session pin is
  settled. It is skipped for canceled runs, start/wait errors, non-zero exits,
  hydration, replay, an open session browser, `-no-session`, and while a
  compaction is running. If the turn starts an automatic compaction, the
  suggestion starts only after that compaction's result is reaped, so it never
  reads a session while the compactor appends to it.
- **Apply.** The result populates `input.Placeholder` with dim ghost text only
  when the request identity is current, the session pin is still the one the
  request named, no run/hydration/compaction/browser is active, the input is
  empty, and no other suggestion is showing. A draft, a typed character, or a
  session change wins over the suggestion. Applying it adds no timeline row and
  does not change the composer's `ready` state.
- **Accept and dismiss.** `Ctrl+S` with an empty input submits the suggestion as
  an ordinary user prompt, bypassing slash-command parsing so model output can
  never execute a command. Any composer key that can edit or recall input
  dismisses the ghost before it is applied. `Ctrl+U`, `Ctrl+N`, `/new`,
  `/resume`, `/sessions`, submit, quit, and an unobserved exit cancel an
  in-flight request. Scrolling, tool-output cycling, and clipboard copy do not.
- **Failure and cancellation.** Cancellation and `suggested:false` are silent. A
  missing binary, non-zero exit, malformed document, or timeout produces at most
  one visible warning per REPL process and a content-free debug-log lifecycle
  line. A suggestion failure never changes the session, the input, or the
  status line.
- **Provenance.** The automatically loaded project-local `.cake-repl.toml`
  cannot enable suggestions or select `cake-suggest-bin`; each is ignored with a
  startup warning. It may set `enabled = false` to disable the feature for that
  project. XDG config and an explicit `-config` are trusted. `-no-session`
  disables suggestions because there is no pinned session to read.
- **Scope.** Suggestions are one-shot per completed turn. There is no history
  of past suggestions, no automatic submission, no inline editing of the ghost,
  and no fallback that reads session files when `cake-suggest` is missing. The
  feature stays experimental until live suggestion quality is evaluated.

### Consequences

- Good, because the user can accept an obvious next prompt with Ctrl+S and
  never has to distinguish suggested text from a draft.
- Good, because the engine boundary is preserved: the suggester is an external
  CLI with a JSON contract, and the REPL never learns cake's session format.
- Good, because the composer never blocks on a suggestion and a late result
  cannot clobber typed input or target another session.
- Bad, because enabling the feature requires a second external binary and a
  metered model call after every completed turn.
- Bad, because the ghost is not editable in place: a user who wants to change
  one word must accept it first or type their own prompt from scratch.
- Neutral, because the REPL adds no timeline noise for success or no-op; the
  existing one-warning-per-process rule keeps a misconfiguration visible.

## More Information

- This follows the external-compactor boundary in
  [ADR 023](023-compact-context-between-turns-through-an-external-compactor.md) and the
  untrusted-project-config rule in
  [ADR 006](006-project-local-config-cannot-select-the-cake-executable.md) and
  [ADR 024](024-ignore-project-local-compaction-tuning.md).
- The subprocess discipline (session boundary, SIGTERM then bounded teardown,
  bounded output, content-free debug logging) follows
  [ADR 027](027-isolate-engine-subprocesses-from-the-repl-controlling-terminal.md).
- cake-repl task 127 implements the REPL side. `cake-suggest` is a separate
  sibling binary; its internal design and model choice are out of scope here.
