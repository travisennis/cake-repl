# cake-repl Plan

## Purpose

`cake-repl` is a standalone terminal REPL/TUI frontend for the `cake` CLI.

It should treat `cake` as the engine and communicate through stable CLI contracts,
primarily:

```bash
cake --output-format stream-json "prompt"
cake --output-format stream-json --continue "prompt"
cake --output-format stream-json --resume <uuid> "prompt"
```

The project should not link to cake internals, import Rust code, parse human
text output, or duplicate cake's agent loop. It should consume machine-readable
NDJSON records from stdout and present a better interactive terminal experience.

## Recommended Stack

Use Go with:

- `github.com/charmbracelet/bubbletea` for the TUI update loop.
- `github.com/charmbracelet/bubbles` for textarea, viewport, spinner, help,
  keybindings, and list components where useful.
- `github.com/charmbracelet/lipgloss` for styling.

Reasoning:

- The project is a terminal frontend over a subprocess/event-stream boundary.
- Bubble Tea's message/update model maps cleanly to streamed cake events.
- Bubbles provides enough primitives to ship a usable REPL without writing a lot
  of TUI plumbing.
- Keeping this in Go prevents accidental coupling to cake's Rust internals.

## Product Goal

Build a serious, fast REPL for repeated cake conversations:

- Type a prompt.
- Submit it to cake.
- Watch assistant messages, reasoning summaries, tool calls, hook events, and
  completion status stream live.
- Continue the same cake session automatically after a successful turn.
- Start, resume, or inspect session state from slash commands.
- Cancel an active cake process without losing the whole REPL.

The first version should be useful as a daily driver without trying to become a
full IDE.

## Non-Goals For v0.1

Do not build these initially:

- A full session browser.
- A settings editor.
- A diff viewer.
- Worktree management.
- A markdown renderer beyond basic wrapping.
- Hook configuration management.
- Direct OpenAI/provider integration.
- Direct reads of cake's Rust structs or internal modules.
- A replacement for cake's CLI flags.

These can come later after the core event-driven REPL works.

## Core Boundary

`cake-repl` owns:

- Interactive terminal UI.
- Input editing.
- Slash commands.
- Subprocess lifecycle.
- Stream decoding.
- Event rendering.
- Light client-side session state.

`cake` owns:

- Model/provider configuration.
- Agent loop.
- Tool execution.
- Sandbox behavior.
- Session persistence.
- Hook execution.
- Output/event schemas.

The wrapper must respect cake's compatibility surfaces. If `cake-repl` needs
new data, prefer adding a structured cake CLI command later instead of reading
private implementation details.

## Initial Project Layout

Suggested structure:

```text
cake-repl/
  go.mod
  go.sum
  README.md
  plan.md
  cmd/
    cake-repl/
      main.go
  internal/
    app/
      model.go
      update.go
      view.go
      keys.go
      commands.go
    cake/
      runner.go
      events.go
      parser.go
    ui/
      theme.go
      timeline.go
      toolblock.go
      status.go
```

Keep this split pragmatic. If the first implementation is smaller, start with
fewer files and split only when the code becomes clearer by doing so.

## CLI Interface

Initial command:

```bash
cake-repl [flags]
```

Recommended flags:

- `--cake-bin <path>`: cake executable to run. Default: `cake`.
- `--continue`: continue cake's latest session for the current directory on
  first prompt.
- `--resume <uuid>`: resume a specific cake session on first prompt.
- `--model <name>`: pass through to cake.
- `--profile <name>`: pass through to cake.
- `--cwd <path>`: run cake from a specific working directory. Default: current
  directory.
- `--no-color`: disable styling.
- `--debug-log <path>`: optional local debug log for `cake-repl` itself.

Do not add a generic "pass arbitrary flags" mechanism at first. It makes state
harder to reason about. Add explicit pass-through flags as real use cases show
up.

## Slash Commands

Implement these for v0.1:

