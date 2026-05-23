// Package retry_test tests the exponential-backoff retry primitive.
// Requirements: MVP-FND-6.1 through MVP-FND-6.5. Design: §4.2, §20.3.
package retry_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	errs "github.com/fgjcarlos/lgb/internal/errors"
	"github.com/fgjcarlos/lgb/internal/retry"
)

// TestSuccessOnFirstCall asserts MVP-FND-6.1: a function that succeeds on the
// first call returns nil with zero retries.
func TestSuccessOnFirstCall(t *testing.T) {
	var callCount int32
	fn := func(_ context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	}

	ctx := context.Background()
	err := retry.Do(ctx, retry.Options{}, fn)
	if err != nil {
		t.Errorf("Do returned %v; want nil", err)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("fn called %d times; want 1", atomic.LoadInt32(&callCount))
	}
}

// TestExponentialDelayGrowthWithNoJitter asserts MVP-FND-6.3: with Jitter=0.0
// the delays double each attempt up to Max.
//
// We use an injectable sleep function to avoid real time waits in tests.
func TestExponentialDelayGrowthWithNoJitter(t *testing.T) {
	var delays []time.Duration

	// Override the sleep function via Options.Sleep (injectable for tests).
	sleepFn := func(d time.Duration) <-chan time.Time {
		delays = append(delays, d)
		// Return an immediately-firing timer so the test runs fast.
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	var callCount int32
	targetCalls := int32(4) // first call + 3 retries = 4 total calls, 3 delays

	fn := func(_ context.Context) error {
		n := atomic.AddInt32(&callCount, 1)
		if n < targetCalls {
			return errors.New("transient error")
		}
		return nil
	}

	ctx := context.Background()
	err := retry.Do(ctx, retry.Options{
		Initial: 100 * time.Millisecond,
		Max:     1 * time.Second,
		Jitter:  0.0,
		Sleep:   sleepFn,
	}, fn)
	if err != nil {
		t.Errorf("Do returned %v; want nil", err)
	}

	// With Jitter=0 and Initial=100ms: delays should be 100ms, 200ms, 400ms.
	wantDelays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	if len(delays) != len(wantDelays) {
		t.Fatalf("got %d delays %v; want %d %v", len(delays), delays, len(wantDelays), wantDelays)
	}
	for i, want := range wantDelays {
		if delays[i] != want {
			t.Errorf("delay[%d] = %v; want %v", i, delays[i], want)
		}
	}
}

// TestContextCancellationReturnsCtxError asserts MVP-FND-6.4: cancelling the
// context causes Do to return ctx.Err() promptly.
func TestContextCancellationReturnsCtxError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var callCount int32
	fn := func(_ context.Context) error {
		n := atomic.AddInt32(&callCount, 1)
		if n == 2 {
			cancel() // cancel after the second attempt
		}
		return errors.New("always fails")
	}

	// Use a sleep that unblocks immediately but respects ctx cancellation.
	sleepFn := func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	err := retry.Do(ctx, retry.Options{
		Jitter: 0.0,
		Sleep:  sleepFn,
	}, fn)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Do returned %v; want context.Canceled", err)
	}
}

// TestMaxAttemptsExhaustion asserts MVP-FND-6.5: when MaxAttempts is reached,
// Do returns ErrMaxAttempts wrapping the last fn error.
func TestMaxAttemptsExhaustion(t *testing.T) {
	sentinel := errors.New("connection refused")
	var callCount int32

	fn := func(_ context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return sentinel
	}

	sleepFn := func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	ctx := context.Background()
	err := retry.Do(ctx, retry.Options{
		MaxAttempts: 3,
		Jitter:      0.0,
		Sleep:       sleepFn,
	}, fn)

	if err == nil {
		t.Fatal("Do returned nil; want error")
	}
	if !errors.Is(err, errs.ErrMaxAttempts) {
		t.Errorf("errors.Is(err, ErrMaxAttempts) = false; want true; got %v", err)
	}
	// The last fn error must be unwrappable from the returned error.
	if !errors.Is(err, sentinel) {
		t.Errorf("last fn error not in chain: errors.Is(err, sentinel) = false; got %v", err)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("fn called %d times; want 3", atomic.LoadInt32(&callCount))
	}
}

// TestZeroOptionsUsesDefaults asserts MVP-FND-6.2: zero-value Options uses
// safe defaults. We verify it doesn't panic and runs fn at least once.
func TestZeroOptionsUsesDefaults(t *testing.T) {
	// fn succeeds on first attempt — so we just verify no panic and no error.
	called := false
	fn := func(_ context.Context) error {
		called = true
		return nil
	}

	ctx := context.Background()
	err := retry.Do(ctx, retry.Options{}, fn)
	if err != nil {
		t.Errorf("Do returned %v; want nil", err)
	}
	if !called {
		t.Error("fn was never called")
	}
}
