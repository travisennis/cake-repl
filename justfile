golangci_lint_version := "v2.12.2"
goreleaser_version := "v2.16.0"
govulncheck_version := "v1.3.0"

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
    "$(go env GOPATH)/bin/golangci-lint" run

vuln:
    "$(go env GOPATH)/bin/govulncheck" ./...

release-check:
    "$(go env GOPATH)/bin/goreleaser" check
    "$(go env GOPATH)/bin/goreleaser" release --snapshot --clean --skip publish

fix: tidy fmt

ci: fmt-check tidy-check vet test-race lint vuln build release-check

verify: ci

install-tools:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@{{ golangci_lint_version }}
    go install golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }}
    go install github.com/goreleaser/goreleaser/v2@{{ goreleaser_version }}

quick:
    go test ./...
    go vet ./...

release:
    go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl

run *args:
    go run ./cmd/cake-repl {{args}}
