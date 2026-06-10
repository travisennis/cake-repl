# Repository Guidelines

## Project Structure & Module Organization

This is a Go CLI project for `cake-repl`, a terminal REPL frontend for `cake`.

- `cmd/cake-repl/main.go` contains the executable entry point.
- `internal/app/` owns Bubble Tea state, updates, commands, sessions, and keys.
- `internal/cake/` wraps the external `cake` process and parses stream-json NDJSON events.
- `internal/ui/` contains rendering helpers, timeline/status views, theme, and tool block formatting.
- Tests live beside implementation files as `*_test.go`.
- `README.md` documents user-facing behavior; `plan.md` holds planning context.

## Build, Test, and Development Commands

Prefer the `justfile` targets when available:

```bash
just build          # go build -trimpath -o bin/cake-repl ./cmd/cake-repl
just test           # go test ./...
just test-race      # go test -race -cover ./...
just fmt            # go fmt ./...
just fmt-check      # fail if gofmt would change files
just tidy-check     # fail if go mod tidy would change files
just vet            # go vet ./...
just lint           # golangci-lint run
just vuln           # govulncheck ./...
just release-check  # GoReleaser config check and snapshot build
just ci             # full local/CI gate
just install-tools  # install pinned lint, vuln, and release tools
just run            # go run ./cmd/cake-repl
just install        # go install -trimpath ./cmd/cake-repl
just release        # optimized local binary at ./cake-repl
```

Without `just`, run the underlying `go` commands directly. The project requires Go `1.26.3` or newer.

## Coding Style & Naming Conventions

Use standard Go style: tabs from `gofmt`, short package-local names, and explicit error handling. Keep package boundaries clear: app behavior in `internal/app`, cake process/event concerns in `internal/cake`, and terminal rendering in `internal/ui`. Avoid speculative abstractions.

Name tests after behavior, for example `TestParseEvent...` or `TestRunner...`. Keep command and flag names aligned with README examples and the `cake` CLI contract.

## Testing Guidelines

Run `just test` for focused changes and `just ci` before handing off broader changes. Tests use Go's standard `testing` package. Runner tests use fake shell scripts, so they should not require a real `cake` binary. Add focused tests next to changed code, especially for parser behavior, session transitions, command parsing, and UI formatting.

For broader changes, also run:

```bash
just ci
```

## Commit & Pull Request Guidelines

Recent history uses concise Conventional Commit-style prefixes such as `feat:` and `docs:`. Continue that pattern:

```text
feat: add session status command
fix: handle malformed stream-json lines
docs: update install instructions
```

Pull requests should include a short problem/solution summary, verification commands, and screenshots or terminal captures when UI output changes. Link related issues when applicable. Keep PRs scoped; do not mix refactors with behavior changes unless required.

## Security & Configuration Tips

Do not depend on `cake` internals or session files. The integration contract is the `cake --output-format stream-json` NDJSON stream plus `--continue` and `--resume` flags. Avoid logging secrets in `--debug-log`; raw stream lines may include prompt or tool output.
