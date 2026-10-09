# Recover the terminal around an external suspend

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. It must be maintained in accordance with
`docs/workflow/exec-plans.md`. It is complete and lives in
`docs/exec-plans/completed/`.

## Purpose / Big Picture

A user runs `cake-repl` in one terminal and, from another shell, sends it a stop
signal — `kill -TSTP <pid>`. Today the REPL is stopped with the terminal still in
raw mode and the alternate screen still active. The user's shell takes the
terminal back for its prompt, and when the user runs `fg` the REPL resumes with a
terminal it no longer really owns: typed characters echo, the Up arrow arrives as
the literal characters `^[[A` instead of recalling history, mouse reports are
never consumed, and the frame is redrawn from a cursor position the renderer no
longer has, so the shell's job-control notice stays inside the managed region.
The session is not lost, but the REPL is no longer usable, and the only way out
is to quit and restart.

After this change, an external SIGTSTP behaves like a normal job-control
suspension. The shell gets a usable prompt while the REPL is stopped; `fg`
returns to the same conversation, with one correctly placed composer and status
line, working prompt editing and history, consumed mouse reports, and the
session pin, timeline, and composer contents untouched. If a cake run was in
flight when the signal arrived, it is paused rather than killed, and its output
arrives after the resume.

You can see this working by driving the built binary in a tmux pane against the
checked-in fake cake (no real cake, no cost): start it, send the terminal's own
job-control stop, run `fg`, and then press Up and confirm the composer recalls
the prompt instead of showing `^[[A`. The same procedure against the pre-change
binary shows the defect.

## Progress

Implementation and terminal evidence are done; the remaining work is the
regression tests, the documentation sweep, `just ci`, and preflight.

- [x] (2026-10-09T15:05Z) Write this ExecPlan and record the accepted contract in
  `docs/adr/018-recover-terminal-ownership-around-an-external-suspend.md`.
  Docs only; no code yet.
- [x] Milestone 1: reproduce the defect against the pre-change binary and probe
  how a Go program can actually stop itself after taking SIGTSTP (scratch probe,
  not committed).
- [x] Milestone 2: implement the watcher in `cmd/cake-repl/suspend.go`, wire it
  into `cmd/cake-repl/main.go`, and add the Windows counterpart.
- [x] Milestone 3: drive both render modes through idle, running, and
  resize-while-suspended cycles and capture before/after evidence.
- [x] Milestone 4: add the lifecycle regression tests (ordering, abort on a
  failed release, inline anchoring selection, mouse re-enable).
- [x] Milestone 5: documentation sweep — `README.md`,
  `docs/guardrails/session-and-security.md`,
  `docs/guardrails/cli-and-user-output.md`, `ARCHITECTURE.md`, and the
  `drive-tui` skill's mechanics for driving a suspend (the guardrail points at
  the skill for mechanics, so the technique belongs there).
- [x] Preflight review in a subagent and its findings resolved: the three
  CI-blockers it found in the staged snapshot (Windows signature, deprecated
  `x/ansi` constants, unused test parameter) were already repaired, and its doc
  finding (an ADR sentence that had the stop and `fg` in the wrong order) and
  three nits (log tag consistency, a swallowed `kill` error, the duplicated
  `terminalProgram` declaration) are fixed and re-verified.
- [x] Filled the task acceptance notes, moved this plan to
  `docs/exec-plans/completed/`, and committed the branch.

## Surprises & Discoveries

- Observation: a Go program cannot stop itself by re-raising SIGTSTP after
  `signal.Reset`. Once `signal.Notify` has taken SIGTSTP, the runtime keeps the
  signal blocked, and the re-raised signal is swallowed: in a tmux probe the
  process logged the `kill(0, SIGTSTP)` returning success and simply kept
  running, with no job-control stop reported by the shell. This is what forced
  the switch to SIGSTOP, which cannot be caught, ignored, or blocked.
  Evidence: scratch probe `.tmp-sigtest` (deleted before handoff): mode `tstp`
  kept heartbeat-ing after the re-raise; mode `stop` produced
  `zsh: suspended (signal)`, then `[1] + continued` after `fg`.
