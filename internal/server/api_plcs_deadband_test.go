package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
)

// TestPLCAPI_DeadbandRoundTrip verifies #60: a tag deadband sent on POST is
// persisted and returned; a tag without one returns a zero deadband.
func TestPLCAPI_DeadbandRoundTrip(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"plc-db","address":"10.0.0.3:44818","scan_rate":"1s","tags":[` +
		`{"name":"Speed","type":"Float","deadband":{"absolute":0.5,"percent":2}},` +
		`{"name":"Running","type":"Boolean"}]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}

	stored, err := store.Get(context.Background(), "plc-db")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if want := (config.Deadband{Absolute: 0.5, Percent: 2}); stored.Tags[0].Deadband != want {
		t.Errorf("stored deadband = %+v; want %+v", stored.Tags[0].Deadband, want)
	}

	get := doRequest(t, http.MethodGet, baseURL+"/api/plcs/plc-db", "", tok)
	defer get.Body.Close()
	var result struct {
		Data struct {
			Tags []struct {
				Deadband *struct {
					Absolute float64 `json:"absolute"`
					Percent  float64 `json:"percent"`
				} `json:"deadband"`
			} `json:"tags"`
		} `json:"data"`
	}
	if err := json.NewDecoder(get.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tags := result.Data.Tags
	if len(tags) != 2 || tags[0].Deadband == nil || tags[1].Deadband == nil {
		t.Fatalf("expected 2 tags with a deadband object, got %+v", tags)
	}
	if tags[0].Deadband.Absolute != 0.5 || tags[0].Deadband.Percent != 2 {
		t.Errorf("tags[0].deadband = %+v; want {0.5 2}", *tags[0].Deadband)
	}
	if tags[1].Deadband.Absolute != 0 || tags[1].Deadband.Percent != 0 {
		t.Errorf("tags[1].deadband = %+v; want zero", *tags[1].Deadband)
	}
}

// TestPLCAPI_InvalidDeadbandRejected verifies #60: an invalid deadband is
// rejected with 400 invalid_plc and nothing is stored.
func TestPLCAPI_InvalidDeadbandRejected(t *testing.T) {
	store, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"plc-bad","address":"10.0.0.3:44818","scan_rate":"1s","tags":[` +
		`{"name":"Running","type":"Boolean","deadband":{"absolute":1}}]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), "invalid_plc") || !strings.Contains(string(b), "only numeric tag types support a deadband") {
		t.Errorf("unexpected error body: %s", b)
	}
	if list, _ := store.List(context.Background()); len(list) != 0 {
		t.Errorf("store has %d PLCs after rejected create; want 0", len(list))
	}
	if n := mgr.ReloadCount(); n != 0 {
		t.Errorf("manager reloaded %d times after rejected create; want 0", n)
	}
}
