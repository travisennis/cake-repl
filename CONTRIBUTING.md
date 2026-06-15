# Contributing

This is the contributor reference for `cake-repl`: setup, the command catalog,
code style, verification expectations, and the commit/PR/release workflow. For
the high-level map see [`ARCHITECTURE.md`](ARCHITECTURE.md); for agent routing
see [`AGENTS.md`](AGENTS.md).

## Requirements

- Go `1.26.3` or newer (the module targets `go 1.26`).
- [`just`](https://github.com/casey/just) for the task runner (optional; the
  underlying `go` commands work too).
- A `cake` binary on `PATH` only to use the app — the test suite does **not**
  need one (runner tests use fake shell scripts).

Install the pinned lint/vuln/release tools once:

```bash
just install-tools   # golangci-lint, govulncheck, goreleaser (versions pinned in justfile)
```

## Command catalog

Prefer `justfile` targets:

```bash
just build          # go build -trimpath -o bin/cake-repl ./cmd/cake-repl
just run *args      # go run ./cmd/cake-repl
just install        # go install -trimpath ./cmd/cake-repl
just test           # go test ./...
just test-race      # go test -race -cover ./...
just quick          # go test ./... && go vet ./...
just fmt            # go fmt ./...
just fmt-check      # fail if gofmt would change files
just tidy           # go mod tidy
just tidy-check     # fail if go mod tidy would change files
just update-deps    # go get -u ./... && go mod tidy
just vet            # go vet ./...
just lint           # golangci-lint run
just vuln           # govulncheck ./...
just release-check  # goreleaser check + snapshot build
just fix            # tidy + fmt
just ci             # full gate: fmt-check tidy-check vet test-race lint vuln build release-check
```

Without `just`, run the underlying `go` commands shown above.

## Code style

- Standard Go style: `gofmt` tabs, short package-local names, explicit error
  handling. `golangci-lint` config lives in `.golangci.yml`.
- Respect package boundaries (see [`ARCHITECTURE.md`](ARCHITECTURE.md)): app
  behavior in `internal/app`, cake process/event concerns in `internal/cake`,
  terminal rendering in `internal/ui`. The dependency direction is one-way.
- Avoid speculative abstractions; keep `internal/ui` side-effect-free.
- Name tests after behavior, e.g. `TestParseLine_…`, `TestRunner…`.
- Keep command and flag names aligned with `README.md`, `HelpText`, and the
  cake CLI contract.

## Verification expectations

- Focused change: run `just test` (and `just vet`/`just lint` if relevant).
- Touching the subprocess runner or anything concurrent: run `just test-race`.
- Broader change before handoff: run `just ci`.
- Add focused tests next to changed code, especially for stream-json parsing,
  session transitions, command parsing, and UI formatting.

Details on the test layout and the fake-cake harness live in
[`docs/guardrails/testing-and-verification.md`](docs/guardrails/testing-and-verification.md).

## Commit & PR workflow

- Use Conventional Commit prefixes: `feat`, `fix`, `docs`, `style`, `refactor`,
  `perf`, `test`, `build`, `ci`, `chore`, `revert`. PR titles are checked by the
  `semantic-pr` workflow, and `conventional-pre-commit` checks commit messages.

  ```text
  feat: add session status command
  fix: handle malformed stream-json lines
  docs: update install instructions
  ```

- PRs should include a short problem/solution summary, the verification
  commands you ran, and a terminal capture when UI output changes. Keep PRs
  scoped; do not mix refactors with behavior changes unless required.
- A local `pre-commit` config is provided (`.pre-commit-config.yaml`): it runs
  `fmt-check`, `tidy-check`, `test`, and `lint`.

## Release

- Releases are cut by pushing a `v*` tag; the `Release` workflow runs `just ci`
  then `goreleaser release --clean`.
- Validate release config locally with `just release-check` before tagging.
- Tool versions (golangci-lint, govulncheck, goreleaser) are pinned in the
  `justfile`; bump them deliberately and re-run `just ci`. See
  [`docs/guardrails/dependencies-build-ci-release.md`](docs/guardrails/dependencies-build-ci-release.md).
