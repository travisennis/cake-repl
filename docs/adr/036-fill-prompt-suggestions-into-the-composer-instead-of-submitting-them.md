---
status: accepted
date: 2026-11-07
decision-makers: Travis Ennis
---
# Fill prompt suggestions into the composer instead of submitting them

## Context and Problem Statement

ADR 028 represents a prompt suggestion as dim placeholder ghost text and makes
`Ctrl+S` submit that text immediately as an ordinary prompt. The model output is
never real input, so the user cannot correct one word, add a constraint, or
inspect the prompt before spending a turn. A near-miss suggestion is therefore
all-or-nothing: accept it whole or type a replacement from scratch. Typing also
calls `clearSuggestion`, so the ghost disappears permanently the moment the user
starts composing.

The product goal is the same suggestion, but treated as editable draft text.
Acceptance should move the text into the composer; submission should remain a
separate, explicit action. A draft or history action must still beat a late
suggestion result.

## Decision Drivers

- A suggestion is a starting point, not a command. The user should be able to
  edit it before spending a model turn.
- `Ctrl+S` is the REPL's submit binding. A suggestion must not turn one
  keystroke into an unintended submission.
- Model output must never become a slash command merely because it was accepted.
- Typing must not leave a suggestion appended to user text, and a late result
  must not overwrite a draft.
- The ghost is presentation state; restoring it when the composer returns to
  empty costs nothing and preserves the suggestion the user was considering.

## Considered Options

- **Keep `Ctrl+S` submit-on-accept (ADR 028).** Rejected: it cannot satisfy
  edit-before-submit and makes accidental submission easy.
- **Fill the composer with `Ctrl+S`; submit with a second `Ctrl+S` (chosen).**
  The existing acceptance key becomes a reversible edit, and the existing submit
  key remains the only way to spend a turn.
- **Fill with `Tab`, keep `Ctrl+S` submit-on-accept.** Rejected for this change:
  `Tab` already owns slash-command completion, and submit-on-accept still sends
  model text the user did not edit. The composer edit can later gain a `Tab`
  alias, but the safety property does not depend on it.
- **Prefill the textarea in ADR 028's original sense.** Still rejected: the
  original concern was indistinguishable model/user text and accidental submit.
  Explicit fill resolves the first by user action and this ADR resolves the
  second by requiring a second submit.

## Decision Outcome

Chosen option: `Ctrl+S` on a shown suggestion fills the composer and does not
start a run. A second `Ctrl+S` submits the filled text.

- **Fill.** With an empty composer and a shown suggestion, `Ctrl+S` copies the
  normalized ghost into the textarea, clears the ghost and its placeholder, and
  leaves the cursor in the composer. No timeline row, run, history entry, or
  model call is added by the fill.
- **Submit provenance.** The REPL records the exact text it filled. While the
  trimmed composer still equals that text, `Ctrl+S` submits it as an ordinary
  prompt with slash-command parsing bypassed. Editing the text clears the
  provenance implicitly because the comparison no longer matches; a later
  submit parses the user's text normally, including slash commands. Clearing the
  composer also drops the provenance.
- **Working key matching.** If a user fills `/help`, the first `Ctrl+S` only
  fills it and the second submits it as a prompt, never as a command. If the user
  edits it to `/session`, the next submit is a normal slash command because the
  text no longer matches what the suggester produced.
- **Hide and restore.** A composer edit hides the shown ghost instead of
  destroying it; the input value contains only the user's characters. When the
  composer becomes empty again, the same ghost and placeholder return. This
  includes ordinary backspace/delete and `Ctrl+U`, which clears the draft and
  reveals the saved ghost. Typing still cancels an in-flight request, because
  there is no result yet to restore and a late result must not overwrite a
  draft.
- **Explicit discard.** History recall, tab completion, submit, `Ctrl+N`, `/new`,
  `/resume`, `/sessions`, quit, and an unobserved exit continue to discard the
  ghost, held result, and filled-text provenance and cancel the request. A `Tab`
  press that produces no text change is the one exception to the provenance
  drop: it still dismisses the ghost and cancels the request, but an unedited
  filled `/command` must not become executable merely because Tab found no
  completion. These actions express a different draft or session target, not a
  temporary pause.
- **Presentation.** With an empty composer, the footer reads `Ctrl+S insert
  suggestion · type to hide`. With any draft, including filled suggestion text,
  the normal submit hint returns. The ghost remains dim placeholder text and is
  never the textarea value before the user accepts it.

### Consequences

- Good, because a suggestion becomes editable without retyping and without an
  accidental model turn.
- Good, because the slash-command safety boundary survives the extra keystroke.
- Good, because a suggestion survives a momentary draft or a cleared input.
- Bad, because accepting now takes two `Ctrl+S` presses to submit, and the
  second press is not visually distinguishable from an ordinary submit.
- Neutral, because the ghost is still not editable in place before acceptance;
  the user must fill it first.

## More Information

- Partially superseded by
  [ADR 038](038-restore-prompt-suggestions-after-history-navigation.md) for
  history recall: that action now hides and restores the ghost and keeps a held
  result, while filled-text provenance is still dropped. The other discard
  actions are unchanged.
- Partially supersedes ADR 028's **Accept and dismiss** clause and its
  consequence about one-key acceptance. The external `cake-suggest` contract,
  cancellation, failure, provenance, and one-request rules are unchanged.
- The startup trigger is a separate decision in ADR 037.
- `internal/app/suggestion.go` owns fill, hide, restore, and provenance;
  `internal/app/update.go` owns key routing.
