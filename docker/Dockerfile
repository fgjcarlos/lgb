# docker/Dockerfile — multi-stage production build.
#
# Stage 1 (restic-bin): extracts the restic binary from the official upstream
#   image so the final image can include backup functionality without pulling
#   the Go toolchain at runtime.
# Stage 2 (build): compiles the lgb binary with CGO_ENABLED=0 for a fully
#   static binary compatible with the distroless final stage.
# Stage 3 (final): minimal distroless image — no shell, no package manager.
#   Runs as non-root by default (gcr.io/distroless/static-debian12:nonroot).
#
# Requirements: MVP-FND-9.4. Design: §13.2, decision #21, #22 (ADR-0009).
# Run: docker build -f docker/Dockerfile -t lgb:latest .

# ----------------------------
# Stage 1: extract restic binary
# ----------------------------
FROM restic/restic:0.18.0 AS restic-bin

# ----------------------------
# Stage 2: Go build
# ----------------------------
FROM golang:1.24-alpine AS build

WORKDIR /src

# Download dependencies first (leverages layer caching).
COPY go.mod go.sum ./
RUN go mod download

# Copy the full source and build.
COPY . .

ENV CGO_ENABLED=0

# Inject build metadata from VCS; fall back to "dev" when git is unavailable.
RUN VERSION=$(git rev-parse --short HEAD 2>/dev/null || echo dev) && \
    go build \
      -ldflags "-s -w \
        -X github.com/fgjcarlos/lgb/internal/version.Version=${VERSION} \
        -X github.com/fgjcarlos/lgb/internal/version.Commit=${VERSION} \
        -X github.com/fgjcarlos/lgb/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -tags no_embed \
      -o /out/lgb \
      ./cmd/lgb

# ----------------------------
# Stage 3: minimal runtime image
# ----------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build       /out/lgb        /usr/local/bin/lgb
COPY --from=restic-bin  /usr/bin/restic  /usr/local/bin/restic

USER nonroot

ENTRYPOINT ["/usr/local/bin/lgb"]
CMD ["server", "--config", "/etc/lgb/lgb.yaml"]