- `/help`: show commands and keybindings.
- `/exit`, `/quit`, `/q`: exit when idle; ask/cancel when a task is running.
- `/new`: clear local session state; next prompt starts a fresh cake session.
- `/continue`: next prompt uses `cake --continue`.
- `/resume <uuid>`: next prompt uses `cake --resume <uuid>`.
- `/session`: show current session id, current task id, cwd, run mode, and last
  completion summary.
- `/clear`: clear the visible timeline but keep session state.

Later candidates:

- `/model <name>`
- `/profile <name>`
- `/fork [uuid]`
- `/sessions`
- `/open-session`

## Keybindings

Suggested defaults:

- `Ctrl+S`: submit input.
- `Ctrl+J` or `Alt+Enter`: insert newline.
- `Ctrl+C`: cancel active cake task; quit when idle.
- `Esc`: close modal/help or blur current overlay.
- `PageUp/PageDown`: scroll timeline.
- `Ctrl+U`: clear current input.

Avoid overloading plain `Enter` until the input behavior is tested. A safe first
choice is:

- `Enter`: newline.
- `Ctrl+S`: submit.

If that feels too slow, later add a mode where `Enter` submits and
`Alt+Enter` inserts newline.

## Cake Subprocess Behavior

For each submitted prompt, spawn one cake process:

```bash
cake --output-format stream-json [session flag] [pass-through flags] "prompt"
```

Session flag rules:

1. Fresh session: no session flag.
2. Continue latest session: `--continue`.
3. Resume known session: `--resume <uuid>`.
4. After a successful task, default future prompts to `--continue`.
5. After `/new`, clear session id and use no session flag for the next prompt.
6. After `/resume <uuid>`, use `--resume <uuid>` for the next prompt; after it
   succeeds, default future prompts to `--continue`.

Implementation details:

- Use `context.Context` to cancel the running cake process.
- Capture stdout and stderr separately.
- Decode stdout as newline-delimited JSON.
- Do not render stderr during successful runs unless a debug mode is enabled.
- On non-zero exit, show exit code and useful stderr.
- If stdout contains a malformed JSON line, preserve a short raw diagnostic in
  the timeline and keep reading.
- If the process exits without `task_complete`, mark the task as interrupted or
  failed depending on exit status.

## Stream Event Contract

`cake --output-format stream-json` emits one JSON object per stdout line with a
top-level `type` discriminator.

Known event types:

- `task_start`
- `message`
- `reasoning`
- `function_call`
- `function_call_output`
- `hook_event`
- `task_complete`

Unknown event types must be ignored or rendered as debug-only records. Unknown
fields must be ignored. Missing optional fields must not crash the UI.

### Go Event Types

Use a small typed event layer. Example:

```go
type StreamEnvelope struct {
    Type string `json:"type"`
}

type TaskStart struct {
    Type      string `json:"type"`
    SessionID string `json:"session_id"`
    TaskID    string `json:"task_id"`
    Timestamp string `json:"timestamp"`
}

type Message struct {
    Type      string `json:"type"`
    Role      string `json:"role"`
    Content   string `json:"content"`
    ID        string `json:"id,omitempty"`
    Status    string `json:"status,omitempty"`
    Timestamp string `json:"timestamp,omitempty"`
}

type FunctionCall struct {
    Type      string `json:"type"`
    ID        string `json:"id"`
    CallID    string `json:"call_id"`
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
    Timestamp string `json:"timestamp,omitempty"`
}

type FunctionCallOutput struct {
    Type      string `json:"type"`
    CallID    string `json:"call_id"`
    Output    string `json:"output"`
    Timestamp string `json:"timestamp,omitempty"`
}

type TaskComplete struct {
    Type          string `json:"type"`
    Subtype       string `json:"subtype"`
    IsError       bool   `json:"is_error"`
    DurationMS    int64  `json:"duration_ms"`
    TurnCount     int    `json:"turn_count"`
    ToolCallCount int    `json:"tool_call_count"`
    SessionID     string `json:"session_id"`
    TaskID        string `json:"task_id"`
    Result        string `json:"result,omitempty"`
    Error         string `json:"error,omitempty"`
    Usage         Usage  `json:"usage"`
}

type Usage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
    TotalTokens  int `json:"total_tokens"`
}
```

