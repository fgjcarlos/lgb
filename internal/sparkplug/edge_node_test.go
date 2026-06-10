package sparkplug_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/retry"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

type subscribeCall struct {
	Topic   string
	QoS     byte
	Handler mqtt.MessageHandler
}

type unsubscribeCall struct {
	Topic string
}

type mockMQTTClient struct {
	mu             sync.Mutex
	connected      bool
	published      []publishCall
	subscribed     []subscribeCall
	unsubscribed   []unsubscribeCall
	onConnect      func()
	onConnLost     func(error)
	disconnected   bool
	disconnectChan chan struct{} // closed on Disconnect, for ordering assertions
	// connectBlock, if non-nil, is received before Connect returns.
	// Close the channel to unblock. Used in tests to control reconnect timing.
	connectBlock chan struct{}
}

type publishCall struct {
	Topic   string
	QoS     byte
	Payload []byte
}

func (m *mockMQTTClient) Connect(ctx context.Context) error {
	m.mu.Lock()
	block := m.connectBlock
	m.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

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
	ch := m.disconnectChan
	m.connected = false
	m.disconnected = true
	m.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
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

func (m *mockMQTTClient) Unsubscribe(_ context.Context, topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unsubscribed = append(m.unsubscribed, unsubscribeCall{Topic: topic})
	return nil
}

func (m *mockMQTTClient) SetConnectionLost(fn func(error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onConnLost = fn
}

func (m *mockMQTTClient) simulateConnectionLost(err error) {
	m.mu.Lock()
	m.connected = false
	fn := m.onConnLost
	m.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}

func (m *mockMQTTClient) getUnsubscribed() []unsubscribeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]unsubscribeCall, len(m.unsubscribed))
	copy(cp, m.unsubscribed)
	return cp
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

// TestEdgeNode_SetCommandHandler_PostConstruction verifies that a CommandHandler
// can be set AFTER construction (before Start) and that it is invoked when a DCMD
// arrives. This is the wiring seam used by cmd/lgb/cmd/server.go (PR3).
// (TWA-DCMD-3.2 — EdgeNode side)
func TestEdgeNode_SetCommandHandler_PostConstruction(t *testing.T) {
	t.Parallel()

	var cmdMu sync.Mutex
	var called []struct{ Device, Tag string; Value any }

	mc := &mockMQTTClient{}
	// Create EdgeNode WITHOUT an OnCommand — simulates cmd/lgb creating the node
	// before the server (and guard) are available.
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
		// OnCommand deliberately omitted — wired post-construction below.
	})

	// Wire the handler AFTER construction, BEFORE Start.
	handler := func(deviceID, tag string, value any) {
		cmdMu.Lock()
		called = append(called, struct{ Device, Tag string; Value any }{deviceID, tag, value})
		cmdMu.Unlock()
	}
	en.SetCommandHandler(handler)

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	// Find DCMD handler and fire a metric.
	subs := mc.getSubscriptions()
	var dcmdHandler mqtt.MessageHandler
	for _, s := range subs {
		if s.Topic == "spBv1.0/plant-a/DCMD/lgb-1/+" {
			dcmdHandler = s.Handler
			break
		}
	}
	if dcmdHandler == nil {
		t.Fatal("DCMD subscription not found after Start")
	}

	payload := buildDCMDPayload(t, "Feed.Rate", float32(3.0))
	dcmdHandler("spBv1.0/plant-a/DCMD/lgb-1/plc-a", payload)

	cmdMu.Lock()
	defer cmdMu.Unlock()
	if len(called) != 1 {
		t.Fatalf("expected 1 invocation of post-construction handler, got %d", len(called))
	}
	if called[0].Device != "plc-a" {
		t.Errorf("device = %q; want plc-a", called[0].Device)
	}
	if called[0].Tag != "Feed.Rate" {
		t.Errorf("tag = %q; want Feed.Rate", called[0].Tag)
	}
	if v, ok := called[0].Value.(float32); !ok || v != 3.0 {
		t.Errorf("value = %v; want float32(3.0)", called[0].Value)
	}
}

// --- RED Tests: Tasks 3.1–3.7 (written before implementation) ---

