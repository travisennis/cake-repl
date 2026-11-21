---
status: accepted
date: 2026-11-20
---
# Persist prompt history as bounded JSONL

## Context and Problem Statement

Prompt history supports up/down recall across REPL runs. It must survive prompts
that contain newlines, be readable by an older plain-text file, and never
discard the whole history because one record is malformed or oversized. It must
also not echo prompt text in warnings.

## Decision

- Each new entry is one line: `\x02` + JSON-encoded prompt + `\n`. The prefix
  unambiguously marks JSONL records so the loader can tell them from legacy
  plain-text lines.
- The reader accepts both forms. A line starting with `\x02` is decoded as JSON;
  every other line is a legacy plain-text entry kept verbatim. A blank line is
  skipped.
- `maxHistoryRecordBytes` bounds one record including the prefix, encoded
  prompt, and newline. The reader buffer is fixed at that bound, so one record
  always fits.
- An oversized record is drained in bounded chunks, counted, and skipped; the
  records before and after it remain usable. A mid-file read error returns the
  entries read so far plus the error, so the caller can keep them without
  rewriting the file.
- `maxHistoryEntries` trims on load. Surviving entries are rewritten with the
  JSONL prefix, migrating a legacy file forward. The file is rewritten only
  after a complete read; an oversized or unreadable file is never silently
  wiped.
- Appends use `O_APPEND`, which is safe for concurrent writers. A write error is
  best-effort and silent; only the size guard is surfaced.
- Warnings name the count and the limit, never the dropped text.
- Recall is a pure state machine. `idx == len(entries)` means not navigating;
  the in-progress input is stashed when navigation starts and restored when the
  user walks back past the newest entry.

## Consequences

- Good: prompts with newlines round-trip.
- Good: one bad record cannot hide the rest of the history.
- Good: legacy history migrates without a one-shot destructive rewrite.
- Bad: the file has two formats and a prefix byte that is not human-friendly.

## More Information

- [`internal/app/model.go`](../../internal/app/model.go)
- [`internal/app/history.go`](../../internal/app/history.go)
