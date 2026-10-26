# Isolate cake subprocesses from the REPL terminal

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. It must be maintained in accordance with
`docs/workflow/exec-plans.md`. It is active and lives in
`docs/exec-plans/active/`. The accepted decision is recorded in
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

Design is accepted and this plan is written; no implementation code exists yet.
Update this section at every stopping point.

- [x] (2026-10-26) Reproduce the `/dev/tty` path and record it in task 093.
- [x] (2026-10-26) Probe `Setsid` versus `Setpgid` on macOS in a real pty; probe
  leader-only versus process-group termination.
- [x] (2026-10-26) Accept ADR 027 and write this plan.
- [ ] Milestone 1: apply the process attribute to live, replay, and listing
  spawns; add session-identity tests.
- [ ] Milestone 2: make forced cancellation kill the process group and cover a
  descendant that ignores SIGTERM.
- [ ] Milestone 3: stop and continue live runs around an external suspend; cover
  the cycle order and drive the behavior in a pane.
- [ ] Milestone 4: capture terminal evidence in both render modes, verify on
  Linux, update the durable docs, run `just ci` and preflight, and complete
  task 093.

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
- Observation: `syscall.Getsid` is available on macOS and Linux, so a test can
  prove detachment without a terminal: a detached child's session id equals its
  own pid. This makes the core mechanism CI-verifiable even though CI has no
  controlling terminal.
- Observation: `exec.Cmd.WaitDelay` kills only the child pid. Racing a REPL-owned
  group-kill timer against it makes cleanup unreliable because the leader can be
  reaped before the group signal lands, and a reaped pid may be reused. The plan
  therefore fires the group signal at the accepted grace window and sets the
  command's own `WaitDelay` one second later as a redundant fallback.
- Observation: `cmd/cake-repl/suspend.go` stops the REPL's process group with
  `syscall.Kill(0, syscall.SIGSTOP)`. Detaching cake silently removes it from
  that group, so the suspend change is required, not optional.
- Observation: the opt-in compactor (`internal/compact`) is a second subprocess
  path with the same exposure. It is deliberately out of this task's scope and
  is recorded as a known gap in ADR 027.

## Decision Log

- Decision: use `SysProcAttr{Setsid: true}` for the live run, replay, and
  `sessions list` on POSIX; leave Windows unset and document that no isolation
  is claimed there.
  Rationale: it is the only probed mechanism that removes `/dev/tty`, it needs
  no new dependency, and one helper keeps the three spawn sites consistent.
  Date/Author: 2026-10-26, Travis Ennis (accepted in ADR 027).
- Decision: signal the leader with SIGTERM first; at the grace deadline send
  SIGKILL to the process group; do not signal the group after a graceful exit.
  Rationale: cake treats SIGTERM like Ctrl+C and stops its own tool children,
  while a hung cake must not leave descendants. A deliberately daemonized tool
  process should survive a normal turn, as it does today.
  Date/Author: 2026-10-26, Travis Ennis.
- Decision: stop and continue each live run's process group explicitly around
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

## Outcomes & Retrospective

Planning is complete. No behavior has changed yet, so there is no outcome to
compare. At completion this section must state which platforms were exercised,
which terminal captures were retained, whether any tool that needs `/dev/tty`
surfaced in practice, and whether the compactor follow-up was filed. The
expected result is that the pre-fix `stty` reproduction now fails harmlessly and
that cancellation and suspend behavior remain indistinguishable to a user.

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
- `cmd/cake-repl/suspend.go`: `suspendCycle` (release → park → stop → unpark →
  modes → restore) and `watchSuspend`. `suspend_windows.go` is the no-op
  counterpart.
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
  kill. It is currently the command's `WaitDelay`; `Options.waitDelay` is a test
  seam that widens or shortens it.
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

