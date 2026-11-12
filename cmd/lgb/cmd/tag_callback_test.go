package cmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/cov"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// runTagCallback feeds values through buildTagCallback and returns what the
// historian stored and what the Sparkplug handler received.
func runTagCallback(t *testing.T, historianCOV bool, values []float64, db cov.Deadband) (stored []any, sp []sparkplug.TagUpdate) {
	t.Helper()
	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "h.db"), historian.Options{RetentionDays: 1})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	defer store.Close()
	hw := historian.NewWriter(store, historian.WriterOptions{})
	hw.Start(ctx)

	cb := buildTagCallback(ctx, hw, historianCOV, func(u sparkplug.TagUpdate) { sp = append(sp, u) }, testutil.NewLogger(t))
	base := time.Now()
	for i, v := range values {
		cb(plc.TagUpdate{PLCName: "p1", Tag: "Speed", Value: v, Timestamp: base.Add(time.Duration(i) * time.Millisecond), Quality: "good", Deadband: db})
	}
	if err := hw.Stop(ctx); err != nil {
		t.Fatalf("writer Stop: %v", err)
	}
	samples, err := store.Query(ctx, historian.Query{PLCName: "p1", Tag: "Speed", From: base.Add(-time.Hour), To: base.Add(time.Hour), Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, s := range samples {
		stored = append(stored, s.Value)
	}
	return stored, sp
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
