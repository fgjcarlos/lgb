.PHONY: build test vet run clean

BINARY_NAME ?= lgb
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/fgjcarlos/lgb/internal/version.Version=$(VERSION) -X github.com/fgjcarlos/lgb/internal/version.Commit=$(COMMIT) -X github.com/fgjcarlos/lgb/internal/version.Date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/lgb

test:
	go test ./... -race -count=1

vet:
	go vet ./...

run:
	go run ./cmd/lgb

clean:
	rm -rf bin/
