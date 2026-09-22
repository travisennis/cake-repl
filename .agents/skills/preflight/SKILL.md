---
name: preflight
description: Run a focused review-readiness pass on a nearly finished change before commit. Scales the review to change size (XS/S = one pass, M = two passes, L/XL = three sequential passes covering rules conformance, correctness/source-of-truth, and overengineering), selects only the repository lenses the changed behavior touches, and applies a stated finding threshold. Then synthesize and apply the worthwhile fixes.
---

# Preflight

Use this skill after a change is functionally correct and before commit or
handoff. The PR, commit text, task notes, and final response should describe
already-preflighted code.

## Goals

Leave the smallest clear diff that still solves the issue. Run focused
review passes instead of one subjective read. Preserve behavior while
improving readability, correctness, and alignment with repo rules.

## Scale the review to the change size

Pick an effort level from the diff before reading anything else:

```bash
git diff --stat
git status --short
```

Include untracked new files from `git status --short` (or
`git ls-files --others --exclude-standard`) when choosing the scale. A split
into new files can look deceptively small in `git diff --stat` until those
files are staged.

| Change size                                     | Required context           | Review passes        | Compliance note |
| ----------------------------------------------- | -------------------------- | -------------------- | --------------- |
| **XS** (docs/skill/config only, ≤2 files)       | Root AGENTS.md if relevant | One combined pass    | One line        |
| **S** (single module, ≤~50 LOC, no public API)  | Root AGENTS.md, selected lens rows | One combined pass    | One line        |
| **M** (multi-file, ≤~200 LOC, no cross-module)  | + selected lens rows, active task file, design plan if one exists | Pass 1 + Pass 2      | Short block     |
| **L/XL** (cross-module, public API, agent loop, persistence, concurrency, external integrations, security boundaries) | + every lens row the diff touches, with its guardrails and ADRs | All three passes     | Full block      |

