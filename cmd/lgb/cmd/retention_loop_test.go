package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/plcstore"
)

// TestEnforceRetentionTick verifies #85: each tick re-reads the PLC store, so
// an override saved after startup applies on the next tick; a store read
// failure skips the tick instead of falling back to the global default.
func TestEnforceRetentionTick(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	hist, err := historian.Open(ctx, filepath.Join(dir, "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	defer hist.Close()
	plcs, err := plcstore.Open(ctx, filepath.Join(dir, "plcs.db"))
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	defer plcs.Close()

	now := time.Now().UTC()
	tenDaysAgo := now.Add(-10 * 24 * time.Hour)
	if err := hist.InsertBatch(ctx, []historian.Sample{{PLCName: "p1", Tag: "Speed", Timestamp: tenDaysAgo, Value: 1}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	count := func() int {
		got, err := hist.Query(ctx, historian.Query{PLCName: "p1", Tag: "Speed", From: tenDaysAgo.Add(-time.Hour), To: now})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		return len(got)
	}

	failing := func(context.Context) ([]config.PLC, error) { return nil, errors.New("store down") }
	if err := enforceRetentionTick(ctx, hist, failing, 1, now); err == nil {
		t.Fatal("tick with a failing PLC store returned nil error")
	}
	if n := count(); n != 1 {
		t.Fatalf("failing tick deleted samples: %d left; want 1", n)
	}

	if err := plcs.Create(ctx, config.PLC{Name: "p1", Address: "10.0.0.1",
		Tags: []config.TagDef{{Name: "Speed", Type: "Float", RetentionDays: 30}}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := enforceRetentionTick(ctx, hist, plcs.List, 1, now); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("30-day tag override not applied: %d samples left; want 1", n)
	}

	if err := plcs.Update(ctx, "p1", config.PLC{Name: "p1", Address: "10.0.0.1",
		Tags: []config.TagDef{{Name: "Speed", Type: "Float"}}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := enforceRetentionTick(ctx, hist, plcs.List, 1, now); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("global 1-day default not applied after the override was removed: %d samples left; want 0", n)
	}
}
