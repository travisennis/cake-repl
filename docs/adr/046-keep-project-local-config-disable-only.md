---
status: accepted
date: 2026-11-20
---
# Keep project-local config disable-only

## Context and Problem Statement

`cake-repl` loads an XDG config file and an automatic project-local
`.cake-repl.toml`. The project file comes from the repository, not from the
user. It must not be able to select a different executable, enable a metered
operation, or tune a metered model call. It may disable features the trusted
layer enabled.

## Decision

- Merge order is hardcoded defaults, then config file, then CLI flags.
- `ToolColor`, `Compaction.Enabled`, and `Suggestions.Enabled` are booleans whose
  `false` value is meaningful. They merge on TOML key presence, not on a
  non-zero value, so an explicit `false` can disable an inherited `true`.
- The project-local layer may only disable compaction or suggestions. It cannot
  enable them, select `cake-compact` or `cake-suggest`, tune a trigger, or choose
  a summarizer or suggestion model, profile, or reasoning effort.
- Presence checks use `definedFold`, which matches TOML key paths
  case-insensitively like the decoder. A case-sensitive presence check could let
  a differently spelled key decode while its provenance check missed it.
- Every dropped project key is reported in a stable order so the startup warning
  explains why it was ignored. The warning never echoes the configured value.
- A missing file is an empty config, not an error. A file that exists but cannot
  be decoded aborts startup.
- The trust probe is advisory. It decodes only cake's versioned diagnostic
  document, never writes a trust record, and is silent on any failure apart from
  the debug log.

## Consequences

- Good: a repository cannot select or pay for a metered operation.
- Good: the trusted layer can still disable anything.
- Good: a misspelled key in the untrusted layer cannot survive provenance
  stripping.
- Bad: a project cannot opt itself into a feature it needs; the user must set
  the trusted config.

## More Information

- [`internal/config`](../../internal/config)
- [`cmd/cake-repl/main.go`](../../cmd/cake-repl/main.go)
