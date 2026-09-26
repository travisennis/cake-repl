---
status: accepted
date: 2026-09-26
decision-makers: Travis Ennis
---
# Store ahm task records in the user-level home store

## Context and Problem Statement

Task records lived in `.ahm/tasks/` inside this repository, so all 84 records and
their lifecycle history were committed alongside the product. That couples managed
work to the code: every clone carries a copy of another machine's planning state,
the backlog cannot be carried or shared across machines without a git fetch, and
planning churn competes with product history in the same log.

The installed `ahm` provides `ahm store migrate --to home`, which relocates
records to a user-level store under `~/.ahm`, keyed by project identity, and
leaves the project holding only a committed switch. The migration was run on
2026-09-26: 84 records moved to
`~/.ahm/projects/cake-repl-99832c4e/tasks/`, `.ahm/config.json` gained
`"tasks_location": "home"`, and the managed `.ahm/.gitignore` was narrowed.
ADR records were deliberately left under `docs/adr/`.

Moving the records has two consequences that need to be stated rather than
discovered. The home store is not a git repository, so task records leave version
control entirely. And the committed `tasks_location` key makes a fresh checkout on
a machine that has no store entry fail validation until `ahm init` runs.

## Decision Drivers

- Keep managed work out of the repository so a clone does not ship another
  machine's planning state.
- Keep task ids, filenames, and lifecycle transitions stable so existing
  references stay meaningful.
- Keep ADR records in the repository, because decision history is a durable,
  reviewable project artifact rather than personal planning state.
- Make the new-checkout path explicit, so a fresh clone does not silently present
  a failing or empty backlog.
- Keep the change reversible without rewriting records.

## Considered Options

### Keep task records in the repository

Rejected because every clone ships a copy of the backlog, and carrying planning
state between machines depends on git fetch rather than on the tool that owns the
records. The versioning benefit is real but narrow: it is the only property lost
by the chosen option, and `ahm` can restore it on demand.

### Move ADR records to the home store as well

Rejected because ADRs are reviewed in pull requests, indexed in
`docs/adr/index.md`, and are meant to be read by contributors who never run
`ahm`. `ahm` keeps the ADR index under the project root for the same reason.

### Store task records in the user-level home store

Chosen. Records live outside the repository under a key derived from the git
remote, the project keeps a committed switch, and ADR history stays in the repo.

## Decision Outcome

- Task records live in `~/.ahm/projects/cake-repl-99832c4e/tasks/`, in
  `active/`, `completed/`, and `cancelled/` buckets. The store key is derived from
  the git remote `github.com/travisennis/cake-repl`, not from the filesystem
  path, so a clone at any path resolves to the same records.
- `.ahm/config.json` keeps `"tasks_location": "home"` committed, and the managed
  `.ahm/.gitignore` continues to exclude machine-local state. The 84 records are
  removed from the repository.
- ADR records stay under `docs/adr/`, with the ADR index in the project root.
- A checkout with no store entry must run `ahm init` once before `ahm` is usable.
  Until then `ahm` exits 1 with `store_dir_unreadable` and
  `generated_index_missing`.
- Documentation refers to tasks by id, not by repository path. A relative
  Markdown link to a record outside the checkout cannot resolve, so the ADRs
  reference task ids as text.

### Consequences

- Good, because a clone no longer ships planning records, and the backlog follows
  the user across machines.
- Good, because planning churn no longer shares the product's commit history.
- Bad, because the home store is not a git repository, so task records have no
  versioned history after the move. The pre-migration content remains recoverable
  from this repository's history up to the commit that deletes the records.
- Bad, because a fresh clone starts with an empty backlog and a failing `ahm`
  until `ahm init` runs. This is now documented in the task workflow.
- Bad, because the store key derives from the git remote, so a checkout without
  `origin` — a source tarball, or a fork whose remote has been re-pointed —
  resolves to a different key and presents an empty backlog rather than an error.
- Neutral, because `ahm` refuses to migrate records that have uncommitted changes,
  since git history would then be the only record of them; `--force` overrides.
- Neutral, because the migration is reversible with
  `ahm store migrate --to project`, which returns the records to `.ahm/tasks/`
  and commits them again from that point on. It does not restore their earlier
  position in this repository's history.

## More Information

- Reversal: `ahm store migrate --to project`.
- Task workflow, including storage location and the fresh-checkout step:
  [`docs/workflow/tasks.md`](../workflow/tasks.md).
- Verify with `ahm store path` for the resolved store location and
  `ahm status` for validation state.
