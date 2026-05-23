package plc

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/retry"
)

// DriverFactory is a function that creates a Driver for the given PLC configuration.
// Providing a custom factory allows test code to inject mock drivers without
// touching the production gologix wiring.
type DriverFactory func(cfg config.PLC) Driver

// defaultDriverFactory wraps NewDriver as a DriverFactory using default options.
func defaultDriverFactory(cfg config.PLC) Driver {
	return NewDriver(cfg)
}

// plcWorker groups all state owned by a single per-PLC goroutine.
type plcWorker struct {
	driver Driver
	cfg    config.PLC
	cancel context.CancelFunc
}

// Manager owns the lifecycle of all PLC Drivers: start, stop, lookup, and
// hot-reload. It is safe for concurrent use.
//
// Design §4, §6.3, §6.4 — PLC-DRV-2.1, PLC-DRV-2.2, PLC-DRV-2.3.
type Manager struct {
	log     *slog.Logger
	factory DriverFactory

	mu      sync.RWMutex
	workers map[string]*plcWorker // keyed by PLC name
	wg      sync.WaitGroup
}

// NewManager constructs a Manager and eagerly creates one Driver per PLC entry
// in cfg using factory. If factory is nil, NewDriver is used.
//
// Start must be called before any tag operations.
func NewManager(cfg *config.Config, log *slog.Logger, factory DriverFactory) *Manager {
	if log == nil {
		log = slog.Default()
	}
	if factory == nil {
		factory = defaultDriverFactory
	}

	m := &Manager{
		log:     log,
		factory: factory,
		workers: make(map[string]*plcWorker, len(cfg.PLCs)),
	}

	// Eagerly create drivers so Driver(name) works before Start.
	for _, plcCfg := range cfg.PLCs {
		d := factory(plcCfg)
		m.workers[plcCfg.Name] = &plcWorker{driver: d, cfg: plcCfg}
	}

	return m
}

// Start connects all PLCs and launches a per-PLC scan goroutine.
// ctx controls the lifecycle of all goroutines: when ctx is cancelled the
// goroutines exit and Stop() will not block.
//
// Start returns immediately after launching goroutines; connection happens
// asynchronously inside each goroutine via retry.Do.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, w := range m.workers {
		plcCtx, cancel := context.WithCancel(ctx)
		w.cancel = cancel

		// Capture loop variables before goroutine launch.
		workerName := name
		d := w.driver
		plcCfg := w.cfg

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.runWorker(plcCtx, workerName, d, plcCfg)
		}()
	}

	return nil
}

// Stop cancels all per-PLC goroutines, calls Close on each driver, and waits
// for all goroutines to exit. It is safe to call Stop more than once.
func (m *Manager) Stop() error {
	m.mu.Lock()
	// Cancel every per-PLC context.
	for _, w := range m.workers {
		if w.cancel != nil {
			w.cancel()
		}
	}
	m.mu.Unlock()

	// Wait for all goroutines to exit.
	m.wg.Wait()

	// Close all drivers once goroutines have stopped.
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, w := range m.workers {
		if err := w.driver.Close(); err != nil {
			m.log.Warn("plc manager: Close error",
				slog.String("plc", name),
				slog.String("err", err.Error()))
		}
	}

	return nil
}

// Driver returns the Driver for the named PLC. If the PLC is not configured,
// ok is false and d is nil.
func (m *Manager) Driver(name string) (Driver, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workers[name]
	if !ok {
		return nil, false
	}
	return w.driver, true
}

