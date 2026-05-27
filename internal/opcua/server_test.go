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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give the server time to start.
	time.Sleep(200 * time.Millisecond)

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("Start returned (expected after stop): %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return within 3s after Stop")
	}
}

func TestServer_DoubleStart_ReturnsError(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0},
	}

	srv := opcua.New(cfg, &mockTagSource{}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = srv.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)
	defer func() { _ = srv.Stop() }()

	err := srv.Start(ctx)
	if err == nil {
		t.Error("expected error on double Start, got nil")
	}
}
