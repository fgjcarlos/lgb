package sparkplug_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

type subscribeCall struct {
	Topic   string
	QoS     byte
	Handler mqtt.MessageHandler
}

type mockMQTTClient struct {
	mu           sync.Mutex
	connected    bool
	published    []publishCall
	subscribed   []subscribeCall
	onConnect    func()
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

func (m *mockMQTTClient) Subscribe(_ context.Context, topic string, qos byte, handler mqtt.MessageHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribed = append(m.subscribed, subscribeCall{Topic: topic, QoS: qos, Handler: handler})
	return nil
}

func (m *mockMQTTClient) getSubscriptions() []subscribeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]subscribeCall, len(m.subscribed))
	copy(cp, m.subscribed)
	return cp
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
	defer func() {
		if err := en.Stop(); err != nil {
			t.Errorf("Stop returned error: %v", err)
		}
	}()

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
	defer func() {
		if err := en.Stop(); err != nil {
			t.Errorf("Stop returned error: %v", err)
		}
	}()

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
		PLCName:   "plc-a",
		Tag:       "Motor.Speed",
		Value:     float32(100),
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
	defer func() {
		if err := en.Stop(); err != nil {
			t.Errorf("Stop returned error: %v", err)
		}
	}()
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
	defer func() {
		if err := en.Stop(); err != nil {
			t.Errorf("Stop returned error: %v", err)
		}
	}()

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

func TestEdgeNode_Start_SubscribesToNCMDAndDCMD(t *testing.T) {
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
	defer func() { _ = en.Stop() }()

	subs := mc.getSubscriptions()
	if len(subs) < 2 {
		t.Fatalf("expected at least 2 subscriptions (NCMD+DCMD), got %d", len(subs))
	}

	ncmdFound, dcmdFound := false, false
	for _, s := range subs {
		switch s.Topic {
		case "spBv1.0/plant-a/NCMD/lgb-1":
			ncmdFound = true
		case "spBv1.0/plant-a/DCMD/lgb-1/+":
			dcmdFound = true
		}
	}
	if !ncmdFound {
		t.Error("expected NCMD subscription")
	}
	if !dcmdFound {
		t.Error("expected DCMD subscription")
	}
}

func TestEdgeNode_NCMD_Rebirth_TriggersNBIRTH(t *testing.T) {
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

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	// Record publish count before rebirth.
	beforeRebirth := len(mc.getPublished())

	// Find the NCMD subscription handler and invoke it with a rebirth command.
	subs := mc.getSubscriptions()
	var ncmdHandler mqtt.MessageHandler
	for _, s := range subs {
		if s.Topic == "spBv1.0/plant-a/NCMD/lgb-1" {
			ncmdHandler = s.Handler
			break
		}
	}
	if ncmdHandler == nil {
		t.Fatal("NCMD subscription handler not found")
	}

	// Build a rebirth NCMD payload.
	rebirthPayload := buildRebirthNCMD(t)
	ncmdHandler("spBv1.0/plant-a/NCMD/lgb-1", rebirthPayload)

	// After rebirth: NBIRTH + DBIRTH should be re-published.
	pubs := mc.getPublished()
	afterRebirth := pubs[beforeRebirth:]
	if len(afterRebirth) < 2 {
		t.Fatalf("expected at least 2 publishes after rebirth (NBIRTH+DBIRTH), got %d", len(afterRebirth))
	}
	if afterRebirth[0].Topic != "spBv1.0/plant-a/NBIRTH/lgb-1" {
		t.Errorf("first post-rebirth publish topic = %q; want NBIRTH", afterRebirth[0].Topic)
	}
}

func TestEdgeNode_DCMD_InvokesCommandHandler(t *testing.T) {
	t.Parallel()

	var cmdMu sync.Mutex
	var commands []struct{ Device, Tag string; Value any }
	onCmd := func(deviceID, tag string, value any) {
		cmdMu.Lock()
		commands = append(commands, struct{ Device, Tag string; Value any }{deviceID, tag, value})
		cmdMu.Unlock()
	}

	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID:   "plant-a",
		NodeID:    "lgb-1",
		Client:    mc,
		Devices:   []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
		OnCommand: onCmd,
	})

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	// Find the DCMD subscription handler.
	subs := mc.getSubscriptions()
	var dcmdHandler mqtt.MessageHandler
	for _, s := range subs {
		if s.Topic == "spBv1.0/plant-a/DCMD/lgb-1/+" {
			dcmdHandler = s.Handler
			break
		}
	}
	if dcmdHandler == nil {
		t.Fatal("DCMD subscription handler not found")
	}

	// Build a DCMD payload with a float write.
	dcmdPayload := buildDCMDPayload(t, "Motor.Speed", float32(1500.0))
	dcmdHandler("spBv1.0/plant-a/DCMD/lgb-1/plc-a", dcmdPayload)

	cmdMu.Lock()
	defer cmdMu.Unlock()
	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}
	if commands[0].Device != "plc-a" {
		t.Errorf("command device = %q; want plc-a", commands[0].Device)
	}
	if commands[0].Tag != "Motor.Speed" {
		t.Errorf("command tag = %q; want Motor.Speed", commands[0].Tag)
	}
	if v, ok := commands[0].Value.(float32); !ok || v != 1500.0 {
		t.Errorf("command value = %v; want float32(1500.0)", commands[0].Value)
	}
}

// buildRebirthNCMD creates an NCMD payload with "Node Control/Rebirth" = true.
func buildRebirthNCMD(t *testing.T) []byte {
	t.Helper()
	name := "Node Control/Rebirth"
	dt := uint32(11) // Boolean
	payload := &pb.Payload{
		Metrics: []*pb.Payload_Metric{{
			Name:     &name,
			Datatype: &dt,
			Value:    &pb.Payload_Metric_BooleanValue{BooleanValue: true},
		}},
	}
	data, err := proto.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal rebirth NCMD: %v", err)
	}
	return data
}

// buildDCMDPayload creates a DCMD payload with a single float metric.
func buildDCMDPayload(t *testing.T, tag string, val float32) []byte {
	t.Helper()
	dt := uint32(9) // Float
	payload := &pb.Payload{
		Metrics: []*pb.Payload_Metric{{
			Name:     &tag,
			Datatype: &dt,
			Value:    &pb.Payload_Metric_FloatValue{FloatValue: val},
		}},
	}
	data, err := proto.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal DCMD: %v", err)
	}
	return data
}
