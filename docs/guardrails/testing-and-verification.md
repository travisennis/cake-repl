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
- **Stream fixtures live in `internal/cake/testdata/fixtures/*.ndjson`** — one
  scenario per file (happy path, error task, minimal, unknown type, malformed
  midstream), with fixed UUIDs and timestamps so output stays reproducible.
  Add a fixture instead of a new inline NDJSON blob when a scenario is worth
  sharing between the parser and runner tests.
- **Multi-line stream tests** in `parser_integration_test.go` parse a fixture
  through `parseFixture` and assert the full typed event sequence, including
  forward-compatible `Unknown` records and synthetic `ParseError` recovery.
  `runner_pipeline_test.go` replays the same fixtures through a fake cake and
  asserts `Start()` delivers the same events — it is hermetic and part of the
  normal suite.
- **Benchmarks live beside the code they measure** (`*_bench_test.go`) and are
  as hermetic as the rest of the suite: no real `cake`, fixed terminal
  dimensions, deterministic payloads, and an explicitly set color profile so a
  missing TTY does not silently change what is measured. They never run from
  `just test`, `just test-race`, or `just ci` — only under `-bench`, via
  `just bench`.
- **Do not turn a single benchmark run into a design decision.** Measure each
  layer separately so a result is attributable, cover more than one input size,
  and repeat the run (`just bench -count=6`), summarizing with `benchstat`
  (optional developer tool: `go install golang.org/x/perf/cmd/benchstat@latest`;
  deliberately not pinned in `install-tools`, since CI does not use it). Record
  the numbers, the environment, and the exact commands in the task record —
  `internal/app/model_bench_test.go` and task 059 are the worked example.
- **The real-cake smoke test is opt-in twice over.** `TestSmokeRealCake` needs
  both the `integration` build tag and `CAKE_REAL_SMOKE=1` (`just
  test-real-cake`; `CAKE_BIN` overrides the binary). It must skip — never fail —
  when cake is absent, must stay out of `just test`, `just test-race`, and
  `just ci`, and must keep warning that it makes a model-backed cake request
  that can cost money.

## Verification ladder

- Focused change → `just test` (+ `just vet`/`just lint` if relevant).
- Concurrency / `internal/cake/runner.go` → `just test-race`.
- Performance change → `just bench` before and after, plus the normal ladder
  for the code itself.
- Broad change before handoff → `just ci`.
- Dependency or release-config change → also `just vuln` / `just release-check`.

State exactly which of these you ran in the handoff.

## Common failure modes

- Making a real `cake` binary a test dependency, or letting the opt-in smoke
  test fail (rather than skip) when cake is missing.
- Adding tests that race under `-race` (shared state across goroutines).
- Skipping `test-race` after touching the subprocess runner.
- Letting `fmt-check`/`tidy-check` fail because `go fmt`/`go mod tidy` weren't run.

## Related docs

- [`../../CONTRIBUTING.md`](../../CONTRIBUTING.md) — command catalog and style.
- [`cake-integration-and-stream-json.md`](cake-integration-and-stream-json.md) — parser/runner tests.
- [`session-and-security.md`](session-and-security.md) — state-machine tests.
