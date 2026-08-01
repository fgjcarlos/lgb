package plc

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/retry"
)

// TagUpdate represents a single tag read from a PLC scan tick.
// Quality indicates the read status: "good" for a successful read,
// "bad" for a failed read. The zero value "" is treated as "good"
// everywhere (backward compatible with existing producers).
type TagUpdate struct {
	PLCName   string
	Tag       string
	Value     any
	Timestamp time.Time
	Quality   string
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
	done   chan struct{} // closed by runWorker when it returns
}

// Manager owns the lifecycle of all PLC Drivers: start, stop, lookup, and
// hot-reload. It is safe for concurrent use.
//
// Design §4, §6.3, §6.4 — PLC-DRV-2.1, PLC-DRV-2.2, PLC-DRV-2.3.
type Manager struct {
	log       *slog.Logger
	factory   DriverFactory
	tagCb     TagCallback
	callbacks []TagCallback

	mu       sync.RWMutex
	workers  map[string]*plcWorker // keyed by PLC name
	current  map[string]map[string]TagValue
	wg       sync.WaitGroup
	stopped  bool // set to true after Stop; guards Reload-after-Stop

	// reloadMu serializes concurrent Reload calls.
	// Lock order: reloadMu (outer, Reload-only) → mu (inner).
	// Stop takes only mu, so there is no inverse path and no deadlock.
	reloadMu sync.Mutex

	// warnedTags tracks tag names for which we've already logged an unknown-type warning.
	// Populated by AllocDest when encountering an unknown type. Used to ensure we log
	// the warning exactly once per tag name, not on every scan tick.
	warnedTags sync.Map // map[string]struct{} — keyed by tag name
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
		m.workers[plcCfg.Name] = &plcWorker{driver: d, cfg: plcCfg, done: make(chan struct{})}
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
		w.done = make(chan struct{})

		// Capture loop variables before goroutine launch.
		workerName := name
		d := w.driver
		plcCfg := w.cfg
		doneCh := w.done

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer close(doneCh)
			m.runWorker(plcCtx, workerName, d, plcCfg)
		}()
	}

	return nil
}