- Observation: Bubble Tea's `ReleaseTerminal` disables mouse reporting, but
  `RestoreTerminal` re-enables only bracketed paste and focus reporting. Without
  an explicit re-enable the pane's `#{mouse_any_flag}` stays `0` after a resume
  and the wheel no longer scrolls the timeline.
  Evidence: the tmux driver printed `suspended_mouse=0 resumed_mouse=1` only
  with the explicit re-enable; against the pre-change binary it printed
  `suspended_mouse=1` in both states.
- Observation: `RestoreTerminal` already ends with a resize check
  (`go p.checkResize()`), so it both picks up a resize that happened while the
  process was stopped and supplies the render that an idle REPL needs — its
  renderer buffer is empty after the release, so without a message no frame is
  drawn at all. An explicit SIGWINCH nudge was therefore unnecessary.
- Observation: inline mode cannot be restored in place by parking the cursor at
  the bottom of the window. The renderer moves the cursor up by (previous frame
  height − 1) from wherever it is, so the resumed frame lands as many rows high
  as the shell wrote and the previous frame's top remains on screen as
  duplicated chrome (reproduced: two `── ASSISTANT` blocks in one resumed
  capture). Parking the suspension on the alternate screen restores the inline
  region's rows and cursor exactly, and the resumed capture then has exactly one
  composer, one status line, and no duplicates.
  Evidence: `.tmp-captures/inline-02-idle-a-resumed.txt` after the change versus
  the duplicated frame the bottom-parking attempt produced.
- Observation: the resumed job is reported by the shell as
  `zsh: suspended (signal)` rather than `zsh: suspended`, because the stop is
  SIGSTOP. Same job-control semantics; a cosmetic difference worth writing down.

## Decision Log

- Decision: the suspend handler lives in `cmd/cake-repl`, not in `internal/app`.
  Rationale: it owns process-level concerns (signal handling, the process group,
  the terminal) and its only inputs are the `*tea.Program` value and the render
  mode, both of which `main` already holds. `internal/app` and `internal/ui` keep
  their current boundaries and no new message type crosses into the model.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: stop with SIGSTOP after `ReleaseTerminal` (see Surprises).
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: signal the process group, not this process, so the cake child pauses
  with the REPL.
  Rationale: "hold the stream" means the engine is not killed and is not left
  running unwatched; a group stop delivers both, and `fg` continues both with one
  SIGCONT.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: park inline-mode suspensions on the alternate screen (see
  Surprises).
  Rationale: it is the only approach that restores the inline region in place
  without knowing the region's rendered height, and it keeps the alternate-screen
  default mode's behavior identical to what it already was. Accepted cost: the
  shell's own output during the suspension is discarded when the REPL returns.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: a failed `ReleaseTerminal` abandons the cycle and leaves the REPL
  running, instead of stopping anyway.
  Rationale: the defect is stopping with the terminal still in raw mode; falling
  back to it would reintroduce the bug exactly when recovery is already
  uncertain. The failure is written to `-debug-log`.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: write the mouse sequences before `RestoreTerminal` rather than after.
  Rationale: `RestoreTerminal` restarts the renderer, which then writes frames;
  writing our sequences first means no frame can interleave and split them.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: keep the suspend machinery out of `internal/ui` and write the two
  mode sequences through a `writeTerminal` helper that is a no-op unless stdout
  is a character device, matching the existing TTY-guarded exit paths.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: keep the SIGTSTP handler installed for the life of the program and
  never `signal.Reset` it; SIGSTOP needs no re-arming and the handler is what
  makes the next suspend recoverable.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: log the `syscall.Kill` error instead of discarding it, and tag every
  suspend line `suspend:` to match the debug log's existing `stdout:` and `exit:`
  tag format (preflight nit).
  Rationale: if the stop cannot be delivered the REPL silently resumes, and the
  debug log is the only place that can say why; a bare `debug:` prefix would have
  introduced a second tag convention.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: declare `terminalProgram` once in `main.go` instead of in each
  build-tagged watcher (preflight nit).
  Rationale: two declarations of one interface can drift silently, and no build
  tag is needed for a two-method interface.
  Date/Author: 2026-10-09, Travis Ennis (implementation).
