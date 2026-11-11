package plc_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// statusMockDriver is a Driver whose connect error, connected flag, read error
// and read blocking can be toggled at runtime for status-tracking tests.
type statusMockDriver struct {
	mu         sync.Mutex
	connected  bool
	connectErr error
	readErr    error
	block      chan struct{} // when non-nil, ReadTag waits on it
}

func (d *statusMockDriver) Connect(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.connectErr != nil {
		return d.connectErr
	}
	d.connected = true
	return nil
}

func (d *statusMockDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = false
	return nil
}

func (d *statusMockDriver) ReadTag(_ string, dest any) error {
	d.mu.Lock()
	err := d.readErr
	block := d.block
	d.mu.Unlock()
	if block != nil {
		<-block
	}
	if err != nil {
		return err
	}
	if p, ok := dest.(*float32); ok {
		*p = 1
	}
	return nil
}

func (d *statusMockDriver) WriteTag(_ string, _ any) error      { return nil }
func (d *statusMockDriver) ReadMulti(_ []string, _ []any) error { return nil }
func (d *statusMockDriver) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

func (d *statusMockDriver) set(fn func(d *statusMockDriver)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn(d)
}

var _ plc.Driver = (*statusMockDriver)(nil)

func startStatusManager(t *testing.T, d plc.Driver, scanRate string) *plc.Manager {
	t.Helper()
	mgr := plc.NewManager(onePLCFloatConfig(scanRate), nil, func(_ config.PLC) plc.Driver { return d }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = mgr.Stop()
	})
	return mgr
}

// waitStatus polls Manager.Status until cond holds or the deadline passes.
func waitStatus(t *testing.T, mgr *plc.Manager, cond func(plc.Status) bool) plc.Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, ok := mgr.Status("plc-a")
		if !ok {
			t.Fatal("Status(plc-a) not found")
		}
		if cond(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status condition not met; last status: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerStatus_UnknownPLC(t *testing.T) {
	t.Parallel()
	mgr := plc.NewManager(onePLCFloatConfig("50ms"), nil, func(_ config.PLC) plc.Driver { return &statusMockDriver{} }, nil)
	if _, ok := mgr.Status("nope"); ok {
		t.Fatal("Status(nope) ok = true, want false")
	}
}

func TestManagerStatus_BeforeStartIsNeverConnected(t *testing.T) {
	t.Parallel()
	mgr := plc.NewManager(onePLCFloatConfig("250ms"), nil, func(_ config.PLC) plc.Driver { return &statusMockDriver{} }, nil)
	st, ok := mgr.Status("plc-a")
	if !ok {
		t.Fatal("Status(plc-a) not found")
	}
	if st.Name != "plc-a" || st.State != plc.StateNeverConnected || st.ScanHealth != plc.ScanHealthNone {
		t.Fatalf("unexpected status %+v", st)
	}
	if st.ScanRate != 250*time.Millisecond {
		t.Fatalf("ScanRate = %v, want 250ms", st.ScanRate)
	}
	if !st.LastScan.IsZero() || !st.ConnectedSince.IsZero() || st.ScanCount != 0 || st.ReadCount != 0 {
		t.Fatalf("expected zero counters/timestamps, got %+v", st)
	}
}

func TestManagerStatus_ConnectFailureStaysNeverConnected(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{connectErr: errors.New("dial tcp: refused")}
	mgr := startStatusManager(t, d, "50ms")

	st := waitStatus(t, mgr, func(s plc.Status) bool { return s.ConnectErrorCount > 0 })
	if st.State != plc.StateNeverConnected {
		t.Fatalf("State = %q, want %q", st.State, plc.StateNeverConnected)
	}
	if !strings.Contains(st.LastError, "dial tcp: refused") || st.LastErrorAt.IsZero() {
		t.Fatalf("LastError = %q at %v, want the connect error", st.LastError, st.LastErrorAt)
	}
	if st.ScanCount != 0 || st.ScanHealth != plc.ScanHealthNone {
		t.Fatalf("expected no scans, got %+v", st)
	}
}

func TestManagerStatus_ConnectedScansAreCounted(t *testing.T) {
	t.Parallel()
	mgr := startStatusManager(t, &statusMockDriver{}, "20ms")

	st := waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 2 })
	if st.State != plc.StateConnected || st.ConnectedSince.IsZero() {
		t.Fatalf("expected connected with ConnectedSince, got %+v", st)
	}
	if st.LastScan.IsZero() || st.ScanHealth != plc.ScanHealthOK {
		t.Fatalf("expected a recent scan, got %+v", st)
	}
	if st.ReadCount < st.ScanCount || st.ReadErrorCount != 0 || st.LastError != "" {
		t.Fatalf("expected one clean read per scan, got %+v", st)
	}
}