// Stop cancels all per-PLC goroutines, calls Close on each driver, and waits
// for all goroutines to exit. It is safe to call Stop more than once.
func (m *Manager) Stop() error {
	m.mu.Lock()
	m.stopped = true
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

// Reload applies a new configuration hot. It uses a field-level diff
// (reflect.DeepEqual on the full config.PLC including Tags) to decide which
// workers need draining:
//
//   - Removed PLCs (name no longer in cfg): drained and closed.
//   - Changed PLCs (name present but config differs): drained and re-added.
//   - Unchanged PLCs: left untouched, no restart.
//   - New PLCs (name not yet in workers): added.
//
// Concurrent Reload calls are serialized by reloadMu. Reload after Stop is a
// safe no-op. Only drained workers are waited on; m.wg (owned by Stop) is
// never waited inside Reload.
//
// The parent ctx must be the same context passed to Start.
// Design §6.3 (hot-reload sequence), PLC-DRV-2.3, R67-1–R67-4.
func (m *Manager) Reload(ctx context.Context, cfg *config.Config) error {
	// Serialize concurrent Reload calls. Lock order: reloadMu → m.mu.
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	// Reload-after-Stop is a safe no-op (R67-4).
	m.mu.RLock()
	isStopped := m.stopped
	m.mu.RUnlock()
	if isStopped {
		return nil
	}

	// Build a quick lookup of the incoming config by name.
	newCfgByName := make(map[string]config.PLC, len(cfg.PLCs))
	for _, plcCfg := range cfg.PLCs {
		newCfgByName[plcCfg.Name] = plcCfg
	}

	m.mu.Lock()

	// Collect workers to drain: removed PLCs or changed PLCs.
	// Changed PLCs are deleted here and re-added in the add-loop below,
	// since their names will be missing from m.workers at that point.
	var toDrain []string
	for name, w := range m.workers {
		newCfg, exists := newCfgByName[name]
		if !exists {
			// Removed — drain only.
			toDrain = append(toDrain, name)
		} else if !reflect.DeepEqual(w.cfg, newCfg) {
			// Changed — drain old worker; the add-loop will re-create it.
			toDrain = append(toDrain, name)
		}
	}

	// Cancel drained workers and collect their done channels.
	// Done channels are read here under m.mu so they cannot be replaced
	// by a concurrent Start; we wait them outside the lock below.
	drainedDrivers := make([]Driver, 0, len(toDrain))
	drainedDone := make([]chan struct{}, 0, len(toDrain))
	for _, name := range toDrain {
		w := m.workers[name]
		if w.cancel != nil {
			w.cancel()
		}
		drainedDrivers = append(drainedDrivers, w.driver)
		drainedDone = append(drainedDone, w.done)
		delete(m.workers, name)
		delete(m.current, name)
	}

	m.mu.Unlock()

	// Wait ONLY for drained workers' goroutines to exit (R67-2, R67-3).
	// m.wg is NOT waited here — it is owned exclusively by Stop.
	for _, doneCh := range drainedDone {
		<-doneCh
	}

	// Close drained drivers after goroutines have stopped.
	for _, d := range drainedDrivers {
		if err := d.Close(); err != nil {
			m.log.Warn("plc manager: Reload: Close error",
				slog.String("err", err.Error()))
		}
	}

	// Add new PLCs and re-add changed PLCs (their names were deleted above).
	m.mu.Lock()
	for _, plcCfg := range cfg.PLCs {
		if _, exists := m.workers[plcCfg.Name]; !exists {
			d := m.factory(plcCfg)
			plcCtx, cancel := context.WithCancel(ctx)
			doneCh := make(chan struct{})
			w := &plcWorker{driver: d, cfg: plcCfg, cancel: cancel, done: doneCh}
			m.workers[plcCfg.Name] = w

			workerName := plcCfg.Name
			capturedCfg := plcCfg
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				defer close(doneCh)
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

	// lastQuality tracks the most recently emitted quality per tag so that
	// quality-only transitions (R70-4, R70-5) are always emitted.
	lastQuality := make(map[string]string, len(plcCfg.Tags))

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
			dest := m.AllocDest(tag.Type, tag.Name)
			if dest == nil {
				// Unknown type: AllocDest already logged warning once.
				// Emit bad-quality update and skip this tag's read.
				if lastQuality[tag.Name] != "bad" {
					m.emitTagUpdate(TagUpdate{
						PLCName:   name,
						Tag:       tag.Name,
						Value:     nil,
						Timestamp: time.Now(),
						Quality:   "bad",
					})
					lastQuality[tag.Name] = "bad"
				}
				continue
			}
			if err := d.ReadTag(tag.Name, dest); err != nil {
				log.Warn("plc manager: ReadTag error",
					slog.String("tag", tag.Name),
					slog.String("err", err.Error()))
				// R70-1: emit a bad-quality update instead of silently skipping.
				// R70-5: only emit on quality transition (good→bad or first bad).
				if lastQuality[tag.Name] != "bad" {
					m.emitTagUpdate(TagUpdate{
						PLCName:   name,
						Tag:       tag.Name,
						Value:     nil,
						Timestamp: time.Now(),
						Quality:   "bad",
					})
					lastQuality[tag.Name] = "bad"
				}
				continue
			}
				value := deref(dest)
				// Determine quality for this successful read.
				quality := "good"
				prev := lastQuality[tag.Name]
				// R70-4/R70-5: always emit when quality transitions from bad to good,
				// even if the value is numerically unchanged.
				if prev == "bad" || prev == "" {
					// First read or recovery: emit unconditionally.
					update := TagUpdate{
						PLCName:   name,
						Tag:       tag.Name,
						Value:     value,
						Timestamp: time.Now(),
						Quality:   quality,
					}
					m.emitTagUpdate(update)
					lastQuality[tag.Name] = quality
				} else {
					// Steady state: emit normally.
					update := TagUpdate{
						PLCName:   name,
						Tag:       tag.Name,
						Value:     value,
						Timestamp: time.Now(),
						Quality:   quality,
					}
					m.emitTagUpdate(update)
					lastQuality[tag.Name] = quality
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

func (m *Manager) emitTagUpdate(update TagUpdate) {
	m.storeTag(update)

	m.mu.RLock()
	callbacks := make([]TagCallback, 0, len(m.callbacks)+1)
	if m.tagCb != nil {
		callbacks = append(callbacks, m.tagCb)
	}
	callbacks = append(callbacks, m.callbacks...)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		cb(update)
	}
}

// AddTagCallback registers an additional callback for future scanned tag
// updates. It is safe to call before or after Start.
func (m *Manager) AddTagCallback(cb TagCallback) {
	if cb == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, cb)
}

// WriteTag writes val to the named tag on the named PLC by delegating to the
// underlying driver. It acquires a read lock to look up the worker, then calls
// driver.WriteTag outside the lock (driver serializes its own I/O).
//
// Returns ErrPLCNotFound when the named PLC is not registered in this Manager.
// Any driver-level error is returned as-is.
//
// Requirements: Design §2, task 2.05.
func (m *Manager) WriteTag(plcName, tag string, val any) error {
	m.mu.RLock()
	w, ok := m.workers[plcName]
	m.mu.RUnlock()
	if !ok {
		return ErrPLCNotFound
	}
	return w.driver.WriteTag(tag, val)
}

func (m *Manager) storeTag(update TagUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current[update.PLCName] == nil {
		m.current[update.PLCName] = make(map[string]TagValue)
	}
	q := update.Quality
	if q == "" {
		q = "good"
	}
	m.current[update.PLCName][update.Tag] = TagValue{
		Value:     update.Value,
		Timestamp: update.Timestamp,
		Quality:   q,
	}
}

// AllocDest returns a typed pointer for the given tag type and tag name.
// For known types, it returns a typed pointer (e.g., *bool, *int32, *float64).
// For unknown types, it logs a warning exactly once per tag name and returns nil.
// The caller must handle nil returns by skipping the ReadTag and emitting bad-quality.
func (m *Manager) AllocDest(typeName, tagName string) any {
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
		// Unknown type: log warning once per tag name.
		if _, loaded := m.warnedTags.LoadOrStore(tagName, struct{}{}); !loaded {
			m.log.Warn("unknown tag type",
				slog.String("tag", tagName),
				slog.String("type", typeName),
			)
		}
		return nil
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