Read only the context the changed surface calls for, found through
[Lenses](#lenses). Locate it with targeted commands such as
`git diff -- <paths>`, `rg --files docs/guardrails docs/adr`, and
`rg -n <symbol> internal/`.

Authorities, in priority order:

- repo root `AGENTS.md`
- the guardrail each selected lens names
- `ahm task show <id>` output when the work came from a task; open the task
  file under `.ahm/tasks/` only when `ahm` is unavailable or when reviewing
  manual edits to the task file itself; use `.ahm/tasks/index.md` only as a
  fallback queue artifact when `ahm` is unavailable
- the relevant design plan when one exists for the current work (see
  `docs/exec-plans/active/`)
- the ADRs the lens row names
- the changed files and enough nearby context to review them

This repository has no nested `AGENTS.md` files. Routing is by lens, not by
directory; do not invent a nested instruction file to read.

## Lenses

A lens is a changed behavior plus the document that owns its rules. Select
lenses from the behavior the diff changes, not from the paths it touches: a
name alone does not select a lens.

| Lens | Selected when the diff changes | Read |
| --- | --- | --- |
| **Stream-json decode** | `internal/cake/events.go`, `parser.go`, or any code turning stream records into typed events | [cake integration and stream-json](../../../docs/guardrails/cake-integration-and-stream-json.md); ADRs [008](../../../docs/adr/008-bounded-single-line-reads-from-the-cake-stream.md), [011](../../../docs/adr/011-replay-resumed-sessions-through-cake-stream-json.md) |
| **Engine boundary** | the cake command line built in `Options.Args`, a new or changed cake flag, or process lifecycle in `runner.go` | [cake integration and stream-json](../../../docs/guardrails/cake-integration-and-stream-json.md) |
| **Session run mode** | `session.go`, run-mode transitions in `update.go`, `--resume` / `--fork` / `--no-session` handling, `Ctrl+N` | [sessions, security, and subprocess lifecycle](../../../docs/guardrails/session-and-security.md); ADRs [001](../../../docs/adr/001-session-run-mode-pinned-to-resume-to-prevent-hijack.md), [004](../../../docs/adr/004-ctrl-n-creates-an-isolated-new-session-boundary.md), [012](../../../docs/adr/012-remove-continue-and-expose-cake-session-controls.md) |
| **Render boundary** | anything in `internal/ui/`, `ui.Sanitize`, timeline caching, styling, or how an event reaches the screen | [CLI, commands, and user output](../../../docs/guardrails/cli-and-user-output.md); ADRs [003](../../../docs/adr/003-tool-output-expansion-key-binding.md), [005](../../../docs/adr/005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md), [009](../../../docs/adr/009-opt-in-sgr-passthrough-for-tool-output.md), [010](../../../docs/adr/010-bound-per-tool-result-retention-and-record-the-unbounded-timeline-default.md) |
| **Command surface** | a slash command, key binding, `HelpText`, tab completion, or a startup flag | [CLI, commands, and user output](../../../docs/guardrails/cli-and-user-output.md); `README.md`, `internal/app/commands.go` |
| **Config and on-disk state** | `internal/config/`, `cmd/cake-repl/main.go` precedence and validation, the debug log, the history file, anything persisted | [sessions, security, and subprocess lifecycle](../../../docs/guardrails/session-and-security.md); ADRs [002](../../../docs/adr/002-config-file-for-repl-defaults.md), [006](../../../docs/adr/006-project-local-config-cannot-select-the-cake-executable.md), [007](../../../docs/adr/007-model-and-profile-slash-commands-are-idle-only.md) |
| **Test evidence** | tests, fixtures under `internal/cake/testdata/fixtures/`, the fake-cake harness, or benchmarks | [testing and verification](../../../docs/guardrails/testing-and-verification.md) |
| **Build and toolchain** | `go.mod`, `justfile`, `.golangci.yml`, `.github/`, `.goreleaser.yaml` | [dependencies, build, CI, and release](../../../docs/guardrails/dependencies-build-ci-release.md); [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) |
| **Instruction and doc surfaces** | `README.md`, `ARCHITECTURE.md`, `AGENTS.md`, guardrails, ADRs, or a skill | [documentation](../../../docs/guardrails/documentation.md), and [agent-facing instructions](../../../docs/guardrails/agent-instructions.md) for a prose edit |

Do not read every lens as a precaution. When the change also alters a durable
contract, run the documentation impact matrix in the documentation guardrail
for that surface.

`AGENTS.md` routes the work an agent is about to do; this table routes the
review of work already done, and adds the ADRs the change must be checked
against. A guardrail that is renamed or moved appears in both, so fix both.

## Review passes

Treat each pass as a clean read with its own focus. Do not blur findings
across passes.

### Pass 1: Rules and documentation conformance

- Are we following `AGENTS.md`, the guardrails named by the selected
  lenses, the design plan, and the relevant ADRs?
- Did we drift from documented repo patterns or ownership boundaries?
- If the changed surface is user-visible CLI/API/config/file-format/workflow
  behavior, did we update the affected docs in the same change or record why
  the behavior is intentionally undocumented?
- If the work came from a task or ExecPlan, does the implementation match
  its acceptance notes and recorded decisions?
- Did we update the task, design plan, guardrail, or ADR notes when the change
  discovered something durable?

### Pass 2: Correctness and source of truth

Run the lenses selected above. Each one names the guardrail that owns this
project's rules for its surface; check the change against that document
rather than against a language-generic checklist.

Then, for the changed code itself:

- Are we preserving the project's canonical types, schemas, identifiers, and
  state machines, or did we stringify, parse, reshape, or duplicate a
  project-owned representation?
- Did we introduce stringly typed sentinels, loosely shaped maps or JSON,
  global state, or duplicated constants where the project normally uses a
  struct, a typed event, or shared config?
- Are fallible boundaries explicit about failure, with useful context and
  without swallowing parse, process, filesystem, network, or persistence
  errors?
- Are the subprocess handle, event channel, goroutine, and render-cache
  lifecycle boundaries consistent with nearby code?
- Could an existing compiler or linter check, test helper, fixture, or
  narrower type catch this mistake earlier? Prefer a check that fails in CI
  over a comment that asks nicely.
- **What can the tests not see?** Name the evidence the change relies on. A
  green `just test` cannot prove forward compatibility with a newer cake —
  the fixtures under `internal/cake/testdata/fixtures/` are ours. An
  unverifiable claim is unproven, not a finding-shaped certainty.

### Pass 3: Overengineering and simplification

- Did we write more code than needed?
- Did we create helpers, abstractions, factories, wrappers, or indirection
  without enough payoff?
- Could the same result be expressed more directly?
- Are new modules, traits, builders, or generic helpers justified by real
  reuse or by an existing design boundary?

## Between-pass hygiene

Ground each pass in narrow local evidence. Use the smallest check that fits
the change:

- `git diff --stat` and `git diff -- <paths>` to keep review anchored
- `gofmt -l .` (or `just fmt-check`) when formatting is affected
- focused tests in the changed package, e.g. `go test ./internal/cake/...`,
  then the narrowest matching row of the repo's verification ladder

[`testing-and-verification.md`](../../../docs/guardrails/testing-and-verification.md)
owns that ladder and [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) owns the
commands and their triggers; neither is copied here. Take the narrowest row the
change matches, and save `just ci` for handoff.

For docs-only or skill-only edits, verify rendered Markdown and links by
inspection or `rg --files`; full CI is not required.

## What counts as a finding

Hold every candidate finding to this bar before reporting it:

- A runtime defect counts only when the changed code produces concrete
  incorrect behavior on a supported path. State the triggering input and the
  user-visible, compatibility, security, or lifecycle consequence. No
  consequence, no finding.
- New machinery counts only when it does not map to the task, a documented
  contract, or an evidenced risk. Name the unnecessary piece and the smallest
  removal or direct replacement.
- Findings stay scoped to what the diff introduced, exposed, or worsened. A
  pre-existing problem found while reading adjacent code is in scope only when
  the change newly reaches it on a supported path, depends on it for
  correctness, or makes its consequence part of the changed behavior.
- Another design being cleaner, more symmetric, or easier to explain is not a
  defect. When the repository does not settle the question, report only a
  concrete inconsistency with an already supported path.
- Review the complete diff the change intends to ship — from the merge base
  of the intended target branch (`git diff "$(git merge-base HEAD <base>)"`)
  when the work is on a branch, otherwise the working tree — not just the
  last fix you made. Passing tests do not justify leftover machinery that no
  longer matches the request.

## Synthesis

After running the passes for the chosen scale, synthesize into one balanced
report with these headings:

- "How did we do?"
- "Feedback to keep"
- "Feedback to ignore"
- "Plan of attack"
- "Preflight compliance" (one line for XS/S; short block for M; full block for
  L/XL — see template below)

## What to fix automatically

In an unattended implementation flow, apply worthwhile feedback before
commit. Prioritize:

- type drift, unnecessary cloning/string conversion, duplicated type defs
- violations of documented repo boundaries or guardrails
- dead helpers, dead code, debug leftovers, placeholder text
- new panic/abort paths, placeholder exceptions, debug prints, commented-out
  code, broad lint suppressions, or ignored errors in production paths
- errors lacking actionable context at CLI/API/UI/database/config/process/
  network/external-service boundaries
- unnecessary wrappers or indirection removable locally without widening
  scope

Leave out feedback that is speculative, conflicts across passes, or would
widen scope materially. Mention it briefly in the synthesis.

## Compliance note

Make the chosen context auditable. Length scales with change size.

**XS / S example:**

```markdown
### Preflight compliance
- XS docs-only change to one skill file. Root AGENTS.md skim only; no lens
  selected beyond the doc surface; no CI required.
```

**M / L / XL template:**

```markdown
### Preflight compliance

- Root AGENTS.md: read
- Lenses: <lens names> / none, because <reason>
- Guardrails and ADRs read: <paths> / not applicable because <reason>
- Task context: <task id> / not applicable because <reason>
- ExecPlan: <plan id> / not applicable because <reason>
- Documentation impact: <docs checked/updated, or intentionally none because ...>
- Changed files and diff: reviewed via `git diff --stat` and targeted diffs
- Validation: <commands run>
```

Do not write blanket "no guardrail or design plan to check" claims unless you
actually looked for a relevant one and can explain why the changed area has no
such surface.

## Steps

1. Run `git diff --stat` and `git status --short`. Pick a scale from the
   table, counting untracked new files.
2. Select the lenses the changed behavior touches and read only their
   authorities, plus the task or design plan when one exists.
3. Run the review passes for that scale, with a narrow evidence check
   between them.
4. Synthesize findings into the balanced report.
5. Apply worthwhile feedback that is clearly in scope.
6. Rerun the narrowest affected validation, then the repo's documented
   final validation command when the finished work changed code, config, or
   dependencies.
7. Update task notes, ExecPlan notes, commit text, and PR/final response to
   describe the post-preflight state.

## Stop rules

- Do not turn this into a refactor unrelated to the ticket.
- Do not churn stable code outside the changed area just to make it
  prettier.
- If a cleanup is subjective and not clearly better, leave it alone.
- Do not blindly apply every finding from every pass.
- Do not run broad or slow checks repeatedly when a focused test already
  covers the current pass; save the repo's broad validation command for
  final validation.
- Do not escalate the scale beyond what the diff justifies just to feel
  thorough.
- Stop patching and escalate to a design decision when a second related
  finding would add another compatibility case, state field, retry, or
  fallback branch to the same design, or when a third round reports findings
  of the same class. Group the findings by root cause, reassess the whole
  diff against the original request, and report the class and the suspected
  design flaw instead of another patch.
