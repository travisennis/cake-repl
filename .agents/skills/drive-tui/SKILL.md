---
name: drive-tui
description: Drive cake-repl in a headless tmux session against a real cake binary to observe and capture what it actually puts on the screen. Use when verifying a rendering or interaction change — timeline items, the composer, the status line, key bindings, resize, color, or a new stream event's rendering — because the build and lint gates never drive the program or a terminal. Never use it for behavior a `go vet` or `go build` change would catch.
---

# Driving the REPL in a terminal

`cli-and-user-output.md` requires a terminal capture for a UI/output change. A
successful build shows only that the item kinds format; it does not show that
the program renders correctly in a terminal. This skill drives the real binary
in a tmux pane and produces that capture.

It follows an **observe → act → observe** loop: wait for a predicate you can see,
act, then snapshot the result. Never `sleep` and assume the screen is ready;
never act twice on one screen you have not read.

## What it needs

- `tmux` 3.x (`tmux -V`). It is a host tool for this skill only, not a project
  or CI dependency.
- A real `cake` binary on `PATH`, or `-cake-bin <absolute path>`.
- A built REPL: `just build` (run `bin/cake-repl` directly, not `just run`;
  `go run` adds a second process per restart and its stderr noise lands in the
  pane).
- `perl` for the color pass's SGR normalization. Like tmux, it is a host tool
  for this skill only.

A live prompt spends model quota. Prefer a local, cheap model for a rendering
check, and keep the prompt tiny.

## Isolation: the same screen twice

Every run must start from the same screen or the capture proves nothing. Launch
with all of these:

| Lever | Why |
| --- | --- |
| `-no-config` | the user's XDG and project-local config cannot change the defaults |
| `-no-color` | termenv `Ascii`, so the plain capture is stable and readable; the color pass omits it |
| `-no-session` | nothing is written to a cake session |
| `-cwd <temp>/project` | isolates the run; the base name is the `cwd:` field on the status line, so keep it stable (`project`) |
| `-history-file <temp>/history` | prompt history starts empty on every run |
| fixed `-x`/`-y` on `tmux new-session` | the layout is width-sensitive; 100x30 is the default here |

## Procedure

### 1. Freeze the screen

`just build` writes only the gitignored `bin/cake-repl`; nothing else in the
repository changes.

```bash
root=$(git rev-parse --show-toplevel)
just build
run=$(mktemp -d "${TMPDIR:-/tmp}/cake-repl-drive.XXXXXX")
mkdir -p "$run/project" "$run/captures"
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
  "$root/bin/cake-repl -no-config -no-color -no-session \
     -cwd $run/project -history-file $run/history"
```

Confirm the size took: `tmux display -p -t "$session" '#{window_width}x#{window_height}'`.

### 3. Wait for a predicate, never sleep

Poll a visible marker with a bounded deadline. Each Bash call is a fresh shell:
`$root`, `$run`, `$session`, and any function you define vanish between calls,
so either keep a whole step in one call or stash the values and re-read them.

```bash
wait_for() {                      # wait_for <session> <text> [seconds]
    _s=$1 _want=$2 _limit=${3:-60}
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
```

Markers this REPL renders: `prompt · ready` / `prompt · running` /
`prompt · compacting` / `prompt · stopping task` / `prompt · stopping compaction` /
`prompt · loading history` / `prompt · stopping history` /
`prompt · browsing sessions` / `prompt · exiting`, and the status line
`[ idle ]` / `[ <spinner> running ]` / `[ <spinner> compacting ]` /
`[ stopping task ]` / `[ stopping compaction ]` / `[ loading history ]` /
`[ stopping history ]` / `[ browsing sessions ]` / `[ exiting ]`. The spinner
glyph advances every frame, so match the stable word, not the glyph. Wait on the
text the change is about, not on chrome.

### 4. Act

Send text and the submit key as **separate** writes, then observe. Submit is
`Ctrl+S`; `Enter` inserts a newline, so sending Enter does not submit.

```bash
tmux send-keys -t "$session" -l "hello there"   # literal text
wait_for "$session" 'hello there'               # the composer accepted it
tmux send-keys -t "$session" C-s                # submit
wait_for "$session" 'running'                   # the run started
wait_for "$session" '[ idle ]'                  # the run finished
tmux capture-pane -pt "$session" -S -200
```

`[ idle ]` is already on screen before the task starts, so waiting for it
immediately after `C-s` can snapshot the pre-submit frame. Wait for the typed
text, then for the run to start, then for `[ idle ]`.

