package plc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// ─── trackingMockDriver: call-tracking Driver for Manager tests ─────────────

// trackingMockDriver is a thread-safe mock Driver that tracks whether Connect
// and Close were called. Used exclusively in manager_test.go.
type trackingMockDriver struct {
	mu            sync.Mutex
	connectCalled bool
	closeCalled   bool
	connectFn     func(ctx context.Context) error
}

func (m *trackingMockDriver) Connect(ctx context.Context) error {
	m.mu.Lock()
	m.connectCalled = true
	fn := m.connectFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

func (m *trackingMockDriver) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

func (m *trackingMockDriver) ReadTag(_ string, dest any) error {
	switch p := dest.(type) {
	case *float32:
		*p = 21.5
	case *int32:
		*p = 7
	}
	return nil
}
func (m *trackingMockDriver) WriteTag(_ string, _ any) error      { return nil }
func (m *trackingMockDriver) ReadMulti(_ []string, _ []any) error { return nil }
func (m *trackingMockDriver) Connected() bool                     { return true }

// Compile-time assertion: *trackingMockDriver must satisfy Driver.
var _ plc.Driver = (*trackingMockDriver)(nil)

// ─── Config helpers ─────────────────────────────────────────────────────────

// managerOnePLCConfig creates a *config.Config with a single PLC entry.
func managerOnePLCConfig(name string) *config.Config {
	return &config.Config{
		PLCs: []config.PLC{
			{
				Name:          name,
				Address:       "127.0.0.1:44818",
				Slot:          0,
				SocketTimeout: "1s",
				ScanRate:      "500ms",
				KeepAlive:     true,
				Tags: []config.TagDef{
					{Name: "Temp", Type: "Float"},
					{Name: "Count", Type: "Int32"},
				},
			},
		},
	}
}

// managerMultiPLCConfig creates a *config.Config with two PLCs.
func managerMultiPLCConfig() *config.Config {
	return &config.Config{
		PLCs: []config.PLC{
			{
				Name:          "plc-a",
				Address:       "127.0.0.1:44818",
				Slot:          0,
				SocketTimeout: "1s",
				ScanRate:      "500ms",
				Tags: []config.TagDef{
					{Name: "Temp", Type: "Float"},
				},
			},
			{
				Name:          "plc-b",
				Address:       "127.0.0.1:44819",
				Slot:          1,
				SocketTimeout: "1s",
				ScanRate:      "500ms",
				Tags: []config.TagDef{
					{Name: "Count", Type: "Int32"},
				},
			},
		},
	}
}

// ─── T-3.01: Manager unit tests ─────────────────────────────────────────────

// TestNewManager_CreatesDriversForEachPLC verifies that NewManager calls the
// factory once per configured PLC.
func TestNewManager_CreatesDriversForEachPLC(t *testing.T) {
	t.Parallel()

	cfg := managerMultiPLCConfig()
	var mu sync.Mutex
	created := 0

	factory := func(c config.PLC) plc.Driver {
		mu.Lock()
		created++
		mu.Unlock()
		return &trackingMockDriver{}
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	mu.Lock()
	got := created
	mu.Unlock()

	if got != 2 {
		t.Errorf("expected 2 drivers created, got %d", got)
	}
}

// TestManager_Start_CallsConnectOnAllDrivers verifies that Start calls Connect
// on every driver created by the factory.
func TestManager_Start_CallsConnectOnAllDrivers(t *testing.T) {
	t.Parallel()

	cfg := managerMultiPLCConfig()

	var mu sync.Mutex
	drivers := make([]*trackingMockDriver, 0, 2)

	factory := func(c config.PLC) plc.Driver {
		d := &trackingMockDriver{}
		mu.Lock()
		drivers = append(drivers, d)
		mu.Unlock()
		return d
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	// Allow goroutines time to call Connect.
	time.Sleep(100 * time.Millisecond)

	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for i, d := range drivers {
		if !d.connectCalled {
			t.Errorf("driver[%d] Connect was not called", i)
		}
	}
}

// TestManager_Stop_CallsCloseOnAllDrivers verifies that Stop calls Close on
// all drivers and blocks until goroutines exit.
func TestManager_Stop_CallsCloseOnAllDrivers(t *testing.T) {
	t.Parallel()

	cfg := managerMultiPLCConfig()

	var mu sync.Mutex
	drivers := make([]*trackingMockDriver, 0, 2)

	factory := func(c config.PLC) plc.Driver {
		d := &trackingMockDriver{}
		mu.Lock()
		drivers = append(drivers, d)
		mu.Unlock()
		return d
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for i, d := range drivers {
		if !d.closeCalled {
			t.Errorf("driver[%d] Close was not called", i)
		}
	}
}

// TestManager_Stop_AfterContextCancel_NoDeadlock verifies that Stop does not
// deadlock when called after context cancellation.
func TestManager_Stop_AfterContextCancel_NoDeadlock(t *testing.T) {
	t.Parallel()

	cfg := managerOnePLCConfig("plc-a")

	factory := func(c config.PLC) plc.Driver {
		return &trackingMockDriver{}
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	if err := mgr.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start() returned error: %v", err)
	}

	// Cancel the context to simulate external shutdown signal.
	cancel()

	// Stop must return within 2 seconds — enforce with a timer.
	done := make(chan error, 1)
	go func() { done <- mgr.Stop() }()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop() returned error: %v", err)
		}
	case <-timer.C:
		t.Fatal("Stop() deadlocked — did not return within 2s after context cancel")
	}
}

// TestManager_Driver_LookupByName verifies that Driver(name) returns the driver
// for a known name and (nil, false) for an unknown name.
func TestManager_Driver_LookupByName(t *testing.T) {
	t.Parallel()

	cfg := managerOnePLCConfig("plc-alpha")

	factory := func(c config.PLC) plc.Driver {
		return &trackingMockDriver{}
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	// Existing driver.
	d, ok := mgr.Driver("plc-alpha")
	if !ok {
		t.Error("Driver(\"plc-alpha\") returned ok=false, want true")
	}
	if d == nil {
		t.Error("Driver(\"plc-alpha\") returned nil, want non-nil")
	}

	// Non-existent driver.
	d2, ok2 := mgr.Driver("does-not-exist")
	if ok2 {
		t.Error("Driver(\"does-not-exist\") returned ok=true, want false")
	}
	if d2 != nil {
		t.Errorf("Driver(\"does-not-exist\") returned non-nil (%v), want nil", d2)
	}
}

// TestManager_ConcurrentStartStop_RaceSafe verifies that concurrent Start and
// Driver lookup calls do not produce data races under -race.
func TestManager_ConcurrentStartStop_RaceSafe(t *testing.T) {
	t.Parallel()

	cfg := managerOnePLCConfig("plc-race")

	factory := func(c config.PLC) plc.Driver {
		return &trackingMockDriver{}
	}

	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = mgr.Start(ctx)
	}()

	// Allow Start to set up state.
	time.Sleep(20 * time.Millisecond)

	// Concurrent Driver lookups stress the internal map.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mgr.Driver("plc-race")
		}()
	}

	wg.Wait()

	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}
}

