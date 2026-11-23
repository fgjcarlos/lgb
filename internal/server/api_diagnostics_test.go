package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// Statuses makes statusPLCManager a plcDiagnosticsProvider.
func (m *statusPLCManager) Statuses() []plc.Status {
	out := make([]plc.Status, 0, len(m.statuses))
	for _, st := range m.statuses {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type plcDiagnosticsItem struct {
	Name               string   `json:"name"`
	State              string   `json:"state"`
	ScanHealth         string   `json:"scan_health"`
	LastScan           *string  `json:"last_scan"`
	ScanCount          uint64   `json:"scan_count"`
	ReadErrorCount     uint64   `json:"read_error_count"`
	ConnectErrorCount  uint64   `json:"connect_error_count"`
	WriteCount         *uint64  `json:"write_count"`
	WriteErrorCount    *uint64  `json:"write_error_count"`
	ScanLatencyAvgMs   *float64 `json:"scan_latency_avg_ms"`
	ScanLatencyMaxMs   *float64 `json:"scan_latency_max_ms"`
	ScanLatencySamples *int     `json:"scan_latency_samples"`
}

type diagnosticsBody struct {
	Data  []plcDiagnosticsItem `json:"data"`
	Error *apiError            `json:"error"`
}

func getDiagnostics(t *testing.T, url, token string) (int, diagnosticsBody) {
	t.Helper()
	resp := doRequest(t, http.MethodGet, url, "", token)
	defer resp.Body.Close()
	var body diagnosticsBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode, body
}

func newDiagnosticsTestManager() *statusPLCManager {
	m := newStatusTestManager()
	st := m.statuses["packaging"]
	st.WriteCount, st.WriteErrorCount = 4, 1
	st.ScanLatencyAvg, st.ScanLatencyMax, st.ScanLatencySamples = 2500*time.Microsecond, 7*time.Millisecond, 10
	m.statuses["packaging"] = st
	return m
}

func TestDiagnostics_ViewerGetsEveryPLC(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, newDiagnosticsTestManager(), Opts{AuthTokens: tokens})
	defer stop()

	token, err := tokens.Issue(1, "viewer1", auth.RoleViewer)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	code, body := getDiagnostics(t, baseURL+"/api/diagnostics", token)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %+v)", code, body)
	}
	if len(body.Data) != 2 || body.Data[0].Name != "offline" || body.Data[1].Name != "packaging" {
		t.Fatalf("data = %+v, want offline, packaging", body.Data)
	}
	p := body.Data[1]
	if p.State != "connected" || p.ScanHealth != "ok" || p.LastScan == nil || p.ScanCount != 10 ||
		p.ReadErrorCount != 2 || p.ConnectErrorCount != 1 {
		t.Fatalf("unexpected status fields: %+v", p)
	}
	if p.WriteCount == nil || *p.WriteCount != 4 || p.WriteErrorCount == nil || *p.WriteErrorCount != 1 {
		t.Fatalf("write counters = %v/%v, want 4/1", p.WriteCount, p.WriteErrorCount)
	}
	if p.ScanLatencyAvgMs == nil || *p.ScanLatencyAvgMs != 2.5 || p.ScanLatencyMaxMs == nil || *p.ScanLatencyMaxMs != 7 ||
		p.ScanLatencySamples == nil || *p.ScanLatencySamples != 10 {
		t.Fatalf("latency = %v/%v/%v, want 2.5/7/10", p.ScanLatencyAvgMs, p.ScanLatencyMaxMs, p.ScanLatencySamples)
	}

	o := body.Data[0]
	if o.ScanLatencyAvgMs != nil || o.ScanLatencyMaxMs != nil || o.ScanLatencySamples == nil || *o.ScanLatencySamples != 0 {
		t.Fatalf("never-scanned PLC latency = %v/%v/%v, want null/null/0", o.ScanLatencyAvgMs, o.ScanLatencyMaxMs, o.ScanLatencySamples)
	}
}

func TestDiagnostics_RoleGate(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, newDiagnosticsTestManager(), Opts{AuthTokens: tokens})
	defer stop()

	resp := doRequest(t, http.MethodGet, baseURL+"/api/diagnostics", "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", resp.StatusCode)
	}
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin} {
		token, err := tokens.Issue(1, "u", role)
		if err != nil {
			t.Fatalf("issue %s token: %v", role, err)
		}
		if code, _ := getDiagnostics(t, baseURL+"/api/diagnostics", token); code != http.StatusOK {
			t.Fatalf("role %s: status = %d, want 200", role, code)
		}
	}
}

func TestDiagnostics_NoPLCsIsEmptyList(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, &statusPLCManager{statuses: map[string]plc.Status{}})
	defer stop()

	resp := doRequest(t, http.MethodGet, baseURL+"/api/diagnostics", "", "")
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(raw["data"]) != "[]" {
		t.Fatalf("got %d data=%s, want 200 []", resp.StatusCode, raw["data"])
	}
}

func TestDiagnostics_ManagerWithoutStatusIs503(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, &snapshotPLCManager{})
	defer stop()

	code, body := getDiagnostics(t, baseURL+"/api/diagnostics", "")
	if code != http.StatusServiceUnavailable || body.Error == nil || body.Error.Code != "diagnostics_unavailable" {
		t.Fatalf("got %d %+v, want 503 diagnostics_unavailable", code, body.Error)
	}
}