For `hook_event`, either parse the fields needed for display or keep it as a
generic `map[string]any` initially.

## UI Model

Suggested app state:

```go
type Model struct {
    Cwd string
    CakeBin string

    Width int
    Height int

    Input textarea.Model
    Timeline viewport.Model
    Spinner spinner.Model

    Running bool
    Cancel context.CancelFunc

    SessionID string
    TaskID string
    NextRunMode RunMode
    LastSummary *TaskSummary

    PendingCalls map[string]ToolCall
    Items []TimelineItem
    Err error
}

type RunMode int

const (
    RunFresh RunMode = iota
    RunContinue
    RunResume
)
```

The model should store structured timeline items, not pre-rendered strings only.
That allows resizing to reflow content later.

## Timeline Rendering

Render a compact transcript:

- User prompts: optionally shown, dimmed or labeled.
- Assistant messages: main content, wrapped.
- Reasoning summaries: dim/yellow style, compact.
- Tool calls: grouped with their matching output.
- Hook events: hidden by default or shown as one-line diagnostics when relevant.
- Completion: duration, turns, tool calls, token usage, session id.
- Errors: visible, red, with exit code and stderr summary.

Tool output should be truncated by default. Suggested initial limits:

- Show first 2,000 characters.
- Preserve line boundaries where possible.
- Add a clear truncation marker with original byte length.

Tool argument display should be concise:

- `bash`: show `$ command` and cwd when present.
- `read`: show path and line range.
- `edit`: show path and first few edit previews.
- `write`: show path, line count, byte count, and first non-empty line.
- Unknown tools: JSON summary truncated to 160 chars.

Important: cake tool names may appear as lowercase (`bash`, `read`, `edit`,
`write`) or display-case depending on future changes. Normalize names for
rendering, but preserve the original name in data.

## Layout

Initial layout:

```text
┌ timeline ───────────────────────────────────────┐
│ streamed task output                            │
│ assistant response                              │
│ tool blocks                                     │
├ input ──────────────────────────────────────────┤
│ multiline prompt textarea                       │
└ status: session abc... | idle/running | model ──┘
```

Implementation notes:

- Keep a single full-screen Bubble Tea program.
- Timeline takes all remaining height.
- Input area should have a minimum height of 3 lines and grow to a capped height.
- Status line should always remain visible.
- On narrow terminals, prioritize readable text over decorative borders.
- Avoid UI cards inside UI cards. Keep this practical and dense.

## Error Handling

Handle these cases explicitly:

- `cake` binary not found.
- `cake` exits with code 1, 2, or 3.
- Invalid `/resume` UUID format.
- Process cancellation.
- Broken stdout pipe.
- Malformed stream line.
- EOF before `task_complete`.
- Terminal resize during active streaming.

Do not panic on malformed cake output. A frontend should degrade visibly, not
crash.

## Testing Strategy

Unit tests:

- Stream parser decodes each known event type.
- Unknown event type is handled safely.
- Malformed JSON returns a parser error without killing the runner.
- Tool argument formatting for bash/read/edit/write.
- Slash command parser.
- Run mode transitions:
  - fresh -> success -> continue
  - resume -> success -> continue
  - new -> fresh
  - failure behavior is explicit

Integration tests:

- Use a fake cake executable script that emits known NDJSON and exits 0.
- Use a fake cake executable that emits malformed lines.
- Use a fake cake executable that exits non-zero with stderr.
- Use a fake long-running cake executable and verify cancellation.

Manual checks:

- Run against real `cake` from the cake repo.
- Submit single-line prompt.
- Submit multiline prompt.
- Cancel a running prompt.
- Resize terminal while running.
- Verify `--continue` behavior after first successful turn.
- Verify `/new` starts a fresh session.
- Verify `/resume <uuid>` resumes a known session.

## Implementation Milestones

### Milestone 1: Skeleton

