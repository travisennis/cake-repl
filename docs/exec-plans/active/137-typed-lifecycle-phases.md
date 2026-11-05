# Type the app lifecycle phases and session generation

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must
be kept up to date as work proceeds. It is maintained in accordance with
[`docs/workflow/exec-plans.md`](../../workflow/exec-plans.md).

## Purpose / Big Picture

The REPL currently answers lifecycle questions by inspecting overlapping
booleans and pointers: `running`, `hydrating`, `replayPending`, `replayRun`,
`compacting`, `suggesting`, `newSessionPending`, `exitAfter`, and the browser
fields. `submit`, `execCommand`, `canSuggest`, the Ctrl+C dispatch, the title,
the status line, and the composer hint each re-derive a different subset. That
is why a canceled replay can still refuse a prompt with "waiting for session
history to load…", and why the footer can advertise submit while the gate is
closed.

After this work the model answers those questions from one typed lifecycle
state. A user sees the practical difference at the edges:

- Ctrl+N during a run or replay starts a fresh local session immediately, but a
  prompt or `/new`/`/resume`/`/sessions` is refused with a drain-specific
  message until the canceled child is reaped.
- Ctrl+C during compaction cancels it without opening the prompt gate before
  the reaped result is handled.
- A late result from the session the user left cannot change the new session's
  pin, status context, timeline, or composer.
- The status line, terminal title, composer activity label, and footer hint
  name the same operation and only advertise actions the gate accepts.
- A read-only suggestion or session listing can keep draining without blocking
  a prompt; it is still tracked until reaped.

This plan is the design deliverable for task 137. Implementation is split
across tasks 147, 138, 140, 141, 139, and 143. Tasks 142, 145, and 148 remain
deferred; 144 stays independent.

## Progress

- [x] (2026-11-04T19:32Z) Read the ExecPlan and task workflows, the sessions
  and security guardrail, `ARCHITECTURE.md`, and ADRs 001, 004, 011, 023, 025,
  026, 028, 030, 031, and 032.
- [x] (2026-11-04T19:32Z) Audited the current gates in `submit`, `execCommand`,
  `canSuggest`, the Ctrl+C dispatch, `activityState`, `statusLine`, and
  `composerHint`.
- [x] (2026-11-04T19:32Z) Wrote the phase, outcome, generation, admission,
  transition, presentation, and migration design below.
- [x] (2026-11-04T19:32Z) Added the durable lifecycle invariant to
  `ARCHITECTURE.md`.
- [x] (2026-11-04T19:32Z) Ran the docs-only checks recorded under Validation.
- [x] (2026-11-04T20:00Z) 147: extracted `pendingOperation`
  (`internal/app/lifecycle.go`), adopted it for compaction and suggestion, and
  test-covered the caller gate policy for compaction, listing, and suggestion.
- [x] (2026-11-04T20:29Z) 138: typed the run phase and shared admission gate,
  added the session generation boundary and `completedTurn`, and covered the
  Ctrl+N and Ctrl+C drains, stale results, and run/compaction mutual exclusion.
- [x] (2026-11-04T20:10Z) 140: tracked the compaction start notice by a
  stable timeline identity instead of a text prefix; covered duplicate results
  as no-ops and prompt, `/new`, `/resume`, and `/sessions` refusal through a
  canceled compaction's drain.
- [x] (2026-11-05T01:57Z) 141: derived one suggestion phase for disabled,
  idle, in-flight, draining, held, and shown; centralized ghost and placeholder
  writes; added `admitSuggestion`; and used the shared completed-turn answer.
- [ ] 139: type replay hydration and its post-boundary drain.
- [ ] 143: derive title, status, composer activity, hint, and view from the
  presentation state.
- [ ] Move this plan to `docs/exec-plans/completed/` when 143 lands.

## Surprises & Discoveries

- Observation: the current `newSessionPending` flag answers two different
  questions at once: "suppress results from the session the user left" and "a
  child still holds a resource, so keep the gate closed." It is cleared by
  both `finishRun` and `finishCompaction`, even though production ordering
  prevents those two operations from overlapping.
  Evidence: `internal/app/update.go` `startNewSession` and `finishRun`;
  `internal/app/compaction.go` `finishCompaction`.

