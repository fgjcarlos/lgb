package plc

import (
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

// Connection states reported by Status.State.
const (
	// StateNeverConnected: the worker has not connected yet (not started, or
	// still retrying the initial connection).
	StateNeverConnected = "never_connected"
	// StateConnected: the driver is connected and the scan loop is running.
	StateConnected = "connected"
	// StateDisconnected: the driver was connected before and lost the
	// connection; the worker is reconnecting.
	StateDisconnected = "disconnected"
)

// Scan health values reported by Status.ScanHealth.
const (
	// ScanHealthNone: no scan has completed yet.
	ScanHealthNone = "none"
	// ScanHealthOK: the last scan completed within staleScanFactor × ScanRate.
	ScanHealthOK = "ok"
	// ScanHealthStale: the PLC is connected but the last scan completed more
	// than staleScanFactor × ScanRate ago (stalled reads).
	ScanHealthStale = "stale"
)

// staleScanFactor is how many scan periods may pass without a completed scan
// before a connected PLC is reported as stale.
const staleScanFactor = 3

// defaultScanRate is used when a PLC has no (or an invalid) scan_rate.
const defaultScanRate = time.Second

// Status is a point-in-time snapshot of one PLC worker's operational state.
// Zero time values mean "never".
type Status struct {
	Name              string
	State             string
	ScanRate          time.Duration
	ScanHealth        string
	ConnectedSince    time.Time
	LastScan          time.Time
	LastScanDuration  time.Duration
	ScanCount         uint64
	ReadCount         uint64
	ReadErrorCount    uint64
	ConnectErrorCount uint64
	LastError         string
	LastErrorAt       time.Time
}

// workerStatus is the mutable status owned by one plcWorker. It has its own
// mutex (a leaf lock: never held while acquiring Manager.mu).
type workerStatus struct {
	mu sync.Mutex
	st Status
}

func newWorkerStatus(cfg config.PLC) *workerStatus {
	return &workerStatus{st: Status{
		Name:     cfg.Name,
		State:    StateNeverConnected,
		ScanRate: scanRateOf(cfg),
	}}
}

// scanRateOf parses cfg.ScanRate, falling back to defaultScanRate.
func scanRateOf(cfg config.PLC) time.Duration {
	if cfg.ScanRate != "" {
		if dur, err := time.ParseDuration(cfg.ScanRate); err == nil && dur > 0 {
			return dur
		}
	}
	return defaultScanRate
}

func (ws *workerStatus) setConnected(now time.Time) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.st.State = StateConnected
	ws.st.ConnectedSince = now
}

// setDisconnected marks a previously connected worker as disconnected.
func (ws *workerStatus) setDisconnected() {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.st.State == StateConnected {
		ws.st.State = StateDisconnected
	}
	ws.st.ConnectedSince = time.Time{}
}

func (ws *workerStatus) recordConnectError(err error, now time.Time) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.st.ConnectErrorCount++
	ws.st.LastError = err.Error()
	ws.st.LastErrorAt = now
}

func (ws *workerStatus) recordRead(err error, now time.Time) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.st.ReadCount++
	if err != nil {
		ws.st.ReadErrorCount++
		ws.st.LastError = err.Error()
		ws.st.LastErrorAt = now
	}
}

func (ws *workerStatus) recordScan(start, end time.Time) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.st.ScanCount++
	ws.st.LastScan = end
	ws.st.LastScanDuration = end.Sub(start)
}

// snapshot returns a copy with ScanHealth derived at time now.
func (ws *workerStatus) snapshot(now time.Time) Status {
	ws.mu.Lock()
	st := ws.st
	ws.mu.Unlock()

	switch {
	case st.LastScan.IsZero():
		st.ScanHealth = ScanHealthNone
	case st.State == StateConnected && now.Sub(st.LastScan) > staleScanFactor*st.ScanRate:
		st.ScanHealth = ScanHealthStale
	default:
		st.ScanHealth = ScanHealthOK
	}
	return st
}

// Status returns a snapshot of the named PLC's operational state. ok is false
// when the PLC is not managed (unknown or removed by Reload).
func (m *Manager) Status(name string) (Status, bool) {
	m.mu.RLock()
	w, ok := m.workers[name]
	m.mu.RUnlock()
	if !ok || w.status == nil {
		return Status{}, false
	}
	return w.status.snapshot(time.Now()), true
}
