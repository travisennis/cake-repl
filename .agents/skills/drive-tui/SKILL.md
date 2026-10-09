---
name: drive-tui
description: Drive cake-repl in a headless tmux session against the checked-in fake cake to observe and capture what it actually puts on the screen. Use when verifying a rendering or interaction change — timeline items, the composer, the status line, key bindings, resize, color, or a new stream event's rendering — because the Go tests call Update/View directly and never drive the program or a terminal. Do not use it for behavior a Go test can assert directly, and never point it at a real cake.
---

# Driving the REPL in a terminal

`cli-and-user-output.md` requires a terminal capture for a UI/output change and
records why: the tests call `Update` and `View` directly, so a green suite shows
only that our own item kinds still format — not that the program renders
correctly in a terminal. This skill drives the real binary in a tmux pane
against a fake cake and produces that capture.

It follows an **observe → act → observe** loop, the same shape as a browser-driven
test: wait for a predicate you can see, act, then snapshot the result. Never
`sleep` and assume the screen is ready; never act twice on one screen you have
not read.

## What it needs

- `tmux` 3.x (`tmux -V`). It is a host tool for this skill only, not a project
  or CI dependency — nothing in `go.mod`, the `justfile`, or a workflow
  references it.
- The checked-in fake at [`scripts/fake-cake.sh`](../../../scripts/fake-cake.sh),
  which is hermetic: no real `cake`, no network, no model calls.
- A built binary: `just build` (run it directly, not `just run`; `go run` adds a
  second process per restart and its stderr noise lands in the pane).

## The fake cake

`scripts/fake-cake.sh` ignores its flags and replays a fixture from
`internal/cake/testdata/fixtures/`:

| Variable | Default | Purpose |
| --- | --- | --- |
| `FAKE_CAKE_FIXTURE` | `happy-path` | fixture for a live prompt: `happy-path`, `error-task`, `minimal`, `unknown-type`, `malformed-midstream` |
| `FAKE_CAKE_REPLAY_FIXTURE` | `replay-path` | fixture for `replay <uuid>`, used when the REPL starts with `-resume` |
| `FAKE_CAKE_DELAY` | `0` | seconds between lines, so the running state is observable (`FAKE_CAKE_DELAY=0.4`) |
| `FAKE_CAKE_ARGS_FILE` | unset | append each invocation's argv, to check which flags the REPL passed through |
| `FAKE_CAKE_DEBUG` | unset | print the argv to stderr |

Pass `-cake-bin` an **absolute** path: the REPL runs cake with its working
directory set to `-cwd`, so a relative path resolves against that directory, not
yours.

## Isolation: the same screen twice

Every run must start from the same screen or the capture proves nothing. Launch
with all of these:

| Lever | Why |
| --- | --- |
| `-cake-bin "$root/scripts/fake-cake.sh"` | no real cake, no cost, fixed stream |
| `-no-config` | the user's XDG and project-local config cannot change the defaults |
| `-no-color` | termenv `Ascii`, so the capture is stable and readable on a bare terminal |
| `-no-session` | nothing is written to a cake session |
| `-cwd <temp>/project` | isolates the run; the base name is the `cwd:` field on the status line, so keep it stable (`project`) |
| `-history-file <temp>/history` | prompt history starts empty on every run |
| fixed `-x`/`-y` on `tmux new-session` | the layout is width-sensitive; 100x30 is the default here |

The fixtures carry a fixed session UUID (`11111111-…`), so the status line reads
the same on every run.

## Procedure

### 1. Freeze the screen

`just build` writes only the gitignored `bin/cake-repl`; nothing else in the
repository changes.

```bash
root=$(git rev-parse --show-toplevel)
just build
run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-drive.XXXXXX")
mkdir -p "$run/project" "$run/captures"   # base name `project` keeps the status line stable
```

### 2. Launch

Pick a session name unique to this run (for example `cake-repl-tui-081`) and use
it literally in every later command. Check it is not already taken first —
`tmux ls` — and if it is, pick another name. Never `tmux kill-server`, and never
close a session this skill did not start.

Environment variables go in the command string, not in your shell: a detached
tmux server does not inherit the environment of the shell that talks to it.

```bash
session=cake-repl-tui-081
tmux new-session -d -s "$session" -c "$root" -x 100 -y 30 \
  "FAKE_CAKE_FIXTURE=happy-path FAKE_CAKE_DELAY=0.4 \
   $root/bin/cake-repl \
     -cake-bin $root/scripts/fake-cake.sh \
     -no-config -no-color -no-session \
     -cwd $run/project -history-file $run/history"
```

