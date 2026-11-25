package cov

import (
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for heartbeat tests (#122).
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func heartbeatFilter(d time.Duration, c *fakeClock) *Filter {
	f := NewHeartbeatFilter(d)
	f.now = c.now
	return f
}

var constant = Sample{Key: "plc1/Tag", Value: 5.0, Quality: "good"}

func accept() bool { return true }
func reject() bool { return false }

// TestHeartbeat_ResendsConstantAfterInterval verifies #122: an unchanged value
// is re-delivered once the interval elapsed since the last delivery, not before.
func TestHeartbeat_ResendsConstantAfterInterval(t *testing.T) {
	c := &fakeClock{t: time.Unix(0, 0)}
	f := heartbeatFilter(time.Minute, c)

	if !f.Offer(constant, accept) {
		t.Fatal("first sample not delivered")
	}
	c.advance(time.Minute - time.Nanosecond)
	if f.Offer(constant, accept) {
		t.Fatal("constant value delivered before the heartbeat interval")
	}
	c.advance(time.Nanosecond)
	if !f.Offer(constant, accept) {
		t.Fatal("constant value not re-delivered at the heartbeat interval")
	}
	// The heartbeat delivery resets the interval.
	c.advance(time.Minute - time.Nanosecond)
	if f.Offer(constant, accept) {
		t.Fatal("heartbeat delivery did not reset the interval")
	}
}

// TestHeartbeat_ChangeResetsInterval verifies #122: a delivered change resets
// the interval the same way a heartbeat does.
func TestHeartbeat_ChangeResetsInterval(t *testing.T) {
	c := &fakeClock{t: time.Unix(0, 0)}
	f := heartbeatFilter(time.Minute, c)

	f.Offer(constant, accept)
	c.advance(30 * time.Second)
	changed := constant
	changed.Value = 6.0
	if !f.Offer(changed, accept) {
		t.Fatal("change not delivered")
	}
	c.advance(59 * time.Second) // 89s since the first delivery, 59s since the change
	if f.Offer(changed, accept) {
		t.Fatal("heartbeat fired although a change was delivered within the interval")
	}
	c.advance(time.Second)
	if !f.Offer(changed, accept) {
		t.Fatal("heartbeat not fired one interval after the change")
	}
}

// TestHeartbeat_DroppedDeliveryDoesNotReset verifies #122: a dropped heartbeat
// delivery is not recorded, so the next offer retries it.
func TestHeartbeat_DroppedDeliveryDoesNotReset(t *testing.T) {
	c := &fakeClock{t: time.Unix(0, 0)}
	f := heartbeatFilter(time.Minute, c)

	f.Offer(constant, accept)
	c.advance(time.Minute)
	if f.Offer(constant, reject) {
		t.Fatal("rejected delivery reported as delivered")
	}
	c.advance(time.Second)
	if !f.Offer(constant, accept) {
		t.Fatal("heartbeat not retried after a dropped delivery")
	}
}

// TestHeartbeat_PerConsumerIndependence verifies #122: each consumer's filter
// keeps its own heartbeat state and interval.
func TestHeartbeat_PerConsumerIndependence(t *testing.T) {
	c := &fakeClock{t: time.Unix(0, 0)}
	short, long := heartbeatFilter(time.Minute, c), heartbeatFilter(time.Hour, c)
	slow := heartbeatFilter(time.Minute, c)

	for _, f := range []*Filter{short, long, slow} {
		f.Offer(constant, accept)
	}
	c.advance(time.Minute)
	if !short.Offer(constant, accept) {
		t.Error("short-interval consumer: heartbeat not delivered")
	}
	if long.Offer(constant, accept) {
		t.Error("long-interval consumer: heartbeat delivered early")
	}
	if slow.Offer(constant, reject) {
		t.Error("slow consumer: dropped heartbeat reported as delivered")
	}
	c.advance(time.Second)
	if short.Offer(constant, accept) {
		t.Error("short-interval consumer: affected by the slow consumer's drop")
	}
	if !slow.Offer(constant, accept) {
		t.Error("slow consumer: dropped heartbeat not retried")
	}
}

// TestHeartbeat_DisabledKeepsCurrentBehaviour verifies #122: without a
// heartbeat a constant value is never re-delivered.
func TestHeartbeat_DisabledKeepsCurrentBehaviour(t *testing.T) {
	c := &fakeClock{t: time.Unix(0, 0)}
	for name, f := range map[string]*Filter{"NewFilter": NewFilter(), "zero heartbeat": NewHeartbeatFilter(0)} {
		f.now = c.now
		f.Offer(constant, accept)
		c.advance(24 * time.Hour)
		if f.Offer(constant, accept) {
			t.Errorf("%s: constant value re-delivered without heartbeat", name)
		}
	}
}
