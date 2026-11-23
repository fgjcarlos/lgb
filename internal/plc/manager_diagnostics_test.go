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

func TestManagerStatus_WritesAreCounted(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{}
	mgr := plc.NewManager(onePLCFloatConfig("1s"), nil, func(_ config.PLC) plc.Driver { return d }, nil)

	if err := mgr.WriteTag("plc-a", "Pressure", float32(1)); err != nil {
		t.Fatalf("WriteTag ok: %v", err)
	}
	d.set(func(d *statusMockDriver) { d.writeErr = errors.New("CIP general status 0x05") })
	if err := mgr.WriteTag("plc-a", "Pressure", float32(2)); err == nil {
		t.Fatal("WriteTag: want the driver error")
	}
	// An unknown PLC has no worker: nothing to count.
	if err := mgr.WriteTag("ghost", "Pressure", float32(3)); !errors.Is(err, plc.ErrPLCNotFound) {
		t.Fatalf("WriteTag(ghost) = %v, want ErrPLCNotFound", err)
	}

	st, _ := mgr.Status("plc-a")
	if st.WriteCount != 2 || st.WriteErrorCount != 1 {
		t.Fatalf("WriteCount/WriteErrorCount = %d/%d, want 2/1", st.WriteCount, st.WriteErrorCount)
	}
	if !strings.Contains(st.LastError, "CIP general status 0x05") || st.LastErrorAt.IsZero() {
		t.Fatalf("LastError = %q at %v, want the write error", st.LastError, st.LastErrorAt)
	}
}

func TestManagerStatus_ScanLatencyIsReported(t *testing.T) {
	t.Parallel()
	mgr := startStatusManager(t, &statusMockDriver{}, "20ms")

	st := waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 3 })
	if st.ScanLatencySamples < 1 || st.ScanLatencySamples > plc.ScanLatencyWindow {
		t.Fatalf("ScanLatencySamples = %d, want 1..%d", st.ScanLatencySamples, plc.ScanLatencyWindow)
	}
	if st.ScanLatencyMax < st.ScanLatencyAvg || st.ScanLatencyMax < st.LastScanDuration {
		t.Fatalf("inconsistent latency: avg %v max %v last %v", st.ScanLatencyAvg, st.ScanLatencyMax, st.LastScanDuration)
	}
}

func TestManagerStatus_CountersSurviveReconnect(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{}
	mgr := startStatusManager(t, d, "20ms")
	before := waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 2 })
	_ = mgr.WriteTag("plc-a", "Pressure", float32(1))

	d.set(func(d *statusMockDriver) {
		d.connected = false
		d.connectErr = errors.New("connection reset")
	})
	waitStatus(t, mgr, func(s plc.Status) bool { return s.ConnectErrorCount >= 1 })
	d.set(func(d *statusMockDriver) { d.connectErr = nil })

	deadline := time.Now().Add(3 * time.Second)
	for {
		st, _ := mgr.Status("plc-a")
		if st.State == plc.StateConnected && st.ScanCount > before.ScanCount {
			if st.ConnectErrorCount < 1 || st.WriteCount != 1 || st.ReadCount <= before.ReadCount {
				t.Fatalf("counters reset by reconnect: before %+v after %+v", before, st)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not reconnect and scan; last status %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagerStatus_ReloadKeepsUnchangedCounters(t *testing.T) {
	t.Parallel()
	d := &statusMockDriver{}
	cfg := onePLCFloatConfig("20ms")
	mgr := plc.NewManager(cfg, nil, func(_ config.PLC) plc.Driver { return d }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()
	before := waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 3 })

	if err := mgr.Reload(ctx, onePLCFloatConfig("20ms")); err != nil {
		t.Fatalf("Reload(unchanged): %v", err)
	}
	st, _ := mgr.Status("plc-a")
	if st.ScanCount < before.ScanCount || st.ConnectedSince != before.ConnectedSince {
		t.Fatalf("unchanged PLC status reset by Reload: before %+v after %+v", before, st)
	}
}

func TestManagerStatuses_AllPLCsSortedByName(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{PLCs: []config.PLC{
		{Name: "zeta", Address: "127.0.0.1:44818", ScanRate: "1s"},
		{Name: "alpha", Address: "127.0.0.1:44818", ScanRate: "1s"},
	}}
	mgr := plc.NewManager(cfg, nil, func(_ config.PLC) plc.Driver { return &statusMockDriver{} }, nil)

	got := mgr.Statuses()
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("Statuses() = %+v, want alpha, zeta", got)
	}
	if got[0].State != plc.StateNeverConnected {
		t.Fatalf("State = %q, want never_connected", got[0].State)
	}

	empty := plc.NewManager(&config.Config{}, nil, nil, nil)
	if got := empty.Statuses(); got == nil || len(got) != 0 {
		t.Fatalf("Statuses() on empty manager = %#v, want empty non-nil slice", got)
	}
}

// TestManagerStatus_ConcurrentWritesAndReads runs WriteTag and Status readers
// against a live scan loop; under -race it pins the status lock discipline.
func TestManagerStatus_ConcurrentWritesAndReads(t *testing.T) {
	t.Parallel()
	mgr := startStatusManager(t, &statusMockDriver{}, "1ms")
	waitStatus(t, mgr, func(s plc.Status) bool { return s.ScanCount >= 1 })

	const workers, perWorker = 8, 200
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				_ = mgr.WriteTag("plc-a", "Pressure", float32(j))
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				_, _ = mgr.Status("plc-a")
				_ = mgr.Statuses()
			}
		}()
	}
	wg.Wait()

	if st, _ := mgr.Status("plc-a"); st.WriteCount != workers*perWorker {
		t.Fatalf("WriteCount = %d, want %d", st.WriteCount, workers*perWorker)
	}
}