// TestEdgeNode_NCMD_Rebirth_NoHandlerAccumulation verifies R68-1:
// after N rebirth NCMD commands, Subscribe must have been called exactly twice
// (one for NCMD, one for DCMD) — not 2×N times.
func TestEdgeNode_NCMD_Rebirth_NoHandlerAccumulation(t *testing.T) {
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
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = en.Stop() }()

	// Find the NCMD handler registered during Start.
	subs := mc.getSubscriptions()
	var ncmdHandler mqtt.MessageHandler
	for _, s := range subs {
		if s.Topic == "spBv1.0/plant-a/NCMD/lgb-1" {
			ncmdHandler = s.Handler
			break
		}
	}
	if ncmdHandler == nil {
		t.Fatal("NCMD handler not found after Start")
	}

	rebirthPayload := buildRebirthNCMD(t)
	const N = 3
	for i := 0; i < N; i++ {
		ncmdHandler("spBv1.0/plant-a/NCMD/lgb-1", rebirthPayload)
	}

	// After N rebirths, Subscribe must still be exactly 2 (NCMD + DCMD from Start).
	got := len(mc.getSubscriptions())
	if got != 2 {
		t.Errorf("Subscribe call count = %d; want 2 (subscribe-once, not 2×N)", got)
	}
}

// TestEdgeNode_Rebirth_PublishesCorrectSeq verifies R68-2:
// rebirth republishes NBIRTH with seq = lastSeq+1 (reset to 0 per Sparkplug B spec).
func TestEdgeNode_Rebirth_PublishesCorrectSeq(t *testing.T) {
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
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = en.Stop() }()

	// Find NCMD handler and trigger rebirth.
	subs := mc.getSubscriptions()
	var ncmdHandler mqtt.MessageHandler
	for _, s := range subs {
		if s.Topic == "spBv1.0/plant-a/NCMD/lgb-1" {
			ncmdHandler = s.Handler
			break
		}
	}
	if ncmdHandler == nil {
		t.Fatal("NCMD handler not found")
	}

	beforeRebirth := len(mc.getPublished())
	ncmdHandler("spBv1.0/plant-a/NCMD/lgb-1", buildRebirthNCMD(t))

	pubs := mc.getPublished()
	afterRebirth := pubs[beforeRebirth:]
	if len(afterRebirth) < 1 {
		t.Fatal("expected at least 1 publish after rebirth (NBIRTH)")
	}

	// First publish after rebirth must be NBIRTH with seq=0 (Sparkplug B: reset on each NBIRTH).
	nbirthTopic := "spBv1.0/plant-a/NBIRTH/lgb-1"
	if afterRebirth[0].Topic != nbirthTopic {
		t.Errorf("first post-rebirth topic = %q; want %q", afterRebirth[0].Topic, nbirthTopic)
	}

	// Decode and check seq field.
	var payload pb.Payload
	if err := proto.Unmarshal(afterRebirth[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal NBIRTH: %v", err)
	}
	if payload.Seq == nil {
		t.Fatal("NBIRTH payload has no seq field")
	}
	if *payload.Seq != 0 {
		t.Errorf("NBIRTH seq = %d; want 0 (reset on rebirth per Sparkplug B spec)", *payload.Seq)
	}
}

// TestEdgeNode_ConnectionLost_TransitionsOffline verifies R69-1:
// on connection loss, SM transitions to Offline and DDATA is suppressed.
// After Start, connectBlock is set so reconnect goroutine cannot immediately
// succeed — this lets us observe and assert the Offline state.
func TestEdgeNode_ConnectionLost_TransitionsOffline(t *testing.T) {
	t.Parallel()

	mc := &mockMQTTClient{} // no block for the initial Start → Connect
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if en.State() != sparkplug.Online {
		_ = en.Stop()
		t.Fatalf("state before loss = %v; want Online", en.State())
	}

	// Cancel ctx so the reconnect goroutine exits immediately on connection loss.
	// This prevents the reconnect loop from re-establishing Online before we assert Offline.
	cancel()

	defer func() { _ = en.Stop() }()

	// Simulate connection loss — reconnect loop will exit because ctx is cancelled.
	mc.simulateConnectionLost(errors.New("broker unreachable"))

	// SM must transition to Offline.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if en.State() == sparkplug.Offline {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if en.State() != sparkplug.Offline {
		t.Errorf("state after connection lost = %v; want Offline", en.State())
	}

	// A tag update must NOT be forwarded as DDATA while Offline (reconnect blocked).
	beforeUpdate := len(mc.getPublished())
	en.HandleTagUpdate(sparkplug.TagUpdate{PLCName: "plc-a", Tag: "T1", Value: float32(1.0), Timestamp: time.Now()})
	time.Sleep(50 * time.Millisecond)
	if len(mc.getPublished()) != beforeUpdate {
		t.Error("DDATA published while Offline; want suppressed")
	}
}

