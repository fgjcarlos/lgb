package historian_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/historian"
)

// TestRetentionPolicy_Effective verifies #85: a per-tag override beats the
// per-PLC default, which beats the global default; 0 inherits.
func TestRetentionPolicy_Effective(t *testing.T) {
	t.Parallel()

	policy := historian.NewRetentionPolicy(90, []config.PLC{
		{Name: "p1", RetentionDays: 30, Tags: []config.TagDef{
			{Name: "fast", RetentionDays: 1},
			{Name: "inherit"},
		}},
		{Name: "p2", Tags: []config.TagDef{
			{Name: "kpi", RetentionDays: 365},
			{Name: "plain"},
		}},
	})
	tests := []struct {
		plc, tag   string
		wantDays   int
		wantSource string
	}{
		{"p1", "fast", 1, historian.RetentionSourceTag},
		{"p1", "inherit", 30, historian.RetentionSourcePLC},
		{"p1", "unconfigured", 30, historian.RetentionSourcePLC},
		{"p2", "kpi", 365, historian.RetentionSourceTag},
		{"p2", "plain", 90, historian.RetentionSourceGlobal},
		{"gone", "old", 90, historian.RetentionSourceGlobal},
	}
	for _, tc := range tests {
		days, source := policy.Effective(tc.plc, tc.tag)
		if days != tc.wantDays || source != tc.wantSource {
			t.Errorf("Effective(%q, %q) = %d, %q; want %d, %q", tc.plc, tc.tag, days, source, tc.wantDays, tc.wantSource)
		}
	}
}

// TestEnforceRetention_MostSpecificPolicyWins verifies #85 with samples spread
// across time windows: each series keeps exactly the samples younger than its
// effective retention, in both directions (shorter and longer than the global).
func TestEnforceRetention_MostSpecificPolicyWins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	// ages in days per series; fractional ages via hours below.
	series := map[[2]string][]time.Duration{
		{"p1", "fast"}:  {12 * time.Hour, 2 * day},         // tag 1d (shorter than plc 30d)
		{"p1", "kpi"}:   {100 * day, 300 * day, 500 * day}, // tag 400d (longer than plc 30d and global 90d)
		{"p1", "other"}: {10 * day, 40 * day},              // inherits plc 30d
		{"p2", "plain"}: {80 * day, 90 * day, 100 * day},   // global 90d; exactly 90d is kept
		{"p2", "long"}:  {100 * day, 400 * day},            // tag 365d, PLC without default
		{"gone", "old"}: {80 * day, 100 * day},             // no longer configured: global
	}
	var samples []historian.Sample
	for key, ages := range series {
		for _, age := range ages {
			samples = append(samples, historian.Sample{PLCName: key[0], Tag: key[1], Timestamp: now.Add(-age), Value: age.Hours(), Quality: "good"})
		}
	}
	if err := store.InsertBatch(ctx, samples); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	policy := historian.NewRetentionPolicy(90, []config.PLC{
		{Name: "p1", RetentionDays: 30, Tags: []config.TagDef{
			{Name: "fast", RetentionDays: 1},
			{Name: "kpi", RetentionDays: 400},
			{Name: "other"},
		}},
		{Name: "p2", Tags: []config.TagDef{{Name: "plain"}, {Name: "long", RetentionDays: 365}}},
	})
	if err := store.EnforceRetention(ctx, now, policy); err != nil {
		t.Fatalf("EnforceRetention: %v", err)
	}

	want := map[[2]string][]time.Duration{
		{"p1", "fast"}:  {12 * time.Hour},
		{"p1", "kpi"}:   {300 * day, 100 * day},
		{"p1", "other"}: {10 * day},
		{"p2", "plain"}: {90 * day, 80 * day},
		{"p2", "long"}:  {100 * day},
		{"gone", "old"}: {80 * day},
	}
	for key, wantAges := range want {
		got, err := store.Query(ctx, historian.Query{PLCName: key[0], Tag: key[1], From: now.Add(-1000 * day), To: now})
		if err != nil {
			t.Fatalf("Query %v: %v", key, err)
		}
		var gotAges []time.Duration
		for _, s := range got {
			gotAges = append(gotAges, now.Sub(s.Timestamp))
		}
		if len(gotAges) != len(wantAges) {
			t.Errorf("%s/%s kept ages %v; want %v", key[0], key[1], gotAges, wantAges)
			continue
		}
		for i := range wantAges {
			if gotAges[i] != wantAges[i] {
				t.Errorf("%s/%s kept ages %v; want %v", key[0], key[1], gotAges, wantAges)
				break
			}
		}
	}
}

// TestEnforceRetention_DisabledPolicyKeepsEverything verifies a zero policy
// (no global, no overrides) deletes nothing.
func TestEnforceRetention_DisabledPolicyKeepsEverything(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	old := now.Add(-10000 * 24 * time.Hour)
	if err := store.InsertBatch(ctx, []historian.Sample{{PLCName: "p", Tag: "t", Timestamp: old, Value: 1}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if err := store.EnforceRetention(ctx, now, historian.RetentionPolicy{}); err != nil {
		t.Fatalf("EnforceRetention: %v", err)
	}
	got, err := store.Query(ctx, historian.Query{PLCName: "p", Tag: "t", From: old, To: now})
	if err != nil || len(got) != 1 {
		t.Fatalf("Query = %d samples, %v; want 1 sample kept", len(got), err)
	}
}
