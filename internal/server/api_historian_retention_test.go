package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/historian"
	"github.com/fgjcarlos/lgb/internal/plcstore"
)

type retentionBody struct {
	Data retentionResponse `json:"data"`
}

func getRetention(t *testing.T, baseURL string) (int, retentionBody, string) {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/historian/retention")
	if err != nil {
		t.Fatalf("GET retention: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body retentionBody
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return resp.StatusCode, body, string(raw)
}

// TestHandleHistorianRetention_EffectivePerLevel verifies #85: the endpoint
// reports the global default and, for every PLC and tag in the PLC store, the
// effective retention and the level that set it.
func TestHandleHistorianRetention_EffectivePerLevel(t *testing.T) {
	ctx := context.Background()
	hist, err := historian.Open(ctx, ":memory:", historian.Options{})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	defer hist.Close()
	plcs, err := plcstore.Open(ctx, filepath.Join(t.TempDir(), "plcs.db"))
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	defer plcs.Close()
	for _, p := range []config.PLC{
		{Name: "line1", Address: "10.0.0.1", RetentionDays: 30, Tags: []config.TagDef{
			{Name: "Fast", Type: "Float", RetentionDays: 1},
			{Name: "Other", Type: "Float"},
		}},
		{Name: "line2", Address: "10.0.0.2", Tags: []config.TagDef{{Name: "KPI", Type: "Float", RetentionDays: 400}, {Name: "Plain", Type: "Int32"}}},
	} {
		if err := plcs.Create(ctx, p); err != nil {
			t.Fatalf("Create %s: %v", p.Name, err)
		}
	}

	_, baseURL, stop := startAPITestServerWithOptsAndCfgFn(t, &snapshotPLCManager{},
		Opts{HistStore: hist, PLCStore: plcs}, func(c *config.Config) { c.Historian.RetentionDays = 90 })
	defer stop()

	status, body, raw := getRetention(t, baseURL)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, raw)
	}
	want := retentionResponse{DefaultDays: 90, PLCs: []plcRetention{
		{Name: "line1", RetentionDays: 30, Source: "plc", Tags: []tagRetention{
			{Name: "Fast", RetentionDays: 1, Source: "tag"},
			{Name: "Other", RetentionDays: 30, Source: "plc"},
		}},
		{Name: "line2", RetentionDays: 90, Source: "global", Tags: []tagRetention{
			{Name: "KPI", RetentionDays: 400, Source: "tag"},
			{Name: "Plain", RetentionDays: 90, Source: "global"},
		}},
	}}
	if !reflect.DeepEqual(body.Data, want) {
		t.Errorf("retention = %+v\nwant %+v", body.Data, want)
	}
}

// TestHandleHistorianRetention_ConfigFallback verifies the endpoint falls back
// to the startup config PLCs when no PLC store is wired.
func TestHandleHistorianRetention_ConfigFallback(t *testing.T) {
	hist, err := historian.Open(context.Background(), ":memory:", historian.Options{})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	defer hist.Close()
	_, baseURL, stop := startAPITestServerWithOptsAndCfgFn(t, &snapshotPLCManager{},
		Opts{HistStore: hist}, func(c *config.Config) { c.Historian.RetentionDays = 45 })
	defer stop()

	status, body, raw := getRetention(t, baseURL)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, raw)
	}
	want := retentionResponse{DefaultDays: 45, PLCs: []plcRetention{
		{Name: "packaging", RetentionDays: 45, Source: "global", Tags: []tagRetention{{Name: "Speed", RetentionDays: 45, Source: "global"}}},
	}}
	if !reflect.DeepEqual(body.Data, want) {
		t.Errorf("retention = %+v\nwant %+v", body.Data, want)
	}
}

// TestHandleHistorianRetention_StoreListError500 verifies a PLC store read
// failure surfaces as 500 internal_error instead of a partial view.
func TestHandleHistorianRetention_StoreListError500(t *testing.T) {
	ctx := context.Background()
	hist, err := historian.Open(ctx, ":memory:", historian.Options{})
	if err != nil {
		t.Fatalf("historian.Open: %v", err)
	}
	defer hist.Close()
	plcs, err := plcstore.Open(ctx, filepath.Join(t.TempDir(), "plcs.db"))
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	_ = plcs.Close() // every List call now fails

	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{HistStore: hist, PLCStore: plcs})
	defer stop()

	status, _, raw := getRetention(t, baseURL)
	if status != http.StatusInternalServerError || !strings.Contains(raw, "internal_error") {
		t.Fatalf("expected 500 internal_error, got %d: %s", status, raw)
	}
}

// TestHandleHistorianRetention_DisabledHistorian503 verifies the endpoint
// answers 503 like the historian query when the historian is disabled.
func TestHandleHistorianRetention_DisabledHistorian503(t *testing.T) {
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{})
	defer stop()

	status, _, raw := getRetention(t, baseURL)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", status, raw)
	}
}