Add `internal/cake/process_test.go` (name may be `runner_test.go` if it fits the
existing fixture style) with three tests that run a small shell script through a
temporary executable. The script writes `$$` to a file and then behaves like a
minimal stream. For `Start`, `Replay`, and `ListSessions`, wait for the pid file,
call `syscall.Getsid(pid)`, and require `sid == pid`. Put that test file behind a
`//go:build !windows` tag, because Windows provides neither `Getsid` nor a
session concept; do not rely on a runtime skip for code that would not compile.
Reuse the existing temporary-bin helpers; do not require a real cake. This
milestone is complete when `go test ./internal/cake` passes and the new tests
fail if `configureProcess` is removed from the corresponding spawn site.

Milestone 2 makes cancellation reap the group. Add `pid int` and an `exited`
flag to `Run`, construct the `Run` value after a successful `cmd.Start` but
before launching the wait goroutine, and set `pid` from `cmd.Process.Pid`. In
`Start`, when `cmd.Cancel` sends SIGTERM successfully, start a one-shot timer for
the grace window (`opts.waitDelay`, or three seconds). When it fires, if the run
has not been reaped, send SIGKILL to `-pid`. Set the command's own
`cmd.WaitDelay` to the grace window plus one second so the REPL's group signal
lands first and the leader cannot be reaped before it; the extra second still
bounds a descendant that holds the stdout pipe. Stop the timer and set `exited`
when `cmd.Wait` returns. The SIGTERM path, `cancelRequested`, and `canceledExit`
keep their current meaning: `Canceled` must still be true only when the cancel
was requested while Wait was active and the process was signal-terminated.

In `ListSessions`, the current context cancellation is a direct kill; make it a
group kill instead by setting `cmd.Cancel` to signal `-pid` with SIGKILL and
treating an already-gone process as success. Keep `sessionsWaitDelay` as the
pipe-bounding backstop; the listing has no state to flush, so it still does not
need a graceful signal.

Add a runner test with a temporary shell that ignores SIGTERM, starts
`sleep 30` (or a longer-lived equivalent) in the background, writes the
descendant's pid to a file, and waits. Start it with `waitDelay` around 100 ms,
call `Cancel`, wait for `Result`, and assert that the descendant pid no longer
responds to `kill(pid, 0)` after the result. Assert that `Result.Canceled` is
true and that no signal was sent to the test process itself. Keep this test in
the `//go:build !windows` file because `syscall.Kill` is not available there.

Milestone 3 restores suspend coordination. Add `Stop()` and `Continue()` to
`Run` in build-tagged files: on POSIX they send SIGSTOP or SIGCONT to `-pid`
when the run has not been reaped; on Windows they do nothing. Add
`internal/cake/registry.go` with a `Registry` that holds a mutex and a set of
live runs, `NewRegistry`, `Add`, `Remove`, `StopAll`, and `ContinueAll`. The
registry is a snapshot-and-signal helper: it must not hold its mutex while making
a syscall, and it must tolerate a run that exits between the snapshot and the
signal. `Start` registers a run after a successful start when
`Options.Registry` is non-nil and removes it immediately after `cmd.Wait`
returns, so registration follows process lifetime rather than model state.
`ReplayOptions` gains the same field and passes it through.

Add `Runs *cake.Registry` to `app.Config`. `startRun` passes `m.cfg.Runs` into
`cake.Options`; `startReplayCmd` passes `cfg.Runs` into `cake.ReplayOptions`.
Leave `Model.CancelRunning` unchanged; it cancels through `Run.Cancel`, and the
registry removes the run when the wait goroutine finishes.

In `cmd/cake-repl/main.go`, create `runs := cake.NewRegistry()` before building
the model, assign it to `cfg.Runs`, and pass it to `watchSuspend`. Define the
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
ordinary happy-path run as the control. Capture the adverse wrapper's captured
stderr showing the `stty` failure, and capture the composer afterward: type a
prompt, press Up, submit, and confirm the frame stays raw and readable. Repeat
with the suspend flow from the drive-tui skill: arm `ISIG` with
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
sections), and `README.md`'s Known limitations (tools that need a terminal now
fail; Windows is not isolated). Re-check `docs/guardrails/cake-integration-and-
stream-json.md` and state in the task notes whether it needed a line; the cake
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

Expect the descendant test to finish in roughly the configured wait delay plus a
small margin, `Result.Canceled` to be true, and the descendant to be gone. A
failure leaves a `sleep` process behind; kill it by pid before retrying.

