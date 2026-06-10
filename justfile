default:
    just --list

build:
    go build ./cmd/cake-repl

install:
    go install ./cmd/cake-repl

test:
    go test ./...

fmt:
    gofmt -w cmd internal

vet:
    go vet ./...

release:
    go build -trimpath -ldflags="-s -w" -o cake-repl ./cmd/cake-repl

run *args:
    go run ./cmd/cake-repl {{args}}
