//go:build integration

package plc_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/danomagnum/gologix"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// cipServerAddr is the well-known EtherNet/IP port used by gologix.Server.Serve().
// Integration tests bind to this port — only one test suite may run at a time.
const cipServerAddr = "127.0.0.1:44818"

// startRealCIPSim starts a full gologix CIP server (TCP + router) in-process.
// It uses gologix.Server.Serve() which binds to the EtherNet/IP port (44818).
// It blocks until the port is available, then runs until t.Cleanup fires.
//
// All gologix integration tests MUST call this function rather than the
// lightweight TCP stub in testutil.StartPLCSim.
func startRealCIPSim(t *testing.T) string {
	t.Helper()

	// Verify the port is reachable before we try to bind.
	// If something else holds 44818, skip rather than fail.
	ln, err := net.Listen("tcp", cipServerAddr)
	if err != nil {
		t.Skipf("startRealCIPSim: port %s not available: %v — run tests on a clean machine", cipServerAddr, err)
	}
	ln.Close() // Release the probe listener; gologix.Serve will rebind.

	provider := testutil.NewPLCSimProvider()
	router := gologix.NewRouter()

	path, err := gologix.ParsePath("1,0")
	if err != nil {
		t.Fatalf("startRealCIPSim: ParsePath: %v", err)
	}
	router.Handle(path.Bytes(), provider)

	srv := gologix.NewServer(router)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		// Serve binds 0.0.0.0:44818 (TCP) and 0.0.0.0:2222 (UDP) internally.
		// The error on clean shutdown (listener closed) is ignored.
		_ = srv.Serve()
	}()

	// Wait for the server to accept connections.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, dialErr := net.DialTimeout("tcp", cipServerAddr, 100*time.Millisecond)
		if dialErr == nil {
			c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Cleanup(func() {
		if srv.TCPListener != nil {
			srv.TCPListener.Close()
		}
		if srv.UDPListener != nil {
			srv.UDPListener.Close()
		}
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
		}
	})

	return cipServerAddr
}

// newIntegrationDriver creates a Driver pointing at the given CIP server address.
func newIntegrationDriver(t *testing.T, addr string) plc.Driver {
	t.Helper()
	cfg := config.PLC{
		Name:          "sim",
		Address:       addr,
		Slot:          0,
		SocketTimeout: "5s",
		ScanRate:      "1s",
	}
	return plc.NewDriver(cfg)
}

// ─── T-3.03: gologix integration tests ─────────────────────────────────────

// TestIntegration_ReadTagScalar verifies that ReadTag correctly reads the
// canonical scalar tags from the plcsim: SimBool (bool), SimInt (int16),
// SimFloat (float32).
func TestIntegration_ReadTagScalar(t *testing.T) {
	addr := startRealCIPSim(t)
	d := newIntegrationDriver(t, addr)

	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer d.Close()

	t.Run("SimBool", func(t *testing.T) {
		var b bool
		if err := d.ReadTag("SimBool", &b); err != nil {
			t.Fatalf("ReadTag SimBool: %v", err)
		}
		if !b {
			t.Errorf("ReadTag SimBool: got %v, want true", b)
		}
	})

	t.Run("SimInt", func(t *testing.T) {
		var i int16
		if err := d.ReadTag("SimInt", &i); err != nil {
			t.Fatalf("ReadTag SimInt: %v", err)
		}
		if i != 42 {
			t.Errorf("ReadTag SimInt: got %d, want 42", i)
		}
	})

	t.Run("SimFloat", func(t *testing.T) {
		var f float32
		if err := d.ReadTag("SimFloat", &f); err != nil {
			t.Fatalf("ReadTag SimFloat: %v", err)
		}
		if f != float32(3.14) {
			t.Errorf("ReadTag SimFloat: got %v, want 3.14", f)
		}
	})
}

// TestIntegration_WriteTag verifies that WriteTag persists a value that can
// then be read back via ReadTag.
func TestIntegration_WriteTag(t *testing.T) {
	addr := startRealCIPSim(t)
	d := newIntegrationDriver(t, addr)

	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer d.Close()

	// Write a new value.
	want := float32(9.9)
	if err := d.WriteTag("SimFloat", want); err != nil {
		t.Fatalf("WriteTag SimFloat: %v", err)
	}

	// Read it back and verify.
	var got float32
	if err := d.ReadTag("SimFloat", &got); err != nil {
		t.Fatalf("ReadTag SimFloat after write: %v", err)
	}
	if got != want {
		t.Errorf("ReadTag SimFloat: got %v, want %v", got, want)
	}
}

// TestIntegration_ReadMulti verifies that ReadMulti reads SimBool, SimInt, and
// SimFloat in a single call and returns the correct values.
func TestIntegration_ReadMulti(t *testing.T) {
	addr := startRealCIPSim(t)
	d := newIntegrationDriver(t, addr)

	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer d.Close()

	var b bool
	var i int16
	var f float32

	tags := []string{"SimBool", "SimInt", "SimFloat"}
	dests := []any{&b, &i, &f}

	if err := d.ReadMulti(tags, dests); err != nil {
		t.Fatalf("ReadMulti: %v", err)
	}

	if !b {
		t.Errorf("ReadMulti SimBool: got %v, want true", b)
	}
	if i != 42 {
		t.Errorf("ReadMulti SimInt: got %d, want 42", i)
	}
	if f != float32(3.14) {
		t.Errorf("ReadMulti SimFloat: got %v, want 3.14", f)
	}
}

// TestIntegration_ConnectRetry verifies that Connect retries when the server
// is temporarily unavailable and eventually exhausts retries with an error.
func TestIntegration_ConnectRetry(t *testing.T) {
	// Point at a port that has no listener — should retry and fail.
	cfg := config.PLC{
		Name:          "sim-retry",
		Address:       "127.0.0.1:19999", // nothing listening here
		Slot:          0,
		SocketTimeout: "500ms",
		ScanRate:      "1s",
	}
	d := plc.NewDriver(cfg,
		plc.WithRetryInitial(100*time.Millisecond),
		plc.WithRetryMax(300*time.Millisecond),
		plc.WithMaxAttempts(3),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect should exhaust retries and return a non-nil error.
	err := d.Connect(ctx)
	if err == nil {
		t.Error("Connect to closed port should fail, got nil")
	}
	t.Logf("Connect retry exhausted as expected: %v", err)
}

// TestIntegration_ConcurrentReads verifies that 10 goroutines can call ReadTag
// concurrently without data races (enforced by -race flag).
func TestIntegration_ConcurrentReads(t *testing.T) {
	addr := startRealCIPSim(t)
	d := newIntegrationDriver(t, addr)

	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer d.Close()

	const goroutines = 10
	var wg sync.WaitGroup
	readErrs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			var b bool
			readErrs[idx] = d.ReadTag("SimBool", &b)
		}()
	}
	wg.Wait()

	for i, err := range readErrs {
		if err != nil {
			t.Errorf("goroutine[%d] ReadTag error: %v", i, err)
		}
	}
}
