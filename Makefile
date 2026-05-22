.PHONY: build test vet run clean

BINARY_NAME ?= lgb
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

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
