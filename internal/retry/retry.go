// Package retry provides a context-aware exponential-backoff retry primitive.
//
// Usage:
//
//	err := retry.Do(ctx, retry.Options{
//	    Initial:     100 * time.Millisecond,
//	    Max:         30 * time.Second,
//	    MaxAttempts: 5,
//	    Jitter:      0.25,
//	}, func(ctx context.Context) error {
//	    return connect(ctx)
//	})
//
// Requirements: MVP-FND-6.1 through MVP-FND-6.6. Design: §4.2.
// Pure stdlib — no external dependencies (MVP-FND-6.6).
package retry

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// ErrMaxAttempts is re-exported from internal/errors for ergonomic call sites.
var ErrMaxAttempts = errs.ErrMaxAttempts

// Options configures retry behaviour. Zero values apply safe defaults, except
// for Jitter — use the sentinel value -1 to mean "use default 0.25", so that
// callers can explicitly pass Jitter=0.0 to disable jitter.
//
// In practice, use the DefaultOptions() helper or rely on the zero-value
// semantics: a zero Options will be interpreted with Jitter=-1 (defaulted to
// 0.25) internally UNLESS Sleep is also set (test mode). When Sleep is set and
// Jitter is exactly 0.0, jitter is disabled — this lets tests drive
// deterministic delays.
type Options struct {
	// Initial is the delay before the first retry (default 100 ms).
	Initial time.Duration

	// Max is the maximum delay between retries (default 30 s).
	Max time.Duration

	// MaxAttempts is the total number of calls allowed (0 = unlimited).
	// When exhausted, Do returns ErrMaxAttempts wrapping the last fn error.
	MaxAttempts int

	// Jitter is the ±fraction of random noise applied to each delay.
	// Default (when Sleep is nil and Jitter==0): 0.25.
	// Set explicitly to 0.0 together with a non-nil Sleep to disable jitter.
	// Clamped to [0, 1] after defaults are resolved.
	Jitter float64

	// Sleep is an injectable timer factory used in tests to avoid real waits.
	// When nil, time.After is used and Jitter defaults to 0.25.
	Sleep func(d time.Duration) <-chan time.Time
}

func (o *Options) applyDefaults() {
	if o.Initial <= 0 {
		o.Initial = 100 * time.Millisecond
	}
	if o.Max <= 0 {
		o.Max = 30 * time.Second
	}
	// Only apply the default jitter when Sleep is nil (production path).
	// When Sleep is set (test path), the caller explicitly controls Jitter.
	if o.Sleep == nil && o.Jitter == 0 {
		o.Jitter = 0.25
	}
	if o.Jitter > 1 {
		o.Jitter = 1
	}
	if o.Sleep == nil {
		o.Sleep = time.After
	}
}

// Do calls fn until it returns nil, ctx is cancelled, or MaxAttempts is
// reached. The delay after attempt N (1-indexed) is:
//
//	min(Initial × 2^(N-1), Max) × (1 ± Jitter)
//
// Returns:
//   - nil on success
//   - ctx.Err() on context cancellation (without waiting for the current delay)
//   - fmt.Errorf("retry: %w", ErrMaxAttempts) wrapping last fn error on exhaustion
//
// Per MVP-FND-6.1 and design §4.2.
func Do(ctx context.Context, opts Options, fn func(context.Context) error) error {
	opts.applyDefaults()

	var lastErr error
	for attempt := 1; ; attempt++ {
		// Check if MaxAttempts would be exceeded before calling fn.
		if opts.MaxAttempts > 0 && attempt > opts.MaxAttempts {
			return fmt.Errorf("retry: %w: %w", errs.ErrMaxAttempts, lastErr)
		}

		// Respect ctx cancellation before each attempt.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		// If all attempts exhausted, return ErrMaxAttempts wrapping lastErr.
		if opts.MaxAttempts > 0 && attempt >= opts.MaxAttempts {
			return fmt.Errorf("retry: %w: %w", errs.ErrMaxAttempts, lastErr)
		}

		// Compute delay: min(Initial * 2^(attempt-1), Max).
		delay := opts.Initial
		for i := 1; i < attempt; i++ {
			delay *= 2
			if delay > opts.Max {
				delay = opts.Max
				break
			}
		}

		// Apply ±jitter as a fraction of the computed delay.
		if opts.Jitter > 0 {
			// Random multiplier in range [1-jitter, 1+jitter].
			jitterFactor := 1.0 + opts.Jitter*(2*rand.Float64()-1)
			delay = time.Duration(float64(delay) * jitterFactor)
		}

		// Wait for delay or context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-opts.Sleep(delay):
		}
	}
}