Milestone 3 check:

    go test ./internal/cake ./internal/app ./cmd/cake-repl -run 'Suspend|Registry|Run' -v
    just test-race

Milestone 4, terminal evidence in a scratch directory:

    run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-isolation.XXXXXX")
    mkdir -p "$run/project"
    cat > "$run/adverse-cake" <<'SH'
    #!/bin/sh
    stty sane < /dev/tty
    exec /Users/travisennis/Projects/cake/cake-repl/scripts/fake-cake.sh "$@"
    SH
    chmod +x "$run/adverse-cake"

Launch a pane as a pristine job-control shell and start the REPL from it, as the
drive-tui skill prescribes. The environment is passed on the tmux command line
because a detached tmux server does not inherit this shell's environment. Pass
the scratch log path as `EVENTS_LOG` so the wrapper below can find it:

    repo=/Users/travisennis/Projects/cake/cake-repl
    session=cake-repl-isolation-093
    tmux new-session -d -s "$session" -c "$run/project" -x 100 -y 30 \
      "env -i HOME=$run PATH=/usr/bin:/bin:/usr/sbin:/sbin TERM=xterm-256color \
       EVENTS_LOG=$run/events.log zsh -f"
    tmux send-keys -t "$session" -l "$repo/bin/cake-repl -cake-bin $run/adverse-cake \
      -no-config -no-color -no-session -cwd $run/project -history-file $run/history"
    tmux send-keys -t "$session" Enter

Build `bin/cake-repl` with `just build` before launching. Poll
`tmux capture-pane -pt "$session"` for the composer, type a prompt, and submit.
The expected result is a normal reply from the fake cake and an error line from
the wrapper's failed `stty`, never a change in the pane's modes. Repeat with
`-inline` added to the flags, then repeat both modes with the unmodified
`scripts/fake-cake.sh` as the control.

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
renders the fake cake's normal stream and an error from `stty`; the composer
still accepts text, Up recalls the previous prompt instead of printing `^[[A`,
Ctrl+S works, mouse reports move the timeline rather than appearing as text, and
the status/composer layout is unchanged. The same result holds in full-screen and
inline modes with `-no-color`. The pre-fix binary still shows the corruption;
retain that comparison in the task notes.

Session identity must hold for all three spawns. `syscall.Getsid(pid)` returns
the child's own pid for a live run, a replay, and a listing, and the tests fail
if any spawn site loses the attribute. On Windows those tests are skipped and the
plan claims no isolation.

Cancellation must remain graceful first. A fake cake that handles SIGTERM exits
normally, `Canceled` is true, and no descendant is killed with the group because
the timeout never fires. A fake cake that ignores SIGTERM and holds a background
descendant is killed as a group at the grace deadline, the descendant is gone,
`Result.Canceled` is true, and no signal reaches the REPL or the invoking shell.
A normal turn that leaves a deliberately backgrounded process must not kill it.

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
PID-only behavior. Record that change in the Decision Log rather than silently
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
wrapper's `stty` reports `Device not configured` and the pane stays raw.

## Interfaces and Dependencies

The following signatures are the intended shape; keep names stable so the tests
and the suspend wiring read the same way.

    // internal/cake/process_unix.go (!windows)
    func configureProcess(cmd *exec.Cmd)
    func signalGroup(pgid int, sig syscall.Signal) error

    // internal/cake/process_windows.go (windows)
    func configureProcess(cmd *exec.Cmd)
    func signalGroup(pid int, sig syscall.Signal) error // kill the pid; no group

    // internal/cake/runner.go
    type Options struct {
        // existing fields ...
        Registry *Registry
    }
    type Run struct {
        // existing fields ...
        pid    int
        exited atomic.Bool
    }
    func (r *Run) Stop()
    func (r *Run) Continue()

    // internal/cake/registry.go
    type Registry struct {
        mu   sync.Mutex
        runs map[*Run]struct{}
    }
    func NewRegistry() *Registry
    func (r *Registry) Add(run *Run)
    func (r *Registry) Remove(run *Run)
    func (r *Registry) StopAll()
    func (r *Registry) ContinueAll()

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
