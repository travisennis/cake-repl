# Guardrail: dependencies, build, CI, and release

**Scope.** Read before changing `go.mod`/`go.sum`, the `justfile`, GitHub
workflows (`.github/workflows/`), `.goreleaser.yaml`, `.golangci.yml`, or
`.pre-commit-config.yaml`.

## Compatibility surfaces

- **Go version (MSRV).** Module targets `go 1.26.3`; docs state Go `1.26.3`+.
  Keep `go.mod`, `README.md`, and CI (`go-version-file: go.mod`) consistent if
  you change it. Raising it is a user-visible decision.
- **Pinned tool versions.** `golangci-lint`, `govulncheck`, and `goreleaser`
  versions are pinned in the `justfile` and referenced by the workflows. Bump
  them together with the workflow `version:` fields, never one side only.
  `benchstat` is deliberately outside this set: it is an optional developer
  tool for reading `just bench` output, no CI job uses it, and `install-tools`
  does not install it.
- **Dependency surface.** The runtime stack is the charmbracelet ecosystem
  (bubbletea, bubbles, lipgloss) plus termenv. Prefer the standard library; add
  dependencies deliberately and keep `go mod tidy` clean.
- **Release artifacts.** `.goreleaser.yaml` defines build targets; releases are
  cut by pushing a `v*` tag. The binary version is injected via ldflags into
  `internal/version`.

## Required checks / test focus

- After any dependency change: `just tidy-check` and `just vuln`.
- After workflow/release-config change: `just release-check` (goreleaser check +
  snapshot build).
- Always finish with `just ci` — it is the same gate CI runs.

## Common failure modes

- Bumping a tool in the `justfile` but not the workflow `version:` (or vice
  versa), so local and CI disagree.
- Leaving `go.mod`/`go.sum` untidy (`tidy-check` fails in CI).
- Introducing a dependency flagged by `govulncheck`.
- Changing the Go version in one place only.

## Related docs

- [`../../CONTRIBUTING.md`](../../CONTRIBUTING.md) — command catalog, release steps.
- [`testing-and-verification.md`](testing-and-verification.md) — the `just ci` gate.
