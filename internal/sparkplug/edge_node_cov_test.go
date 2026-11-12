package sparkplug_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/cov"
	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

func startCOVNode(t *testing.T, changeOnValue bool) (*sparkplug.EdgeNode, *mockMQTTClient) {
	t.Helper()
	mc := &mockMQTTClient{}
	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID:       "plant-a",
		NodeID:        "lgb-1",
		Client:        mc,
		Devices:       []sparkplug.DeviceConfig{{DeviceID: "plc-a"}},
		ChangeOnValue: changeOnValue,
	})
	if err := en.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = en.Stop() })
	return en, mc
}

// ddataValues returns the float values of every DDATA published so far.
func ddataValues(t *testing.T, mc *mockMQTTClient) []float64 {
	t.Helper()
	var out []float64
	for _, p := range mc.getPublished() {
		if !strings.Contains(p.Topic, "/DDATA/") {
			continue
		}
		var payload pb.Payload
		if err := proto.Unmarshal(p.Payload, &payload); err != nil {
			t.Fatalf("unmarshal DDATA: %v", err)
		}
		for _, m := range payload.Metrics {
			out = append(out, m.GetDoubleValue())
		}
	}
	return out
}

// waitDDATA waits until want DDATA values were published, then gives the
// publish loop a moment to prove no extra DDATA follows.
func waitDDATA(t *testing.T, mc *mockMQTTClient, want []float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(ddataValues(t, mc)) < len(want) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	got := ddataValues(t, mc)
	if len(got) != len(want) {
		t.Fatalf("DDATA values = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DDATA values = %v; want %v", got, want)
		}
	}
}

func upd(v float64, db cov.Deadband) sparkplug.TagUpdate {
	return sparkplug.TagUpdate{PLCName: "plc-a", Tag: "Speed", Value: v, Timestamp: time.Now(), Quality: "good", Deadband: db}
}

// TestEdgeNode_COV_SuppressesUnchanged verifies #60: with change-on-value
// enabled an unchanged value produces no DDATA.
func TestEdgeNode_COV_SuppressesUnchanged(t *testing.T) {
	t.Parallel()
	en, mc := startCOVNode(t, true)
	for _, v := range []float64{5, 5, 5, 6} {
		en.HandleTagUpdate(upd(v, cov.Deadband{}))
	}
	waitDDATA(t, mc, []float64{5, 6})
}

// TestEdgeNode_COV_Deadband verifies #60: changes within the deadband are
// suppressed, changes beyond it are published.
func TestEdgeNode_COV_Deadband(t *testing.T) {
	t.Parallel()
	en, mc := startCOVNode(t, true)
	db := cov.Deadband{Absolute: 0.5}
	for _, v := range []float64{10, 10.4, 10.5, 11} {
		en.HandleTagUpdate(upd(v, db))
	}
	waitDDATA(t, mc, []float64{10, 11})
}

// TestEdgeNode_COV_Disabled_PublishesEveryUpdate pins the previous behaviour
// when change-on-value is off: every update becomes a DDATA.
func TestEdgeNode_COV_Disabled_PublishesEveryUpdate(t *testing.T) {
	t.Parallel()
	en, mc := startCOVNode(t, false)
	for _, v := range []float64{5, 5, 5} {
		en.HandleTagUpdate(upd(v, cov.Deadband{Absolute: 100}))
	}
	waitDDATA(t, mc, []float64{5, 5, 5})
}

// TestEdgeNode_COV_ResetOnRebirth verifies #60: after a rebirth the current
// value is published again even though it did not change.
func TestEdgeNode_COV_ResetOnRebirth(t *testing.T) {
	t.Parallel()
	en, mc := startCOVNode(t, true)
	en.HandleTagUpdate(upd(5, cov.Deadband{}))
	waitDDATA(t, mc, []float64{5})

	var ncmd mqtt.MessageHandler
	for _, s := range mc.getSubscriptions() {
		if s.Topic == "spBv1.0/plant-a/NCMD/lgb-1" {
			ncmd = s.Handler
		}
	}
	if ncmd == nil {
		t.Fatal("NCMD handler not found")
	}
	ncmd("spBv1.0/plant-a/NCMD/lgb-1", buildRebirthNCMD(t))

	en.HandleTagUpdate(upd(5, cov.Deadband{}))
	waitDDATA(t, mc, []float64{5, 5})
}
