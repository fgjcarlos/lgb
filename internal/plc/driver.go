package plc

import (
	"context"
	"time"

	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// Re-export PLC-domain sentinels from internal/errors for ergonomic local use
// (design §8, PLC-ERR-1.2).
var (
	// ErrPLCConnect is returned when a TCP/CIP connection cannot be established.
	ErrPLCConnect = errs.ErrPLCConnect

	// ErrPLCRead is returned when a tag read operation fails.
	ErrPLCRead = errs.ErrPLCRead

	// ErrPLCWrite is returned when a tag write operation fails.
	ErrPLCWrite = errs.ErrPLCWrite

	// ErrPLCTimeout is returned when a PLC operation exceeds its deadline.
	ErrPLCTimeout = errs.ErrPLCTimeout
)

// Driver is the boundary interface for PLC tag I/O.
//
// Implementations MUST be safe for sequential use from a single goroutine.
// Concurrent use is NOT required — gologix serializes all I/O through its
// internal mutex, so callers should not call methods concurrently.
//
// The Manager is responsible for calling Connect before any tag operations.
// ReadTag/WriteTag/ReadMulti will return ErrPLCConnect if called while
// disconnected (AutoConnect is always disabled).
type Driver interface {
	// Connect establishes a CIP session to the PLC. ctx cancellation is
	// respected before the dial; once a dial is in progress it runs to
	// completion or SocketTimeout (gologix limitation).
	Connect(ctx context.Context) error

	// Close gracefully tears down the CIP session and releases resources.
	Close() error

	// ReadTag reads a single PLC tag into dest. dest must be a pointer to a
	// supported scalar type or a pre-allocated slice. For []bool, len(dest)
	// must be a multiple of 32.
	ReadTag(tag string, dest any) error

	// WriteTag writes val to the named PLC tag. val must be a supported scalar
	// or slice type.
	WriteTag(tag string, val any) error

	// ReadMulti reads multiple tags in sequential calls using ReadTag. tags and
	// dests must have equal length; each dests[i] must be a valid dest for
	// ReadTag(tags[i], dests[i]).
	ReadMulti(tags []string, dests []any) error

	// Connected returns true if the CIP session is currently established.
	Connected() bool
}

// Option is a functional option for configuring a Driver.
type Option func(*options)

// options holds the resolved configuration for a Driver instance.
type options struct {
	// RetryInitial is the initial delay before the first reconnection attempt.
	// Zero resolves to 1 second.
	RetryInitial time.Duration

	// RetryMax is the maximum delay between reconnection attempts.
	// Zero resolves to 30 seconds.
	RetryMax time.Duration

	// MaxAttempts is the total number of connection attempts allowed.
	// Zero means unlimited retries.
	MaxAttempts int
}

// WithRetryInitial sets the initial retry delay (default 1s).
func WithRetryInitial(d time.Duration) Option {
	return func(o *options) { o.RetryInitial = d }
}

// WithRetryMax sets the maximum retry delay (default 30s).
func WithRetryMax(d time.Duration) Option {
	return func(o *options) { o.RetryMax = d }
}

// WithMaxAttempts sets the maximum number of connection attempts (default 0 = unlimited).
func WithMaxAttempts(n int) Option {
	return func(o *options) { o.MaxAttempts = n }
}

// applyDefaults fills in zero-value fields with safe defaults.
func (o *options) applyDefaults() {
	if o.RetryInitial <= 0 {
		o.RetryInitial = time.Second
	}
	if o.RetryMax <= 0 {
		o.RetryMax = 30 * time.Second
	}
}
