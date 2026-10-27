# Isolate cake subprocesses from the REPL terminal

This ExecPlan records a completed cross-cutting change. Its `Progress`,
`Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective`
sections were kept up to date as work proceeded, per
`docs/workflow/exec-plans.md`. It is complete and lives in
`docs/exec-plans/completed/`. The accepted decision is recorded in
`docs/adr/027-isolate-engine-subprocesses-from-the-repl-controlling-terminal.md`.

## Purpose / Big Picture

A cake child can open `/dev/tty` and change the terminal out from under
cake-repl. The reproduced case was a `-cake-bin` wrapper running
`stty sane < /dev/tty`: the REPL's input started echoing, Up arrived as `^[[A`,
key reports were written over the status line, and Ctrl+Z suspended the REPL.
Captured stdout and stderr cannot prevent this because the child never wrote
through them.

After this change, every cake process the REPL spawns — a live prompt, a replay,
and `sessions list` — runs in its own session with no controlling terminal, and
its descendants inherit that boundary. Closing `/dev/tty` fails with `ENXIO`, so
the wrapper's `stty` fails instead of corrupting the pane. Cancellation still
gives cake a three-second graceful window and then kills the whole process
group, so a hung cake cannot leave descendants behind. Suspending the REPL still
pauses engine work: the REPL stops each run's process group explicitly before it
stops itself, and continues them after `fg`.

The result is visible without a real cake. Build the binary, drive it in a tmux
pane with the checked-in fake cake, and use a wrapper that runs
`stty sane < /dev/tty` before `exec`. Before this change the pane is left in
cooked, echoing mode. After it, the wrapper's `stty` fails, the composer stays
raw, Up recalls history, mouse reports are consumed, and Ctrl+Z does not
suspend the REPL. The same run must be checked in full-screen and inline modes
with `-no-color`.

## Progress

Implementation and verification are complete; the progress record follows.

- [x] (2026-10-26) Reproduce the `/dev/tty` path and record it in task 093.
- [x] (2026-10-26) Probe `Setsid` versus `Setpgid` on macOS in a real pty; probe
  leader-only versus process-group termination.
- [x] (2026-10-26) Accept ADR 027 and write this plan.
- [x] (2026-10-27) Apply the branch review: align the group-kill timing, state
  the pipe-ownership policy, guard group signals, record the hangup and listing
  limits, correct the terminal evidence, and file the compactor follow-up.
- [x] (2026-10-27) Milestone 1: add `configureProcess` with `Setsid` to the
  live, replay, and listing spawns; add Getsid session-identity tests.
- [x] (2026-10-27) Milestone 2: add the REPL-owned grace timer, the process-group
  SIGKILL, and tests for a descendant holding both pipes and stdout only.
- [x] (2026-10-27) Milestone 3: add the registry, app/main wiring, and suspend
  stop/continue order; cover registry lifetime and the cycle order.
- [x] (2026-10-27) Run `just test-race` and `just ci`; capture macOS full-screen,
  inline, pre-fix, and suspend evidence; verify Windows/Linux cross-builds.
- [x] (2026-10-27) Milestone 4: update README, ARCHITECTURE, and the security
  guardrail; run preflight and apply its four minor findings; move this plan to
  the completed bucket.
- [x] (2026-10-27) Fill task 093's Acceptance Notes and complete the task.

## Surprises & Discoveries

- Observation: a process group alone does not remove `/dev/tty`. Inside a tmux
  pty on macOS 27.0.1 with Go 1.27.3, an inherited child and a `Setpgid`-only
  child could both read and write `/dev/tty`; a `Setsid` child received
  `ENXIO` ("Device not configured") for both. This is the kernel rule for a
  session leader with no controlling terminal, not a macOS quirk.
  Evidence: the probe output under `Artifacts and Notes`.
- Observation: killing only the process leader is not enough. A shell that
  started `sleep 30` in the background and ignored SIGTERM left the descendant
  alive after a leader-only SIGTERM; a SIGTERM to the group reaped both.
  Evidence: the second probe transcript under `Artifacts and Notes`.
- Observation: macOS exposes `syscall.Getsid`, but Go's Linux `syscall`
  package does not. The tests use a small `getSID` helper: `syscall.Getsid` on
  macOS/BSD and `SYS_GETSID` on Linux. A detached child's session id equals its
  own pid, so the core mechanism is CI-verifiable even though CI has no
  controlling terminal.
- Observation: `exec.Cmd.WaitDelay` kills only the child pid. Racing a REPL-owned
  group-kill timer against it makes cleanup unreliable because the leader can be
  reaped before the group signal lands, and a reaped pid may be reused. The plan
  therefore fires the group signal at the accepted grace window and sets the
  command's own `WaitDelay` one second later as a redundant fallback.
