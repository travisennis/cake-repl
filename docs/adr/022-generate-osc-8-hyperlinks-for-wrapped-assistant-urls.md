---
status: accepted
date: 2026-10-20
decision-makers: Travis Ennis
---
# Generate OSC 8 hyperlinks for wrapped assistant URLs

## Context and Problem Statement

Assistant markdown is rendered by glamour with a word wrap equal to the
timeline body width. Glamour hard-wraps a long URL at the wrap column: the
rendered line ends mid-URL (often at a hyphen or slash) followed by padding
spaces and a newline, and the remainder continues on the next line. A terminal
detects links per line, so a URL split this way is only clickable as the
fragment on the line under the pointer; the rest of the URL is lost to the
opener. Hard newlines are not terminal soft wraps, so the terminal cannot join
them. This affects GitHub and file links in ordinary assistant responses.

The repository already has a hard rule that untrusted stream content can never
write OSC 8 to the terminal (ADR 005) and that the only escape family that
survives rendering is reviewed SGR (ADR 009). Any fix that makes split URLs
clickable must not reopen the forgery hole those decisions closed: a message
must not be able to attach a link target to unrelated visible text.

## Decision Drivers

- A wrapped URL must be openable by clicking any of its visible fragments, not
  only the fragment on one line.
- The generated link's destination must be the full URL whose visible
  characters, or a line-split fragment of them, the link covers, so no link can
  be forged over trusted-looking text.
- Stream bytes still may not determine escape sequences: the decode scrub must
  remain the last thing applied to untrusted rendered output.
- `-no-color` / the Ascii profile must keep emitting no escape bytes at all.
- No width or frame-alignment change: OSC 8 is zero-width to the ansi width
  measurement already in use.

## Considered Options

- **Stop wrapping long URLs.** Rejected: Bubble Tea truncates rendered lines
  that exceed the terminal width, so the URL would be visibly cut instead of
  soft-wrapped, and letting the terminal soft-wrap would desynchronize the
  renderer's frame math.
- **Shorten the displayed URL.** Rejected: it hides the destination the user
  needs to read and still needs a clickable target.
- **Generate OSC 8 hyperlinks around the rendered URL text (chosen).**
  Terminals with OSC 8 support open the destination from any cell of the link,
  including each hard-wrapped segment, and link previews show the full target.

## Decision Outcome

`ui.RenderMarkdown` post-processes the already-scrubbed glamour output under a
color profile and wraps each rendered `http://` or `https://` URL in OSC 8
hyperlinks. The destination is the full visible URL, reconstructed across
glamour's hard line breaks; each line segment is wrapped separately with the
same destination so every fragment is clickable. URLs are found in the rendered
text, not in the markdown source: text following a line break is joined only
when the joined string still occurs in the sanitized source, and the finished
URL must occur there as a complete URL rather than as the prefix of a longer
token. Those guards keep prose from being swallowed into a URL and keep a
hard-wrapped fragment from becoming a truncated target. The source, not the
rendered layout, also decides which run on a continuation line is the URL, so a
blockquote's decorative line prefix does not hide a wrapped fragment. Trailing
sentence punctuation and unbalanced closing brackets stay outside the link.

This is a narrow, generated exception to ADR 005/009, not a passthrough:

- The special SGR scrub runs first and strips every OSC 8 that decoded from
  the stream; generated links are added afterward, in `internal/ui`, from
  inspected `http(s)` text only.
- Link text is always the URL's own rendered characters, never a markdown link
  label such as `[click here](...)`, so display and destination cannot diverge.
- Only assistant markdown is affected. Tool output, errors, warnings, hooks,
  reasoning, and the status line get no generated links and keep only their
  usual theme SGR (plus the opt-in tool color).
- The Ascii profile (`-no-color`) skips link generation and remains byte-plain.
- Non-HTTP(S) schemes are not linked, and URLs are bounded and rejected if
  they contain control bytes. Only ASCII URL text is linked: a non-ASCII rune
  after the URL ends it when it is punctuation or space, and keeps the whole URL
  unlinked otherwise, so an internationalized URL stays plain text instead of
  producing a truncated target.

### Consequences

- Good, because a wrapped GitHub or documentation URL opens in full from any
  visible fragment.
- Good, because the untrusted-content rule is unchanged: stream bytes never
  become OSC 8, and the scrubber still runs before link generation.
- Good, because the terminal's link preview shows the full destination even
  though the visible URL is split.
- Neutral, because OSC 8 support and the modifier that opens a link are
  terminal features; terminals without OSC 8 show the same text as before.
- Neutral, because links are only generated for assistant markdown; a URL in
  tool output remains plain text.
- Bad, because `-no-color` output stays unclickable, matching its existing
  no-escape contract.
- Bad, because only ASCII URL text is linked; an internationalized URL renders
  as plain text.
- Bad, because a URL broken across lines exposes fragments whose visible text
  is not itself a valid URL; the link preview is the way to see the target on
  one line.
- Bad, because link reconstruction adds a scanning pass over rendered
  assistant output and one more security-sensitive render path to test.

## More Information

- [ADR 005](005-untrusted-stream-content-is-sanitized-at-the-ui-render-boundary.md):
  the sanitize-at-render-boundary decision this change carves out of.
- [ADR 009](009-opt-in-sgr-passthrough-for-tool-output.md): the reviewed-SGR
  policy the generated sequences are added after.
- [`../guardrails/cli-and-user-output.md`](../guardrails/cli-and-user-output.md)
  and
  [`../guardrails/session-and-security.md`](../guardrails/session-and-security.md):
  the output and terminal-injection surfaces this decision moves.
- Implementation: `internal/ui/markdown.go`, `internal/ui/hyperlink.go`, and
  their tests.
