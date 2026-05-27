package opcua_test

import (
	"context"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/opcua"
	"github.com/fgjcarlos/lgb/internal/plc"
)

type mockTagSource struct {
	tags map[string]map[string]plc.TagValue
}

func (m *mockTagSource) CurrentTag(plcName, tag string) (plc.TagValue, bool) {
	if m.tags == nil {
		return plc.TagValue{}, false
	}
	if tags, ok := m.tags[plcName]; ok {
		v, ok := tags[tag]
		return v, ok
	}
	return plc.TagValue{}, false
}

func (m *mockTagSource) CurrentSnapshot() map[string]map[string]plc.TagValue {
	return m.tags
}

func TestNew_ReturnsNonNil(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Port: 0},
	}
	srv := opcua.New(cfg, &mockTagSource{}, nil)
	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestServer_StartStop(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    0,
		},
		PLCs: []config.PLC{
			{
				Name: "sim",
				Tags: []config.TagDef{
					{Name: "Motor.Speed", Type: "Float"},
					{Name: "Valve.Open", Type: "Boolean"},
				},
			},
		},
	}

	tags := &mockTagSource{
		tags: map[string]map[string]plc.TagValue{
			"sim": {
				"Motor.Speed": {Value: float32(1200.5), Timestamp: time.Now(), Quality: "good"},
				"Valve.Open":  {Value: true, Timestamp: time.Now(), Quality: "good"},
			},
		},
	}

	srv := opcua.New(cfg, tags, nil)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give the server time to initialize and bind.
	time.Sleep(500 * time.Millisecond)

	// Cancel ctx first — this is the primary shutdown signal for gopcua.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("Start returned (expected after cancel): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return within 10s after context cancellation")
	}

	// Stop should be idempotent and clean.
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func TestServer_StopBeforeStart(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0},
	}

	srv := opcua.New(cfg, &mockTagSource{}, nil)

	// Stop on a never-started server should be a no-op.
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop on unstarted server returned error: %v", err)
	}
}
