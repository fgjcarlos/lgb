package plc

import (
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

func TestWorkerStatus_ScanLatencyRollingWindow(t *testing.T) {
	ws := newWorkerStatus(config.PLC{Name: "p", ScanRate: "1s"})
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	st := ws.snapshot(t0)
	if st.ScanLatencySamples != 0 || st.ScanLatencyAvg != 0 || st.ScanLatencyMax != 0 {
		t.Fatalf("no scans: got %+v", st)
	}

	// One slow scan followed by a full window of fast ones: the slow scan
	// rolls out, so max drops back to the fast duration.
	ws.recordScan(t0, t0.Add(100*time.Millisecond))
	st = ws.snapshot(t0)
	if st.ScanLatencySamples != 1 || st.ScanLatencyAvg != 100*time.Millisecond || st.ScanLatencyMax != 100*time.Millisecond {
		t.Fatalf("one scan: got avg %v max %v n %d", st.ScanLatencyAvg, st.ScanLatencyMax, st.ScanLatencySamples)
	}
	ws.recordScan(t0, t0.Add(10*time.Millisecond))
	st = ws.snapshot(t0)
	if st.ScanLatencyAvg != 55*time.Millisecond || st.ScanLatencyMax != 100*time.Millisecond || st.ScanLatencySamples != 2 {
		t.Fatalf("two scans: got avg %v max %v n %d", st.ScanLatencyAvg, st.ScanLatencyMax, st.ScanLatencySamples)
	}
	for i := 0; i < ScanLatencyWindow; i++ {
		ws.recordScan(t0, t0.Add(10*time.Millisecond))
	}
	st = ws.snapshot(t0)
	if st.ScanLatencySamples != ScanLatencyWindow || st.ScanLatencyAvg != 10*time.Millisecond || st.ScanLatencyMax != 10*time.Millisecond {
		t.Fatalf("full window: got avg %v max %v n %d", st.ScanLatencyAvg, st.ScanLatencyMax, st.ScanLatencySamples)
	}
	if st.ScanCount != uint64(ScanLatencyWindow)+2 {
		t.Fatalf("ScanCount = %d, want cumulative %d", st.ScanCount, ScanLatencyWindow+2)
	}
}
