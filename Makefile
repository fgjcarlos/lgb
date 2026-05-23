.PHONY: build test vet run clean build-all docker-up docker-down lint generate adr-index

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

## lint — run golangci-lint with the project configuration.
## Requirements: MVP-FND-9.9. Design: §19 decision #23.
lint:
	golangci-lint run

## generate — run protobuf codegen for Sparkplug B and any other .proto files.
## Requires: protoc, protoc-gen-go (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)
## Requirements: MVP-FND-1.13, SPK-1.1. Design: §2, §5 decision #6.
generate:
	@if [ -f proto/sparkplug_b.proto ]; then \
		protoc --go_out=internal/sparkplug/pb --go_opt=paths=source_relative proto/sparkplug_b.proto; \
		echo "# generated internal/sparkplug/pb/sparkplug_b.pb.go"; \
	else \
		echo "# no .proto files — skipping protobuf codegen"; \
	fi

## adr-index — list all ADRs in docs/adr/.
## Requirements: MVP-FND-1.14.
adr-index:
	@echo "Architecture Decision Records:"
	@ls docs/adr/*.md 2>/dev/null | sort | while read f; do \
		title=$$(head -1 "$$f" | sed 's/^# //'); \
		echo "  $$f — $$title"; \
	done
