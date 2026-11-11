---
status: accepted
date: 2026-11-10
decision-makers: Travis Ennis
---
# Restore prompt suggestions after history navigation

## Context and Problem Statement

ADR 036 lists history recall with the actions that explicitly discard a prompt
suggestion. That makes a round trip through prompt history destroy the
suggestion even when the composer returns to the same empty draft. History
navigation is a temporary composer state like typing, not a new draft or
session target.

## Decision Drivers

- Up/down history recall is reversible: walking back past the newest entry
  restores the draft that was present before navigation started.
- The suggestion describes the pinned session's next prompt, not the historical
  entry the user is browsing.
- A temporary composer state should not spend the suggestion; only actions that
  express a different draft, run, or session target should discard it.
- The existing hide/restore machinery already preserves a shown ghost behind a
  draft and reveals it when the composer is empty again.

## Considered Options

- **Keep history recall as an explicit discard (ADR 036).** Rejected: returning
  to the original empty draft resurrects no suggestion even though the session
  target and next prompt are unchanged.
- **Preserve the ghost across history recall (chosen).** History navigation uses
  the same hide/restore mechanism as typing, with filled-text provenance still
  dropped. The ghost stays behind the recalled text and reappears when the
  stashed draft is restored to an empty composer.
- **Restore only when the stashed draft is empty and no edit occurred.**
  Rejected: it needs extra navigation state and produces a different rule for
  `Ctrl+U` after recall than for `Ctrl+U` after typing.

## Decision Outcome

Chosen option: preserve the ghost across history recall, because history
navigation is a temporary view of the same draft.

- **History navigation.** A successful `Up` or `Down` history step hides the
  shown ghost instead of clearing it. The recalled text remains the only
  composer value; the ghost is never appended to it. Moving back past the
  newest entry restores the stashed draft, and an empty draft reveals the same
  ghost and placeholder.
- **Cancellation.** History navigation still cancels an in-flight suggestion
  request, matching typing. There is no result yet to preserve, and a late
  result must not overwrite the recalled text.
- **Held results.** A held result is kept across navigation. Unlike a shown
  ghost, it is still dropped when compaction settles if the composer is not
  empty, so a recalled draft wins.
- **Filled-text provenance.** A fill is composer text, not a hint. The first
  history step drops the fill provenance even though the ghost survives, so an
  unedited model-produced `/command` that is recalled again is parsed normally
  instead of regaining the submit bypass. This preserves ADR 036's safety rule.
- **Restored clears.** `Ctrl+U` or backspacing a recalled entry to an empty
  composer reveals the ghost, exactly as clearing a typed draft does.
- **Explicit discard.** Submit, tab completion, a new session, `/new`,
  `/resume`, `/sessions`, quit, and an unobserved exit continue to discard the
  ghost, held result, and filled-text provenance and cancel the request.
  Submitting a recalled entry therefore still spends the suggestion.
- **Presentation.** A recalled draft shows the ordinary submit hint; an empty
  composer after the round trip shows the suggestion hint again.

### Consequences

- Good, because a user can browse history without losing the suggestion they
  were considering.
- Good, because the ghost now follows the same hide/restore rule as typing and
  `Ctrl+U`, so there is one rule for temporary composer state.
- Bad, because clearing a recalled entry with `Ctrl+U` also reveals a ghost the
  user may have mentally dismissed when they started browsing.
- Neutral, because only an already-shown ghost or an already-held result can
  return; an in-flight request is canceled at the first history keystroke and
  no new request starts.

## More Information

- Partially supersedes ADR 031's held-result discard clause for history recall:
  a held result now survives navigation and is subject to the same
  empty-composer check when compaction settles.
- Partially supersedes ADR 036's **Explicit discard** clause for history recall
  only: the ghost now survives the round trip, but filled-text provenance is
  still discarded. The other discard actions, fill behavior, and provenance
  rules are unchanged.
- `internal/app/update.go` owns history key routing; `internal/app/suggestion.go`
  owns hide, restore, and clear.