- Observation: `canSuggest` deliberately ignores compaction but rejects every
  other busy operation, so it cannot simply reuse the prompt gate. ADR 031
  requires a suggestion to start alongside compaction.
  Evidence: `internal/app/suggestion.go` `canSuggest`.

- Observation: Ctrl+N during replay clears `hydrating` but leaves `replayRun`
  and `replayPending` set. `submit` therefore reports "waiting for session
  history to load…" while the canceled replay is actually draining. Keeping
  the gate closed is correct; the phase and the message are wrong.
  Evidence: `internal/app/update.go` `startNewSession` and `submit`.

- Observation: `sessionState.Reset` replaces the whole struct with a zero
  value. A generation stored there must be explicitly carried across the reset
  or every session boundary would restart at the zero epoch.
  Evidence: `internal/app/session.go` `Reset`.

- Observation: the same closed gate currently has different messages for a
  prompt and for a session command. The refusal type must therefore carry the
  capability-specific text instead of exposing one boolean.
  Evidence: `internal/app/update.go` `submit` and `execCommand`.

- Observation: suggestion and listing cancellation invalidate the request
  identity, while run and compaction cancellation retain it so the reaped
  result can still produce an outcome. A single cancel mode in the shared
  helper would let a late suggestion or listing result apply after the user
  typed or closed the browser.
  Evidence: `internal/app/suggestion.go` `cancelSuggestion`; `internal/app/sessions.go`
  `cancelSessionList`; `internal/app/compaction.go` `cancelCompaction`.

## Decision Log

- Decision: one private lifecycle state per operation, with exactly `idle`,
  `in-flight`, and `draining` phases. `outcomeSuccess`, `outcomeNoOp`,
  `outcomeFailure`, and `outcomeCanceled` are completion outcomes, not phases.
  Ghost text and a held suggestion are presentation, not request phases.
  Rationale: the gate depends on whether a resource is held, while the result
  decides what to show. Mixing the two created `newSessionPending`.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: the session generation lives in `sessionState`, starts at 1, and
  advances on `/new`, on Ctrl+N, and on a `/resume` or browser pick that
  actually changes the target. It does not advance when a fresh run merely
  announces its own session id.
  Rationale: generation identifies continuity of the session target. A run
  establishing its first id continues the same target; only leaving a target
  creates a new epoch.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: a stale generation suppresses events and result side effects but
  never prevents the active request from settling its phase. Resource-busy
  gates open only when the child has been reaped.
  Rationale: a stale result that leaves a phase stuck closed reproduces the
  stale-gate defect; dropping the message before settling would leak the
  gate.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: callers use one admission interface for prompt start, session
  retarget, and suggestion start, plus one cancel-target query and one
  presentation derivation. Refusals carry the exact text for the capability.
  Rationale: the three current gates need different policies, but each caller
  should not re-derive them from fields.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: the helper distinguishes `cancel` from `invalidate`. `cancel`
  keeps the active key so a canceled run, replay, or compaction can still
  produce its outcome; `invalidate` moves the key to a drain-only slot so a
  canceled suggestion or listing can never apply after the user's newer
  action.
  Rationale: typing during a suggestion and closing the browser must win even
  if the child completes before cancellation is observed.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: a run, compaction, or replay in either `in-flight` or `draining`
  is resource-busy and gates prompt submission and session retargeting. A
  suggestion or session listing in either phase never gates; it is tracked for
  teardown only.
  Rationale: this is the gate policy in
  [`docs/guardrails/session-and-security.md`](../../guardrails/session-and-security.md).
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: all quit paths converge on one exit phase that cancels every child
  and waits for every operation to settle before returning `tea.Quit`. The
  process-level registry remains the backstop for exits the Update loop never
  observes.
  Rationale: scattered `if exitAfter` branches make it possible to quit while
  a replay or listing is still draining.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: task 146 folds into 138 and 141. The run state records whether the
  current run delivered a clean completion, the session still records its last
  completion, and one accessor answers the shared compaction/suggestion
  question.
  Rationale: both post-turn gates must agree without adding a separate task or
  a third source of truth.
  Date/Author: 2026-11-04 / Travis Ennis.