- Decision: keep the `.agents/skills/drive-tui/SKILL.md` addition in this change
  and record its evidence as deferred.
  Rationale: the guardrail for agent-facing instructions requires naming the
  motivating failure (task 092: a suspend was unrecoverable and the mechanics for
  reproducing one existed nowhere) and the behavior the edit should change (a
  future agent driving a suspend arms the pane's line discipline and checks the
  mouse flag instead of guessing). The narrowest probe — a fresh agent session
  given a suspend-rendering task, checking whether it retrieves and follows §5a —
  is not run in this change. Deferred probe: in a fresh session, ask an agent to
  verify a suspend/resume rendering change and check whether it arms ISIG, sends
  Ctrl+Z, and reads `#{mouse_any_flag}` rather than only calling `capture-pane`.
  Date/Author: 2026-10-09, Travis Ennis (implementation).

## Outcomes & Retrospective

The recovery works in both render modes. Four cycles were driven against the
built binary in a tmux pane: two idle, one with a cake run in flight, and one
with the terminal resized while stopped, plus a fifth started while `-resume`
replay hydration was streaming. Every cycle released the terminal
(`suspended_fg=zsh`, `suspended_mouse=0`), and every resume re-owned it
(`resumed_fg=cake-repl`, `resumed_mouse=1`) with exactly one composer, one status
line, and one state field per captured frame; the running cycle resumed with the
run still in flight, the resize cycle came back at the new width, and the
hydration cycle finished with the replayed conversation and the resume pin
intact. Against the pre-change binary the same driver resumed with
`icanon isig echo` enabled, left the mouse flag on, and echoed the Up key into
the frame as `^[[A`.

What was cheap: the release/restore pair, because `ReleaseTerminal` and
`RestoreTerminal` are supported API and do most of the work. What needed a call:
how to actually stop the process (SIGTSTP after `Notify` is unreliable — see
Surprises), and inline mode, which needed the alternate-screen parking because
the renderer's row accounting cannot be reset from outside the library.

Remaining gaps, all documented: an uncatchable SIGSTOP suspension cannot release
the terminal first; a REPL killed while stopped can leave its child stopped; a
job continued with `bg` instead of `fg` has the terminal re-owned while a shell
still holds it, exactly as Bubble Tea's own suspend behaves; and inline mode's
shell output during a suspension is discarded.

## Context and Orientation

`cake-repl` is a single Go binary that renders a Bubble Tea terminal REPL and
treats the external `cake` CLI as an engine: each prompt spawns one
`cake --output-format stream-json` process whose NDJSON records are rendered as a
timeline. `internal/cake` owns all interaction with cake; `internal/app` holds
the Bubble Tea model; `internal/ui` is pure rendering. The dependency direction
is one way: `app` depends on `cake` and `ui`, and neither of those depends on
`app`.

Terms used here:

- **Raw mode** — the terminal state a full-screen program needs: no line
  buffering (`ICANON` off), no echo (`ECHO` off), no keyboard signal generation
  (`ISIG` off), so that keystrokes and arrow keys arrive as data.
- **Alternate screen** — a second terminal buffer (`CSI ?1049h` / `?1049l`)
  that saves the cursor and the visible screen and restores both on exit. It is
  cake-repl's default render mode (`-inline` turns it off).
- **Catchable signal** — a signal a process can install a handler for. SIGTSTP
  is catchable; SIGSTOP is not, and cannot be ignored or blocked either.
- **Process group** — the set of processes a terminal stops and continues
  together. A shell with job control puts each job in its own group; cake and the
  REPL it spawns share the REPL's group because the child is started without
  changing it.
- **Release/restore** — `tea.Program.ReleaseTerminal()` gives the terminal back
  (restores termios, shows the cursor, disables mouse and bracketed paste, leaves
  the alternate screen, stops the renderer); `tea.Program.RestoreTerminal()`
  re-owns it (raw mode, input reader, alternate screen, repaint, resize check).

Key files:

