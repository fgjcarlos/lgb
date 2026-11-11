package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// statusPLCManager is a PLCManager that also reports per-PLC status.
type statusPLCManager struct {
	mockPLCManager
	statuses map[string]plc.Status
}

func (m *statusPLCManager) Status(name string) (plc.Status, bool) {
	st, ok := m.statuses[name]
	return st, ok
}

type plcStatusBody struct {
	Data struct {
		Name               string     `json:"name"`
		State              string     `json:"state"`
		ScanRate           string     `json:"scan_rate"`
		ScanHealth         string     `json:"scan_health"`
		ConnectedSince     *time.Time `json:"connected_since"`
		LastScan           *time.Time `json:"last_scan"`
		LastScanDurationMs *float64   `json:"last_scan_duration_ms"`
		ScanCount          uint64     `json:"scan_count"`
		ReadCount          uint64     `json:"read_count"`
		ReadErrorCount     uint64     `json:"read_error_count"`
		ConnectErrorCount  uint64     `json:"connect_error_count"`
		LastError          string     `json:"last_error"`
		LastErrorAt        *time.Time `json:"last_error_at"`
	} `json:"data"`
	Error *apiError `json:"error"`
}

func getPLCStatus(t *testing.T, url, token string) (int, plcStatusBody) {
	t.Helper()
	resp := doRequest(t, http.MethodGet, url, "", token)
	defer resp.Body.Close()
	var body plcStatusBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode, body
}

func newStatusTestManager() *statusPLCManager {
	ts := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	return &statusPLCManager{statuses: map[string]plc.Status{
		"packaging": {
			Name: "packaging", State: plc.StateConnected, ScanRate: 500 * time.Millisecond,
			ScanHealth: plc.ScanHealthOK, ConnectedSince: ts, LastScan: ts.Add(time.Second),
			LastScanDuration: 1500 * time.Microsecond, ScanCount: 10, ReadCount: 20,
			ReadErrorCount: 2, ConnectErrorCount: 1, LastError: "read timeout", LastErrorAt: ts.Add(2 * time.Second),
		},
		"offline": {Name: "offline", State: plc.StateNeverConnected, ScanRate: time.Second, ScanHealth: plc.ScanHealthNone},
	}}
}

func TestPLCStatus_ViewerGetsConnectedStatus(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, newStatusTestManager(), Opts{AuthTokens: tokens})
	defer stop()

	token, err := tokens.Issue(1, "viewer1", auth.RoleViewer)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	code, body := getPLCStatus(t, baseURL+"/api/plcs/packaging/status", token)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %+v)", code, body)
	}
	d := body.Data
	ts := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	if d.Name != "packaging" || d.State != "connected" || d.ScanRate != "500ms" || d.ScanHealth != "ok" {
		t.Fatalf("unexpected identity fields: %+v", d)
	}
	if d.ConnectedSince == nil || !d.ConnectedSince.Equal(ts) || d.LastScan == nil || !d.LastScan.Equal(ts.Add(time.Second)) {
		t.Fatalf("unexpected timestamps: %+v", d)
	}
	if d.LastScanDurationMs == nil || *d.LastScanDurationMs != 1.5 {
		t.Fatalf("last_scan_duration_ms = %v, want 1.5", d.LastScanDurationMs)
	}
	if d.ScanCount != 10 || d.ReadCount != 20 || d.ReadErrorCount != 2 || d.ConnectErrorCount != 1 {
		t.Fatalf("unexpected counters: %+v", d)
	}
	if d.LastError != "read timeout" || d.LastErrorAt == nil || !d.LastErrorAt.Equal(ts.Add(2*time.Second)) {
		t.Fatalf("unexpected last error: %+v", d)
	}
}

func TestPLCStatus_NeverConnectedHasNullTimestamps(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, newStatusTestManager())
	defer stop()

	code, body := getPLCStatus(t, baseURL+"/api/plcs/offline/status", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	d := body.Data
	if d.State != "never_connected" || d.ScanHealth != "none" {
		t.Fatalf("unexpected state: %+v", d)
	}
	if d.ConnectedSince != nil || d.LastScan != nil || d.LastScanDurationMs != nil || d.LastErrorAt != nil {
		t.Fatalf("expected null timestamps, got %+v", d)
	}
}

func TestPLCStatus_RoleGate(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, newStatusTestManager(), Opts{AuthTokens: tokens})
	defer stop()

	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs/packaging/status", "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", resp.StatusCode)
	}
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin} {
		token, err := tokens.Issue(1, "u", role)
		if err != nil {
			t.Fatalf("issue %s token: %v", role, err)
		}
		if code, _ := getPLCStatus(t, baseURL+"/api/plcs/packaging/status", token); code != http.StatusOK {
			t.Fatalf("role %s: status = %d, want 200", role, code)
		}
	}
}

func TestPLCStatus_UnknownPLCIs404(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, newStatusTestManager())
	defer stop()

	code, body := getPLCStatus(t, baseURL+"/api/plcs/ghost/status", "")
	if code != http.StatusNotFound || body.Error == nil || body.Error.Code != "plc_not_found" {
		t.Fatalf("got %d %+v, want 404 plc_not_found", code, body.Error)
	}
}

func TestPLCStatus_ManagerWithoutStatusIs503(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, &snapshotPLCManager{})
	defer stop()

	code, body := getPLCStatus(t, baseURL+"/api/plcs/packaging/status", "")
	if code != http.StatusServiceUnavailable || body.Error == nil || body.Error.Code != "status_unavailable" {
		t.Fatalf("got %d %+v, want 503 status_unavailable", code, body.Error)
	}
}