Confirm the size took: `tmux display -p -t "$session" '#{window_width}x#{window_height}'`.

### 3. Wait for a predicate, never sleep

Poll a visible marker with a bounded deadline. Each Bash call is a fresh shell:
`$root`, `$run`, `$session`, and any function you define vanish between calls,
so either keep a whole step in one call or stash the values (for example in
`$run/session`) and re-read them.

```bash
wait_for() {                      # wait_for <session> <text> [seconds]
    _s=$1 _want=$2 _limit=${3:-30}
    _deadline=$((SECONDS + _limit))
    until tmux capture-pane -pt "$_s" -S -200 | grep -qF -- "$_want"; do
        if [ "$SECONDS" -ge "$_deadline" ]; then
            echo "timed out after ${_limit}s waiting for: $_want" >&2
            tmux capture-pane -pt "$_s" -S -200 >&2
            return 1
        fi
        sleep 0.1
    done
}

wait_for "$session" 'prompt · ready'          # launched and idle
wait_for "$session" 'running'                 # a task is in flight
wait_for "$session" '[ idle ]'                # the task finished
```

Markers this REPL renders: the composer title `prompt · ready` / `prompt ·
running`, and the status line `[ idle ]` / `[ <spinner> running ]`. The spinner
glyph advances every frame, so match the stable word (`running`, `prompt ·
running`), not the glyph. Any other text you expect — an assistant message, a
tool block, a hint — is a valid predicate too.
Wait on the text the change is about, not on chrome: for a change *to* the
chrome (a hint, a status-line field, a key-binding label), wait for the new text
itself, since the old chrome renders whether or not the change worked.

### 4. Act

Send text and the submit key as **separate** writes, then observe. Submit is
`Ctrl+S`; `Enter` inserts a newline, so sending Enter does not submit.

```bash
tmux send-keys -t "$session" -l "hello there"   # literal text
tmux send-keys -t "$session" C-s                # submit
wait_for "$session" '[ idle ]'
tmux capture-pane -pt "$session" -S -200
```

Other bindings worth driving: `C-c` cancels a running task and quits when idle,
`C-n` starts a new session, `C-u` clears input, `C-o` cycles tool output, `Up`
recalls history, `Tab` completes a command, `Esc` closes the `/sessions`
browser. Snapshot after each one; a keypress can land in the next view.

The fake answers *every* invocation with a stream fixture, including
`cake sessions list --json`, so the `/sessions` browser renders its
"sessions unavailable" state instead of a list. That still exercises the modal
open and close, but it is not evidence about the listing itself.

### 5. Resize

```bash
before=$(tmux capture-pane -pt "$session")
tmux resize-window -t "$session" -x 120 -y 40
deadline=$((SECONDS + 10))
until [ "$(tmux capture-pane -pt "$session")" != "$before" ]; do
    [ "$SECONDS" -lt "$deadline" ] || { echo "resize never redrew" >&2; break; }
    sleep 0.1
done
tmux display -p -t "$session" '#{window_width}x#{window_height}'   # 120x40
```

For a width-sensitive change, assert on wrapped **content**, not on the border
width: pick a fixture or prompt whose text wraps differently at the two widths.

### 5a. Suspend and resume

Driving a stop signal needs a job-control shell between tmux and the binary, so
launch the pane as a pristine interactive shell and start the REPL from it. That
shell is also the only `fg` there is:

```bash
run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-drive.XXXXXX")
tmux new-session -d -s "$session" -c "$run/project" -x 100 -y 30 \
  "env -i HOME=$run PATH=/usr/bin:/bin:/usr/sbin:/sbin TERM=xterm-256color zsh -f"
tmux send-keys -t "$session" -l "$root/bin/cake-repl -cake-bin $root/scripts/fake-cake.sh \
  -no-config -no-color -no-session -cwd $run/project -history-file $run/history"
tmux send-keys -t "$session" Enter
```

`env -i` with `HOME` pointed at the scratch directory keeps the pane free of user
rc files and history, so a capture cannot disclose them.

The REPL owns the terminal and leaves `ISIG` off, so Ctrl+Z is just a keystroke.
To raise the real signal, arm the pane's line discipline first and then send the
key — that raises SIGTSTP for the pane's foreground process group, which is the
job the shell made for the REPL, so the shell is never signalled by mistake:

```bash
tty=$(tmux display -p -t "$session" '#{pane_tty}')
stty -f "$tty" isig                      # re-arm before EVERY cycle; raw mode clears it
tmux send-keys -t "$session" C-z
sleep 1                                  # the shell now owns the terminal
```

