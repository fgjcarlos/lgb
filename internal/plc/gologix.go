package plc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danomagnum/gologix"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/retry"
)

// gologixClient is the minimal interface that gologixDriver requires from the
// underlying CIP client. It is satisfied by *gologix.Client and can be replaced
// with a fake in unit tests.
type gologixClient interface {
	Connect() error
	Disconnect() error
	Read(tag string, data any) error
	Write(tag string, val any) error
	Connected() bool
}

// gologixDriver is the concrete PLC adapter wrapping a single *gologix.Client.
// It implements [Driver] and is created via [NewDriver].
//
// Thread safety: Driver methods must not be called concurrently. The gologix
// client serializes all CIP I/O through its internal mutex; the driver adds
// an atomic.Bool for the connected state that is safe for concurrent reads.
type gologixDriver struct {
	client    gologixClient
	cfg       config.PLC
	opts      options
	connected atomic.Bool
	mu        sync.Mutex // serializes Connect/Close calls
}

// NewDriver constructs a gologixDriver for the given PLC configuration.
// The returned Driver has AutoConnect disabled; callers MUST call Connect before
// any tag operations.
func NewDriver(cfg config.PLC, optFns ...Option) Driver {
	o := options{}
	for _, fn := range optFns {
		fn(&o)
	}
	o.applyDefaults()

	// Build the real gologix client.
	client := buildClient(cfg)
	return &gologixDriver{
		client: client,
		cfg:    cfg,
		opts:   o,
	}
}

// NewDriverWithClient constructs a gologixDriver with an injected client.
// This is intended for unit tests only — production code MUST use NewDriver.
func NewDriverWithClient(cfg config.PLC, client gologixClient, optFns ...Option) Driver {
	o := options{}
	for _, fn := range optFns {
		fn(&o)
	}
	o.applyDefaults()
	return &gologixDriver{
		client: client,
		cfg:    cfg,
		opts:   o,
	}
}

// buildClient creates a *gologix.Client configured from cfg.
// AutoConnect is always set to false (design decision #1).
func buildClient(cfg config.PLC) *gologix.Client {
	c := gologix.NewClient(cfg.Address)

	// Design decision #1: AutoConnect MUST be false. Our retry.Do controls
	// reconnection explicitly to prevent double-reconnect races.
	c.AutoConnect = false

	// Design decision #6: SocketTimeout as context substitute.
	if cfg.SocketTimeout != "" {
		if d, err := time.ParseDuration(cfg.SocketTimeout); err == nil && d > 0 {
			c.SocketTimeout = d
		}
	}

	// Design decision #8: build path from slot number.
	if cfg.Path != "" {
		// Caller provided an explicit CIP path string (e.g. "1,2").
		if p, err := gologix.ParsePath(cfg.Path); err == nil {
			c.Controller.Path = p
		}
	} else {
		// Build the standard backplane path from slot.
		slot := cfg.Slot
		if slot < 0 || slot > 15 {
			slot = 0
		}
		if p, err := gologix.ParsePath(fmt.Sprintf("1,%d", slot)); err == nil {
			c.Controller.Path = p
		}
	}

	return c
}

// Connect establishes a CIP session. ctx cancellation is respected before each
// retry attempt via retry.Do; once a dial is in progress it runs to completion
// or SocketTimeout (gologix limitation, design §5 decision #6).
func (d *gologixDriver) Connect(ctx context.Context) error {
	// Check context before acquiring the mutex.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	err := retry.Do(ctx, retry.Options{
		Initial:     d.opts.RetryInitial,
		Max:         d.opts.RetryMax,
		MaxAttempts: d.opts.MaxAttempts,
	}, func(ctx context.Context) error {
		// Respect context cancellation between attempts.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return d.client.Connect()
	})
	if err != nil {
		d.connected.Store(false)
		return err
	}

	d.connected.Store(true)
	return nil
}

// Close gracefully tears down the CIP session. It is idempotent — a second call
// when already disconnected returns nil without error.
func (d *gologixDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.connected.Load() {
		// Already disconnected — idempotent, return nil.
		return nil
	}

	err := d.client.Disconnect()
	d.connected.Store(false)
	if err != nil {
		// Log but do not surface the error — the connection is closed regardless.
		// Disconnect errors from gologix are typically "already disconnected"
		// states that occur during normal shutdown sequences.
		return nil
	}
	return nil
}

// ReadTag reads a single tag value from the PLC into dest.
//
// For []bool destinations, len(dest) MUST be a multiple of 32 (gologix
// encodes BOOL arrays as packed 32-bit words). This is validated before
// dispatching to the client to provide a clear error message.
func (d *gologixDriver) ReadTag(tag string, dest any) error {
	// Validate []bool length before touching the client (design §5 decision #7).
	if boolSlice, ok := dest.([]bool); ok {
		n := len(boolSlice)
		if n%32 != 0 {
			return fmt.Errorf("plc: read %q: []bool length must be a multiple of 32, got %d: %w",
				tag, n, ErrPLCRead)
		}
	}

	err := d.client.Read(tag, dest)
	if err != nil {
		return translateError(err, "read", tag)
	}
	return nil
}

// WriteTag writes val to the named PLC tag.
func (d *gologixDriver) WriteTag(tag string, val any) error {
	err := d.client.Write(tag, val)
	if err != nil {
		return translateError(err, "write", tag)
	}
	return nil
}

// ReadMulti reads multiple tags sequentially. tags and dests must have the
// same length; if not, an error wrapping ErrPLCRead is returned immediately
// without touching the client (design §5 decision #7).
func (d *gologixDriver) ReadMulti(tags []string, dests []any) error {
	if len(tags) != len(dests) {
		return fmt.Errorf("plc: ReadMulti: len(tags)=%d != len(dests)=%d: %w",
			len(tags), len(dests), ErrPLCRead)
	}
	for i, tag := range tags {
		if err := d.ReadTag(tag, dests[i]); err != nil {
			return fmt.Errorf("plc: ReadMulti tag[%d] %q: %w", i, tag, err)
		}
	}
	return nil
}

// Connected returns true if the CIP session is currently established.
// It is safe to call concurrently.
func (d *gologixDriver) Connected() bool {
	return d.connected.Load()
}