// TestEdgeNode_Reconnect_RepublishesBirths verifies R69-2:
// on successful reconnect, NBIRTH is published with bdSeq incremented.
func TestEdgeNode_Reconnect_RepublishesBirths(t *testing.T) {
	t.Parallel()

	fastRetry := &retry.Options{
		Initial:     time.Millisecond,
		Max:         10 * time.Millisecond,
		MaxAttempts: 0,
	}
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID:      "plant-a",
		NodeID:       "lgb-1",
		Client:       mc,
		Devices:      []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
		RetryOptions: fastRetry,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = en.Stop() }()

	initialBdSeq := en.BdSeq()
	beforeLoss := len(mc.getPublished())

	// Simulate connection loss — the reconnect goroutine picks this up.
	// retry.Do has 1s initial delay, but the mock Connect returns immediately.
	// The first retry fires after ~1s; we wait up to 3s.
	mc.simulateConnectionLost(errors.New("lost"))

	// Wait for reconnect to publish NBIRTH (fast retry: ≤10ms delay).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		pubs := mc.getPublished()
		for _, p := range pubs[beforeLoss:] {
			if p.Topic == "spBv1.0/plant-a/NBIRTH/lgb-1" {
				goto found
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for NBIRTH after reconnect")
found:

	pubs := mc.getPublished()
	newPubs := pubs[beforeLoss:]

	nbirthTopic := "spBv1.0/plant-a/NBIRTH/lgb-1"
	if newPubs[0].Topic != nbirthTopic {
		t.Errorf("first post-reconnect publish = %q; want %q", newPubs[0].Topic, nbirthTopic)
	}

	// bdSeq must have incremented from the initial value.
	if en.BdSeq() <= initialBdSeq {
		t.Errorf("bdSeq after reconnect = %d; want > %d (initial)", en.BdSeq(), initialBdSeq)
	}

	// Unsubscribe must have been called before resubscribing (R68-1 — prevents
	// handler accumulation when CleanSession=true drops server-side subscriptions).
	unsubs := mc.getUnsubscribed()
	if len(unsubs) == 0 {
		t.Error("expected Unsubscribe to be called on reconnect; got 0 calls")
	}
}

// TestEdgeNode_NBIRTH_ContainsBdSeqMetric verifies R69-3:
// every NBIRTH payload must contain a metric named "bdSeq".
func TestEdgeNode_NBIRTH_ContainsBdSeqMetric(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
	})

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = en.Stop() }()

	pubs := mc.getPublished()
	if len(pubs) < 1 {
		t.Fatal("expected at least 1 publish (NBIRTH)")
	}
	if pubs[0].Topic != "spBv1.0/plant-a/NBIRTH/lgb-1" {
		t.Fatalf("first publish = %q; want NBIRTH", pubs[0].Topic)
	}

	var payload pb.Payload
	if err := proto.Unmarshal(pubs[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal NBIRTH: %v", err)
	}

	found := false
	for _, m := range payload.Metrics {
		if m.Name != nil && *m.Name == "bdSeq" {
			found = true
			break
		}
	}
	if !found {
		t.Error("NBIRTH payload does not contain a metric named 'bdSeq'")
	}
}

