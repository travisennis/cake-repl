# Guardrail: testing and verification

**Scope.** Read when adding or changing tests, or deciding what to run before
handoff. Command definitions live in [`../../CONTRIBUTING.md`](../../CONTRIBUTING.md);
this guardrail covers the project's test conventions and the verification ladder.

## Compatibility surfaces

- The `just ci` gate is the contract for "broadly verified": `fmt-check`,
  `tidy-check`, `vet`, `test-race`, `lint`, `vuln`, `build`, `release-check`.
  CI runs exactly this. Keep changes green against it.

## Test conventions

- Tests live beside implementation as `*_test.go`; use Go's standard `testing`
  package; name tests after behavior.
- **Runner tests use fake `cake` shell scripts** (successful streams, malformed
  lines, non-zero exits, cancellation). They must not require a real `cake`
  binary — keep it that way so the suite stays hermetic.
- Put focused tests next to changed code, especially for: stream-json parsing,
  session run-mode transitions, slash-command parsing, and UI formatting.

## Verification ladder

- Focused change → `just test` (+ `just vet`/`just lint` if relevant).
- Concurrency / `internal/cake/runner.go` → `just test-race`.
- Broad change before handoff → `just ci`.
- Dependency or release-config change → also `just vuln` / `just release-check`.

State exactly which of these you ran in the handoff.

## Common failure modes

- Making a real `cake` binary a test dependency.
- Adding tests that race under `-race` (shared state across goroutines).
- Skipping `test-race` after touching the subprocess runner.
- Letting `fmt-check`/`tidy-check` fail because `go fmt`/`go mod tidy` weren't run.

## Related docs

- [`../../CONTRIBUTING.md`](../../CONTRIBUTING.md) — command catalog and style.
- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — parser/runner tests.
- [`session-and-security.md`](session-and-security.md) — state-machine tests.
