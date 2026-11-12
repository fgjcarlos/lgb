package server

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

func speed(v float64) plc.TagUpdate {
	return plc.TagUpdate{PLCName: "packaging", Tag: "Speed", Value: v, Quality: "good", Timestamp: time.Now()}
}

func drain(ch chan plc.TagUpdate) []any {
	var out []any
	for {
		select {
		case u := <-ch:
			out = append(out, u.Value)
		default:
			return out
		}
	}
}

func assertValues(t *testing.T, name string, got []any, want ...any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s got %v; want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s got %v; want %v", name, got, want)
		}
	}
}

// TestTagHub_COV_SuppressesUnchanged verifies #60: a COV subscriber does not
// receive updates whose value did not change beyond the deadband.
func TestTagHub_COV_SuppressesUnchanged(t *testing.T) {
	h := newTagHub()
	sub := newTagSubscriber(16, true)
	sub.setFilter("", "")
	h.register(sub)

	for _, v := range []float64{5, 5, 6, 6} {
		h.publish(speed(v))
	}
	assertValues(t, "subscriber", drain(sub.ch), 5.0, 6.0)
}

// TestTagHub_COV_Disabled_ReceivesEveryUpdate pins the previous behaviour
// when realtime change-on-value is off.
func TestTagHub_COV_Disabled_ReceivesEveryUpdate(t *testing.T) {
	h := newTagHub()
	sub := newTagSubscriber(16, false)
	sub.setFilter("", "")
	h.register(sub)

	for _, v := range []float64{5, 5, 5} {
		h.publish(speed(v))
	}
	assertValues(t, "subscriber", drain(sub.ch), 5.0, 5.0, 5.0)
}

// TestTagHub_COV_PerSubscriberIndependence verifies #60: a slow subscriber
// whose buffer is full does not change what a fast subscriber receives, and
// the update it missed is re-delivered once it has room.
func TestTagHub_COV_PerSubscriberIndependence(t *testing.T) {
	h := newTagHub()
	slow := newTagSubscriber(1, true)
	fast := newTagSubscriber(16, true)
	slow.setFilter("", "")
	fast.setFilter("", "")
	h.register(slow)
	h.register(fast)

	h.publish(speed(1)) // both receive 1; slow's buffer is now full
	h.publish(speed(2)) // slow drops 2
	assertValues(t, "slow", drain(slow.ch), 1.0)
	assertValues(t, "fast", drain(fast.ch), 1.0, 2.0)

	h.publish(speed(2)) // unchanged for fast; slow never got 2
	assertValues(t, "slow after drain", drain(slow.ch), 2.0)
	assertValues(t, "fast after drain", drain(fast.ch))
}

// TestTagHub_COV_ResubscribeResends verifies that changing the subscription
// filter resets COV, so the client gets current values again.
func TestTagHub_COV_ResubscribeResends(t *testing.T) {
	h := newTagHub()
	sub := newTagSubscriber(16, true)
	sub.setFilter("packaging", "")
	h.register(sub)

	h.publish(speed(5))
	sub.setFilter("packaging", "Speed")
	h.publish(speed(5))
	assertValues(t, "subscriber", drain(sub.ch), 5.0, 5.0)
}

// TestServer_TagsWebSocket_ChangeOnValue verifies the WebSocket feed honours
// changeOnValue.realtime end to end.
func TestServer_TagsWebSocket_ChangeOnValue(t *testing.T) {
	srv, baseURL, stop := startAPITestServerWithOptsAndCfgFn(t, &snapshotPLCManager{}, Opts{}, func(c *config.Config) {
		c.ChangeOnValue.Realtime = true
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+baseURL[len("http"):]+"/api/ws/tags?plc=packaging&tag=Speed", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	var msg struct {
		Type  string  `json:"type"`
		Value float64 `json:"value"`
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil || msg.Type != "subscribed" {
		t.Fatalf("subscription ack: %#v, %v", msg, err)
	}
	for _, v := range []float64{7, 7, 7, 8} {
		srv.PublishTagUpdate(speed(v))
	}
	for _, want := range []float64{7, 8} {
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("read update: %v", err)
		}
		if msg.Type != "tag_update" || msg.Value != want {
			t.Fatalf("got %#v; want tag_update %v (duplicates must be suppressed)", msg, want)
		}
	}
}
