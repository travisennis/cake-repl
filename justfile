golangci_lint_version := "v2.12.2"
goreleaser_version := "v2.16.0"
govulncheck_version := "v1.3.0"

# go install puts binaries in GOBIN when set, GOPATH/bin otherwise. Resolve
# the same directory and put it first on PATH so recipes prefer the pinned
# tools from install-tools over anything else installed on the machine.
gobin := `go env GOBIN`
tool_dir := if gobin != "" { gobin } else { `go env GOPATH` / "bin" }
export PATH := tool_dir + ":" + env_var("PATH")

default:
    just --list

build:
    mkdir -p bin
    go build -trimpath -o bin/cake-repl ./cmd/cake-repl

install:
    go install -trimpath ./cmd/cake-repl

test:
    go test ./...

test-race:
    go test -race -cover ./...

# Opt-in only, never part of `ci`: runs the real-cake smoke test, which spawns
# the actual cake binary and makes a model-backed request that can cost money.
# Set CAKE_BIN to use a cake other than the one on PATH.
test-real-cake:
    CAKE_REAL_SMOKE=1 go test -tags=integration -count=1 -run TestSmokeRealCake ./internal/cake/

# Benchmarks only, with allocations; extra flags pass through. Never in `ci`.
bench *args:
    go test -run '^$' -bench . -benchmem -timeout=30m {{ args }} ./...

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

lint:
    golangci-lint run

vuln:
    govulncheck ./...

release-check:
    goreleaser check
    goreleaser release --snapshot --clean --skip publish

fix: tidy fmt

ci: fmt-check tidy-check vet test-race lint vuln build release-check

verify: ci

install-tools:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@{{ golangci_lint_version }}
    go install golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }}
    go install github.com/goreleaser/goreleaser/v2@{{ goreleaser_version }}

# CI variant: prebuilt binaries instead of compiling from source. goreleaser
# comes from goreleaser-action in the workflows; govulncheck publishes no
# prebuilt binaries but is a small, quick build.
install-tools-ci:
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/{{ golangci_lint_version }}/install.sh | sh -s -- -b "{{ tool_dir }}" {{ golangci_lint_version }}
    go install golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }}

quick:
    go test ./...
    go vet ./...

release:
    go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl

run *args:
    go run ./cmd/cake-repl {{args}}
