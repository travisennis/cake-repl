---
status: accepted
date: 2026-08-01
decision-makers: Travis Ennis
---
# Model and profile slash commands are idle-only

## Context and Problem Statement

`--model` and `--profile` are startup-only flags, so switching model or
profile mid-session requires quitting and restarting the REPL. `/model` and
`/profile` follow the existing session-command pattern (`/new`, `/continue`,
`/resume` mutate what the next `cake.Start` uses), but their semantics —
especially behavior during an active run — were not decided.

## Decision Drivers

- Session commands are rejected during an active run (task 051 policy) to
  avoid silent clobbering; new slash commands must not re-open that bug class.
- The command grammar must be unambiguous and documentable.
- Changes apply to the next prompt, consistent with session commands.

## Considered Options

1. **Idle-only.** `/model` and `/profile` are rejected with a warning while a
   task runs, like `/new`, `/continue`, `/resume`.
2. **Any time.** Allowed during a run, applying to the next prompt.

Grammar variants considered: no-argument status, `<name>` set, literal
`default` clear, and whether an empty quoted argument is meaningful.

## Decision Outcome

Chosen option: **1, idle-only**, with the grammar:

- `/model` with no argument prints the current model.
- `/model <name>` sets the model for the next prompt.
- `/model default` clears an override and uses cake's default.
- `/profile` behaves identically.
- An empty quoted argument is not part of the grammar and is a parse error.

During an active run both commands are rejected with a warning to finish or
cancel first, matching the `/new` `/continue` `/resume` policy in the
session-and-security guardrail. Values apply to the next prompt's `cake.Start`
and are read from mutable session state rather than the startup `Config`,
which keeps the initial flag values as defaults.

### Consequences

- Good, because mid-run state is never ambiguous and the task 051 clobbering
  class stays closed.
- Good, because the grammar is explicit and fits the existing slash-command
  documentation surface.
- Neutral, because model/profile remain startup-only until implemented.
- Bad, none identified; the idle-only restriction matches existing session
  commands.

## More Information

- Task 023.
- Session-and-security guardrail, active-run restriction.

