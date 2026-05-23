---
change: mvp-foundation
domain: retry
phase: spec
date: 2026-05-22
status: draft
---

# Retry Specification

## Purpose

Exponential backoff retry primitive in `internal/retry/`. Provides a reusable, context-aware retry function for all reconnect domains (PLC, MQTT, OPC UA). Not wired to any consumer in Phase 0.

## Requirements

### [MVP-FND-6.1] `Do` function signature

The package MUST export a single `Do` function with the following contract:

```
func Do(ctx context.Context, opts Options, fn func(ctx context.Context) error) error
```

`context.Context` MUST be the first argument. `Options` MUST be a value type (struct). `fn` MUST receive the context so it can respect cancellation internally.

#### Scenario: Function signature matches contract

- GIVEN the `internal/retry` package is compiled
- WHEN `retry.Do` is referenced in a test
- THEN it compiles with the signature `(context.Context, retry.Options, func(context.Context) error) error`

---

### [MVP-FND-6.2] Options struct and defaults

The `Options` struct MUST define the following fields with defaults applied when zero values are provided:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Initial` | `time.Duration` | 100 ms | Delay before the first retry |
| `Max` | `time.Duration` | 30 s | Maximum delay between retries |
| `MaxAttempts` | `int` | 0 (unlimited) | 0 means retry until ctx cancelled |
| `Jitter` | `float64` | 0.25 | ±25% random jitter applied to each interval |

#### Scenario: Zero-value Options uses defaults

- GIVEN `retry.Do(ctx, retry.Options{}, fn)` is called
- WHEN fn fails on the first attempt
- THEN the second attempt is delayed approximately 100 ms (±25% jitter)

---

### [MVP-FND-6.3] Exponential backoff with jitter

`Do` MUST double the delay after each failed attempt up to `opts.Max`. Each interval MUST have ±`opts.Jitter` random noise applied. The delay after attempt N (1-indexed) MUST be approximately `min(opts.Initial × 2^(N-1), opts.Max) × (1 ± jitter)`.

#### Scenario: Delay grows exponentially

- GIVEN `Initial=100ms`, `Max=1s`, `Jitter=0.0` (no jitter for determinism)
- WHEN fn fails on attempts 1 through 4
- THEN delays are approximately 100 ms, 200 ms, 400 ms, 800 ms in order

---

### [MVP-FND-6.4] Context cancellation contract

When `ctx` is cancelled before `fn` succeeds, `Do` MUST return `ctx.Err()` promptly. It MUST NOT wait for the current retry delay to expire before returning after cancellation. It MUST NOT call `fn` again after `ctx` is cancelled.

#### Scenario: Cancellation returns context error

- GIVEN `fn` always returns an error and `MaxAttempts=0`
- WHEN the context is cancelled after 2 attempts
- THEN `Do` returns `context.Canceled`
- AND `fn` is not called again after cancellation

---

### [MVP-FND-6.5] MaxAttempts exhaustion

When `opts.MaxAttempts > 0` and all attempts fail, `Do` MUST return `ErrMaxAttempts` wrapping the last error from `fn`. Callers MUST be able to retrieve the last underlying error via `errors.Unwrap`.

#### Scenario: Exhausted attempts returns ErrMaxAttempts

- GIVEN `MaxAttempts=3` and `fn` always returns `errors.New("connection refused")`
- WHEN `Do` is called
- THEN `fn` is called exactly 3 times
- AND the returned error satisfies `errors.Is(err, retry.ErrMaxAttempts)`
- AND `errors.Unwrap(err)` contains `"connection refused"`

---

### [MVP-FND-6.6] Pure stdlib — no external dependencies

The `internal/retry` package MUST be implemented using only Go stdlib. It MUST NOT introduce any external dependency. It MUST compile under `CGO_ENABLED=0` on all four target platforms.

#### Scenario: No external imports in retry package

- GIVEN the `internal/retry` package source
- WHEN `go list -deps ./internal/retry` is run
- THEN no packages outside the Go standard library appear in the output