- Observation: `WaitDelay` does not, by itself, bound a descendant that holds
  only the runner's stdout pipe. `Start` reads `StdoutPipe` to EOF before calling
  `cmd.Wait`, so the reader is blocked before `Wait` can enforce `WaitDelay`. A
  2026-10-27 probe with a 300 ms `WaitDelay` and a descendant holding stdout but
  not stderr unblocked only after the descendant's 2 s lifetime; holding both
  pipes unblocked at 300 ms. The REPL-owned group SIGKILL is the real bound, and
  the descendant test needs a stderr-redirected variant to prove it.
- Observation: detaching the child also removes the terminal's group-directed
  hangup signal, so a terminal close or an abrupt REPL death can leave cake
  running; normal quit and cancel paths still stop it. This is documented as a
  limit rather than solved with platform-specific parent-death signals.
- Observation: `cmd/cake-repl/suspend.go` stops the REPL's process group with
  `syscall.Kill(0, syscall.SIGSTOP)`. Detaching cake silently removes it from
  that group, so the suspend change is required, not optional.
- Observation: the opt-in compactor (`internal/compact`) is a second subprocess
  path with the same exposure. It is deliberately out of this task's scope and
  is recorded as a known gap in ADR 027.
- Observation (2026-10-27 implementation): removing the `configureProcess` call
  from `Start` makes `TestStartDetachesSession` fail with `sid != pid`, so the
  test observes the attribute rather than merely the process starting.
- Observation (2026-10-27 drive-tui): the post-fix adverse wrapper records
  `stty status=1` and `/dev/tty: Device not configured`, while the pane stays at
  `-icanon -isig -echo`; the pre-fix master binary leaves it at
  `icanon isig echo` and echoes `^[[A`. The suspend probe kept the timestamp log
  at one line through the stop and grew it from one to five lines after `fg`.
- Observation: Linux was cross-compiled through the GoReleaser snapshot and the
  package tests compile for Linux, but no Linux host was available locally; CI
  is Ubuntu and must be the execution check there.

## Decision Log

- Decision: use `SysProcAttr{Setsid: true}` for the live run, replay, and
  `sessions list` on POSIX; leave Windows unset and document that no isolation
  is claimed there.
  Rationale: it is the only probed mechanism that removes `/dev/tty`, it needs
  no new dependency, and one helper keeps the three spawn sites consistent.
  Date/Author: 2026-10-26, Travis Ennis (accepted in ADR 027).
- Decision: signal the leader with SIGTERM first; at the grace deadline send
  SIGKILL to the process group only when the run has not been reaped; do not
  signal the group after the run is reaped. `cmd.WaitDelay` is the later,
  PID-only fallback.
  Rationale: cake treats SIGTERM like Ctrl+C and stops its own tool children,
  while a hung cake must not leave descendants. A descendant that closed the
  inherited pipes can survive; one that still holds them blocks the runner and
  is killed with the group.
  Date/Author: 2026-10-26, Travis Ennis; clarified 2026-10-27 after review.
- Decision: stop and continue each tracked run's process group explicitly around
  the existing self-stop, with the order stop runs → stop self → continue runs →
  restore.
  Rationale: ADR 018 requires engine work not to run unwatched during a
  suspension, and the REPL's group signal no longer reaches a detached child.
  Date/Author: 2026-10-26, Travis Ennis.
- Decision: fail closed when the process attribute cannot be applied; never
  retry unisolated.
  Rationale: a silent fallback would make the terminal guarantee untestable and
  turn a security boundary into a best-effort behavior.
  Date/Author: 2026-10-26, Travis Ennis.
- Decision: accept that tools needing a terminal now fail, and add no opt-out
  flag in this change.
  Rationale: a pseudo-terminal fallback reintroduces the defect, and an opt-out
  is a new compatibility surface the task does not need.
  Date/Author: 2026-10-26, Travis Ennis.
- Decision: keep `internal/compact` out of scope and record it as a known gap.
  Rationale: task 093 names live, replay, and listing; the compactor is
  separately configurable and can be brought under the same helper in a
  follow-up without changing this decision.
  Date/Author: 2026-10-26, Travis Ennis.
- Decision: prove detachment with `Getsid` in Go tests and prove the `/dev/tty`
  consequence with drive-tui captures, not a mandatory tmux CI dependency.
  Rationale: the test runner has no controlling terminal, so a unit test cannot
  observe `ENXIO`; session identity is the portable observable.
  Date/Author: 2026-10-26, Travis Ennis.
- Decision: guard every process-group signal with `pid > 0` and accept the narrow
  reaped-pid-reuse race; do not add platform-specific pid handles.
  Rationale: pid 0 would signal the REPL's own group, while pid reuse requires a
  reap between the registry snapshot and the signal; the guard and a logged
  failure keep the common cases safe and visible.
  Date/Author: 2026-10-27, Travis Ennis.