- Decision: no new ADR is required. Existing ADRs 001, 004, 011, 023, 028,
  031, and 034 already define the external session, replay, compaction,
  suggestion, and teardown behavior. This plan types the internal model and
  removes duplicated gates without changing those contracts.
  Date/Author: 2026-11-04 / Travis Ennis.

## Context and Orientation

`cake-repl` is a Bubble Tea terminal REPL in Go. Each prompt starts one
external `cake --output-format stream-json` process. Startup `-resume <uuid>`
also starts one read-only cake replay process. Optional post-turn operations
start external `cake-compact` and `cake-suggest` processes. Every child is
model-owned, cancellable, and registered with `internal/childproc` for
teardown.

A **phase** is the lifecycle of one request: `idle` (no request), `in-flight`
(the request is active), or `draining` (cancellation was requested and the
child has not yet been reaped). A **completion outcome** is what happened when
the request ended: success, no-op, failure, or canceled. A **request key**
identifies one request: its session generation, its per-operation request id,
and the session id it targeted. A **session generation** is a monotonic epoch
for the current session target. **Resource-busy** means a run, replay, or
compaction is in either `in-flight` or `draining`.

The relevant current files are:

- `internal/app/model.go`: the model fields, initialization, title, and
  `CancelRunning`.
- `internal/app/update.go`: `Update`, `submit`, `execCommand`,
  `startNewSession`, `startRun`, `finishRun`, `finishReplay`, replay messages,
  and the Ctrl+C dispatch.
- `internal/app/compaction.go`: `startCompaction` and `finishCompaction`.
- `internal/app/suggestion.go`: `canSuggest`, start, result, drain, apply, and
  clear paths.
- `internal/app/sessions.go`: the browser listing and its request identity.
- `internal/app/session.go`: the pure `sessionState` run-mode machine.
- `internal/app/shutdown.go` and `internal/childproc`: child tracking and
  reaping.
- `internal/app/view.go`: `View`, `activityState`, `statusLine`, and
  `composerHint`.
- `internal/app/commands.go`: the slash-command gate for `/new`, `/resume`,
  and `/sessions`.

## Plan of Work

### 1. Add one private lifecycle module

Create `internal/app/lifecycle.go`. It owns the phase type, the outcome type,
the request key, the admission result, and one state value per operation. No
phase value or boolean escapes the module; callers ask it questions.

The target types are:

    type phase uint8

    const (
        phaseIdle phase = iota
        phaseInFlight
        phaseDraining
    )

    type outcome uint8

    const (
        outcomeNone outcome = iota
        outcomeSuccess
        outcomeNoOp
        outcomeFailure
        outcomeCanceled
    )

    type sessionGeneration uint64

    type operationKey struct {
        generation sessionGeneration
        request    uint64
        sessionID  string
    }

    type admission struct {
        allowed bool
        reason  refusalReason
        text    string
    }

    type refusalReason uint8

    const (
        refuseNone refusalReason = iota
        refuseExiting
        refuseBrowserOpen
        refuseRunActive
        refuseRunDraining
        refuseReplayLoading
        refuseReplayDraining
        refuseCompactionActive
        refuseCompactionDraining
    )

    type activity uint8

    type presentation struct {
        activity    activity
        label       string
        hint        string
        titleMarker string
        browser     bool
    }

    type cancelTarget uint8

    const (
        cancelNothing cancelTarget = iota
        cancelBrowser
        cancelRun
        cancelReplay
        cancelCompaction
        cancelExit
    )

Run, replay, compaction, suggestion, browser, and exit each have a state value.
The common pending-operation shape holds the phase, active key, cancel
function, and done channel. Run and replay add their `*cake.Run` and their own
result data. Suggestion adds ghost text, held text, and the held key as
presentation fields. Browser adds its listing sub-operation and the map of
canceled listing drains. Exit has only `idle` and `draining`; its cancel
fan-out is synchronous.

The private helper contract for 147 is:

    func (o *pendingOperation) begin(key operationKey, cancel context.CancelFunc, done chan struct{})
    func (o *pendingOperation) cancel()
    func (o *pendingOperation) invalidate()
    func (o *pendingOperation) active() bool
    func (o *pendingOperation) draining() bool
    func (o *pendingOperation) settle(key operationKey) bool