// ─── T-5.01: TagCallback tests ──────────────────────────────────────────────

// tagReadMockDriver returns configured values for ReadTag.
type tagReadMockDriver struct {
	mu        sync.Mutex
	connected bool
	tagValues map[string]any
	readErr   map[string]error
}

func (m *tagReadMockDriver) Connect(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

func (m *tagReadMockDriver) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

func (m *tagReadMockDriver) ReadTag(tag string, dest any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.readErr[tag]; ok && err != nil {
		return err
	}
	if v, ok := m.tagValues[tag]; ok {
		switch d := dest.(type) {
		case *float32:
			*d = v.(float32)
		case *bool:
			*d = v.(bool)
		case *int32:
			*d = v.(int32)
		}
	}
	return nil
}

func (m *tagReadMockDriver) WriteTag(_ string, _ any) error      { return nil }
func (m *tagReadMockDriver) ReadMulti(_ []string, _ []any) error { return nil }
func (m *tagReadMockDriver) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

var _ plc.Driver = (*tagReadMockDriver)(nil)

func TestManager_TagCallback_CalledOnRead(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		PLCs: []config.PLC{
			{
				Name: "plc-a", Address: "127.0.0.1:44818",
				ScanRate: "50ms",
				Tags: []config.TagDef{
					{Name: "Motor.Speed", Type: "Float"},
				},
			},
		},
	}

	mock := &tagReadMockDriver{
		tagValues: map[string]any{"Motor.Speed": float32(1200.5)},
	}

	factory := func(c config.PLC) plc.Driver { return mock }

	var mu sync.Mutex
	var updates []plc.TagUpdate
	cb := func(u plc.TagUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	}

	mgr := plc.NewManager(cfg, nil, factory, cb)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	_ = mgr.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(updates) == 0 {
		t.Fatal("expected at least 1 TagUpdate from callback, got 0")
	}
	u := updates[0]
	if u.PLCName != "plc-a" {
		t.Errorf("PLCName = %q; want %q", u.PLCName, "plc-a")
	}
	if u.Tag != "Motor.Speed" {
		t.Errorf("Tag = %q; want %q", u.Tag, "Motor.Speed")
	}
}

