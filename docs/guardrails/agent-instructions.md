# Guardrail: agent-facing instructions

**Scope.** Read before adding, removing, or reordering instructions in
[`../../AGENTS.md`](../../AGENTS.md), the skills under `.agents/skills/`, or any
other prose whose purpose is to change how an agent behaves.

This guardrail governs the evidence an instruction change requires. It does not
govern user documentation, external contracts, or architecture, which are
covered by [documentation](documentation.md),
[cake integration and stream-json](cake-integration-and-stream-json.md), and
[`../../CONTRIBUTING.md`](../../CONTRIBUTING.md).

## What AGENTS.md is for

`AGENTS.md` is a map, not a manual. It carries four things:

- what the project is and which compatibility surfaces matter;
- the operating loop expected for all work;
- a small set of task classes; and
- links to the documents, commands, and proof appropriate to each class.

Anything else belongs beside the work it governs. Every global instruction
spends attention and narrows the agent's choices, so a rule that applies to one
route belongs in that route or in the guardrail the route names.

## Rules

- Name a compatibility surface; do not restate its value. The MSRV, pinned tool
  versions, and the debug-log file mode belong to `go.mod`, the `justfile`, and
  [session and security](session-and-security.md). The map says the category
  exists so the agent knows where to look.
- Give every routed link a reason: `[Doc](path), for <what it settles>`. An
  unannotated list of links forces the agent to open all of them or guess.
- Route on the decision, not the directory. Open each task class with the
  concrete nouns that trigger it, so classification survives files moving.
- Keep procedure out of the map. A skill that owns a procedure should be
  invoked, not summarized in the operating loop.
- Do not restate a rule that another document already owns. Link to the owner.

## Required evidence

A consistency edit — one that fixes drift, links, or duplication against code or
existing documentation — needs only the normal documentation checks.

A behavior-shaping edit — one that adds, removes, or reorders what an agent
should do — additionally requires:

- Name the observed failure motivating the edit, citing a session, commit, or
  `ahm` record, in the managed task or the commit message.
- State the observable behavior the edit is expected to change.
- Verify with the narrowest fresh probe that exercises the instruction: run a
  representative task in a fresh agent session and check that the instruction
  was retrieved and followed.
- When a probe is not run, record that verification is deferred and which probe
  would establish it.

A green consistency check shows that the documents agree with each other; it
does not show that an agent behaves differently. An instruction no trajectory
ever used has no evidence of effect.

## Provenance and retirement

Motivating failure: a 2026-07-27 review of `AGENTS.md` across four sibling
projects found instructions accumulating without evidence of effect. In this
repository, the compatibility-surface list restated the Go MSRV and the
debug-log file mode — values owned by `go.mod` and
[session and security](session-and-security.md) — and two operating-loop steps
described review and subagent policy already owned by the
[`preflight`](../../.agents/skills/preflight/SKILL.md) skill. Routed links
carried no retrieval reason.

Verification is deferred. The probe that would establish effect: in a fresh
session, give a task that touches one routed surface and check whether the agent
opened only that route's documents rather than all of them or none.

Retire this guardrail when deferred probes accumulate without ever being run,
which would show the requirement is producing paperwork rather than evidence, or
when a mechanical check can establish the same thing. Removing it for document
volume alone is not a reason; record the evidence that it stopped working.