`cancel` moves the operation to `draining`, calls the cancel function, and
keeps the request key, because a canceled run, replay, or compaction still
produces an outcome that the finish handler must classify. `invalidate` also
moves to `draining` and calls the cancel function, but moves the key out of the
active slot into a draining slot; a late suggestion or listing result can only
close that drain and never apply to the composer or browser. This is how typing
during a suggestion or closing the browser stays deterministic even if the
child wins the cancellation race.

`settle` returns false without changing the current state when `key.request`
does not identify either the active or the draining request. When it matches
the draining request, it clears the drain, moves the operation to `idle` when
no other drain remains, and returns false. When it matches the active request,
it moves the operation to `idle`, clears the key, cancel function, and done
channel, and returns true. The caller then checks the
generation before applying side effects. The helper does not know whether its
caller gates input; that remains caller policy. The browser's listing
sub-operation keeps its current map of multiple canceled drains; 147 may leave
that one path alone if a single draining slot would not reduce code.

`stage 147` may land before or after `138`. If it lands first, `138` adopts it
for run and replay. If `138` lands first, it implements the same contract
locally and `147` extracts it from run, compaction, and suggestion. Either
order preserves the behavior and tests in this plan.

### 2. Own the session generation in `sessionState`

Add an unexported `generation sessionGeneration` and a `Generation()` accessor
to `sessionState`. `New` initializes it to 1. `Reset` increments the previous
generation before clearing the remaining session fields, so `/new` and Ctrl+N
cannot rewind it. `UseResume(id)` increments only when the target actually
changes; re-selecting the same target is a no-op and keeps the reported
identity.

Do not increment on `OnTaskStart`, `OnTaskComplete`, or `OnRunEnded`. A fresh
run that announces a session id is establishing the current target, not
leaving it. A failed run without an id also stays in the same generation.
The boundary advances the generation before canceling old children. The
Update loop is single-threaded, so any child result already queued still
carries the old key and enters the settle-then-suppress path described below.

Every child request starts with a key captured from the current generation
and, where it targets a session, the target session id. Events and results
carry the same key. The policy is:

- If the key matches the draining request of an invalidated suggestion or
  listing, clear that drain and settle to idle; never apply the result.
- If the request id matches neither the active nor a draining request, the
  message belongs to an already-superseded request. Drop it without settling
  the current phase.
- If the key is active but the generation or target session is stale, settle
  the phase because the child is reaped, then suppress every side effect.
- If the key is active and current, settle the phase and apply the result.

This distinction is the core of the design: **the generation decides whether a
result may apply; the phase decides whether a resource still blocks new work.**
An invalidated read-only request has no outcome to apply, but it still has a
drain to settle.

### 3. Replace the caller gates with one admission interface

Expose only these callers:

    func (m Model) admitPrompt() admission
    func (m Model) admitRetarget() admission
    func (m Model) admitSuggestion() admission
    func (m Model) cancelTarget() cancelTarget
    func (m Model) presentationState() presentation

`admitPrompt` and `admitRetarget` check, in order: exit, browser open, run
busy, replay busy, compaction busy. They allow otherwise. `admitSuggestion`
checks exit, browser open, run busy, and replay busy, but deliberately ignores
compaction. The suggestion's own request, ghost, held text, draft, and
completed-turn conditions stay in `canSuggest`; it starts only after a clean
completed turn.

`cancelTarget` returns the browser when it is open, otherwise the run,
replay, or compaction when busy, otherwise exit. A suggestion is never the
target of Ctrl+C. Ctrl+C during a run or compaction cancels that operation but
does not exit; Ctrl+C during replay cancels and exits, matching the startup
hydration behavior. A second Ctrl+C during a draining run or compaction
re-cancels it without opening the gate.

The refusal texts are part of the behavior, not incidental strings. Preserve
the existing in-flight strings and add the drain-specific strings:

