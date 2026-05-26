package plc

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/retry"
)

// TagUpdate represents a single tag read from a PLC scan tick.
type TagUpdate struct {
	PLCName   string
	Tag       string
	Value     any
	Timestamp time.Time
}

// TagValue is the in-memory current value for one PLC tag.
type TagValue struct {
	Value     any
	Timestamp time.Time
	Quality   string
}

// TagCallback is invoked by the scan loop for each successful tag read.
type TagCallback func(update TagUpdate)

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
	tagCb   TagCallback

	mu      sync.RWMutex
	workers map[string]*plcWorker // keyed by PLC name
	current map[string]map[string]TagValue
	wg      sync.WaitGroup
}

// NewManager constructs a Manager and eagerly creates one Driver per PLC entry
// in cfg using factory. If factory is nil, NewDriver is used.
//
// Start must be called before any tag operations.
func NewManager(cfg *config.Config, log *slog.Logger, factory DriverFactory, tagCb TagCallback) *Manager {
	if log == nil {
		log = slog.Default()
	}
	if factory == nil {
		factory = defaultDriverFactory
	}

	m := &Manager{
		log:     log,
		factory: factory,
		tagCb:   tagCb,
		workers: make(map[string]*plcWorker, len(cfg.PLCs)),
		current: make(map[string]map[string]TagValue, len(cfg.PLCs)),
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

// CurrentTag returns the latest scanned value for a PLC tag.
func (m *Manager) CurrentTag(plcName, tag string) (TagValue, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tags, ok := m.current[plcName]
	if !ok {
		return TagValue{}, false
	}
	value, ok := tags[tag]
	return value, ok
}

// CurrentSnapshot returns a defensive copy of the full in-memory tag store.
func (m *Manager) CurrentSnapshot() map[string]map[string]TagValue {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[string]map[string]TagValue, len(m.current))
	for plcName, tags := range m.current {
		tagCopy := make(map[string]TagValue, len(tags))
		for tag, value := range tags {
			tagCopy[tag] = value
		}
		snapshot[plcName] = tagCopy
	}
	return snapshot
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
		delete(m.current, name)
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
			if !d.Connected() {
				log.Warn("plc manager: not connected, attempting reconnect")
				if err := reconnect(ctx, d, log); err != nil {
					return
				}
			}
			for _, tag := range plcCfg.Tags {
				dest := allocDest(tag.Type)
				if err := d.ReadTag(tag.Name, dest); err != nil {
					log.Warn("plc manager: ReadTag error",
						slog.String("tag", tag.Name),
						slog.String("err", err.Error()))
					continue
				}
				value := deref(dest)
				update := TagUpdate{
					PLCName:   name,
					Tag:       tag.Name,
					Value:     value,
					Timestamp: time.Now(),
				}
				m.storeTag(update)
				if m.tagCb != nil {
					m.tagCb(update)
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

func (m *Manager) storeTag(update TagUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current[update.PLCName] == nil {
		m.current[update.PLCName] = make(map[string]TagValue)
	}
	m.current[update.PLCName][update.Tag] = TagValue{
		Value:     update.Value,
		Timestamp: update.Timestamp,
		Quality:   "good",
	}
}

func allocDest(typeName string) any {
	switch typeName {
	case "Boolean":
		return new(bool)
	case "Int8":
		return new(int8)
	case "Int16":
		return new(int16)
	case "Int32":
		return new(int32)
	case "Int64":
		return new(int64)
	case "UInt8":
		return new(uint8)
	case "UInt16":
		return new(uint16)
	case "UInt32":
		return new(uint32)
	case "UInt64":
		return new(uint64)
	case "Float":
		return new(float32)
	case "Double":
		return new(float64)
	case "String":
		return new(string)
	default:
		return new(any)
	}
}

func deref(ptr any) any {
	switch p := ptr.(type) {
	case *bool:
		return *p
	case *int8:
		return *p
	case *int16:
		return *p
	case *int32:
		return *p
	case *int64:
		return *p
	case *uint8:
		return *p
	case *uint16:
		return *p
	case *uint32:
		return *p
	case *uint64:
		return *p
	case *float32:
		return *p
	case *float64:
		return *p
	case *string:
		return *p
	default:
		return ptr
	}
}