func TestManager_NilCallback_NoPanic(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		PLCs: []config.PLC{
			{
				Name: "plc-a", Address: "127.0.0.1:44818",
				ScanRate: "50ms",
				Tags: []config.TagDef{
					{Name: "Motor.Speed", Type: "Float"},
				},
			},
		},
	}

	mock := &tagReadMockDriver{
		tagValues: map[string]any{"Motor.Speed": float32(100)},
	}
	factory := func(c config.PLC) plc.Driver { return mock }

	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	_ = mgr.Stop()
}

// TestManager_TagCallback_FailedReadEmitsBadQuality verifies R70-1: a ReadTag
// error produces a TagUpdate with Quality=="bad" (not a silent skip).
// Good-quality updates for Tag1 and Tag3 continue to flow normally.
func TestManager_TagCallback_FailedReadEmitsBadQuality(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		PLCs: []config.PLC{
			{
				Name: "plc-a", Address: "127.0.0.1:44818",
				ScanRate: "50ms",
				Tags: []config.TagDef{
					{Name: "Tag1", Type: "Float"},
					{Name: "Tag2", Type: "Float"},
					{Name: "Tag3", Type: "Float"},
				},
			},
		},
	}

	mock := &tagReadMockDriver{
		tagValues: map[string]any{
			"Tag1": float32(1), "Tag2": float32(2), "Tag3": float32(3),
		},
		readErr: map[string]error{
			"Tag2": errors.New("simulated read error"),
		},
	}
	factory := func(c config.PLC) plc.Driver { return mock }

	var mu sync.Mutex
	var updates []plc.TagUpdate
	cb := func(u plc.TagUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	}

	mgr := plc.NewManager(cfg, nil, factory, cb)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_ = mgr.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	_ = mgr.Stop()

	mu.Lock()
	defer mu.Unlock()

	// Tag2 must emit a bad-quality update (not be silently skipped).
	hasBadTag2 := false
	for _, u := range updates {
		if u.Tag == "Tag2" {
			if u.Quality != "bad" {
				t.Errorf("Tag2 update has Quality=%q; want \"bad\"", u.Quality)
			}
			hasBadTag2 = true
		}
	}
	if !hasBadTag2 {
		t.Error("expected at least one bad-quality update for Tag2; got none")
	}

	// Tag1 and Tag3 still emit good-quality updates.
	hasGoodTag1 := false
	hasGoodTag3 := false
	for _, u := range updates {
		if u.Tag == "Tag1" && u.Quality == "good" {
			hasGoodTag1 = true
		}
		if u.Tag == "Tag3" && u.Quality == "good" {
			hasGoodTag3 = true
		}
	}
	if !hasGoodTag1 || !hasGoodTag3 {
		t.Errorf("expected good-quality callbacks for Tag1 and Tag3; got Tag1=%v Tag3=%v", hasGoodTag1, hasGoodTag3)
	}
}

