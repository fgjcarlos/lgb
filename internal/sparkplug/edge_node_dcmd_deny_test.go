package sparkplug_test

import (
	"context"
	"sync"
	"testing"
)

// TestEdgeNode_DCMD_Deny_PublishesNothing verifies TWA-DCMD-3.2 (#62
// SUGGESTION-2): a DCMD the handler denies is dropped silently — the edge node
// publishes nothing on MQTT in response (no error message, no DDATA).
func TestEdgeNode_DCMD_Deny_PublishesNothing(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	invoked := 0
	mc := &mockMQTTClient{}
	en := newGuardTestNode(mc, nil)
	// A denying handler: the server's guard-backed closure audits and returns
	// without writing; here it only records that it was consulted.
	en.SetCommandHandler(func(string, string, any) {
		mu.Lock()
		invoked++
		mu.Unlock()
	})
	if err := en.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() { _ = en.Stop() }()

	dcmd := findDCMDHandler(t, mc)
	before := len(mc.getPublished())

	dcmd("spBv1.0/plant-a/DCMD/lgb-1/plc-a", buildDCMDPayload(t, "Feed.Rate", float32(3.0)))

	mu.Lock()
	got := invoked
	mu.Unlock()
	if got != 1 {
		t.Fatalf("handler invocations = %d; want 1 (the deny decision must be reached)", got)
	}
	if after := mc.getPublished(); len(after) != before {
		t.Errorf("publishes after denied DCMD = %d; want %d (no MQTT publish on deny): %+v",
			len(after), before, after[before:])
	}
}
