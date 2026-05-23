.PHONY: build test vet run clean build-all docker-up docker-down lint

BINARY_NAME ?= lgb
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# LDFLAGS injects build metadata into internal/version per MVP-FND-1.7 and design §25.
# Target: -X github.com/fgjcarlos/lgb/internal/version.{Version,Commit,Date}
LDFLAGS := -X github.com/fgjcarlos/lgb/internal/version.Version=$(VERSION) -X github.com/fgjcarlos/lgb/internal/version.Commit=$(COMMIT) -X github.com/fgjcarlos/lgb/internal/version.Date=$(DATE)

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -tags no_embed -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/lgb

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
# -tags no_embed prevents requiring frontend/dist at cross-compile time.
build-all:
	mkdir -p bin
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -tags no_embed -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-amd64       ./cmd/lgb
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -tags no_embed -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-arm64       ./cmd/lgb
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -tags no_embed -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-darwin-arm64      ./cmd/lgb
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags no_embed -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/lgb

## docker-up — start the development stack (gateway + plcsim + mqtt).
## Requires LGB_AUTH_JWT_SECRET to be set in the shell or in docker/.env.dev.
## See docker/.env.dev.example. Requirements: MVP-FND-9.7.
docker-up:
	docker compose -f docker-compose.dev.yml up -d

## docker-down — stop the development stack and remove volumes.
## Requirements: MVP-FND-9.7.
docker-down:
	docker compose -f docker-compose.dev.yml down -v

## lint — run golangci-lint if .golangci.yml is present, or skip gracefully.
## Slice 4 adds the full linter config. Requirements: MVP-FND-9.9 (stub).
lint:
	@if [ -f .golangci.yml ]; then \
		golangci-lint run; \
	else \
		echo "# .golangci.yml not found — skipping lint (will be added in slice 4)"; \
	fi