| Blocking state | Prompt start (`Ctrl+S`) | Session retarget (`/new`, `/resume`, `/sessions`) |
| --- | --- | --- |
| Run in flight | `a task is already running — Ctrl+C cancels it` | `finish or cancel the running task first` |
| Run draining | `the canceled task is still stopping — wait for it to exit` | `wait for the canceled task to exit first` |
| Replay loading | `waiting for session history to load…` | `wait for session history to load first` |
| Replay draining | `waiting for canceled session history to exit…` | `wait for canceled session history to exit first` |
| Compaction in flight | `context compaction is running — wait for it or press Ctrl+C to cancel` | `wait for context compaction to finish (Ctrl+C cancels it)` |
| Compaction draining | `canceled context compaction is still stopping — wait for it to exit` | `wait for canceled context compaction to exit first` |
| Browser open | `close the session browser first` | `close the session browser first` |
| Exiting | silent; input ignored | silent; input ignored |

A refusal reason maps to a capability-specific string, so the same run-busy
reason produces the two distinct existing messages. `admitSuggestion` never
displays its refusal; an automatic suggestion has no user-facing warning.

### 4. Define the legal transitions

The operation phases and their legal transitions are:

| Event | Run | Replay | Compaction |
| --- | --- | --- | --- |
| Start | `idle → in-flight`; capture a key and one run | `idle → in-flight`; capture a key, starting or started | `idle → in-flight`; capture a key and show the start notice |
| Success | `in-flight` or `draining → idle`; outcome success only for a clean completed turn; pin the same generation | `in-flight` or `draining → idle`; outcome success; keep the explicit pin | `in-flight` or `draining → idle`; outcome success; apply marker and estimate only for the same key |
| No-op | not applicable; an empty prompt never starts | not applicable | `compacted:false` → idle; outcome no-op; resolve the start notice as skipped |
| Failure | `in-flight` or `draining → idle`; outcome failure; report the failure; pin an announced same-generation id | `in-flight` or `draining → idle`; outcome failure; warn; keep the pin | `in-flight` or `draining → idle`; outcome failure; warn; keep session and figure |
| Cancellation | `in-flight → draining`; cancel the child; keep the gate closed; same-generation events may still apply, and a same-generation announced id still pins on the reaped result | `in-flight → draining`; cancel the child; stop applying records; keep the gate closed | `in-flight → draining`; cancel the child; keep the gate closed until the result is handled |
| Supersede | Ctrl+N advances the generation; settle on the reaped result, then drop its events, outcome, and pin | Ctrl+N advances the generation; settle on the reaped result, then drop records, warning, and context | A stale key settles on the reaped result but applies no marker or estimate; a stale held suggestion is cleared |
| Session boundary | `/new` and `/resume` require idle; Ctrl+N advances the generation and cancels a busy run into draining | `/new` and `/resume` require idle; Ctrl+N advances the generation and cancels into draining | `/new` and `/resume` require idle; Ctrl+N advances the generation, cancels into draining, and keeps the gate closed |
| Reap / drain | `draining → idle`; open the gate if no other resource-busy phase remains | `draining → idle`; open the gate if no other resource-busy phase remains | `draining → idle`; resolve the notice, apply a same-generation held suggestion, then open the gate |

| Event | Suggestion | Browser | Exit |
| --- | --- | --- | --- |
| Start | `idle → in-flight`; no input gate | `idle` or `draining → in-flight` (overlay open; old listing drains continue); gate prompt and retarget; cancel any suggestion | any state where exit is accepted (not an open browser) → `draining`; ignore input; cancel every child |
| Success | Active `in-flight → idle`; outcome success; show the ghost, or hold it while compaction is busy. An invalidated drain settles without applying | A current listing success updates the loading/ready/empty presentation; the browser remains in flight | Process exits after all operations settle |
| No-op | Active `suggested:false` → idle; outcome no-op; silent | An empty listing is presentation, not a phase | not applicable |
| Failure | Active `in-flight → idle`; outcome failure; warn once for an operational or malformed failure, while a wrong-session document stays debug-only. An invalidated drain is silent | A current listing failure is the browser's non-fatal error presentation | not applicable |
| Cancellation | `in-flight → draining`; invalidate the request and cancel the child; never gate input | Close `in-flight → draining` when a listing is active, otherwise idle; invalidate and cancel the listing; no gate after close | Idempotent; keep draining |
| Supersede | A stale generation settles the active request but applies nothing; an invalidated request only clears its drain; a session boundary clears ghost and held text | A superseded listing request does not touch the current browser | not applicable |
| Session boundary | `/new`, Ctrl+N, `/resume`, and a browser pick cancel the request; a changed target advances the generation | Enter closes the browser and retargets through `resumeSession`; Esc closes without a generation change | No retarget while exiting |
| Reap / drain | `draining → idle`; a new suggestion may start, but input was never gated | `draining → idle`; no input gate | `tea.Quit` only when every operation is idle |

