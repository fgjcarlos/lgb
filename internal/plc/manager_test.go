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

func TestManager_TagCallback_SkipsFailedReads(t *testing.T) {
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

	for _, u := range updates {
		if u.Tag == "Tag2" {
			t.Error("callback was called for Tag2 which should have failed")
		}
	}
	hasTag1 := false
	hasTag3 := false
	for _, u := range updates {
		if u.Tag == "Tag1" {
			hasTag1 = true
		}
		if u.Tag == "Tag3" {
			hasTag3 = true
		}
	}
	if !hasTag1 || !hasTag3 {
		t.Errorf("expected callbacks for Tag1 and Tag3; got Tag1=%v Tag3=%v", hasTag1, hasTag3)
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
