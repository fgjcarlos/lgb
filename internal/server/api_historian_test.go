package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/historian"
)

// newHistorianTestServer builds a *Server backed by a real in-memory historian
// store. Returns (baseURL, histStore, stop).
func newHistorianTestServer(t *testing.T) (string, *historian.Store, func()) {
	t.Helper()
	ctx := context.Background()
	store, err := historian.Open(ctx, ":memory:", historian.Options{})
	if err != nil {
		t.Fatalf("open historian store: %v", err)
	}
	_, baseURL, stopSrv := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{HistStore: store})
	stop := func() {
		stopSrv()
		_ = store.Close()
	}
	return baseURL, store, stop
}

// ─── GET /api/historian/query ────────────────────────────────────────────────

func TestHandleHistorianQuery_ValidParams200(t *testing.T) {
	baseURL, store, stop := newHistorianTestServer(t)
	defer stop()

	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []historian.Sample{
		{PLCName: "PLC1", Tag: "Speed", Timestamp: t0, Value: 10.0, Quality: "good"},
		{PLCName: "PLC1", Tag: "Speed", Timestamp: t0.Add(time.Second), Value: 20.0, Quality: "good"},
	}
	if err := store.InsertBatch(ctx, samples); err != nil {
		t.Fatalf("insert batch: %v", err)
	}

	from := t0.Add(-time.Second).Format(time.RFC3339)
	to := t0.Add(2 * time.Second).Format(time.RFC3339)
	url := fmt.Sprintf("%s/api/historian/query?plc=PLC1&tag=Speed&from=%s&to=%s", baseURL, from, to)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			PLCName   string `json:"plc_name"`
			Tag       string `json:"tag"`
			Timestamp string `json:"timestamp"`
			Value     any    `json:"value"`
		} `json:"data"`
		Pagination struct {
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
			Count  int `json:"count"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 2 {
		t.Errorf("expected 2 samples, got %d", len(body.Data))
	}
	if body.Pagination.Count != 2 {
		t.Errorf("expected pagination.count=2, got %d", body.Pagination.Count)
	}
}

func TestHandleHistorianQuery_EmptyWindow200(t *testing.T) {
	baseURL, _, stop := newHistorianTestServer(t)
	defer stop()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	from := t0.Format(time.RFC3339)
	to := t0.Add(time.Hour).Format(time.RFC3339)
	url := fmt.Sprintf("%s/api/historian/query?plc=PLC1&tag=Speed&from=%s&to=%s", baseURL, from, to)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// data must be [] (empty array), not null
	if body.Data == nil {
		t.Error("expected data to be [] not null")
	}
	if len(body.Data) != 0 {
		t.Errorf("expected 0 samples, got %d", len(body.Data))
	}
}

func TestHandleHistorianQuery_MissingTag400(t *testing.T) {
	baseURL, _, stop := newHistorianTestServer(t)
	defer stop()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	from := t0.Format(time.RFC3339)
	to := t0.Add(time.Hour).Format(time.RFC3339)
	// No `tag` param
	url := fmt.Sprintf("%s/api/historian/query?plc=PLC1&from=%s&to=%s", baseURL, from, to)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "bad_request")
}

func TestHandleHistorianQuery_UnparseableFrom400(t *testing.T) {
	baseURL, _, stop := newHistorianTestServer(t)
	defer stop()

	url := fmt.Sprintf("%s/api/historian/query?plc=PLC1&tag=Speed&from=not-a-date&to=2026-01-01T01:00:00Z", baseURL)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "bad_request")
}

func TestHandleHistorianQuery_LimitClamped(t *testing.T) {
	baseURL, _, stop := newHistorianTestServer(t)
	defer stop()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	from := t0.Format(time.RFC3339)
	to := t0.Add(time.Hour).Format(time.RFC3339)

	// limit=0 should be rejected (below 1)
	url := fmt.Sprintf("%s/api/historian/query?plc=PLC1&tag=Speed&from=%s&to=%s&limit=0", baseURL, from, to)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("limit=0: expected 400, got %d", resp.StatusCode)
	}

	// limit=1001 should be rejected (above 1000)
	url = fmt.Sprintf("%s/api/historian/query?plc=PLC1&tag=Speed&from=%s&to=%s&limit=1001", baseURL, from, to)
	resp2, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query limit=1001: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("limit=1001: expected 400, got %d", resp2.StatusCode)
	}

	// limit=500 should be accepted
	url = fmt.Sprintf("%s/api/historian/query?plc=PLC1&tag=Speed&from=%s&to=%s&limit=500", baseURL, from, to)
	resp3, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET historian/query limit=500: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("limit=500: expected 200, got %d", resp3.StatusCode)
	}
	var body struct {
		Pagination struct {
			Limit int `json:"limit"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp3.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Pagination.Limit != 500 {
		t.Errorf("expected pagination.limit=500, got %d", body.Pagination.Limit)
	}
}