The browser's `loading`, `ready`, `empty`, and `error` variants are
presentation states inside `in-flight`, not lifecycle phases. Task 142 may
collapse those fields later without changing this contract.

### 5. Fold the completed-turn answer from task 146

`runLifecycle` records `sawComplete` and its `outcome`. A clean completed turn
is exactly `outcomeSuccess` for the current generation. `completedTurn()` or
its equivalent is the one accessor used by both `shouldCompactAfter` and
`canSuggest`; `session.LastComplete` remains the per-session record. A
canceled, errored, or incomplete run does not qualify. Replay hydration does
not create a live completed turn, so it never triggers a post-turn operation.

### 6. Derive every presentation surface once

`presentationState()` reads the lifecycle, the suggestion's presentation
fields, and the composer draft. It returns an activity, a label, a footer
hint, a title marker, and the view choice used by `View`, `statusLine`,
`composerHint`, `activityState`, and the title command. The existing title
markers `[working]` and `[compacting]` are preserved. New activity states get
matching markers (for example `[loading]`, `[stopping]`, `[browsing]`, and
`[exiting]`) so a terminal with no visible status line still names the
operation; task 143 finalizes the exact strings and docs. Idle keeps no
marker.

The activity labels are `ready`, `running`, `compacting <elapsed>`,
`stopping task`, `stopping compaction`, `loading history`, `stopping history`,
`browsing sessions`, and `exiting`. The composer hint is chosen from the same
state: submit hints only when `admitPrompt` allows, cancel/new-session hints
while a run or compaction is in flight, new-session only while one drains, and
browser/exit hints while those states own input. A visible suggestion keeps its
existing footer. This is the only place that decides which operation is named,
so the status line and composer cannot disagree.

Replay is still not a live task: it renders as `loading history` or `stopping
history`, never as `running`. Task 139 updates the replay gate wording in the
guardrail; task 143 updates the sentence that says the running state stays idle
to say hydration never renders as running while its own loading or draining
state is allowed, plus the output guardrail and `README.md` if the new labels
change that surface.

### 7. Converge every quit path on the exit phase

`beginExit()` moves exit to `draining`, cancels every cancellable operation,
and refuses all further input. Each finish handler settles its phase and then
calls `maybeQuit()`; `maybeQuit()` returns `tea.Quit` only when every operation
is idle. This covers a suggestion or listing that is still draining when the
user quits. `main`'s `childproc.Registry` remains the unobserved-exit backstop
and keeps the same five-second `ChildTeardownTimeout`.

## Invariant Matrix

| Invariant | Enforcement | Behavioral test |
| --- | --- | --- |
| Run and compaction never overlap | Compaction starts only from a clean completed run; `admitPrompt` and `admitRetarget` reject a busy run | Start compaction from `finishRun`; assert no prompt/retarget can start it alongside a run |
| Suggestion may overlap compaction | `admitSuggestion` ignores compaction; result is held while compaction gates the composer | Start both; result arrives early; ghost appears only after compaction is reaped |
| Browser is exclusive with run, compaction, and an applicable suggestion | Open is idle-only; opening invalidates a suggestion and cancels it to a teardown-only drain; run/compaction gates reject `/sessions` | Open browser with each operation; assert refusal, no overlay, and no suggestion application |
| Replay is read-only, but a canceled replay drains before another cake process starts | Replay phase in flight or draining gates prompt and retarget | Ctrl+N during replay; assert drain text; accept a prompt only after the result |
| Replay never overlaps a run, compaction, suggestion, or browser | Startup replay is the only live operation; `admitPrompt`, `admitRetarget`, `admitSuggestion`, and the automatic triggers reject while replay is busy | Start with `-resume`; assert prompt, retarget, suggestion, and browser are refused until the replay is reaped |
| One mutating run at a time | Run phase is a single value; prompt gate rejects any busy run | Two submits; only one child starts |
| Quit drains every child within `ChildTeardownTimeout` | Exit phase plus the process registry waits on done channels | Quit during run, replay, compaction, suggestion, and listing; assert each is reaped |
| A stale generation never mutates the new session | Key check before every event/result side effect | Ctrl+N during each operation; assert no pin, context, timeline, or ghost change |
| A stale result still releases a resource-busy gate when reaped | Phase settlement precedes the generation check | Ctrl+N during run and compaction; submit is refused until the reaped result, then accepted |
| Suggestion and listing drains never gate input | Only run/replay/compaction are resource-busy | Type a prompt while each read-only child drains; it starts |
| Every gate and surface has one source | Callers use the admission methods and `presentationState()` | Table test over reachable states asserts text and accepted actions |

