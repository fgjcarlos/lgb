package plc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	errs "github.com/fgjcarlos/lgb/internal/errors"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// ─── Mock Driver (interface contract) ───────────────────────────────────────

// mockDriver is a test implementation of the Driver interface.
// It is used to verify the interface contract is correctly defined.
type mockDriver struct {
	connected bool
}

func (m *mockDriver) Connect(_ context.Context) error         { m.connected = true; return nil }
func (m *mockDriver) Close() error                            { m.connected = false; return nil }
func (m *mockDriver) ReadTag(_ string, _ any) error           { return nil }
func (m *mockDriver) WriteTag(_ string, _ any) error          { return nil }
func (m *mockDriver) ReadMulti(_ []string, _ []any) error     { return nil }
func (m *mockDriver) Connected() bool                         { return m.connected }

// Compile-time assertion: *mockDriver must satisfy Driver.
var _ plc.Driver = (*mockDriver)(nil)

// TestMockDriverSatisfiesInterface verifies the interface is correctly defined.
// This test always passes once the Driver interface exists with the correct methods.
func TestMockDriverSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var d plc.Driver = &mockDriver{}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

// ─── Error re-export tests ───────────────────────────────────────────────────

// TestErrReExports verifies that error sentinels are re-exported from internal/plc
// and point to the same underlying sentinel values as internal/errors.
func TestErrReExports(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		plcErr error
		srcErr error
	}{
		{"ErrPLCConnect", plc.ErrPLCConnect, errs.ErrPLCConnect},
		{"ErrPLCRead", plc.ErrPLCRead, errs.ErrPLCRead},
		{"ErrPLCWrite", plc.ErrPLCWrite, errs.ErrPLCWrite},
		{"ErrPLCTimeout", plc.ErrPLCTimeout, errs.ErrPLCTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.plcErr == nil {
				t.Fatalf("%s: got nil, want non-nil", tc.name)
			}
			if !errors.Is(tc.plcErr, tc.srcErr) {
				t.Errorf("%s: plc.%s is not the same sentinel as errs.%s", tc.name, tc.name, tc.name)
			}
		})
	}
}

// ─── gologixDriver adapter tests ─────────────────────────────────────────────

// fakePLCClient implements the gologixClient interface for unit testing.
// All methods are controlled via fields and mutexes.
type fakePLCClient struct {
	mu          sync.Mutex
	connectErr  error
	disconnectErr error
	readErr     error
	writeErr    error
	connected   bool
	connectCalls int
}

func (f *fakePLCClient) Connect() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectCalls++
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connected = true
	return nil
}

func (f *fakePLCClient) Disconnect() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.disconnectErr != nil {
		return f.disconnectErr
	}
	f.connected = false
	return nil
}

func (f *fakePLCClient) Read(tag string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readErr
}

func (f *fakePLCClient) Write(tag string, val any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeErr
}

func (f *fakePLCClient) Connected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

// newTestDriver creates a gologixDriver with an injected fake client for unit tests.
func newTestDriver(fake *fakePLCClient, opts ...plc.Option) plc.Driver {
	cfg := config.PLC{
		Name:          "test",
		Address:       "127.0.0.1",
		Slot:          0,
		SocketTimeout: "5s",
		ScanRate:      "1s",
	}
	return plc.NewDriverWithClient(cfg, fake, opts...)
}

// TestNewDriverReturnsDriver verifies NewDriver returns a value assignable to Driver.
func TestNewDriverReturnsDriver(t *testing.T) {
	t.Parallel()
	cfg := config.PLC{
		Name:          "test",
		Address:       "127.0.0.1",
		SocketTimeout: "5s",
	}
	var d plc.Driver = plc.NewDriver(cfg)
	if d == nil {
		t.Fatal("NewDriver returned nil")
	}
}

// TestGologixDriver_ConnectedFalseBeforeConnect verifies initial state.
func TestGologixDriver_ConnectedFalseBeforeConnect(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)
	if d.Connected() {
		t.Error("expected Connected() == false before Connect()")
	}
}

// TestGologixDriver_ConnectedTrueAfterConnect verifies state transitions.
func TestGologixDriver_ConnectedTrueAfterConnect(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() returned error: %v", err)
	}
	if !d.Connected() {
		t.Error("expected Connected() == true after successful Connect()")
	}
}

// TestGologixDriver_Connect_CancelledContext verifies that a cancelled context
// causes Connect to return ctx.Err() and leaves Connected() == false.
func TestGologixDriver_Connect_CancelledContext(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := d.Connect(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if d.Connected() {
		t.Error("expected Connected() == false after cancelled Connect()")
	}
}

// TestGologixDriver_Disconnect_AfterConnect verifies disconnection state.
func TestGologixDriver_Disconnect_AfterConnect(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if d.Connected() {
		t.Error("expected Connected() == false after Close()")
	}
}

// TestGologixDriver_Disconnect_Idempotent verifies that calling Close twice
// does not panic and returns nil.
func TestGologixDriver_Disconnect_Idempotent(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}
	// Second call should not panic and return nil.
	if err := d.Close(); err != nil {
		t.Errorf("second Close() returned unexpected error: %v", err)
	}
}

// TestGologixDriver_ReadTag_BoolSliceNotMultipleOf32 verifies that a []bool
// with length not a multiple of 32 returns an error wrapping ErrPLCRead
// BEFORE calling the client.
func TestGologixDriver_ReadTag_BoolSliceNotMultipleOf32(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// len(10) is NOT a multiple of 32.
	dest := make([]bool, 10)
	err := d.ReadTag("BoolArray", dest)
	if err == nil {
		t.Fatal("expected error for []bool len=10, got nil")
	}
	if !errors.Is(err, errs.ErrPLCRead) {
		t.Errorf("expected error wrapping ErrPLCRead, got %v", err)
	}
}

// TestGologixDriver_ReadMulti_LengthMismatch verifies that ReadMulti returns
// ErrPLCRead when len(tags) != len(dests).
func TestGologixDriver_ReadMulti_LengthMismatch(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	var v1 int16
	var v2 float32
	// 2 tags, 1 dest — mismatch.
	err := d.ReadMulti([]string{"Tag1", "Tag2"}, []any{&v1})
	if err == nil {
		t.Fatal("expected error for len mismatch, got nil")
	}
	if !errors.Is(err, errs.ErrPLCRead) {
		t.Errorf("expected error wrapping ErrPLCRead, got %v", err)
	}
	_ = v2
}

// TestGologixDriver_Connected_ConcurrentSafe verifies that concurrent calls
// to Connected() do not cause data races (-race flag will catch violations).
func TestGologixDriver_Connected_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	fake := &fakePLCClient{}
	d := newTestDriver(fake)

	var wg sync.WaitGroup
	const goroutines = 20
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Connected()
		}()
	}
	wg.Wait()
}