func TestManagerStatus_ReadErrorsAreCounted(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{readErr: errors.New("tag not found")}
	mgr := startStatusManager(t, d, "20ms")

	st := waitStatus(t, mgr, func(s plc.Status) bool { return s.ReadErrorCount >= 2 })
	if st.ReadCount < st.ReadErrorCount {
		t.Fatalf("ReadCount %d < ReadErrorCount %d", st.ReadCount, st.ReadErrorCount)
	}
	if !strings.Contains(st.LastError, "tag not found") {
		t.Fatalf("LastError = %q, want the read error", st.LastError)
	}
}

func TestManagerStatus_LostConnectionIsDisconnected(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{}
	mgr := startStatusManager(t, d, "20ms")
	waitStatus(t, mgr, func(s plc.Status) bool { return s.State == plc.StateConnected && s.ScanCount >= 1 })

	d.set(func(d *statusMockDriver) {
		d.connected = false
		d.connectErr = errors.New("connection reset")
	})

	st := waitStatus(t, mgr, func(s plc.Status) bool { return s.State == plc.StateDisconnected })
	if !st.ConnectedSince.IsZero() {
		t.Fatalf("ConnectedSince = %v, want zero while disconnected", st.ConnectedSince)
	}
	// No scans run while reconnecting, so the old last scan must not read as healthy.
	waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanHealth == plc.ScanHealthStale })
	waitStatus(t, mgr, func(s plc.Status) bool { return strings.Contains(s.LastError, "connection reset") })

	d.set(func(d *statusMockDriver) { d.connectErr = nil })
	// retry backoff starts at 1s; allow for it.
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, _ := mgr.Status("plc-a")
		if st.State == plc.StateConnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not reconnect; last status %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagerStatus_StalledScanIsStale(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{}
	mgr := startStatusManager(t, d, "20ms")
	waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 1 })

	block := make(chan struct{})
	d.set(func(d *statusMockDriver) { d.block = block })
	defer close(block)

	waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanHealth == plc.ScanHealthStale })
}

func TestManagerStatus_ReloadResetsChangedAndDropsRemoved(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{}
	mgr := plc.NewManager(onePLCFloatConfig("20ms"), nil, func(_ config.PLC) plc.Driver { return d }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()
	waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 3 })

	// Changed config: the worker is replaced and its status starts over.
	changed := onePLCFloatConfig("250ms")
	if err := mgr.Reload(ctx, changed); err != nil {
		t.Fatalf("Reload(changed): %v", err)
	}
	st, ok := mgr.Status("plc-a")
	if !ok {
		t.Fatal("Status(plc-a) not found after Reload")
	}
	if st.ScanRate != 250*time.Millisecond || st.ScanCount != 0 || !st.LastScan.IsZero() {
		t.Fatalf("expected a fresh status at 250ms, got %+v", st)
	}

	// Removed PLC: status is no longer reported.
	if err := mgr.Reload(ctx, &config.Config{}); err != nil {
		t.Fatalf("Reload(empty): %v", err)
	}
	if _, ok := mgr.Status("plc-a"); ok {
		t.Fatal("Status(plc-a) ok = true after removal, want false")
	}
}