## Gate Mapping

| Current caller or derivation | New source |
| --- | --- |
| `submit` replay/run/compaction and `newSessionPending` branches | `admitPrompt()` and its capability-specific refusal |
| `execCommand` `needsIdle` branches | `admitRetarget()` |
| `canSuggest` busy-state checks | `admitSuggestion()` plus suggestion/session conditions |
| Ctrl+C dispatch | `cancelTarget()` |
| `activityState` | `presentationState().activity` |
| `statusLine` state segment | `presentationState().label` |
| `composerHint` | `presentationState().hint` |
| `windowTitle` and `titleCmd` | `presentationState()` (task 143) |
| `View` browser/timeline choice | `presentationState().browser` (task 143) |

## Concrete Steps

All commands run in `/Users/travisennis/Projects/cake/cake-repl`.

The design work is docs-only:

    git status --short --branch
    git diff --check
    ahm status

The dependency edges are: 147 depends on 137; 138 depends on 137; 140
on 147; 141 on 140 and 147; 139 on 138; 143 on 138, 139, 140, and 141. Task
146 is folded into 138 and 141, not scheduled separately. Work 138 and 147 in
either order. This recommended order starts with 147 because the helper
removes duplication before the run migration:

Implementation order, one task branch and task lifecycle per slice:

1. 147: add `pendingOperation` and its tests; adopt it only where it removes
   duplication. It owns mechanism, not gate policy.
2. 138: add the run and exit phases, `completedTurn()`, generation boundaries,
   and the admission gate. Preserve the exact refusal strings above. Add
   `just test-race` because the run lifecycle and messages change.
3. 140: put compaction on the helper and its key; keep the gate closed through
   cancel and drain; test success, no-op, failure, cancellation, and stale
   results.
4. 141: put suggestion on the helper; keep ghost and held text as presentation;
   use the shared completed-turn answer; preserve ADR 031 overlap and the
   no-gate rule.
5. 139: replace hydration booleans with the replay phase; distinguish loading
   from draining; test Ctrl+N, a prompt during the drain, and acceptance after
   the reaped result.
6. 143: replace the presentation switches with `presentationState()`; cover
   idle, run in flight, run draining, compaction in flight, compaction
   draining, replay loading, replay draining, browser loading/error/empty/list,
   suggestion with empty and non-empty input, and not-ready. No
   running-plus-compacting test; 138 owns that impossible state.

Do not start 142, 145, or 148 until this core lands. 144 is independent.

## Validation and Acceptance

The design is complete when all of the following are observable:

- The plan names every phase and every legal transition, including start,
  success, no-op, failure, cancellation, supersede, session boundary, and
  drain. Outcomes and presentation are explicitly separate types.
- The plan defines the admission interface and maps `submit`, `execCommand`,
  `canSuggest`, Ctrl+C, `activityState`, `statusLine`, and `composerHint` to it.
- The plan defines the session generation and explains how `/new`, Ctrl+N, and
  `/resume` advance it. It states the suppression rule and the rule that a
  stale result still settles the phase before releasing the gate.
- The invariant matrix and migration order are explicit, and each slice has a
  behavioral test list.
- `ARCHITECTURE.md` contains the durable invariant.
- This plan is in `docs/exec-plans/active/` and is linked from task 137.

Per-slice behavioral tests:

- 147 settles only the active request, cancels into `draining`, and proves that
  the helper itself does not gate; compaction, suggestion, and listing callers
  choose their own policy.