// Reload applies a new configuration hot. It stops goroutines for PLCs that
// were removed or changed, creates drivers for new or changed PLCs, and starts
// goroutines for the new set. The parent ctx must be the same context passed
// to Start.
//
// Design §6.3 (hot-reload sequence).
func (m *Manager) Reload(ctx context.Context, cfg *config.Config) error {
	// Build a quick lookup of new config by name.
	newCfgByName := make(map[string]config.PLC, len(cfg.PLCs))
	for _, plcCfg := range cfg.PLCs {
		newCfgByName[plcCfg.Name] = plcCfg
	}

	m.mu.Lock()

	// Collect workers to drain (removed or changed PLCs).
	var toDrain []string
	for name := range m.workers {
		if _, exists := newCfgByName[name]; !exists {
			toDrain = append(toDrain, name)
		}
	}

	// Cancel and remove drained workers.
	drainedDrivers := make([]Driver, 0, len(toDrain))
	for _, name := range toDrain {
		w := m.workers[name]
		if w.cancel != nil {
			w.cancel()
		}
		drainedDrivers = append(drainedDrivers, w.driver)
		delete(m.workers, name)
	}

	m.mu.Unlock()

	// Wait for drained goroutines to exit.
	// Since we cancelled their contexts, runWorker will exit quickly.
	m.wg.Wait()

	// Close drained drivers.
	for _, d := range drainedDrivers {
		if err := d.Close(); err != nil {
			m.log.Warn("plc manager: Reload: Close error",
				slog.String("err", err.Error()))
		}
	}

	// Add new PLCs.
	m.mu.Lock()
	for _, plcCfg := range cfg.PLCs {
		if _, exists := m.workers[plcCfg.Name]; !exists {
			d := m.factory(plcCfg)
			plcCtx, cancel := context.WithCancel(ctx)
			w := &plcWorker{driver: d, cfg: plcCfg, cancel: cancel}
			m.workers[plcCfg.Name] = w

			workerName := plcCfg.Name
			capturedCfg := plcCfg
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				m.runWorker(plcCtx, workerName, d, capturedCfg)
			}()
		}
	}
	m.mu.Unlock()

	return nil
}

// runWorker is the per-PLC goroutine body. It connects the driver via
// retry.Do (respecting ctx cancellation) and then enters the scan loop,
// ticking at ScanRate. On tick: a tag read is performed (Phase 1: log-only
// since there is no tag store yet). On read error: log WARN and reconnect.
// Exits when ctx is cancelled.
//
// Design §6.1 (connection lifecycle), §6.4 (reconnect on failure).
func (m *Manager) runWorker(ctx context.Context, name string, d Driver, plcCfg config.PLC) {
	log := m.log.With(slog.String("plc", name))

	// Phase 1 connection via retry.Do.
	connectErr := retry.Do(ctx, retry.Options{
		Initial:     time.Second,
		Max:         30 * time.Second,
		MaxAttempts: 0, // unlimited — exit only on ctx cancel
	}, func(ctx context.Context) error {
		return d.Connect(ctx)
	})
	if connectErr != nil {
		// ctx was cancelled or context expired — exit gracefully.
		if ctx.Err() != nil {
			return
		}
		log.Error("plc manager: failed to connect", slog.String("err", connectErr.Error()))
		return
	}

	log.Info("plc manager: connected")

	// Determine scan rate from config; default to 1 second if absent or unparseable.
	scanRate := time.Second
	if plcCfg.ScanRate != "" {
		if dur, err := time.ParseDuration(plcCfg.ScanRate); err == nil && dur > 0 {
			scanRate = dur
		}
	}

	ticker := time.NewTicker(scanRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Phase 1: no tag store yet — tick is a no-op / heartbeat.
			// Future phases will read configured tags here and publish to the store.
			if !d.Connected() {
				log.Warn("plc manager: not connected, attempting reconnect")
				if err := reconnect(ctx, d, log); err != nil {
					// Context cancelled — exit.
					return
				}
			}
		}
	}
}

// reconnect attempts to re-establish the driver connection via retry.Do.
// Returns nil when connected, or ctx.Err() when the context is cancelled.
func reconnect(ctx context.Context, d Driver, log *slog.Logger) error {
	if err := d.Close(); err != nil {
		log.Warn("plc manager: reconnect: Close error", slog.String("err", err.Error()))
	}

	return retry.Do(ctx, retry.Options{
		Initial:     time.Second,
		Max:         30 * time.Second,
		MaxAttempts: 0,
	}, func(ctx context.Context) error {
		return d.Connect(ctx)
	})
}
