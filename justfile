set positional-arguments

deadcode_version := "v0.50.0"
golangci_lint_version := "v2.15.0"
goreleaser_version := "v2.17.1"
govulncheck_version := "v1.7.0"

gobin := `go env GOBIN`
tool_dir := if gobin != "" { gobin } else { `go env GOPATH` / "bin" }
export PATH := tool_dir + ":" + env_var("PATH")

default:
    just --list

_check-branch-name name:
    @name={{ quote(name) }}; \
    case "$name" in \
        -*|/*|*/|*..*|*.lock|*/.*|*.|*[!A-Za-z0-9._/-]*) \
            echo "ERROR: branch name may use only letters, digits, dot, underscore, hyphen, and /, and may not start with '-' or '/', end with '/', or contain '..'" >&2; \
            exit 1 ;; \
    esac; \
    case "${name%%/*}" in \
        feat|fix|docs|style|refactor|perf|build|ci|chore|revert) ;; \
        *) echo "ERROR: branch name must start with a commit type, for example feat/timeline-compaction" >&2; exit 1 ;; \
    esac; \
    case "$name" in \
        */*) ;; \
        *) echo "ERROR: branch name must be <type>/<slug>, for example feat/timeline-compaction" >&2; exit 1 ;; \
    esac

branch name: (_check-branch-name name)
    @git switch master
    @git pull --ff-only
    @git switch --create {{ quote(name) }} --no-track

integrate name: (_check-branch-name name)
    #!/bin/sh
    set -eu
    name={{ quote(name) }}
    test "$(git branch --show-current)" = master || { echo "ERROR: run integrate from master" >&2; exit 1; }
    test -z "$(git status --porcelain)" || { echo "ERROR: worktree must be clean before integrating" >&2; exit 1; }
    git rev-parse --verify --quiet "refs/heads/$name" >/dev/null || { echo "ERROR: no such branch: $name" >&2; exit 1; }
    if git merge-base --is-ancestor "$name" master; then echo "ERROR: $name has no commits to integrate (already contained in master)" >&2; exit 1; fi
    git pull --ff-only
    git switch "$name"
    git rebase master
    if test -z "$(git rev-list "master..$name")"; then
        echo "NOTE: $name had no commits left after rebase (already applied); master is unchanged" >&2
        git switch master
        exit 0
    fi
    git switch master
    git merge --ff-only "$name"

hooks:
    @command -v prek > /dev/null || { echo "ERROR: prek is not installed; see https://github.com/j178/prek" >&2; exit 1; }
    prek install --hook-type pre-commit --hook-type commit-msg

build:
    mkdir -p bin
    go build -trimpath -o bin/cake-repl ./cmd/cake-repl

install:
    go install -trimpath ./cmd/cake-repl

fmt:
    go fmt ./...

fmt-check:
    test -z "$(gofmt -l .)"

tidy:
    go mod tidy

tidy-check:
    go mod tidy -diff

update-deps:
    go get -u ./...
    go mod tidy

vet:
    go vet ./...

lint: comment-check
    golangci-lint run

comment-check:
    go run ./tools/commentcheck -baseline tools/commentcheck/allowlist.txt -limit tools/commentcheck/limit.txt .

comment-count:
    go run ./tools/commentcheck -count .

comment-baseline:
    go run ./tools/commentcheck -baseline tools/commentcheck/allowlist.txt -limit tools/commentcheck/limit.txt -write-baseline .

deadcode:
    #!/bin/sh
    set -eu
    out=$(deadcode ./...)
    if [ -n "$out" ]; then
        printf '%s\n' "$out"
        echo "deadcode: unreachable functions found; land the caller or delete the code" >&2
        exit 1
    fi

vuln:
    govulncheck ./...

release-check:
    goreleaser check
    goreleaser release --snapshot --clean --skip publish

fix: tidy fmt

ci: fmt-check tidy-check vet lint deadcode vuln build release-check

verify: ci

install-tools:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@{{ golangci_lint_version }}
    go install golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }}
    go install golang.org/x/tools/cmd/deadcode@{{ deadcode_version }}
    go install github.com/goreleaser/goreleaser/v2@{{ goreleaser_version }}

install-tools-ci:
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/{{ golangci_lint_version }}/install.sh | sh -s -- -b "{{ tool_dir }}" {{ golangci_lint_version }}
    go install golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }}
    go install golang.org/x/tools/cmd/deadcode@{{ deadcode_version }}

quick:
    go vet ./...

release:
    go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl

run *args:
    go run ./cmd/cake-repl "$@"
