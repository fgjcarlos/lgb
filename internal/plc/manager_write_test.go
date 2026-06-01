package plc_test

import (
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// ─── writeTrackingDriver extends trackingMockDriver to track WriteTag calls. ─

type writeTrackingDriver struct {
	trackingMockDriver
	writtenTag string
	writtenVal any
	writeErr   error
}

func (d *writeTrackingDriver) WriteTag(tag string, val any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writtenTag = tag
	d.writtenVal = val
	return d.writeErr
}

var _ plc.Driver = (*writeTrackingDriver)(nil)

// TestManager_WriteTag_Succeeds verifies that WriteTag("Silo-1", "Feed.Rate", 2.5)
// delegates to the driver and returns nil when the PLC exists.
// (Design §2, task 2.04)
func TestManager_WriteTag_Succeeds(t *testing.T) {
	t.Parallel()

	drv := &writeTrackingDriver{}
	factory := func(cfg config.PLC) plc.Driver { return drv }

	cfg := &config.Config{
		PLCs: []config.PLC{
			{Name: "Silo-1", Address: "127.0.0.1:44818",
				ScanRate: "1s", SocketTimeout: "1s"},
		},
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	if err := mgr.WriteTag("Silo-1", "Feed.Rate", 2.5); err != nil {
		t.Fatalf("WriteTag returned unexpected error: %v", err)
	}

	drv.mu.Lock()
	defer drv.mu.Unlock()
	if drv.writtenTag != "Feed.Rate" {
		t.Errorf("expected driver.WriteTag called with tag %q, got %q", "Feed.Rate", drv.writtenTag)
	}
	if drv.writtenVal != 2.5 {
		t.Errorf("expected driver.WriteTag called with val 2.5, got %v", drv.writtenVal)
	}
}

// TestManager_WriteTag_PLCNotFound verifies that WriteTag returns ErrPLCNotFound
// when the requested PLC is not in the manager.
// (Design §2, task 2.04)
func TestManager_WriteTag_PLCNotFound(t *testing.T) {
	t.Parallel()

	factory := func(cfg config.PLC) plc.Driver { return &writeTrackingDriver{} }

	cfg := &config.Config{
		PLCs: []config.PLC{
			{Name: "Silo-1", Address: "127.0.0.1:44818",
				ScanRate: "1s", SocketTimeout: "1s"},
		},
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	err := mgr.WriteTag("DoesNotExist", "Feed.Rate", 1.0)
	if err == nil {
		t.Fatal("expected error for unknown PLC, got nil")
	}
	if err != plc.ErrPLCNotFound {
		t.Errorf("expected plc.ErrPLCNotFound, got %v", err)
	}
}