- `cmd/cake-repl/main.go` — flag parsing and program startup; the watcher is
  installed here, and this file already holds the other terminal-level helpers
  (`stdoutIsTerminal`, `eraseInlineComposer`, `resetTerminalTitle`).
- `cmd/cake-repl/suspend.go` (new) — the POSIX watcher: the SIGTSTP handler, the
  suspend cycle, inline parking, and the mouse re-enable.
- `cmd/cake-repl/suspend_windows.go` (new) — the same signature as a no-op, since
  Windows has no SIGTSTP and no shell job control.
- `cmd/cake-repl/main_test.go` — where the cycle's ordering tests belong.
- `internal/app/model.go` — `ComposerRows`, `Init`, and the model the terminal
  never needs to change.
- `scripts/fake-cake.sh` and `internal/cake/testdata/fixtures/` — the hermetic
  fake cake used for every terminal capture.

## Plan of Work

The work is one new file in `cmd/cake-repl` plus a two-line change in `main`, so
the plan describes the pieces rather than a long sequence of edits.

`watchSuspend(prog terminalProgram, inline bool, logf func(string, ...any))`
returns a stop function. It registers SIGTSTP with `signal.Notify`, and one
goroutine runs one `suspendCycle` per signal. There is no re-arming to do: the
handler is never removed, and SIGSTOP stops the process without disturbing it.

`suspendCycle` is a struct of injected steps — `release`, `park`, `stop`,
`unpark`, `modes`, `restore`, `logf` — and `run()` calls them in that order,
returning early (without stopping) if the release fails. Injecting each step is
what makes the ordering contract testable without a terminal or a real signal;
it is also the only reason the steps are fields rather than a straight-line
function.

`parkSequence(inline)` and `leaveParkSequence(inline)` return the alternate-screen
enter/leave pair for inline mode and the empty string otherwise; `parkCursor` in
an earlier draft was a closure that hid that choice, but a closure cannot be
observed by a test, so the selection is a pure function the watcher wraps in
`writeTerminal` instead. `mouseSequence()` returns the two reviewed mouse mode
sequences, and `writeTerminal(seq)` writes a non-empty sequence to stdout only
when stdout is a character device.

In `main`, immediately before `p.Run()`:

    if stdoutIsTerminal() {
        stopSuspend := watchSuspend(p, *inline, debugLogf(cfg.DebugLog))
        defer stopSuspend()
    }

`debugLogf` is a small `io.Writer`-backed printf used for the lifecycle
diagnostics that have no timeline surface; it writes nothing when `-debug-log`
is unset.

Milestone 4 adds `cmd/cake-repl/suspend_test.go`: that `run()` calls release,
park, stop, unpark, modes, restore in exactly that order; that a failing release
stops before `stop` is ever called; that both failures reach the debug log; that
the park/leave pair is the alternate-screen pair for inline mode only; that
`mouseSequence` re-enables cell-motion plus SGR reporting; and that installing
and removing the watcher does not panic. The tests are new coverage for new
code, so they do not "fail before the change" in a literal sense; they pin the
contract that the driven session above then verifies end to end. No test needs a
terminal, a signal, or cake.

Milestone 5 updates the durable surfaces listed in Progress, following
`docs/guardrails/documentation.md`: the README gains the behavior and its
limits, `session-and-security.md` gains the terminal-ownership and process-group
rules, `cli-and-user-output.md` gains the inline-mode consequence and the
required capture for a suspend change, `ARCHITECTURE.md` gains the invariant that
the REPL releases the terminal before stopping and restores every audited mode
after SIGCONT, and `.agents/skills/drive-tui/SKILL.md` gains the mechanics for
raising the signal from a pane's line discipline, since the guardrail defers
those mechanics to the skill.

## Concrete Steps

All commands run from the repository root, `/Users/travisennis/Projects/cake/cake-repl`.

Build and unit-test the change:

    just build
    just test ./cmd/...
    just test

Reproduce the defect and confirm the fix in a terminal. The procedure needs
`tmux` (a host tool for this skill only) and the checked-in fake cake; it never
starts a real cake. It follows `.agents/skills/drive-tui/SKILL.md`: build with
`just build`, run the built binary in a tmux pane against
`scripts/fake-cake.sh`, and wait for a visible marker instead of sleeping.

