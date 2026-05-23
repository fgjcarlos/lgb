package sparkplug_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/sparkplug"
)

type mockMQTTClient struct {
	mu         sync.Mutex
	connected  bool
	published  []publishCall
	onConnect  func()
	disconnected bool
}

type publishCall struct {
	Topic   string
	QoS     byte
	Payload []byte
}

func (m *mockMQTTClient) Connect(_ context.Context) error {
	m.mu.Lock()
	m.connected = true
	fn := m.onConnect
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}

func (m *mockMQTTClient) Disconnect(_ uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	m.disconnected = true
}

func (m *mockMQTTClient) Publish(_ context.Context, topic string, qos byte, _ bool, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return errors.New("not connected")
	}
	m.published = append(m.published, publishCall{Topic: topic, QoS: qos, Payload: payload})
	return nil
}

func (m *mockMQTTClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *mockMQTTClient) SetOnConnect(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onConnect = fn
}

func (m *mockMQTTClient) getPublished() []publishCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]publishCall, len(m.published))
	copy(cp, m.published)
	return cp
}

func (m *mockMQTTClient) wasDisconnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.disconnected
}

func TestNewEdgeNode_ReturnsNonNil(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
	})
	if en == nil {
		t.Fatal("NewEdgeNode returned nil")
	}
}

func TestEdgeNode_Start_PublishesNBIRTHAndDBIRTH(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{
			{DeviceID: "plc-a", Tags: []sparkplug.TagDef{{Name: "Motor.Speed", SparkplugType: "Float"}}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer en.Stop()

	pubs := mc.getPublished()
	if len(pubs) < 2 {
		t.Fatalf("expected at least 2 publishes (NBIRTH+DBIRTH), got %d", len(pubs))
	}

	if pubs[0].Topic != "spBv1.0/plant-a/NBIRTH/lgb-1" {
		t.Errorf("first publish topic = %q; want NBIRTH topic", pubs[0].Topic)
	}
	if pubs[1].Topic != "spBv1.0/plant-a/DBIRTH/lgb-1/plc-a" {
		t.Errorf("second publish topic = %q; want DBIRTH topic", pubs[1].Topic)
	}

	if en.State() != sparkplug.Online {
		t.Errorf("state after Start = %v; want Online", en.State())
	}
}

func TestEdgeNode_Stop_PublishesDDEATHAndDisconnects(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{
			{DeviceID: "plc-a"},
		},
	})

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	beforeStop := len(mc.getPublished())
	if err := en.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	pubs := mc.getPublished()
	ddeath := pubs[beforeStop:]
	if len(ddeath) < 1 {
		t.Fatal("expected at least 1 DDEATH publish on Stop")
	}
	if ddeath[0].Topic != "spBv1.0/plant-a/DDEATH/lgb-1/plc-a" {
		t.Errorf("DDEATH topic = %q; want DDEATH topic", ddeath[0].Topic)
	}
	if mc.wasDisconnected() != true {
		t.Error("expected Disconnect to be called")
	}
	if en.State() != sparkplug.Offline {
		t.Errorf("state after Stop = %v; want Offline", en.State())
	}
}

func TestEdgeNode_HandleTagUpdate_WhenOnline(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{
			{DeviceID: "plc-a"},
		},
	})

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer en.Stop()

	beforeUpdate := len(mc.getPublished())
	en.HandleTagUpdate(sparkplug.TagUpdate{
		PLCName:   "plc-a",
		Tag:       "Motor.Speed",
		Value:     float32(1200.5),
		Timestamp: time.Now(),
	})

	// Give publisher goroutine time to drain.
	time.Sleep(100 * time.Millisecond)

	pubs := mc.getPublished()
	ddata := pubs[beforeUpdate:]
	if len(ddata) < 1 {
		t.Fatal("expected at least 1 DDATA publish after HandleTagUpdate")
	}
	if ddata[0].Topic != "spBv1.0/plant-a/DDATA/lgb-1/plc-a" {
		t.Errorf("DDATA topic = %q; want DDATA topic", ddata[0].Topic)
	}
}

func TestEdgeNode_HandleTagUpdate_WhenOffline_Dropped(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
	})

	en.HandleTagUpdate(sparkplug.TagUpdate{
		PLCName: "plc-a",
		Tag:     "Motor.Speed",
		Value:   float32(100),
		Timestamp: time.Now(),
	})

	time.Sleep(50 * time.Millisecond)

	pubs := mc.getPublished()
	if len(pubs) != 0 {
		t.Errorf("expected 0 publishes when offline, got %d", len(pubs))
	}
}

func TestEdgeNode_SeqResetsOnStart(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
	})

	ctx := context.Background()
	_ = en.Start(ctx)
	_ = en.Stop()

	_ = en.Start(ctx)
	defer en.Stop()
}

func TestEdgeNode_ConcurrentHandleTagUpdate(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
	})

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer en.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			en.HandleTagUpdate(sparkplug.TagUpdate{
				PLCName:   "plc-a",
				Tag:       "Motor.Speed",
				Value:     float32(100),
				Timestamp: time.Now(),
			})
		}()
	}
	wg.Wait()
}