- Decision: document the terminal-hangup orphan case and the untracked listing as
  limits instead of adding parent-death signals or listing tracking here.
  Rationale: normal quit and cancel paths still stop the engine, and a read-only
  listing cannot touch the terminal; these are accepted consequences of the
  boundary, not silent behavior.
  Date/Author: 2026-10-27, Travis Ennis.
- Decision: track the compactor follow-up as task 124.
  Rationale: ADR 027 leaves the opt-in compactor outside this boundary; a
  separate task keeps the gap visible without changing this decision.
  Date/Author: 2026-10-27, Travis Ennis.
- Decision: have `Run.Stop` and `Run.Continue` return the signal error and have
  `Registry.StopAll`/`ContinueAll` log non-`ESRCH` failures; keep the method
  names public and the registry's messages tagged `suspend:`.
  Rationale: the registry owns the logging sink, while a swallowed error would
  hide a failed engine pause; the names remain the interface shown above.
  Date/Author: 2026-10-27, Travis Ennis (implementation clarification).
- Decision: close the stop-snapshot/self-stop registration window with a
  `suspended` flag and a cycle mutex: `StopAll` marks the registry suspended,
  `Add` stops a run registered while suspended, and `ContinueAll` clears it.
  Rationale: a queued start can register after the snapshot but before the
  self-stop; the small state machine keeps that run from working unwatched
  without holding the registry mutex across the group signal.
  Date/Author: 2026-10-27, Travis Ennis (preflight finding).

## Outcomes & Retrospective

Implemented. Every POSIX live, replay, and session-listing child now starts in
its own session with no controlling terminal. The adverse wrapper that opens
`/dev/tty` fails with `stty status=1` and `Device not configured`; the pane
stays at `-icanon -isig -echo` in full-screen and inline modes, Up recalls
history instead of echoing `^[[A`, and no mouse report appears as text. The
pre-fix master binary leaves the same pane at `icanon isig echo` and echoes
`^[[A`, so the comparison is retained in the task notes.

Cancellation still gives cake a graceful window. A fake cake that ignores
SIGTERM and leaves a descendant holding both pipes or only stdout is gone with
the group at the configured grace deadline, `Result.Canceled` is true, and the
stdout-only variant proves the group SIGKILL rather than the later `WaitDelay`
did the work. A normal graceful exit stops the timer and leaves an intentionally
backgrounded process that closed its pipes alone.

Suspend pauses the detached engine. During the stopped interval the timestamp
log did not grow (one line before, one line after one second), the shell owned
the pane, and mouse reporting was off; after `fg` the log grew to five lines,
mouse reporting returned, and the composer was raw and usable.

Platforms: macOS arm64 with Go 1.27.3 was exercised end to end, including the
full-screen, inline, pre-fix, and suspend captures. Windows cross-built through
the GoReleaser snapshot but was not run, so no Windows isolation is claimed.
Linux cross-built and its tests compile, but no Linux host was available
locally; the Ubuntu CI job is the execution check there. No real cake was used.
The `stty` wrapper is the tool-needing-terminal case that surfaced in practice;
it failed harmlessly as designed. The opt-in compactor remains outside this
boundary and task 124 still tracks it. No terminal captures were committed:
they encode session ids and scratch paths and live only in the temporary
directories named in the task notes.

The registry also pauses a run that registers between the stop snapshot and the
REPL's self-stop, so that window cannot leave engine work running unwatched.

Preflight found no blockers or majors. It caught a stale suspend comment, a
guardrail sentence that still implied the REPL's group stop reaches the detached
engine, the registration window above, and the ADR's soon-to-move ExecPlan link;
all four are fixed.

The main lesson is that `cmd.WaitDelay` is not the bound for a manual stdout
reader: the REPL-owned group timer is what makes a stdout-only descendant
finite. The second is that process lifetime, not model state, must own registry
membership; registration after `Start` and removal inside the wait goroutine
kept every suspend signal on a live group.

## Context and Orientation

cake-repl is a single Go binary that drives the external `cake` CLI. Each prompt
starts one `cake --output-format stream-json` child and renders its newline-
delimited JSON events. A replay uses the same process lifecycle with cake's
read-only `replay <uuid>` command, and the session picker runs the read-only
`cake sessions list --json` command. The external contract is documented in
`docs/guardrails/cake-integration-and-stream-json.md`; do not add, remove, or
reorder cake flags for this work.

The relevant code is:

- `internal/cake/runner.go`: `Options`, `Start`, `Replay`, `Run`, `Cancel`, the
  stdout reader, the `exec.Cmd` cancellation closure, and the bounded stderr
  tail. `Start` is used by both live prompts and replay.
- `internal/cake/sessions.go`: `ListSessions` and `SessionsOptions`, the separate
  listing subprocess.
- `internal/app/update.go`: `startRun` assigns `m.run`; `startReplayCmd` starts a
  replay; `replayStartedMsg` assigns `m.replayRun`; `finishRun` and
  `finishReplay` drop them.