Observe from the shell's side rather than the app's: while the REPL is stopped,
`tmux display -p -t "$session" '#{pane_current_command}'` is the shell, and
`#{mouse_any_flag}` is `0` if the REPL released mouse reporting. Resume with a
literal `fg` plus Enter, then check the modes and the input path:

```bash
tmux send-keys -t "$session" -l 'fg'; tmux send-keys -t "$session" Enter
sleep 1
stty -f "$tty" -a | sed -n 2p          # lflags: -icanon -isig ... -echo when raw mode is back
tmux display -p -t "$session" '#{mouse_any_flag}'   # 1 again
tmux send-keys -t "$session" Up        # the composer must recall history, not show ^[[A
tmux send-keys -t "$session" Escape '[<0;25;10M'    # a synthetic SGR mouse report must not appear in the frame
```

A resumed frame that shows the shell's own notice, `^[[A`, or two composers is
failing. Run the cycle idle, with a run in flight (`FAKE_CAKE_DELAY` keeps it on
screen), and with the window resized while the REPL is stopped.

### 6. Capture the artifact

```bash
# -S -<n> is the lines of scrollback above the visible pane to include
tmux capture-pane -pt "$session" -S -300 > "$run/captures/after-prompt.txt"
```

Name the file after the state, and capture every state the change affects
(idle, running, `-resume` hydration, resized, `-no-color` and colored). The
capture is the handoff/PR artifact: paste it into the PR body or handoff under a
"Terminal capture" heading, and attach the file if the host supports it. Keep it
out of the repository — it carries a session UUID and machine paths, and a
tracked capture would rot. For styled output, drop `-no-color` **only** when the
change is about color, and add `-e` to `capture-pane` to keep the SGR
sequences.

### 7. Tear down only your session

```bash
tmux send-keys -t "$session" C-c        # quits when idle; exercises the exit path
deadline=$((SECONDS + 10))
until ! tmux has-session -t "$session" 2>/dev/null; do
    [ "$SECONDS" -lt "$deadline" ] || break
    sleep 0.1
done
tmux kill-session -t "$session" 2>/dev/null || true   # backstop if it did not exit
```

## Worked example

A whole run in one call. `<slug>` is your unique session suffix; the Bash tool's
own `timeout` bounds the call.

```bash
set -eu
root=$(git rev-parse --show-toplevel); just build
run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-drive.XXXXXX")
mkdir -p "$run/project" "$run/captures"
session=cake-repl-tui-<slug>

tmux new-session -d -s "$session" -c "$root" -x 100 -y 30 \
  "FAKE_CAKE_FIXTURE=happy-path FAKE_CAKE_DELAY=0.4 $root/bin/cake-repl \
     -cake-bin $root/scripts/fake-cake.sh -no-config -no-color -no-session \
     -cwd $run/project -history-file $run/history"

wait_for() {                      # wait_for <session> <text> [seconds]
    _d=$((SECONDS + ${3:-30}))
    until tmux capture-pane -pt "$1" -S -200 | grep -qF -- "$2"; do
        [ "$SECONDS" -lt "$_d" ] || { tmux capture-pane -pt "$1" -S -200 >&2; return 1; }
        sleep 0.1
    done
}
wait_for "$session" 'prompt · ready'
tmux capture-pane -pt "$session" -S -200 > "$run/captures/idle.txt"

tmux send-keys -t "$session" -l "hello there"
tmux send-keys -t "$session" C-s
wait_for "$session" 'running'
tmux capture-pane -pt "$session" -S -200 > "$run/captures/running.txt"
wait_for "$session" '[ idle ]'
tmux capture-pane -pt "$session" -S -200 > "$run/captures/result.txt"

tmux send-keys -t "$session" C-c
_d=$((SECONDS + 10))
until ! tmux has-session -t "$session" 2>/dev/null; do
    [ "$SECONDS" -lt "$_d" ] || break
    sleep 0.1
done
tmux kill-session -t "$session" 2>/dev/null || true
echo "captures in $run/captures"
```

## Failure modes

- **Sleeping instead of waiting.** A bare `sleep` before a snapshot captures
  whatever was on screen, which is how a stale frame becomes "evidence".
- **Killing the wrong session.** `tmux kill-server` or a name you did not verify
  takes down the human's session too.
- **A relative `-cake-bin`.** The REPL resolves it against `-cwd`, so the run
  silently fails to start unless the path is absolute.
- **Skipping `-no-config`.** A developer's config changes the defaults, and the
  capture stops being reproducible.
- **Leaving the app running.** Snapshot the exit path too; a UI change can break
  terminal restore without changing the visible frames.
- **Committed captures.** They encode a session UUID and local paths; keep them
  in the temp directory.