Start a pane running a pristine shell (no rc files, no history, so captures
cannot disclose user state) and launch the REPL inside it:

    root=$(git rev-parse --show-toplevel)
    run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-susp.XXXXXX"); mkdir -p "$run/project"
    session=cake-repl-tui-092
    tmux new-session -d -s "$session" -c "$run/project" -x 100 -y 30 \
      "env -i HOME=$run PATH=/usr/bin:/bin:/usr/sbin:/sbin TERM=xterm-256color zsh -f"
    tmux send-keys -t "$session" -l \
      "FAKE_CAKE_FIXTURE=happy-path FAKE_CAKE_DELAY=0.4 $root/bin/cake-repl \
         -cake-bin $root/scripts/fake-cake.sh -no-config -no-color -no-session \
         -cwd $run/project -history-file $run/history -debug-log $run/debug.log"
    tmux send-keys -t "$session" Enter

Wait for `prompt · ready` in `tmux capture-pane -pt "$session"`, then arm the
pane's job-control key and stop the job the way a terminal does. The REPL leaves
ISIG off in raw mode, so the tty has to be armed from outside for Ctrl+Z to
raise SIGTSTP for the foreground process group:

    tty=$(tmux display -p -t "$session" '#{pane_tty}')
    stty -f "$tty" isig
    tmux send-keys -t "$session" C-z

Expect the shell's notice, its prompt, and the mouse flag released:

    zsh: suspended (signal)  ... cake-repl ...
    mac%
    tmux display -p -t "$session" '#{mouse_any_flag}'   # 0

Then foreground it and check the modes and input:

    tmux send-keys -t "$session" -l 'fg'; tmux send-keys -t "$session" Enter
    stty -f "$tty" -a | sed -n 2p     # lflags: -icanon -isig ... -echo
    tmux send-keys -t "$session" Up   # the composer recalls the prompt

With the alternate screen (the default), the resumed frame is the whole
conversation with one composer and one status line. With `-inline`, the resumed
frame is the conversation with exactly one composer and one status line and no
duplicated blocks. On the pre-change binary the same steps resume with
`lflags: icanon isig ... echo`, leave `#{mouse_any_flag}` at `1`, and echo `^[[A`
into the frame instead of recalling the prompt.

Tear down only the session this procedure started:

    tmux send-keys -t "$session" C-c
    tmux kill-session -t "$session"

Broad gate before handoff:

    just ci

## Validation and Acceptance

The accepted behavior is what the acceptance criteria in task 092 name; the
evidence for each is the driven session above.

- An external SIGTSTP releases terminal ownership: while stopped, the pane's
  foreground process is the shell (`#{pane_current_command}` = `zsh`), the mouse
  flag is `0`, and the shell's own notice and prompt are legible.
- After `fg`, Up recalls history, prompt editing and submission work, and a
  synthetic SGR mouse report (`ESC [ < 0 ; 25 ; 10 M`) does not appear in the
  frame as literal text.
- Resumed frames contain exactly one composer and one status line: the driver
  counts `prompt ·`, the bracketed state field, and `Ctrl+S submit` per capture
  and each is `1`.
- Session pin, timeline, composer contents, and history survive: the captures
  show the same conversation and the same `resume 11111111` next-run field, and
  the Up key recalls the prompt submitted before the suspension.
- Multiple cycles and both render modes work, including a cycle started while a
  run is in flight, a cycle during which the window is resized, and a cycle
  started while startup `-resume` replay hydration was still streaming; the
  running cycle resumes with the composer in its running state and then
  completes, the resize cycle resumes at the new width, and the hydrated cycle
  finishes with the replayed prompt and answer in the timeline and `next: resume`
  unchanged.
- The regression tests pass: `TestSuspendCycleRunsStepsInOrder`,
  `TestSuspendCycleSkipsStopWhenReleaseFails`,
  `TestSuspendCycleLogsFailures`, `TestParkSequencesAreAltScreenOnlyForInline`,
  `TestMouseSequenceReenablesReporting`, and
  `TestWatchSuspendInstallsAndRemovesHandler`.