- `internal/app/model.go`: `Config`, `New`, and `CancelRunning`. `Config` is how
  main passes engine settings into the model.
- `cmd/cake-repl/main.go`: builds `app.Config`, creates the Bubble Tea program,
  and installs `watchSuspend` when stdout is a terminal.
- `cmd/cake-repl/suspend.go`: `suspendCycle` (release → park → stopRuns →
  stop → continueRuns → unpark → modes → restore) and `watchSuspend`.
  `suspend_windows.go` is the no-op counterpart.
- `cmd/cake-repl/main_test.go` and `cmd/cake-repl/suspend_test.go`: the existing
  wiring and ordering tests.
- `scripts/fake-cake.sh`: the checked-in fake engine used by tests and by the
  drive-tui skill. It makes no network calls and needs no real cake.

Terms used here:

- **Controlling terminal** is the terminal a process is attached to for job
  control and `/dev/tty`. A process can have pipe stdout and stderr and still
  reach that terminal through `/dev/tty`.
- **Session** is a POSIX process grouping that can own at most one controlling
  terminal. `setsid(2)` creates a new session with none; the calling process
  becomes the session leader. In Go this is the `SysProcAttr.Setsid` field,
  applied in the child between `fork` and `exec`.
- **Process group** is the unit used by terminal job control and by
  `kill(-pgid, signal)`. A session leader starts as the leader of its own group,
  so after `Setsid` the child's process-group id equals its pid.
- **Grace window** is the three-second interval after SIGTERM before a forced
  kill. The REPL's own timer uses it; `cmd.WaitDelay` is set one second longer as
  a PID-only fallback. `Options.waitDelay` is a test seam that widens or shortens
  both.
- **Drive-tui** is the repository's manual terminal verification method: run the
  built binary in a detached tmux pane with `scripts/fake-cake.sh`, then capture
  the pane with `tmux capture-pane`. It is documented in
  `.agents/skills/drive-tui/SKILL.md` and is never pointed at a real cake.

The one-way dependency direction is `cmd/cake-repl` → `internal/app` →
`internal/cake` / `internal/compact` / `internal/ui`. Keep terminal policy inside
`internal/cake`; do not make `internal/ui` aware of processes, and do not import
`internal/app` from `internal/cake`.

## Plan of Work

Milestone 1 adds the process boundary. Create
`internal/cake/process_unix.go` with a build tag of `!windows` and a
`configureProcess(cmd *exec.Cmd)` function that sets
`cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}`. Create the matching
`internal/cake/process_windows.go` whose `configureProcess` does nothing; a
comment must say that Windows has no controlling-terminal equivalent and that
`CONIN$`/`CONOUT$` remain reachable. Call `configureProcess(cmd)` in `Start`
immediately after `exec.CommandContext`, next to the existing `cmd.Dir` setup,
and in `ListSessions` at the same point. `Replay` already delegates to `Start`,
so it inherits the policy; add a test rather than a second call site.

Add `internal/cake/process_test.go` with three tests that run a small shell
script through a temporary executable. Build the script with the existing
`writeFakeCake` helper (`internal/cake/runner_test.go`), and reuse `waitForFile`
and `killPIDFile` (`internal/cake/sessions_test.go`). The script writes `$$` to a
file and then stays alive until the test has read it, so the pid cannot exit
before `Getsid`. For `Start` and `Replay`, call the test's `getSID` helper after
`waitForFile`, require `sid == pid`, then cancel the run and drain `Result`. For
`ListSessions`, call it in a goroutine and follow the pattern in
`TestListSessionsCancellationStopsHelper`: wait for the pid file, call the
helper, then cancel the context (or let the fake exit) and collect the result.
Put the common test file behind a `//go:build !windows` tag and add small
platform `getSID` helpers: `syscall.Getsid` on macOS/BSD and `SYS_GETSID` on
Linux. Windows has neither a session concept nor the syscall, so do not rely on
a runtime skip for code that would not compile. Do not require a real cake.
This milestone is complete when `go test ./internal/cake` passes and the new
tests fail if `configureProcess` is
removed from the corresponding spawn site. Also update the `Options.waitDelay`
comment in `internal/cake/runner.go`: it now sets the REPL's grace timer, and
`cmd.WaitDelay` is one second longer.

Milestone 2 makes cancellation reap the group. Add `pid int` and an
`exited atomic.Bool` to `Run`, construct the `Run` value after a successful
`cmd.Start` but before launching the wait goroutine, and set `pid` from
`cmd.Process.Pid`.
In `Start`, when `cmd.Cancel` sends SIGTERM successfully, start a one-shot timer
for the grace window (`opts.waitDelay`, or three seconds). When it fires, if the
run has not been reaped, send SIGKILL to `-pid`. Guard every group signal with
`pid > 0`; pid 0 would target the REPL's own process group. Set the command's
own `cmd.WaitDelay` to the grace window plus one second as a redundant, PID-only
fallback. The extra second is not what bounds a descendant holding stdout:
`Start` reads `StdoutPipe` to EOF before calling `cmd.Wait`, so the group SIGKILL
is the bound. Stop the timer and set `exited` when `cmd.Wait` returns. The
SIGTERM path, `cancelRequested`, and `canceledExit` keep their current meaning:
`Canceled` must still be true only when the cancel was requested while Wait was
active and the process was signal-terminated.

