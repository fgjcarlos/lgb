package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/cov"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// startTagCallback builds the callback under test; collect stops the
// historian writer and returns what it stored and what Sparkplug received.
func startTagCallback(t *testing.T, historianCOV bool, heartbeat time.Duration) (cb plc.TagCallback, collect func() ([]any, []sparkplug.TagUpdate)) {
	t.Helper()
	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "h.db"), historian.Options{})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	hw := historian.NewWriter(store, historian.WriterOptions{})
	hw.Start(ctx)

	var sp []sparkplug.TagUpdate
	cb = buildTagCallback(ctx, hw, historianCOV, heartbeat, func(u sparkplug.TagUpdate) { sp = append(sp, u) }, testutil.NewLogger(t))
	return cb, func() ([]any, []sparkplug.TagUpdate) {
		if err := hw.Stop(ctx); err != nil {
			t.Fatalf("writer Stop: %v", err)
		}
		samples, err := store.Query(ctx, historian.Query{PLCName: "p1", Tag: "Speed", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 100})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		var stored []any
		for _, s := range samples {
			stored = append(stored, s.Value)
		}
		return stored, sp
	}
}

func speedUpdate(v float64, ts time.Time, db cov.Deadband) plc.TagUpdate {
	return plc.TagUpdate{PLCName: "p1", Tag: "Speed", Value: v, Timestamp: ts, Quality: "good", Deadband: db}
}

// runTagCallback feeds values through buildTagCallback and returns what the
// historian stored and what the Sparkplug handler received.
func runTagCallback(t *testing.T, historianCOV bool, values []float64, db cov.Deadband) (stored []any, sp []sparkplug.TagUpdate) {
	t.Helper()
	cb, collect := startTagCallback(t, historianCOV, 0)
	base := time.Now()
	for i, v := range values {
		cb(speedUpdate(v, base.Add(time.Duration(i)*time.Millisecond), db))
	}
	return collect()
}

// TestBuildTagCallback_HistorianHeartbeat verifies #122: with
// changeOnValue.historianHeartbeat a constant value is stored again once the
// interval elapsed, not before.
func TestBuildTagCallback_HistorianHeartbeat(t *testing.T) {
	const heartbeat = 200 * time.Millisecond
	cb, collect := startTagCallback(t, true, heartbeat)
	cb(speedUpdate(10, time.Now(), cov.Deadband{}))
	cb(speedUpdate(10, time.Now().Add(time.Millisecond), cov.Deadband{})) // within the interval
	time.Sleep(heartbeat + 50*time.Millisecond)
	cb(speedUpdate(10, time.Now(), cov.Deadband{}))
	if stored, _ := collect(); len(stored) != 2 {
		t.Fatalf("historian stored %v; want 2 samples (first and heartbeat)", stored)
	}
}

// TestBuildTagCallback_HistorianCOV verifies #60: with changeOnValue.historian
// the historian stores only changes beyond the deadband, while Sparkplug still
// receives every update with its deadband (it applies its own filter).
func TestBuildTagCallback_HistorianCOV(t *testing.T) {
	db := cov.Deadband{Absolute: 1}
	stored, sp := runTagCallback(t, true, []float64{10, 10, 10.5, 12}, db)
	if len(stored) != 2 {
		t.Fatalf("historian stored %v; want 2 samples (10 and 12)", stored)
	}
	if len(sp) != 4 {
		t.Fatalf("sparkplug handler got %d updates; want 4 (independent of historian COV)", len(sp))
	}
	for _, u := range sp {
		if u.Deadband != db {
			t.Fatalf("sparkplug update deadband = %+v; want %+v", u.Deadband, db)
		}
	}
}

// TestBuildTagCallback_HistorianCOVOff pins the default: one historian sample
// per scan.
func TestBuildTagCallback_HistorianCOVOff(t *testing.T) {
	stored, _ := runTagCallback(t, false, []float64{10, 10, 10}, cov.Deadband{})
	if len(stored) != 3 {
		t.Fatalf("historian stored %v; want 3 samples", stored)
	}
}

// TestBuildTagCallback_StalledHistorianDoesNotBlock covers issue #123: with the
// historian store blocked and its buffer full, the scan callback returns at
// once and Sparkplug keeps receiving every update. With historian COV on, a
// dropped sample is not recorded as delivered, so the same value is stored
// once the historian recovers.
func TestBuildTagCallback_StalledHistorianDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "h.db"), historian.Options{})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	release := make(chan struct{})
	store.SetBeforeWriteHook(func() { <-release })
	hw := historian.NewWriter(store, historian.WriterOptions{BufferSize: 2, BatchSize: 1, FlushInterval: time.Hour})
	hw.Start(ctx)

	var sp []sparkplug.TagUpdate
	cb := buildTagCallback(ctx, hw, true, 0, func(u sparkplug.TagUpdate) { sp = append(sp, u) }, testutil.NewLogger(t))

	const updates = 50
	base := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < updates; i++ {
			cb(speedUpdate(float64(i), base.Add(time.Duration(i)*time.Millisecond), cov.Deadband{}))
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("scan callback blocked while the historian store was stalled")
	}
	if len(sp) != updates {
		t.Fatalf("sparkplug got %d updates; want %d", len(sp), updates)
	}
	if hw.Dropped() == 0 {
		t.Fatal("Dropped() = 0; want the samples that did not fit in the buffer")
	}

	// The last value (49) was dropped, so COV must offer it again.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		dropped := hw.Dropped()
		cb(speedUpdate(updates-1, base.Add(time.Second), cov.Deadband{}))
		if hw.Dropped() == dropped || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := hw.Stop(ctx); err != nil {
		t.Fatalf("writer Stop: %v", err)
	}
	samples, err := store.Query(ctx, historian.Query{PLCName: "p1", Tag: "Speed", From: base.Add(-time.Hour), To: base.Add(time.Hour), Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if n := len(samples); n == 0 || fmt.Sprint(samples[n-1].Value) != fmt.Sprint(updates-1) {
		t.Fatalf("stored %v; want the last value %d stored after recovery", samples, updates-1)
	}
}