## Idempotence and Recovery

The change is additive: one new file, one no-op Windows counterpart, and a
guarded install in `main`. There is no migration and nothing is written to disk
except the optional `-debug-log` lines. Re-running the terminal procedure is safe
and starts from the same screen each time as long as the pane is recreated;
`stty -f "$tty" isig` is re-armed per cycle because the REPL re-enters raw mode
on every resume. The scratch driver, probe, and captures used to develop this
live under `.tmp-*` in the repository root and are deleted before handoff;
nothing in `docs/`, `scripts/`, or the test suite depends on them.

If the change has to be backed out, deleting the two new files and the guarded
install in `main` restores the previous behavior exactly, since nothing else
changed.

## Artifacts and Notes

Before the change (default render mode, `-no-color`), a cycle resumed with the
shell's rows inside the managed region and cooked termios:

    zsh: suspended  FAKE_CAKE_FIXTURE=happy-path ... cake-repl-baseline-bin
                                                                           %
    mac% fg
    [1]  + continued  FAKE_CAKE_FIXTURE=happy-path ... cake-repl-baseline-bin
    ^[[A

    resumed_termios: lflags: icanon isig iexten echo echoe -echok echoke ...

After the change, the same cycle in inline mode:

    resumed_termios: lflags: -icanon -isig -iexten -echo echoe -echok echoke ...

and the resumed frame keeps one composer, one status line, and the conversation:

    ── ASSISTANT
      hello there


    ⚙ bash $ ls
      file.txt

    ✓ done in 4.2s · 1 turn · 1 tool call · 100 in / 50 out tokens · session 11111111
    ╭─  prompt · ready ─────────────────────────────────────────────╮
    │┃ hello there                                                  │
    ╰─  Ctrl+S submit · Enter newline · /help · 1 line ─────────────╯
     [ idle ] │ session: 11111111 · next: resume 11111111 · cwd: project

## Interfaces and Dependencies

No new dependency. The implementation uses the standard library
(`os/signal`, `syscall`) plus two sequence constants from
`github.com/charmbracelet/x/ansi`, which `cmd/cake-repl` already imports for its
cursor and erase sequences.

Types and functions that must exist at the end of the work, all in package
`main` under `cmd/cake-repl`:

    // suspend.go (!windows)
    type suspendCycle struct {
        release func() error
        park    func()
        stop    func()
        unpark  func()
        modes   func()
        restore func() error
        logf    func(string, ...any)
    }
    func (c suspendCycle) run() bool

    func watchSuspend(prog terminalProgram, inline bool, logf func(string, ...any)) func()
    func parkSequence(inline bool) string
    func leaveParkSequence(inline bool) string
    func mouseSequence() string
    func writeTerminal(seq string)

    // main.go (both platforms)
    type terminalProgram interface {
        ReleaseTerminal() error
        RestoreTerminal() error
    }
    func suspendLogf(w io.Writer) func(string, ...any)

The suite has no terminal, signal, or cake dependency: `*tea.Program` satisfies
`terminalProgram`, and the tests drive `suspendCycle` with function literals.

## Revision notes

- 2026-10-09: Milestones 1–5 revised as they completed. The interface section
  dropped the `parkCursor`/`unparkCursor`/`reenableMouse` closures for the pure
  `parkSequence`/`leaveParkSequence`/`mouseSequence` functions, because the
  closures' render-mode choice was not observable from a test; the Validation
  section now names the tests that exist rather than claiming new tests fail on
  the old binary; and Progress records the completed milestones. The Decision
  Log gained the reason for writing the mouse sequences before
  `RestoreTerminal`.
- 2026-10-09: revised again after the preflight review. `debugLogf` became
  `suspendLogf` and `terminalProgram` moved to `main.go`; the ADR's Decision
  Outcome now reads in mechanism order; the Decision Log records the preflight
  fixes and the deferred probe for the `drive-tui` skill addition. A suspend
  during `-resume` replay hydration was also driven and captured, extending
  Milestone 3's evidence beyond the live-run cycles.
