#!/bin/sh
# fake-cake stands in for the real cake CLI so a driven cake-repl session can be
# reproduced without a model, a network, or a session on disk.
#
# Point the REPL at it with an absolute path:
#
#   bin/cake-repl -cake-bin "$PWD/scripts/fake-cake.sh" ...
#
# It ignores the CLI flags it is given and replays one checked-in stream-json
# fixture from internal/cake/testdata/fixtures/ instead. It is hermetic by
# construction: the only input is a file in this repository.
#
# The Go runner tests keep their own inline fake shell scripts
# (writeFakeCake in internal/cake/runner_test.go); this script exists for the
# drive-tui skill and for anyone driving the REPL by hand.
#
# Environment:
#   FAKE_CAKE_FIXTURE         fixture for a live prompt (default: happy-path)
#   FAKE_CAKE_REPLAY_FIXTURE  fixture for `replay <uuid>` (default: replay-path)
#   FAKE_CAKE_SESSIONS        response for `sessions list --json`: `valid`
#                             (default), `empty`, or `error`
#   FAKE_CAKE_DELAY           seconds to wait after each line (default: 0), so a
#                             running state or a loading listing is observable
#   FAKE_CAKE_ARGS_FILE       when set, append the argv of every invocation
#                             (for checking which flags the REPL passed through)
#   FAKE_CAKE_DEBUG           when set, print the argv to stderr
set -eu

fixtures_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../internal/cake/testdata/fixtures" && pwd)

fixture=${FAKE_CAKE_FIXTURE:-happy-path}
case "$*" in
    *" replay "*) fixture=${FAKE_CAKE_REPLAY_FIXTURE:-replay-path} ;;
esac
delay=${FAKE_CAKE_DELAY:-0}

if [ -n "${FAKE_CAKE_DEBUG:-}" ]; then
    printf 'fake-cake argv: %s\n' "$*" >&2
fi
if [ -n "${FAKE_CAKE_ARGS_FILE:-}" ]; then
    { printf '%s\n' '--'; printf '%s\n' "$@"; } >> "$FAKE_CAKE_ARGS_FILE"
fi

# `cake sessions list --json` is a different shape: it answers with the read-only
# listing envelope (or a failure), not a stream fixture, so a driven session can
# exercise the /sessions browser's loading, list, empty, and error states.
if [ "${1:-}" = "sessions" ]; then
    case "${FAKE_CAKE_SESSIONS:-valid}" in
        valid)
            printf '%s\n' '{"schema_version":1,"command":"sessions list","status":"ok","summary":{"count":2},"data":{"sessions":['
            sleep "$delay"
            printf '%s\n' '{"session_id":"11111111-2222-3333-4444-555555555555","first_prompt":"fixture session one","timestamp":"2026-10-07T11:20:44Z"},'
            sleep "$delay"
            printf '%s\n' '{"session_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","first_prompt":"fixture session two","timestamp":"2026-10-06T09:00:00Z"}]}}'
            exit 0
            ;;
        empty)
            printf '%s\n' '{"schema_version":1,"data":{"sessions":[]}}'
            exit 0
            ;;
        error)
            printf 'fake-cake: sessions unavailable\n' >&2
            exit 1
            ;;
        *)
            printf 'fake-cake: unknown FAKE_CAKE_SESSIONS: %s\n' "$FAKE_CAKE_SESSIONS" >&2
            exit 2
            ;;
    esac
fi

file="$fixtures_dir/$fixture.ndjson"
if [ ! -f "$file" ]; then
    printf 'fake-cake: no such fixture: %s\n' "$file" >&2
    printf 'fake-cake: available: %s\n' \
        "$(ls "$fixtures_dir" | sed -e 's/\.ndjson$//' | tr '\n' ' ')" >&2
    exit 2
fi

while IFS= read -r line; do
    printf '%s\n' "$line"
    if [ "$delay" != "0" ]; then
        sleep "$delay"
    fi
done < "$file"
