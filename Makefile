.PHONY: build test vet run clean build-all

BINARY_NAME ?= lgb
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# LDFLAGS injects build metadata into internal/version per MVP-FND-1.7 and design §25.
# Target: -X github.com/fgjcarlos/lgb/internal/version.{Version,Commit,Date}
LDFLAGS := -X github.com/fgjcarlos/lgb/internal/version.Version=$(VERSION) -X github.com/fgjcarlos/lgb/internal/version.Commit=$(COMMIT) -X github.com/fgjcarlos/lgb/internal/version.Date=$(DATE)

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/lgb

test:
	go test ./... -race -count=1

vet:
	go vet ./...

run:
	go run ./cmd/lgb

clean:
	rm -rf bin/

# build-all cross-compiles the binary for all four target platforms.
# Used by CI and release workflows. CGO_ENABLED=0 is mandatory (ADR-0009).
build-all:
	mkdir -p bin
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-amd64    ./cmd/lgb
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-arm64    ./cmd/lgb
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-darwin-arm64   ./cmd/lgb
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/lgb
