# Task Workflow

Use this reference to choose, prepare, work, and close tasks. The `ahm task ...`
commands are the primary interface: they own task identity, front matter,
storage, lifecycle transitions, and index regeneration. This reference focuses
on the decisions and order of work that the CLI cannot determine.

This document is project-owned prose. `ahm` does not print it, validate it, or
keep it current; edit it here when the project's task practice changes.

## Choose And Inspect Work

If the user names a task id or title, use that task even if another task is
higher in the queue:

```bash
ahm task show <id>
```

If the user asks for the next task, run `ahm task next`. Use `ahm task ready`
for a broader ready queue, or `ahm task list` and `ahm task blocked` to inspect
other states.

When choosing from the queue:

1. Work lower priority numbers first: `P0`, then `P1` through `P4`.
2. Start only `Pending` tasks. `Open` tasks need triage, and `Blocked` tasks
   are not directly workable. A `Tracking` task whose child tasks are all
   completed or cancelled appears in `ahm task ready` and is warned on by
   `ahm doctor`; complete the tracker itself instead of starting
   implementation work. Resume an `In Progress` task only when the user
   asks.
3. Check dependencies before starting. Work an incomplete dependency first or
   explain why the requested task is blocked.
4. Treat parent trackers as planning records and work their children in the
   stated order or the order shown by `ahm task ready`.
5. Use `--label` filters when the user asks for work in a particular area or
   risk category. Run `ahm task labels` to inspect the current vocabulary.

Before editing, read the full task and inspect the relevant repository state.
If the task is vague, stale, or conflicts with the current implementation,
record the discovery or ask for the missing product decision before proceeding.

## Create And Triage Tasks

Create tasks through the CLI:

```bash
ahm task create "Short imperative title" [flags]
```

The command allocates the id, writes the front matter and initial body, places
the task in the active bucket, and regenerates indexes. Use `--description` for
a concise summary or `--body-file` for a detailed record. The body should give
a future worker enough context to understand:

- the problem and why it matters;
- the relevant files, commands, modules, or observed behavior;
- useful implementation direction without unnecessarily prescribing the fix;
- concrete acceptance criteria and expected verification.

New tasks default to `Open`. Use `ahm task accept <id>` to move a triaged task
to the `Pending` queue. A fully scoped task may be created directly as
`Pending` with `--status Pending`.

Accept a task only when its problem and scope are clear, its priority, effort,
labels, and dependencies are reasonable, its acceptance criteria are useful,
and any required design plan or ADR exists. Leave it `Open` while product
choices, dependencies, evidence, planning, or other material questions remain
unresolved.

Use metadata to support those decisions:

- Priority runs from `P0` for urgent blockers to `P4` for deferred work.
- Effort `XS` and `S` are localized, `M` is moderate, and `L` or `XL` requires
  a design plan before implementation.
- Every task should have stable `type:*` and `area:*` labels. Add `risk:*`
  labels only when they affect routing or verification.

## Work A Task

Follow this procedure for every implementation task, whether or not it has a
design plan:

1. Run `ahm task show <id>`. Confirm that the task still matches the repository,
   is ready to work, and has no incomplete dependencies. Leave the task `Open`
   while unresolved questions prevent clear scope or acceptance criteria.
2. Run `ahm task start <id>`. If the user explicitly asks to resume an existing
   `In Progress` task, continue it without restarting the lifecycle.
3. Before implementation, settle any required decision or planning work. See
   [ADR workflow](adrs.md) when the task introduces or changes a durable
   architectural decision, including persisted state, configuration, security
   boundaries, migrations, breaking behavior, or major dependencies. See
   [ExecPlan workflow](exec-plans.md) for `L` and `XL` tasks, and for smaller
   work that is cross-cutting or substantially uncertain.
4. Implement only the task's problem and acceptance scope. Preserve unrelated
   worktree changes, and do not commit unless the user explicitly asks.
5. Run the repository's routed verification commands. Record material results
   and complete the task's Acceptance Notes so the record explains how the
   outcome was verified.
6. Before task completion, assess documentation impact. Documentation is an
   ordinary project deliverable, not an ahm record type, so follow the
   project's own documentation guidance when durable user or contributor
   knowledge may have changed. Record the documents checked and updated, or
   the reason no update was needed, in the Acceptance Notes.
   Do not require documentation changes for every task.
7. Run `ahm task complete <id>` and provide the repository's required handoff.
   The `ahm task complete` command must run before any git commit that includes
   the task's implementation — committing an uncompleted task breaks the
   lifecycle contract.

## Change Or Close A Task

Use commands for lifecycle and queue metadata whenever one exists:

```bash
ahm task accept <id>
ahm task start <id>
ahm task dep add <id> <dependency-id>
ahm task comment <id> <text>
ahm task complete <id>
ahm task cancel <id> --reason <text>
ahm task reopen <id>
```

Edit the task body directly when adding or revising durable working context.
Prefer commands for front matter and status changes so file placement and
indexes remain consistent.

Before completion, replace placeholder or unchecked Acceptance Notes with the
actual outcome and verification. `ahm task complete` warns about incomplete
acceptance notes. Set `"strict_acceptance": true` in `.ahm/config.json` to block
completion unless the issue is fixed or `--force` is explicitly used. The
command moves the task, updates eligible dependents, and regenerates indexes.

Cancellation requires a reason. `ahm task cancel` records it, moves the task,
and regenerates indexes.

## Storage And Manual Fallback

Task source records live under `.ahm/tasks/`: active records under
`.ahm/tasks/active/`, completed records under `.ahm/tasks/completed/`, and
cancelled records under `.ahm/tasks/cancelled/`. Task ids and filenames remain
stable across lifecycle moves.

`.ahm/tasks/index.md` and its linked indexes are generated, read-only views.
Never edit them directly. Normal `ahm task ...` mutations regenerate indexes
automatically. Run `ahm index` only after manually changing task metadata,
location, or linkage; body-only edits do not require it. Preview an index
regeneration with `ahm --dry-run index`.

If `ahm` is unavailable, inspect the task source files and generated index as a
fallback. Avoid manual creation or lifecycle moves when possible. If a manual
metadata or location change is unavoidable, preserve the task id and filename,
keep active, completed, and cancelled records in their matching buckets, and
run `ahm index` once the CLI is available again.
