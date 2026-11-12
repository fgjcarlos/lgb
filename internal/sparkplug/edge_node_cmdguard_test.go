package sparkplug_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
)

// syncBuffer is a goroutine-safe log sink for slog assertions.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newGuardTestNode(mc *mockMQTTClient, log *slog.Logger) *sparkplug.EdgeNode {
	return sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
		Log:     log,
	})
}

func findDCMDHandler(t *testing.T, mc *mockMQTTClient) mqtt.MessageHandler {
	t.Helper()
	for _, s := range mc.getSubscriptions() {
		if s.Topic == "spBv1.0/plant-a/DCMD/lgb-1/+" {
			return s.Handler
		}
	}
	t.Fatal("DCMD subscription not found after Start")
	return nil
}

// TestEdgeNode_SetCommandHandler_AfterStart_Panics verifies the structural
// ordering guard (#62 WARNING-2): wiring the DCMD handler after Start is a
// programming error and must fail loudly instead of silently leaving DCMD
// unenforced.
func TestEdgeNode_SetCommandHandler_AfterStart_Panics(t *testing.T) {
	t.Parallel()

	mc := &mockMQTTClient{}
	en := newGuardTestNode(mc, nil)
	if err := en.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("SetCommandHandler after Start did not panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "SetCommandHandler called after Start") {
			t.Errorf("panic = %v; want message containing %q", r, "SetCommandHandler called after Start")
		}
	}()
	en.SetCommandHandler(func(string, string, any) {})
}

// TestEdgeNode_SetCommandHandler_AfterStop_Allowed verifies that the guard is
// scoped to a running node: after Stop the handler may be rewired before the
// next Start.
func TestEdgeNode_SetCommandHandler_AfterStop_Allowed(t *testing.T) {
	t.Parallel()

	mc := &mockMQTTClient{}
	en := newGuardTestNode(mc, nil)
	if err := en.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := en.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetCommandHandler after Stop panicked: %v", r)
		}
	}()
	en.SetCommandHandler(func(string, string, any) {})
}

// TestEdgeNode_Start_WithoutCommandHandler_Warns verifies that starting with no
// DCMD handler is allowed (no write guard configured is a legitimate setup) but
// is surfaced as a warning, and that Start still succeeds.
func TestEdgeNode_Start_WithoutCommandHandler_Warns(t *testing.T) {
	t.Parallel()

	var logs syncBuffer
	mc := &mockMQTTClient{}
	en := newGuardTestNode(mc, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := en.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "no DCMD command handler set") {
		t.Errorf("logs = %q; want a WARN containing %q", out, "no DCMD command handler set")
	}
}

// TestEdgeNode_Start_WithCommandHandler_NoWarning verifies the warning is not
// emitted when the handler is wired before Start (the production path).
func TestEdgeNode_Start_WithCommandHandler_NoWarning(t *testing.T) {
	t.Parallel()

	var logs syncBuffer
	mc := &mockMQTTClient{}
	en := newGuardTestNode(mc, slog.New(slog.NewTextHandler(&logs, nil)))
	en.SetCommandHandler(func(string, string, any) {})
	if err := en.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	if out := logs.String(); strings.Contains(out, "no DCMD command handler set") {
		t.Errorf("logs = %q; unexpected missing-handler warning", out)
	}
}

// TestEdgeNode_SetCommandHandler_AfterFailedStart_Allowed verifies that a Start
// that fails to connect does not leave the ordering guard armed (#62).
func TestEdgeNode_SetCommandHandler_AfterFailedStart_Allowed(t *testing.T) {
	t.Parallel()

	mc := &mockMQTTClient{connectBlock: make(chan struct{})}
	en := newGuardTestNode(mc, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := en.Start(ctx); err == nil {
		t.Fatal("expected Start to fail on a cancelled connect")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetCommandHandler after failed Start panicked: %v", r)
		}
	}()
	en.SetCommandHandler(func(string, string, any) {})
}
