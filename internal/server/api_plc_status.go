package server

import (
	"net/http"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// plcStatusProvider is the optional PLCManager capability behind
// GET /api/plcs/{name}/status.
type plcStatusProvider interface {
	Status(name string) (plc.Status, bool)
}

var _ plcStatusProvider = (*plc.Manager)(nil)

// plcStatusResponse is the JSON shape of one PLC's operational status.
// Timestamps that never happened are null.
type plcStatusResponse struct {
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
	WriteCount         uint64     `json:"write_count"`
	WriteErrorCount    uint64     `json:"write_error_count"`
	LastError          string     `json:"last_error"`
	LastErrorAt        *time.Time `json:"last_error_at"`
	ScanLatencyAvgMs   *float64   `json:"scan_latency_avg_ms"`
	ScanLatencyMaxMs   *float64   `json:"scan_latency_max_ms"`
	ScanLatencySamples int        `json:"scan_latency_samples"`
}

func durationMs(d time.Duration) *float64 {
	ms := float64(d) / float64(time.Millisecond)
	return &ms
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func toPLCStatusResponse(st plc.Status) plcStatusResponse {
	resp := plcStatusResponse{
		Name:               st.Name,
		State:              st.State,
		ScanRate:           st.ScanRate.String(),
		ScanHealth:         st.ScanHealth,
		ConnectedSince:     optionalTime(st.ConnectedSince),
		LastScan:           optionalTime(st.LastScan),
		ScanCount:          st.ScanCount,
		ReadCount:          st.ReadCount,
		ReadErrorCount:     st.ReadErrorCount,
		ConnectErrorCount:  st.ConnectErrorCount,
		WriteCount:         st.WriteCount,
		WriteErrorCount:    st.WriteErrorCount,
		LastError:          st.LastError,
		LastErrorAt:        optionalTime(st.LastErrorAt),
		ScanLatencySamples: st.ScanLatencySamples,
	}
	if !st.LastScan.IsZero() {
		resp.LastScanDurationMs = durationMs(st.LastScanDuration)
	}
	if st.ScanLatencySamples > 0 {
		resp.ScanLatencyAvgMs = durationMs(st.ScanLatencyAvg)
		resp.ScanLatencyMaxMs = durationMs(st.ScanLatencyMax)
	}
	return resp
}

// plcDiagnosticsProvider is the optional PLCManager capability behind
// GET /api/diagnostics.
type plcDiagnosticsProvider interface {
	Statuses() []plc.Status
}

var _ plcDiagnosticsProvider = (*plc.Manager)(nil)

// registerPLCStatusRoute registers GET /api/plcs/{name}/status and
// GET /api/diagnostics. They read the live PLC manager, not the PLC store, so
// they are registered regardless of whether a store is wired. Viewer+ when
// auth is enabled.
func (s *Server) registerPLCStatusRoute(mux *http.ServeMux) {
	s.registerViewerRoute(mux, "GET /api/plcs/{name}/status", s.handlePLCStatus)
	s.registerViewerRoute(mux, "GET /api/diagnostics", s.handleDiagnostics)
}

func (s *Server) registerViewerRoute(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	if s.authTokens == nil {
		mux.HandleFunc(pattern, h)
		return
	}
	// RequireRole is an exact-match allowlist, so viewer+ lists every role.
	mux.Handle(pattern, withMiddleware(h,
		authMiddleware(s.authTokens),
		auth.RequireRole(auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin)))
}

// handleDiagnostics returns the operational status of every managed PLC,
// sorted by name, in a {"data":[...]} envelope.
func (s *Server) handleDiagnostics(w http.ResponseWriter, _ *http.Request) {
	provider, ok := s.plcMgr.(plcDiagnosticsProvider)
	if !ok {
		writeAPIError(w, http.StatusServiceUnavailable, "diagnostics_unavailable", "PLC diagnostics are not available")
		return
	}
	statuses := provider.Statuses()
	out := make([]plcStatusResponse, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, toPLCStatusResponse(st))
	}
	writeJSON(w, http.StatusOK, struct {
		Data []plcStatusResponse `json:"data"`
	}{Data: out})
}

func (s *Server) handlePLCStatus(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.plcMgr.(plcStatusProvider)
	if !ok {
		writeAPIError(w, http.StatusServiceUnavailable, "status_unavailable", "PLC status is not available")
		return
	}
	name := r.PathValue("name")
	st, ok := provider.Status(name)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "plc_not_found", "PLC not found or not running: "+name)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Data plcStatusResponse `json:"data"`
	}{Data: toPLCStatusResponse(st)})
}