// TestEdgeNode_LWT_BdSeqIsZero verifies R69-4:
// the LWT NDEATH payload must carry bdSeq=0 (accepted paho limitation).
// This test validates BuildNDEATH(0) produces the correct payload shape.
func TestEdgeNode_LWT_BdSeqIsZero(t *testing.T) {
	t.Parallel()
	data, err := sparkplug.BuildNDEATH(0)
	if err != nil {
		t.Fatalf("BuildNDEATH(0): %v", err)
	}

	var payload pb.Payload
	if err := proto.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal NDEATH: %v", err)
	}

	if len(payload.Metrics) == 0 {
		t.Fatal("NDEATH payload has no metrics")
	}
	var bdSeqVal uint64
	found := false
	for _, m := range payload.Metrics {
		if m.Name != nil && *m.Name == "bdSeq" {
			found = true
			if lv, ok := m.Value.(*pb.Payload_Metric_LongValue); ok {
				bdSeqVal = lv.LongValue
			}
			break
		}
	}
	if !found {
		t.Fatal("NDEATH has no 'bdSeq' metric")
	}
	if bdSeqVal != 0 {
		t.Errorf("LWT NDEATH bdSeq = %d; want 0 (frozen, accepted paho limitation)", bdSeqVal)
	}
}

// TestEdgeNode_Stop_PublishesNDEATH_CorrectOrder verifies R69-5:
// Stop must publish: DDEATHs → NDEATH → Disconnect, in that order.
// NDEATH.bdSeq must equal the current bdSeq (not 0).
func TestEdgeNode_Stop_PublishesNDEATH_CorrectOrder(t *testing.T) {
	t.Parallel()
	mc := &mockMQTTClient{disconnectChan: make(chan struct{})}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "plant-a",
		NodeID:  "lgb-1",
		Client:  mc,
		Devices: []sparkplug.DeviceConfig{
			{DeviceID: "plc-a"},
			{DeviceID: "plc-b"},
		},
	})

	ctx := context.Background()
	if err := en.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	beforeStop := len(mc.getPublished())
	currentBdSeq := en.BdSeq()

	if err := en.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	pubs := mc.getPublished()
	stopPubs := pubs[beforeStop:]

	// Must have at least 2 DDEATHs + 1 NDEATH = 3 publishes.
	if len(stopPubs) < 3 {
		t.Fatalf("expected ≥3 stop publishes (DDEATH×2 + NDEATH), got %d", len(stopPubs))
	}

	// First two must be DDEATHs.
	ddeath1 := stopPubs[0].Topic
	ddeath2 := stopPubs[1].Topic
	if ddeath1 != "spBv1.0/plant-a/DDEATH/lgb-1/plc-a" && ddeath1 != "spBv1.0/plant-a/DDEATH/lgb-1/plc-b" {
		t.Errorf("pub[0] = %q; want DDEATH for plc-a or plc-b", ddeath1)
	}
	if ddeath2 != "spBv1.0/plant-a/DDEATH/lgb-1/plc-a" && ddeath2 != "spBv1.0/plant-a/DDEATH/lgb-1/plc-b" {
		t.Errorf("pub[1] = %q; want DDEATH for plc-a or plc-b", ddeath2)
	}

	// Last publish before Disconnect must be NDEATH.
	ndeathPub := stopPubs[len(stopPubs)-1]
	ndeathTopic := "spBv1.0/plant-a/NDEATH/lgb-1"
	if ndeathPub.Topic != ndeathTopic {
		t.Errorf("last stop publish = %q; want NDEATH (%q)", ndeathPub.Topic, ndeathTopic)
	}

	// NDEATH must carry current bdSeq (not 0).
	var ndeath pb.Payload
	if err := proto.Unmarshal(ndeathPub.Payload, &ndeath); err != nil {
		t.Fatalf("unmarshal NDEATH: %v", err)
	}
	var gotBdSeq uint64
	for _, m := range ndeath.Metrics {
		if m.Name != nil && *m.Name == "bdSeq" {
			if lv, ok := m.Value.(*pb.Payload_Metric_LongValue); ok {
				gotBdSeq = lv.LongValue
			}
			break
		}
	}
	if gotBdSeq != currentBdSeq {
		t.Errorf("NDEATH bdSeq = %d; want %d (current bdSeq)", gotBdSeq, currentBdSeq)
	}

	// Disconnect must have been called after NDEATH.
	if !mc.wasDisconnected() {
		t.Error("Disconnect not called after NDEATH")
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