- Initialize Go module.
- Add Bubble Tea, Bubbles, Lip Gloss.
- Add `cmd/cake-repl/main.go`.
- Start a full-screen TUI with textarea, viewport, status line.
- Implement quit keybinding.

Done when `go run ./cmd/cake-repl` opens a stable terminal UI.

### Milestone 2: Static Input Flow

- Implement multiline input.
- Implement submit command.
- Add timeline item for submitted user prompt.
- Add `/help`, `/exit`, `/clear`.

Done when prompts can be entered and displayed locally without spawning cake.

### Milestone 3: Cake Runner

- Implement subprocess runner.
- Decode stdout NDJSON.
- Send parsed events into Bubble Tea messages.
- Capture stderr and exit code.
- Implement cancellation with `context.Context`.

Done when fake cake scripts can drive the UI deterministically.

### Milestone 4: Event Rendering

- Render task start.
- Render assistant messages.
- Render reasoning summaries.
- Render tool calls grouped with outputs.
- Render task completion summary.
- Render errors.

Done when a realistic stream-json transcript is readable.

### Milestone 5: Session Commands

- Track session id and task id from `task_start` and `task_complete`.
- Implement `/new`.
- Implement `/continue`.
- Implement `/resume <uuid>`.
- Implement `/session`.
- Apply correct session flag to each cake invocation.

Done when repeated turns continue correctly through cake.

### Milestone 6: Polish And Packaging

- Add README with install/run instructions.
- Add tests for parser, command handling, and run mode transitions.
- Add `go test ./...`.
- Add basic release build command documentation.
- Verify on macOS terminal.

Done when another user can clone, build, and run the project.

## README Content To Add

The README should include:

- What `cake-repl` is.
- Requirements:
  - Go version.
  - `cake` installed and available on PATH, or use `--cake-bin`.
- Install/build:

```bash
go build ./cmd/cake-repl
```

- Run:

```bash
cake-repl
cake-repl --continue
cake-repl --resume <uuid>
cake-repl --cake-bin ../cake/target/debug/cake
```

- Keybindings.
- Slash commands.
- Known limitations.

## Open Design Questions

Resolve during implementation:

1. Should `Enter` submit or insert newline by default?
   - Conservative first choice: `Enter` inserts newline, `Ctrl+S` submits.
2. Should failed turns keep `NextRunMode` as continue/resume?
   - Conservative first choice: do not advance session mode on failure unless a
     `task_start` supplied a session id and the user explicitly continues.
3. Should hook events be hidden by default?
   - Conservative first choice: hide successful hook noise, show deny/error/stop
     outcomes.
4. Should assistant message chunks be appended as separate items or merged?
   - Conservative first choice: append as they arrive, then improve merging if
     cake emits partial chunks in the future.
5. Should the app read persisted session files?
   - Conservative first choice: no for v0.1. Add structured cake commands later
     if session browsing becomes important.

## Risks

- `stream-json` schema may evolve. Mitigation: ignore unknown fields/types and
  test parser behavior.
- Long tool outputs can overwhelm the UI. Mitigation: truncate aggressively and
  add later expansion controls.
- Subprocess cancellation can leave partial cake records. Mitigation: render
  cancellation as a normal terminal state.
- Direct session-file reading would duplicate cake rules. Mitigation: avoid it
  initially.
- Too much UI structure can slow down the first useful version. Mitigation:
  build the REPL first, dashboard later if it earns its keep.

## Acceptance Criteria For v0.1

The project is ready for initial use when:

- `go run ./cmd/cake-repl` opens the TUI.
- A prompt can be submitted to a real cake binary.
- `stream-json` records render live.
- Tool calls and outputs are grouped.
- A successful first turn causes later turns to continue the session.
- `/new`, `/resume <uuid>`, `/session`, `/clear`, and `/help` work.
- `Ctrl+C` cancels a running cake process and exits when idle.
- Non-zero cake exits show useful stderr and exit code.
- `go test ./...` passes.
- README explains how to build and run.