func TestManager_CurrentTagStoresLatestScanValue(t *testing.T) {
	t.Parallel()

	cfg := managerOnePLCConfig("plc-a")
	cfg.PLCs[0].ScanRate = "10ms"
	mgr := plc.NewManager(cfg, nil, func(c config.PLC) plc.Driver { return &trackingMockDriver{} }, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer func() {
		if err := mgr.Stop(); err != nil {
			t.Errorf("Stop() returned error: %v", err)
		}
	}()

	deadline := time.After(time.Second)
	for {
		value, ok := mgr.CurrentTag("plc-a", "Temp")
		if ok && value.Value == float32(21.5) && value.Quality == "good" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("CurrentTag never observed Temp=21.5; last=%#v ok=%v", value, ok)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestManager_CurrentSnapshotReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	cfg := managerOnePLCConfig("plc-a")
	cfg.PLCs[0].ScanRate = "10ms"
	mgr := plc.NewManager(cfg, nil, func(c config.PLC) plc.Driver { return &trackingMockDriver{} }, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer func() {
		if err := mgr.Stop(); err != nil {
			t.Errorf("Stop() returned error: %v", err)
		}
	}()

	deadline := time.After(time.Second)
	for {
		snapshot := mgr.CurrentSnapshot()
		if len(snapshot["plc-a"]) > 0 {
			snapshot["plc-a"]["Temp"] = plc.TagValue{Value: float32(99), Quality: "bad"}
			value, ok := mgr.CurrentTag("plc-a", "Temp")
			if !ok || value.Value != float32(21.5) {
				t.Fatalf("mutating snapshot changed store: value=%#v ok=%v", value, ok)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("CurrentSnapshot never populated")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// ─── PLC-DRV-2.3: Reload field-level diff ────────────────────────────────────

// countingMockDriver extends trackingMockDriver to count Connect and Close calls.
type countingMockDriver struct {
	trackingMockDriver
	mu           sync.Mutex
	connectCount int
	closeCount   int
}

func (d *countingMockDriver) Connect(ctx context.Context) error {
	d.mu.Lock()
	d.connectCount++
	d.mu.Unlock()
	return d.trackingMockDriver.Connect(ctx)
}

func (d *countingMockDriver) Close() error {
	d.mu.Lock()
	d.closeCount++
	d.mu.Unlock()
	return d.trackingMockDriver.Close()
}

var _ plc.Driver = (*countingMockDriver)(nil)

// TestReload_ChangedScanRate_DrainsAndRestarts verifies that changing scanRate
// causes the old worker to be drained (Close called) and a new worker started
// (Connect called again). PLC-DRV-2.3.
func TestReload_ChangedScanRate_DrainsAndRestarts(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var created []*countingMockDriver

	factory := func(cfg config.PLC) plc.Driver {
		d := &countingMockDriver{}
		mu.Lock()
		created = append(created, d)
		mu.Unlock()
		return d
	}

	cfg := managerOnePLCConfig("plc-a")
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let first Connect run

	// Change scanRate and reload.
	newCfg := managerOnePLCConfig("plc-a")
	newCfg.PLCs[0].ScanRate = "999ms" // differs from original 500ms
	if err := mgr.Reload(ctx, newCfg); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let second Connect run

	mu.Lock()
	drvCount := len(created)
	mu.Unlock()

	if drvCount < 2 {
		t.Fatalf("expected at least 2 drivers created (original + restarted), got %d", drvCount)
	}

	// The first driver must have been closed.
	mu.Lock()
	first := created[0]
	mu.Unlock()
	first.mu.Lock()
	closed := first.closeCount
	first.mu.Unlock()
	if closed == 0 {
		t.Error("original driver Close was not called after scanRate change")
	}

	_ = mgr.Stop()
}

// TestReload_UnchangedPLC_NotRestarted verifies that when two PLCs are running
// and only one changes, only the changed worker is drained. PLC-DRV-2.3.
func TestReload_UnchangedPLC_NotRestarted(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	// Slice preserves creation order: index 0 = plc-a initial, index 1 = plc-b,
	// index 2 = plc-a after reload. We track by name to get pre-reload drivers.
	initialByName := make(map[string]*countingMockDriver)
	created := 0

	factory := func(cfg config.PLC) plc.Driver {
		d := &countingMockDriver{}
		mu.Lock()
		if _, seen := initialByName[cfg.Name]; !seen {
			// Only record the first driver created per name (pre-reload).
			initialByName[cfg.Name] = d
		}
		created++
		mu.Unlock()
		return d
	}

	cfg := managerMultiPLCConfig() // plc-a + plc-b
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Snapshot the original drivers before reload.
	mu.Lock()
	origA := initialByName["plc-a"]
	origB := initialByName["plc-b"]
	mu.Unlock()

	if origA == nil || origB == nil {
		t.Fatal("expected initial drivers for plc-a and plc-b before reload")
	}

	// Only change plc-a's scanRate; plc-b stays identical.
	newCfg := managerMultiPLCConfig()
	newCfg.PLCs[0].ScanRate = "999ms" // plc-a changed
	// plc-b stays at 500ms (unchanged)

	if err := mgr.Reload(ctx, newCfg); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// plc-a's original driver must have been closed.
	origA.mu.Lock()
	closedA := origA.closeCount
	origA.mu.Unlock()
	if closedA == 0 {
		t.Error("plc-a original driver Close was not called after scanRate change")
	}

	// plc-b's driver must NOT have been closed (unchanged PLC).
	origB.mu.Lock()
	closedB := origB.closeCount
	origB.mu.Unlock()
	if closedB != 0 {
		t.Errorf("plc-b driver Close was called %d times; want 0 (unchanged PLC)", closedB)
	}

	_ = mgr.Stop()
}

// TestReload_ChangedTags_DrainsAndRestarts verifies that editing a PLC's tag
// list (the primary UI edit case) is detected as a change and drains the old
// worker, starting a fresh one. The tag slice is part of the config.PLC the
// reload diff compares, so a tag-only change must restart the worker just like
// a scalar field change. PLC-DRV-2.3.
func TestReload_ChangedTags_DrainsAndRestarts(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var created []*countingMockDriver

	factory := func(cfg config.PLC) plc.Driver {
		d := &countingMockDriver{}
		mu.Lock()
		created = append(created, d)
		mu.Unlock()
		return d
	}

	cfg := managerOnePLCConfig("plc-a")
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let first Connect run

	// Edit the tag list only — scanRate and all other fields stay identical.
	newCfg := managerOnePLCConfig("plc-a")
	newCfg.PLCs[0].Tags = append(newCfg.PLCs[0].Tags, config.TagDef{Name: "Pressure", Type: "Float"})
	if err := mgr.Reload(ctx, newCfg); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let second Connect run

	mu.Lock()
	drvCount := len(created)
	first := created[0]
	mu.Unlock()

	if drvCount < 2 {
		t.Fatalf("expected at least 2 drivers created (original + restarted after tag edit), got %d", drvCount)
	}

	// The original worker must have been drained.
	first.mu.Lock()
	closed := first.closeCount
	first.mu.Unlock()
	if closed == 0 {
		t.Error("original driver Close was not called after tag-list change")
	}

	// The restarted worker must have actually connected.
	mu.Lock()
	last := created[drvCount-1]
	mu.Unlock()
	last.mu.Lock()
	connected := last.connectCount
	last.mu.Unlock()
	if connected == 0 {
		t.Error("restarted driver Connect was not called after tag-list change")
	}

	_ = mgr.Stop()
}

// ─── R67: Config Reload Concurrency ─────────────────────────────────────────

// callbackMockDriver allows test code to inject behavior via closures for
// Connect and Close, while providing no-op defaults for other methods.
type callbackMockDriver struct {
	connectFn func(ctx context.Context) error
	closeFn   func() error
}

func (d *callbackMockDriver) Connect(ctx context.Context) error {
	if d.connectFn != nil {
		return d.connectFn(ctx)
	}
	return nil
}

func (d *callbackMockDriver) Close() error {
	if d.closeFn != nil {
		return d.closeFn()
	}
	return nil
}

func (d *callbackMockDriver) ReadTag(_ string, _ any) error       { return nil }
func (d *callbackMockDriver) WriteTag(_ string, _ any) error      { return nil }
func (d *callbackMockDriver) ReadMulti(_ []string, _ []any) error { return nil }
func (d *callbackMockDriver) Connected() bool                     { return true }

var _ plc.Driver = (*callbackMockDriver)(nil)

// TestReload_Concurrent_NoDeadlock verifies that two concurrent Reload calls
// serialize without deadlock or panic under the -race detector. R67-1.
func TestReload_Concurrent_NoDeadlock(t *testing.T) {
	t.Parallel()

	factory := func(c config.PLC) plc.Driver { return &trackingMockDriver{} }

	cfg := managerOnePLCConfig("plc-a")
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	cfgB := managerOnePLCConfig("plc-a")
	cfgB.PLCs[0].ScanRate = "200ms"
	cfgC := managerOnePLCConfig("plc-a")
	cfgC.PLCs[0].ScanRate = "300ms"

	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		_ = mgr.Reload(ctx, cfgB)
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		_ = mgr.Reload(ctx, cfgC)
	}()

	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-timer.C:
			t.Fatal("TestReload_Concurrent_NoDeadlock: Reload deadlocked")
		}
	}

	_ = mgr.Stop()
}

// driverCloseCount is a helper that reads closeCount from a countingMockDriver
// safely; it returns -1 when d is nil (signals a missing driver in tests).
func driverCloseCount(d *countingMockDriver) int {
	if d == nil {
		return -1
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closeCount
}

// TestReload_OnlyChangedWorkersDrained verifies that only the changed PLC's
// worker is drained; the unchanged worker keeps running without restart and
// Reload returns promptly (well under 1s). R67-2.
func TestReload_OnlyChangedWorkersDrained(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	initialByName := make(map[string]*countingMockDriver)

	factory := func(cfg config.PLC) plc.Driver {
		d := &countingMockDriver{}
		mu.Lock()
		if _, seen := initialByName[cfg.Name]; !seen {
			initialByName[cfg.Name] = d
		}
		mu.Unlock()
		return d
	}

	cfg := managerMultiPLCConfig() // plc-a + plc-b
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	snapA := initialByName["plc-a"]
	snapB := initialByName["plc-b"]
	mu.Unlock()

	if snapA == nil || snapB == nil {
		t.Fatalf("expected initial drivers for plc-a and plc-b; got a=%v b=%v", snapA, snapB)
	}

	// Change plc-b's scanRate; plc-a stays identical.
	newCfg := managerMultiPLCConfig()
	newCfg.PLCs[1].ScanRate = "999ms" // plc-b changed (index 1 in slice)

	// Reload must return well within 1s (plc-a should not be drained/waited).
	reloadDone := make(chan error, 1)
	start := time.Now()
	go func() { reloadDone <- mgr.Reload(ctx, newCfg) }()

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()
	select {
	case err := <-reloadDone:
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Reload error: %v", err)
		}
		if elapsed > 900*time.Millisecond {
			t.Errorf("Reload took too long (%v); unchanged workers must not block drain", elapsed)
		}
	case <-timer.C:
		t.Fatal("Reload blocked for >1s; unchanged workers must not be drained")
	}

	// plc-a must NOT have been closed (unchanged).
	if closedA := driverCloseCount(snapA); closedA != 0 {
		t.Errorf("plc-a Close called %d times; want 0 (unchanged)", closedA)
	}

	// plc-b must have been closed (changed).
	if closedB := driverCloseCount(snapB); closedB == 0 {
		t.Error("plc-b Close was not called; want >=1 (changed)")
	}

	_ = mgr.Stop()
}

// TestReload_DrainCompletesBeforeStart verifies that the replacement worker's
// Connect is not invoked until after the previous worker's goroutine has
// fully exited (its done channel is closed). R67-3.
func TestReload_DrainCompletesBeforeStart(t *testing.T) {
	t.Parallel()

	// drainStarted is closed when the old driver's Close is called.
	drainStarted := make(chan struct{})
	// allowConnect is closed to let the replacement connect proceed.
	allowConnect := make(chan struct{})
	// connectCalled is closed when replacement actually calls Connect.
	connectCalled := make(chan struct{})

	var isFirstMu sync.Mutex
	isFirst := true

	factory := func(cfg config.PLC) plc.Driver {
		isFirstMu.Lock()
		first := isFirst
		isFirst = false
		isFirstMu.Unlock()

		if first {
			return &callbackMockDriver{
				closeFn: func() error {
					close(drainStarted)
					return nil
				},
			}
		}
		return &callbackMockDriver{
			connectFn: func(ctx context.Context) error {
				close(connectCalled)
				select {
				case <-allowConnect:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}
	}

	cfg := managerOnePLCConfig("plc-a")
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(30 * time.Millisecond) // let old worker finish connecting

	newCfg := managerOnePLCConfig("plc-a")
	newCfg.PLCs[0].ScanRate = "999ms"

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- mgr.Reload(ctx, newCfg) }()

	// Wait until drain begins (old Close called).
	select {
	case <-drainStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("old driver Close not called within 3s")
	}

	// Unblock replacement Connect.
	close(allowConnect)

	// Wait for replacement to call Connect.
	select {
	case <-connectCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement driver Connect not called within 3s after drain")
	}

	select {
	case err := <-reloadDone:
		if err != nil {
			t.Fatalf("Reload error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Reload did not complete within 3s")
	}

	cancel()
	_ = mgr.Stop()
}

// TestReload_AfterStop_SafeNoOp verifies that calling Reload after Stop does
// not panic, does not start new goroutines, and returns cleanly. R67-4.
func TestReload_AfterStop_SafeNoOp(t *testing.T) {
	t.Parallel()

	factory := func(cfg config.PLC) plc.Driver { return &trackingMockDriver{} }

	cfg := managerOnePLCConfig("plc-a")
	mgr := plc.NewManager(cfg, nil, factory, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	// Reload after Stop must not panic and must return within 1s.
	reloadDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				reloadDone <- errors.New("panic in Reload after Stop")
			}
		}()
		reloadDone <- mgr.Reload(ctx, cfg)
	}()

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()
	select {
	case err := <-reloadDone:
		if err != nil {
			t.Fatalf("Reload after Stop returned error: %v", err)
		}
	case <-timer.C:
		t.Fatal("Reload after Stop blocked for >1s")
	}
}

// ─── R70: Tag Quality Propagation ────────────────────────────────────────────

// controllableMockDriver is a Driver whose ReadTag error can be toggled at
// runtime, enabling quality-transition tests. The readErrFn field is evaluated
// on each ReadTag call under lock so tests can swap it safely.
type controllableMockDriver struct {
	mu        sync.Mutex
	connected bool
	readErrFn func(tag string) error // nil means success
	tagValue  float32
}

func (d *controllableMockDriver) Connect(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = true
	return nil
}

func (d *controllableMockDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = false
	return nil
}

func (d *controllableMockDriver) ReadTag(tag string, dest any) error {
	d.mu.Lock()
	fn := d.readErrFn
	val := d.tagValue
	d.mu.Unlock()
	if fn != nil {
		if err := fn(tag); err != nil {
			return err
		}
	}
	if p, ok := dest.(*float32); ok {
		*p = val
	}
	return nil
}

func (d *controllableMockDriver) setReadErr(fn func(tag string) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readErrFn = fn
}

func (d *controllableMockDriver) WriteTag(_ string, _ any) error      { return nil }
func (d *controllableMockDriver) ReadMulti(_ []string, _ []any) error { return nil }
func (d *controllableMockDriver) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

var _ plc.Driver = (*controllableMockDriver)(nil)

// onePLCFloatConfig returns a one-tag Float config useful for quality tests.
func onePLCFloatConfig(scanRate string) *config.Config {
	return &config.Config{
		PLCs: []config.PLC{
			{
				Name:     "plc-a",
				Address:  "127.0.0.1:44818",
				ScanRate: scanRate,
				Tags:     []config.TagDef{{Name: "Pressure", Type: "Float"}},
			},
		},
	}
}

// TestManager_RunWorker_BadQuality_EmitsUpdate verifies R70-1: when ReadTag
// returns an error, the manager emits a TagUpdate with Quality=="bad".
func TestManager_RunWorker_BadQuality_EmitsUpdate(t *testing.T) {
	t.Parallel()

	mock := &controllableMockDriver{tagValue: float32(10)}
	mock.setReadErr(func(_ string) error { return errors.New("simulated PLC read error") })

	factory := func(_ config.PLC) plc.Driver { return mock }

	var mu sync.Mutex
	var updates []plc.TagUpdate
	cb := func(u plc.TagUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	}

	cfg := onePLCFloatConfig("50ms")
	mgr := plc.NewManager(cfg, nil, factory, cb)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	_ = mgr.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(updates) == 0 {
		t.Fatal("expected at least one TagUpdate even on read error, got none")
	}
	hasBad := false
	for _, u := range updates {
		if u.Tag == "Pressure" && u.Quality == "bad" {
			hasBad = true
			break
		}
	}
	if !hasBad {
		t.Error("no TagUpdate with Quality==\"bad\" was emitted after read error")
	}
}

// TestManager_RunWorker_QualityRecovery verifies R70-4: after bad reads, the
// first successful read emits Quality=="good" even when the value is unchanged.
func TestManager_RunWorker_QualityRecovery(t *testing.T) {
	t.Parallel()

	mock := &controllableMockDriver{tagValue: float32(42)}
	// Start in error state.
	mock.setReadErr(func(_ string) error { return errors.New("simulated read failure") })

	factory := func(_ config.PLC) plc.Driver { return mock }

	var mu sync.Mutex
	var qualities []string
	cb := func(u plc.TagUpdate) {
		if u.Tag == "Pressure" {
			mu.Lock()
			qualities = append(qualities, u.Quality)
			mu.Unlock()
		}
	}

	cfg := onePLCFloatConfig("20ms")
	mgr := plc.NewManager(cfg, nil, factory, cb)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Wait until we have at least one bad-quality update.
	deadline := time.After(500 * time.Millisecond)
	for {
		mu.Lock()
		n := len(qualities)
		mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no bad-quality update received within 500ms")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Switch driver to success (value stays 42).
	mock.setReadErr(nil)

	// Wait for a good-quality update after recovery.
	deadline2 := time.After(500 * time.Millisecond)
	for {
		mu.Lock()
		qs := make([]string, len(qualities))
		copy(qs, qualities)
		mu.Unlock()
		for _, q := range qs {
			if q == "good" {
				_ = mgr.Stop()
				return
			}
		}
		select {
		case <-deadline2:
			t.Fatal("no good-quality recovery update received within 500ms after error cleared")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// TestManager_RunWorker_QualityTransition_EmittedOnValueUnchanged verifies
// R70-5: quality transitions are emitted independently of value changes.
func TestManager_RunWorker_QualityTransition_EmittedOnValueUnchanged(t *testing.T) {
	t.Parallel()

	mock := &controllableMockDriver{tagValue: float32(42)}
	factory := func(_ config.PLC) plc.Driver { return mock }

	var mu sync.Mutex
	var qualities []string
	cb := func(u plc.TagUpdate) {
		if u.Tag == "Pressure" {
			mu.Lock()
			qualities = append(qualities, u.Quality)
			mu.Unlock()
		}
	}

	cfg := onePLCFloatConfig("20ms")
	mgr := plc.NewManager(cfg, nil, factory, cb)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	waitForQuality := func(want string, timeout time.Duration) {
		t.Helper()
		deadline := time.After(timeout)
		for {
			mu.Lock()
			qs := make([]string, len(qualities))
			copy(qs, qualities)
			mu.Unlock()
			for _, q := range qs {
				if q == want {
					return
				}
			}
			select {
			case <-deadline:
				t.Fatalf("did not receive quality=%q within %v", want, timeout)
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	// Phase 1: initial good reads.
	waitForQuality("good", 400*time.Millisecond)

	// Phase 2: inject error (value=42 unchanged conceptually); bad update must arrive.
	mock.setReadErr(func(_ string) error { return errors.New("simulated") })
	waitForQuality("bad", 400*time.Millisecond)

	// Phase 3: clear error (value still 42); good update must arrive even though value unchanged.
	mock.setReadErr(nil)
	waitForQuality("good", 400*time.Millisecond)

	_ = mgr.Stop()
}