Other bindings worth driving: `C-c` cancels a running task and quits when idle,
`C-n` starts a new session, `C-u` clears input, `C-o` cycles tool output, `Up`
recalls history, `Tab` completes a command, `Esc` closes the `/sessions`
browser. Snapshot after each one; a keypress can land in the next view.

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
width: pick a prompt whose text wraps differently at the two widths.

### 5a. Suspend and resume

Driving a stop signal needs a job-control shell between tmux and the binary, so
launch the pane as a pristine interactive shell and start the REPL from it. That
shell is also the only `fg` there is:

```bash
tmux new-session -d -s "$session" -c "$run/project" -x 100 -y 30 \
  "env -i HOME=$run PATH=/usr/bin:/bin:/usr/sbin:/sbin TERM=xterm-256color zsh -f"
tmux send-keys -t "$session" -l "$root/bin/cake-repl -no-config -no-color -no-session \
  -cwd $run/project -history-file $run/history"
tmux send-keys -t "$session" Enter
```

The REPL owns the terminal and leaves `ISIG` off, so Ctrl+Z is just a keystroke.
To raise the real signal, arm the pane's line discipline first and then send the
key:

```bash
tty=$(tmux display -p -t "$session" '#{pane_tty}')
stty -f "$tty" isig                      # re-arm before EVERY cycle
tmux send-keys -t "$session" C-z
sleep 1                                  # the shell now owns the terminal
```

While the REPL is stopped, `tmux display -p -t "$session" '#{pane_current_command}'`
is the shell, and `#{mouse_any_flag}` is `0` if the REPL released mouse
reporting. Resume with a literal `fg` plus Enter, then check the modes and the
input path:

```bash
tmux send-keys -t "$session" -l 'fg'; tmux send-keys -t "$session" Enter
sleep 1
stty -f "$tty" -a | sed -n 2p          # lflags: -icanon -isig ... -echo when raw mode is back
tmux display -p -t "$session" '#{mouse_any_flag}'   # 1 again
tmux send-keys -t "$session" Up        # the composer must recall history, not show ^[[A
```

A resumed frame that shows the shell's own notice, `^[[A`, or two composers is
failing. Run the cycle idle and with a run in flight.

### 6. Capture the artifact

```bash
tmux capture-pane -pt "$session" -S -300 > "$run/captures/after-prompt.txt"
```

Name the file after the state, and capture every state the change affects
(idle, running, `-resume` hydration, resized, `-no-color` and colored). The
capture is the handoff/PR artifact: paste it into the PR body or handoff under a
"Terminal capture" heading. Keep it out of the repository — it carries a session
UUID and machine paths, and a tracked capture would rot. A plain capture proves
text and layout, never color or intensity; when the change is about styling, run
the color pass in step 6a.

### 6a. Color and style pass

Run a second session from the same size, with the same isolation flags but
**without** `-no-color`. Set a deterministic color profile, and use a separate
history file and session name so the plain capture stays frozen.

```bash
color_session=cake-repl-tui-081-color
tmux new-session -d -s "$color_session" -c "$root" -x 100 -y 30 \
  "TERM=xterm-256color COLORTERM=truecolor \
   $root/bin/cake-repl -no-config -no-session \
     -cwd $run/project -history-file $run/history-color"
```

Wait for `prompt · ready`, send a small prompt, submit with `C-s`, wait for the
changed text to render, then for `[ idle ]`. Capture the escapes and translate
each SGR sequence to a readable tag:

```bash
tmux capture-pane -p -e -t "$color_session" -S -300 \
  > "$run/captures/after-prompt-color.ansi"
perl -pe 's{\e\[([0-9;]*)m}{join("", map {"<$_>"} split /;/, $1)}ge' \
  "$run/captures/after-prompt-color.ansi" \
  > "$run/captures/after-prompt-color.tags"
```

Read the expected codes from the theme role in `internal/ui/theme.go` (`1` bold,
`2` faint, `4` underline, `39` default foreground), and assert the role on the
changed line plus its contrast case.

The `.ansi` file is the handoff artifact; keep the `.tags` file beside it as the
readable assertion. The plain capture from step 6 is the `-no-color` check: both
captures must show the same text, and only the `.ansi` capture may carry
escapes. Tear down `$color_session` with the step 7 loop as well.

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

If step 6a ran, repeat this block with `$color_session`; never kill the tmux
server.

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
- **Verifying styling with `-no-color`.** Termenv `Ascii` erases color and
  intensity, so a style regression renders as plain text and looks "correct".
  Run the color pass in step 6a for any styling change.
- **Committed captures.** They encode a session UUID and local paths; keep them
  in the temp directory.