In `ListSessions`, the current context cancellation is a direct kill; make it a
group kill instead by setting `cmd.Cancel` to signal `-pid` with SIGKILL, guard
the group id with `> 0`, and treat an already-gone process as success. Keep
`sessionsWaitDelay` as the pipe-bounding backstop; the listing has no state to
flush, so it still does not need a graceful signal.

Add a runner test with a temporary shell that ignores SIGTERM, starts
`sleep 30` (or a longer-lived equivalent) in the background, writes the
descendant's pid to a file, and waits. Start it with `waitDelay` around 100 ms,
call `Cancel`, wait for `Result`, and assert that the descendant pid no longer
responds to `kill(pid, 0)` after the result. Assert that `Result.Canceled` is
true and that no signal was sent to the test process itself. Keep this test in
the `//go:build !windows` file because `syscall.Kill` is not available there.
Add a second variant that starts `sleep 30 2>/dev/null &` so only stdout is held;
that variant must also finish at the grace deadline, proving the group kill
rather than `WaitDelay` did the work.

Milestone 3 restores suspend coordination. Add `Stop()` and `Continue()` to
`Run` in build-tagged files: on POSIX they send SIGSTOP or SIGCONT to `-pid`
only when the run has not been reaped and `pid > 0`; on Windows they do nothing.
Add `internal/cake/registry.go` with a `Registry` that holds a mutex, a set of
live runs, and a `logf` sink; `NewRegistry(logf func(string, ...any))` treats a
nil sink as a no-op. The registry exposes `Add`, `Remove`, `StopAll`, and
`ContinueAll`, and is a snapshot-and-signal helper: it must not hold its mutex
while making a syscall, and it must tolerate a run that exits between the
snapshot and the signal. That tolerance covers `ESRCH`; a run reaped between the
snapshot and the signal can have its pid reused, so the narrow race is documented
as an accepted limit rather than closed with platform-specific pid handles.
`StopAll` marks the registry suspended before its snapshot, and `Add` uses a
cycle mutex to stop a run that registers before the self-stop; `ContinueAll`
clears the state. That closes the snapshot-to-self-stop window.
`Start` registers a run after a successful start when `Options.Registry` is
non-nil and removes it immediately after `cmd.Wait` returns, so registration
follows process lifetime rather than model state. `ReplayOptions` gains the same
field and passes it through. Have `StopAll` and `ContinueAll` log a non-`ESRCH`
failure through `logf`, so a failed engine stop is visible in `-debug-log` as the
existing self-stop failure is; start those messages with `suspend:` to match
`suspendLogf`'s tag contract.

Add `Runs *cake.Registry` to `app.Config`. `startRun` passes `m.cfg.Runs` into
`cake.Options`; `startReplayCmd` passes `cfg.Runs` into `cake.ReplayOptions`.
Leave `Model.CancelRunning` unchanged; it cancels through `Run.Cancel`, and the
registry removes the run when the wait goroutine finishes.

In `cmd/cake-repl/main.go`, create
`runs := cake.NewRegistry(suspendLogf(cfg.DebugLog))` before building the model,
assign it to `cfg.Runs`, and pass it to `watchSuspend`. Define the
narrow interface the watcher needs, for example
`type engineRuns interface { StopAll(); ContinueAll() }`, in `main.go` so the
Unix and Windows suspend files share one signature. Add `stopRuns` and
`continueRuns` fields to `suspendCycle` and run them in this order: release,
park, stopRuns, stop (the existing self-stop, which blocks until SIGCONT),
continueRuns, unpark, modes, restore. If `release` fails, the cycle still
abandons before `stopRuns`, so a failed release leaves engine work running
rather than stopping half the system. Update `watchSuspend` in both
`suspend.go` and `suspend_windows.go`; Windows remains a no-op.

Extend `cmd/cake-repl/suspend_test.go` to record the two new steps and pin the
order, including that a failed release never reaches `stopRuns`. Add a small
`internal/cake` test that `Registry.StopAll` and `ContinueAll` tolerate an empty
registry and an already-reaped run; behavior against real processes is covered
by the terminal milestone because SIGSTOP is not directly observable portably.

