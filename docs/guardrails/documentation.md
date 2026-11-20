# Guardrail: documentation

**Scope.** Read before changing user-facing docs, architecture docs, or agent
routing. Also use this guardrail when behavior, configuration, compatibility, or
workflow changes require documentation updates.

## Compatibility surfaces

- **User reference.** `README.md` is the user-facing source for install, run,
  flags, key bindings, slash commands, config files, and known limitations. The
  canonical development-command catalog is owned by `CONTRIBUTING.md`;
  `README.md` retains basic build and install examples.
- **Runtime help.** `internal/app/commands.go` `HelpText` mirrors slash
  commands and key bindings shown in `README.md`.
- **Architecture and guardrails.** `ARCHITECTURE.md` documents stable module
  boundaries and invariants. `docs/guardrails/` contains workflow-specific
  agent rules; move detailed rules there instead of growing `AGENTS.md`.

## Required checks

- Docs-only changes: inspect rendered Markdown where practical and run
  `gofmt`-equivalent checks only when Go files changed.
- Docs that change CLI flags, config shape, commands, key bindings, or output:
  cross-check `README.md`, `HelpText`, `AGENTS.md`, and the relevant guardrail.
- Code plus docs: use the verification route for the code change; `just ci` is
  the broad gate.

## Documentation impact matrix

Use this matrix when a change touches a durable surface.

| Changed surface                                 | Required docs                                                                                                                      |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| CLI flags or startup behavior                   | `README.md`, `docs/guardrails/cli-and-user-output.md`, `AGENTS.md` compatibility surface when the category changes                 |
| Slash commands or key bindings                  | `README.md`, `internal/app/commands.go` `HelpText`, `docs/guardrails/cli-and-user-output.md`                                       |
| Config file shape, paths, or precedence         | `README.md`, `docs/guardrails/cli-and-user-output.md`, `ARCHITECTURE.md` if architectural                                          |
| Persisted state or files on disk                | `README.md` when user-visible, relevant guardrail, `ARCHITECTURE.md` when it introduces a new storage boundary                     |
| Session behavior                                | `README.md`, `docs/guardrails/session-and-security.md`                                                                             |
| cake invocation contract or stream-json contract | `ARCHITECTURE.md`, `docs/guardrails/cake-integration-and-stream-json.md`                                                          |
| Security, logging, secrets, or subprocess lifecycle | `docs/guardrails/session-and-security.md`, `README.md` when user-visible                                                        |
| Build, dependency, CI, or release behavior      | `CONTRIBUTING.md` (canonical command catalog), `docs/guardrails/dependencies-build-ci-release.md`                                  |
| Architecture/module boundaries                  | `ARCHITECTURE.md`, relevant guardrail                                                                                              |
| User-visible terminal output                    | `README.md` when behavior changes, `docs/guardrails/cli-and-user-output.md`, screenshots or manual capture when required by that guardrail |
| Any other durable contract not listed above     | The most relevant existing row's doc requirements                                                                                  |

State durable decisions in the code, the guardrail, or `ARCHITECTURE.md` where
they belong; do not create separate decision records.

## Durable-surface checklist

Before committing or handing off, explicitly check whether the change affects
any durable surface:

- CLI flags or startup behavior.
- Config file shape, precedence, or paths.
- Slash commands or key bindings.
- Persisted state or files on disk.
- Subprocess/cake contract.
- Security, logging, secrets, or session behavior.
- Architecture/module boundaries.
- User-visible terminal output.

## Preflight requirements for high-impact areas

For `area:config`, persisted behavior, security/session, or CLI changes, the
`preflight` skill's first pass must explicitly answer:

- Which user docs changed?
- Which guardrails changed?

## Common failure modes

- Documenting flags in `README.md` but leaving `docs/guardrails/` or
  `AGENTS.md` stale.
- Adding a feature that changes persisted state, config shape, security
  posture, or compatibility behavior without updating a guardrail.
- Letting runtime `/help` drift from README slash commands and key bindings.
- Treating `README.md` as enough for a durable behavior change that also needs
  a guardrail or architecture update.

## Related docs

- [`../../README.md`](../../README.md) - user-facing reference.
- [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md) - module map and invariants.
- [`../../CONTRIBUTING.md`](../../CONTRIBUTING.md) - command catalog and checks.
- [`cli-and-user-output.md`](cli-and-user-output.md) - flags, commands,
  key bindings, and output.
