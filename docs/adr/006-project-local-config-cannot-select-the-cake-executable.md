---
status: accepted
date: 2026-08-01
decision-makers: Travis Ennis
---
# Project-local config cannot select the cake executable

## Context and Problem Statement

ADR 002 chose project-local `.cake-repl.toml` deliberately and named "a local
cake binary path" as a motivation for the layer. The same file can set
`cake-bin`, and that value becomes the executable `internal/cake/runner.go`
spawns — including a relative path resolved inside the checkout. Cloning an
untrusted repository and running `cake-repl` inside it therefore executes a
binary of the repository's choosing with no prompt, allowlist, or UI
indication. This is the same exposure class as direnv's `.envrc` and
`.vscode/settings.json` task definitions, which all gate it behind an explicit
trust step. ADR 002 analyzed session hijack only; it never weighed executable
selection.

## Decision Drivers

- A repo-controlled file must not silently select which program runs.
- The project-local layer should keep most of its value (`model`, `profile`,
  `output-limit`, `max-timeline-items`).
- The change should be small and testable; no trust-prompt machinery for a
  single-user local tool.

## Considered Options

1. **Accept and document.** Keep behavior; record the exposure in ADR 002's
   consequences and the README.
2. **Restrict the layer.** `cake-bin` allowed only from the XDG config,
   `-config`, and `-cake-bin`; ignored with a visible warning in the
   project-local file.
3. **Constrain the value.** Require an absolute path and refuse anything
   resolving inside the project directory.
4. **Trust prompt.** Record approved project-local config files by path and
   content hash; prompt on first use or on change.

## Decision Outcome

Chosen option: **2, restrict the layer.**

`cake-bin` may be set from the XDG config file, the `-config` file, or the
`-cake-bin` flag. A `cake-bin` key in project-local `.cake-repl.toml` is
ignored, and cake-repl prints a visible warning at startup naming the file and
key. All other project-local keys keep their project-local behavior.
Precedence for `cake-bin` becomes: hardcoded default < XDG/`-config` file <
`-cake-bin` flag.

This supersedes in part ADR 002's project-local layer: the "local cake binary
path" motivation no longer applies to that layer; `-cake-bin` still covers the
use case explicitly.

### Consequences

- Good, because no repo-controlled file can select the executable; the
  exposure class closes without a trust prompt.
- Good, because project-local config keeps the settings that carry most of its
  value.
- Neutral, because a project-local `cake-bin` is inert; the startup warning
  makes that visible.
- Bad, because ADR 002's stated motivation for the project-local layer is
  narrowed; per-project binaries now require `-cake-bin`.

## More Information

- Task 063.
- [ADR 002](002-config-file-for-repl-defaults.md).
- `internal/config/config.go`, `internal/cake/runner.go`.