Milestone 4 produces evidence and closes the documentation. Drive the built
binary in a tmux pane in full-screen and inline modes using an adverse wrapper
that runs `stty sane < /dev/tty` before `exec scripts/fake-cake.sh "$@"`, plus an
ordinary happy-path run as the control. A successful run does not render
captured stderr, so have the wrapper append the `stty` status and error text to a
scratch file (for example `"$run/stty.log"`) and capture that file, or run a
separate negative variant that exits non-zero. Capture the composer afterward:
type a prompt, press Up, submit, and confirm the frame stays raw and readable.
Repeat with the suspend flow from the drive-tui skill: arm `ISIG` with
`stty -f "$(tmux display -p -t "$session" '#{pane_tty}')" isig`, send Ctrl+Z,
confirm the shell owns the pane, then `fg` and confirm the REPL repaints and the
timeline continues. To prove the engine actually paused rather than merely
became invisible, wrap the fake cake in a scratch logger that appends a
timestamped line per stream event to a file; the file must not grow between the
stop and `fg`, and must grow afterward. Do not commit the wrapper or logger.

Verify the Linux behavior with the new `Getsid` tests in CI, and, when a Linux
host is available, run the same small probe under `script -q /dev/null` to
observe `ENXIO`. Record the exact evidence and the Windows limit in the task's
Acceptance Notes.

Then update the durable surfaces required by the documentation impact matrix:
`docs/guardrails/session-and-security.md` (process lifecycle and terminal
ownership paragraphs), `ARCHITECTURE.md` (the engine isolation and suspend
sections), `README.md`'s Known limitations (tools that need a terminal now fail;
Windows is not isolated; a terminal hangup or abrupt REPL death can leave an
engine child running; a read-only listing is not paused by a suspend; the
compactor remains a separate, unisolated terminal path), and the "Sessions,
Security, And Subprocess Lifecycle" route in `AGENTS.md` (ADR 027 is linked).
Re-check
`docs/guardrails/cake-integration-and-stream-json.md` and state in the task notes
whether it needed a line; the cake
flags, cwd, and stream contract do not change. Run `just test-race`, `just ci`,
and the preflight skill in a subagent, then fill the Acceptance Notes and run
`ahm task complete 093`.

## Concrete Steps

Start implementation from an up-to-date master in a new branch; the ADR and this
plan land on their own documentation branch first.

    cd /Users/travisennis/Projects/cake/cake-repl
    git switch master
    git pull --ff-only
    just branch fix/093-isolate-engine-subprocesses

Milestone 1 edits and check:

    go test ./internal/cake -run 'TestStartDetachesSession|TestReplayDetachesSession|TestListSessionsDetachesSession' -v

Expect three passes and no requirement for a terminal. Run the same tests after
temporarily deleting one `configureProcess` call; the corresponding test must
fail with `sid != pid`.

Milestone 2 check:

    go test ./internal/cake -run 'TestCancelKillsDescendantGroup|TestCancel' -v
    just test-race

Expect the descendant test to finish in roughly the configured grace window plus
a small margin, not the later `WaitDelay`; `Result.Canceled` must be true and the
descendant gone. A failure leaves a `sleep` process behind; kill it by pid before
retrying.

Milestone 3 check:

    go test ./internal/cake ./internal/app ./cmd/cake-repl -run 'Suspend|Registry|Run' -v
    just test-race

Milestone 4, terminal evidence in a scratch directory:

    run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-isolation.XXXXXX")
    mkdir -p "$run/project"
    cat > "$run/adverse-cake" <<'SH'
    #!/bin/sh
    err=$(stty sane < /dev/tty 2>&1); status=$?
    printf 'stty status=%s output=%s\n' "$status" "$err" >> "${STTY_LOG:-/dev/null}"
    exec /Users/travisennis/Projects/cake/cake-repl/scripts/fake-cake.sh "$@"
    SH
    chmod +x "$run/adverse-cake"

Launch a pane as a pristine job-control shell and start the REPL from it, as the
drive-tui skill prescribes. The environment is passed on the tmux command line
because a detached tmux server does not inherit this shell's environment. Pass
the scratch log paths as `EVENTS_LOG` and `STTY_LOG` so the wrapper below can
find them:

    repo=/Users/travisennis/Projects/cake/cake-repl
    session=cake-repl-isolation-093
    tmux new-session -d -s "$session" -c "$run/project" -x 100 -y 30 \
      "env -i HOME=$run PATH=/usr/bin:/bin:/usr/sbin:/sbin TERM=xterm-256color \
       EVENTS_LOG=$run/events.log STTY_LOG=$run/stty.log zsh -f"
    tmux send-keys -t "$session" -l "$repo/bin/cake-repl -cake-bin $run/adverse-cake \
      -no-config -no-color -no-session -cwd $run/project -history-file $run/history"
    tmux send-keys -t "$session" Enter