- 138 asserts the prompt and retarget refusal texts for a running and a
  draining run, Ctrl+N during a run, acceptance after the reaped result, no
  stale pin or event, and that exit waits for the run.
- 140 asserts the compaction gate stays closed after Ctrl+C, the canceled
  result is reaped before a prompt starts, Ctrl+N drops the stale estimate and
  notice, and the held suggestion waits for the reaped compaction.
- 141 asserts a suggestion in flight or draining never blocks a prompt, ghost
  and held text are presentation, the completed-turn accessor rejects
  canceled/errored/incomplete runs, and a second request waits for the drain.
- 139 asserts live hydration uses the loading refusal, a post-Ctrl+N prompt
  uses the drain refusal, the prompt is accepted only after the result, and no
  stale record enters the timeline.
- 143 asserts a table of reachable states where the title, status, activity,
  hint, and view agree and the hint advertises only accepted actions. Keep the
  existing strings unless a new drain or presentation state requires one.

Docs-only verification for task 137: `git diff --check` reports no whitespace
errors; `ahm status` reports a valid store with current generated indexes; a
rendered read of the plan and `ARCHITECTURE.md` shows no broken links or
contradictions. No production code is changed, so no Go build or test command
is used for this task.

## Idempotence and Recovery

This plan is additive documentation. Re-running the docs checks is harmless.
Each implementation slice is independent enough to revert by reverting its
commit; no persisted state or configuration migration is introduced. Do not
run two implementation branches against the same lifecycle fields at once. If
a slice leaves a phase stuck, the failure is in settlement order: the active
request must move to `idle` even when its generation is stale.

## Artifacts and Notes

The design's safety property is:

    generation mismatch  => suppress application
    active request reaped => settle phase and release the resource gate

The first line prevents a late result from changing the session the user left.
The second prevents stale suppression from becoming a stuck gate. They must be
implemented in that order, not collapsed into one boolean.

The current refusal strings being preserved are recorded in the gate table
above. The new strings are only for the previously undifferentiated drain
states: a canceled task, replay, or compaction that has not yet been reaped.

## Interfaces and Dependencies

`internal/app/lifecycle.go` is package-private and depends only on
`context`, `time`, and `internal/cake` types needed by the run and replay
states. `internal/app/session.go` owns `sessionGeneration` and its monotonic
transitions. `internal/cake`, `internal/compact`, `internal/suggest`, and
`internal/childproc` are unchanged; this is an app-state refactor.

Required caller signatures:

    func (m Model) admitPrompt() admission
    func (m Model) admitRetarget() admission
    func (m Model) admitSuggestion() admission
    func (m Model) cancelTarget() cancelTarget
    func (m Model) presentationState() presentation

Required state transitions:

    func (s *sessionState) Generation() sessionGeneration
    func (s *sessionState) Reset()                 // preserves and increments the generation
    func (s *sessionState) UseResume(id string)    // increments only on an actual target change
    func (p *pendingOperation) begin(key operationKey, cancel context.CancelFunc, done chan struct{})
    func (p *pendingOperation) cancel()
    func (p *pendingOperation) invalidate()
    func (p *pendingOperation) settle(key operationKey) bool

## Outcomes & Retrospective

The design milestone is complete. It replaces the ad-hoc lifecycle flags with
one private phase model, separates completion outcomes and ghost presentation
from phases, defines a monotonic session generation, and gives every caller a
single admission source with exact refusal texts. It also records the
resource-busy gate policy explicitly: run, compaction, and canceled replay
block prompt and retarget until reaped; suggestion and listing drains never
gate.

The implementation is not complete. This plan stays in the active bucket
until task 143 lands; its Progress section tracks the remaining slices. The
main lesson is that `newSessionPending` conflated two concerns that must stay
separate in the implementation: generation invalidation and resource-busy
draining. The design's settlement order is the guard against reproducing the
stale gate.

No ADR is added because the external behavior is already governed by ADR 001,
ADR 004, ADR 011, ADR 023, ADR 028, ADR 031, and ADR 034. The durable
architecture change is recorded in `ARCHITECTURE.md`; the user-facing status
and refusal wording changes are assigned to tasks 139 and 143.
