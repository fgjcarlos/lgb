package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// TestPLCAPI_RetentionDaysRoundTrip verifies #85: retention_days on a PLC and
// its tags is persisted on POST and returned on GET; omitted means 0.
func TestPLCAPI_RetentionDaysRoundTrip(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"plc-rt","address":"10.0.0.3:44818","scan_rate":"1s","retention_days":30,"tags":[` +
		`{"name":"Speed","type":"Float","retention_days":7},` +
		`{"name":"Running","type":"Boolean"}]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}

	stored, err := store.Get(context.Background(), "plc-rt")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.RetentionDays != 30 || stored.Tags[0].RetentionDays != 7 || stored.Tags[1].RetentionDays != 0 {
		t.Errorf("stored retention = plc %d, tags %d/%d; want 30, 7/0", stored.RetentionDays, stored.Tags[0].RetentionDays, stored.Tags[1].RetentionDays)
	}

	get := doRequest(t, http.MethodGet, baseURL+"/api/plcs/plc-rt", "", tok)
	defer get.Body.Close()
	var result struct {
		Data struct {
			RetentionDays *int `json:"retention_days"`
			Tags          []struct {
				RetentionDays *int `json:"retention_days"`
			} `json:"tags"`
		} `json:"data"`
	}
	if err := json.NewDecoder(get.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := result.Data
	if d.RetentionDays == nil || *d.RetentionDays != 30 || len(d.Tags) != 2 ||
		d.Tags[0].RetentionDays == nil || *d.Tags[0].RetentionDays != 7 ||
		d.Tags[1].RetentionDays == nil || *d.Tags[1].RetentionDays != 0 {
		t.Errorf("GET retention_days not returned as 30, 7/0: %+v", d)
	}
}

// TestPLCAPI_InvalidRetentionDaysRejected verifies #85: an out-of-range
// retention_days is rejected with 400 invalid_plc and nothing is stored.
func TestPLCAPI_InvalidRetentionDaysRejected(t *testing.T) {
	store, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"plc-bad","address":"10.0.0.3:44818","scan_rate":"1s","tags":[` +
		`{"name":"Speed","type":"Float","retention_days":-1}]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), "invalid_plc") || !strings.Contains(string(b), "tags[0].retentionDays: must be between 0 and 36500, got -1") {
		t.Errorf("unexpected error body: %s", b)
	}
	if list, _ := store.List(context.Background()); len(list) != 0 {
		t.Errorf("store has %d PLCs after rejected create; want 0", len(list))
	}
	if n := mgr.ReloadCount(); n != 0 {
		t.Errorf("manager reloaded %d times after rejected create; want 0", n)
	}
}