Build `bin/cake-repl` with `just build` before launching. Poll
`tmux capture-pane -pt "$session"` for the composer, type a prompt, and submit.
The expected result is a normal reply from the fake cake; `$run/stty.log`
contains `stty status=1` and the `Device not configured` text; the REPL does not
show a timeline error because a successful run does not render captured stderr;
and the pane's modes never change. Repeat with `-inline` added to the flags,
then repeat both modes with the unmodified `scripts/fake-cake.sh` as the control.

For the suspend pause proof, create the timestamp logger next to the wrapper:

    cat > "$run/logging-cake" <<'SH'
    #!/bin/sh
    /Users/travisennis/Projects/cake/cake-repl/scripts/fake-cake.sh "$@" |
      while IFS= read -r line; do
        date +%s.%N >> "$EVENTS_LOG"
        printf '%s\n' "$line"
      done
    SH
    chmod +x "$run/logging-cake"

Point the logger at a scratch path under `$run`, not the repository. While the
REPL is stopped, the log's line count must not change; after `fg` and a
completed turn, it must increase. Remove the tmux session with
`tmux kill-session -t "$session"` when done; never use `tmux kill-server`.

Final checks:

    just fmt-check
    just test-race
    just ci
    ahm status

Then run preflight in a subagent, update task 093's Acceptance Notes with the
captures, platforms, and limits, and complete the task.

## Validation and Acceptance

Acceptance is behavioral and is checked in this order.

The adverse wrapper must fail harmlessly. With the wrapper in place, the REPL
renders the fake cake's normal stream and no timeline error, because a successful
run does not display captured stderr; the wrapper's scratch log records the
`stty` failure and its `Device not configured` text. The composer still accepts
text, Up recalls the previous prompt instead of printing `^[[A`, Ctrl+S works,
mouse reports move the timeline rather than appearing as text, and the
status/composer layout is unchanged. The same result holds in full-screen and
inline modes with `-no-color`. The pre-fix binary still shows the corruption;
retain that comparison in the task notes.

Session identity must hold for all three spawns. The test's `getSID` helper
returns the child's own pid for a live run, a replay, and a listing, and the
tests fail if any spawn site loses the attribute. On Windows those tests are
skipped and the plan claims no isolation.

Cancellation must remain graceful first. A fake cake that handles SIGTERM exits
normally, `Canceled` is true, and no descendant is killed with the group because
the timeout never fires when the descendants have closed the inherited pipes. A
fake cake that ignores SIGTERM and holds a background descendant is killed as a
group at the grace deadline, the descendant is gone, `Result.Canceled` is true,
and no signal reaches the REPL or the invoking shell. The stderr-redirected
variant proves the group kill bounds a descendant that holds only stdout. A
normal turn that leaves a deliberately backgrounded process must not kill it,
provided that process has closed the inherited pipes.

Suspend must pause engine work. During the stop, the timestamp logger does not
grow; after `fg`, it resumes and the timeline continues. The alternate screen and
inline modes both repaint correctly, and a failed terminal release still leaves
the REPL and the engine running.

Existing behavior must not regress: successful streams, stderr errors, startup
failure, replay failure, session listing, cancellation, new-session boundaries,
quit, and `-no-color` all keep their current outcomes. `just ci` passes, including
the race tests.

## Idempotence and Recovery

The code changes are additive and can be applied in any order within a
milestone; the tests use temporary directories and the checked-in fake cake, so
rerunning them is safe and leaves no state in the repository or the user's cake
data. A test that fails mid-cancellation may leave a `sleep` descendant; the
group kill normally removes it, and any leftover pid printed by the test can be
killed directly. The tmux session and the scratch wrapper are disposable; remove
the session with `tmux kill-session -t cake-repl-isolation-093`.

If the group-kill timer proves flaky, the safe rollback is to remove only the
timer and keep the attribute; cancellation then falls back to the current
PID-only behavior, and a descendant that holds only stdout will block the runner
until it exits. Record that change in the Decision Log rather than silently
weakening the plan. If the suspend wiring is incomplete, keep the process
attribute and the cancellation work; a detached engine that keeps running during
a suspend is a known degraded state, not a terminal-corruption regression.

## Artifacts and Notes

The macOS probe that selected `Setsid` (trimmed; run inside a tmux pty, Go
1.27.3, macOS 27.0.1):

    inherited: read_dev_tty=0 write_dev_tty=0
    setsid:    read_dev_tty=1 write_dev_tty=1
               /bin/sh: /dev/tty: Device not configured
    setpgid:   read_dev_tty=0 write_dev_tty=0

The termination probe (a shell that starts `sleep 30` in the background, with
`Setsid` applied to the shell):

    leader-only: signal=leader leader_alive=false desc_alive=true
    group:       signal=group  leader_alive=false desc_alive=false

The pre-fix reproduction to retain in task 093: a wrapper containing
`stty sane < /dev/tty` before `exec` leaves the pane with `icanon isig echo`
enabled, Up echoes `^[[A`, and Ctrl+Z suspends the REPL. After the change the
wrapper's `stty` fails with `Device not configured`; because a successful run
does not render captured stderr, record that failure in the wrapper's scratch
log. The pane stays raw.

Implementation evidence (2026-10-27, macOS arm64, Go 1.27.3):

    post-fix adverse wrapper, full-screen and inline:
      stty status=1 output=.../adverse-cake: line 2: /dev/tty: Device not configured
      lflags: -icanon -isig -iexten -echo echoe -echok echoke -echonl echoctl
      no ^[[A, no mouse-report text, composer accepts history and submits

    pre-fix master binary (62cbead), same wrapper:
      stty status=0 output=
      lflags: icanon isig iexten echo echoe -echok echoke -echonl echoctl
      ^[[A echoed into the status line

    suspend pause proof (logging wrapper, FAKE_CAKE_DELAY=1):
      pane command while stopped: zsh
      mouse stopped: 0  mouse resumed: 1
      event log lines: before=1 stopped1=1 stopped2=1 after=5
      resumed lflags: -icanon -isig -iexten -echo ...

## Interfaces and Dependencies

The following signatures are the intended shape; keep names stable so the tests
and the suspend wiring read the same way.

    // internal/cake/process_unix.go (!windows)
    func configureProcess(cmd *exec.Cmd)
    func signalGroup(pgid int, sig syscall.Signal) error // no-op when pgid <= 0
    func killGroup(pid int) error
    func processGone(err error) bool

    // internal/cake/process_windows.go (windows)
    func configureProcess(cmd *exec.Cmd)
    func signalGroup(pid int, sig syscall.Signal) error // kill the pid; no group
    func killGroup(pid int) error
    func processGone(err error) bool

    // internal/cake/runner.go
    type Options struct {
        // existing fields ...
        Registry *Registry
    }
    type Run struct {
        // existing fields ...
        pid        int
        exited     atomic.Bool
        mu         sync.Mutex
        graceTimer *time.Timer
    }
    // Stop and Continue are no-ops on Windows and when the run is reaped or pid
    // is not positive; on POSIX they signal the run's process group and return
    // the signal error so Registry can log a non-ESRCH failure.
    func (r *Run) Stop() error
    func (r *Run) Continue() error

    // internal/cake/registry.go
    type Registry struct {
        mu        sync.Mutex
        runs      map[*Run]struct{}
        logf      func(string, ...any)
        cycleMu   sync.Mutex
        suspended bool
    }
    func NewRegistry(logf func(string, ...any)) *Registry // nil logf discards
    func (r *Registry) Add(run *Run)
    func (r *Registry) Remove(run *Run)
    func (r *Registry) StopAll()     // logs non-ESRCH failures
    func (r *Registry) ContinueAll() // logs non-ESRCH failures

    // internal/app/model.go
    type Config struct {
        // existing fields ...
        Runs *cake.Registry
    }

    // cmd/cake-repl/suspend.go
    type suspendCycle struct {
        // existing fields ...
        stopRuns     func()
        continueRuns func()
    }
    type engineRuns interface {
        StopAll()
        ContinueAll()
    }
    func watchSuspend(prog terminalProgram, inline bool, runs engineRuns, logf func(string, ...any)) func()

The only dependencies are the Go standard library (`os/exec`, `syscall`,
`sync`, `time`, `sync/atomic`) and the existing Bubble Tea program interface
already used by `watchSuspend`. Do not add a dependency, a cake flag, a config
field, or a new user-visible command for this work.

## Revision notes

- 2026-10-27: Applied the branch review. The ADR and plan now agree that the
  REPL-owned grace timer sends the group SIGKILL and `cmd.WaitDelay` is the later
  PID-only fallback; the graceful-exit promise now names pipe ownership rather
  than mere process exit; `WaitDelay` is no longer described as bounding a
  manual stdout reader; group signals are guarded by `pid > 0` and non-`ESRCH`
  failures are logged; the terminal-hangup/orphan case and the untracked listing
  are recorded as limits; the drive-tui steps record the `stty` failure in a
  scratch file because a successful run does not render captured stderr; the
  Ctrl+Z wording, compactor scope, and `setpgid` non-goal were corrected;
  milestone 1 now names the existing test helpers and the goroutine needed for
  `ListSessions`; and the compactor follow-up is task 124.
- 2026-10-27: Implemented the plan on `fix/093-isolate-engine-subprocesses`.
  `Run.Stop`/`Continue` return the signal error so the registry can log a
  failed pause; `killGroup` and `processGone` are the platform helpers used by
  the timer, listing cancellation, and registry. Linux needs a `SYS_GETSID` test
  helper because its `syscall` package does not export `Getsid`; the test
  binaries compile for Linux amd64/arm64 and Windows. The macOS terminal
  evidence and the suspend log counts are recorded above; Linux execution
  remains a CI check. The plan has moved to the completed bucket.
