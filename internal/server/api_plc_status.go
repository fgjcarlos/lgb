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
	LastError          string     `json:"last_error"`
	LastErrorAt        *time.Time `json:"last_error_at"`
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func toPLCStatusResponse(st plc.Status) plcStatusResponse {
	resp := plcStatusResponse{
		Name:              st.Name,
		State:             st.State,
		ScanRate:          st.ScanRate.String(),
		ScanHealth:        st.ScanHealth,
		ConnectedSince:    optionalTime(st.ConnectedSince),
		LastScan:          optionalTime(st.LastScan),
		ScanCount:         st.ScanCount,
		ReadCount:         st.ReadCount,
		ReadErrorCount:    st.ReadErrorCount,
		ConnectErrorCount: st.ConnectErrorCount,
		LastError:         st.LastError,
		LastErrorAt:       optionalTime(st.LastErrorAt),
	}
	if !st.LastScan.IsZero() {
		ms := float64(st.LastScanDuration) / float64(time.Millisecond)
		resp.LastScanDurationMs = &ms
	}
	return resp
}

// registerPLCStatusRoute registers GET /api/plcs/{name}/status. It reads the
// live PLC manager, not the PLC store, so it is registered regardless of
// whether a store is wired. Viewer+ when auth is enabled.
func (s *Server) registerPLCStatusRoute(mux *http.ServeMux) {
	const pattern = "GET /api/plcs/{name}/status"
	if s.authTokens == nil {
		mux.HandleFunc(pattern, s.handlePLCStatus)
		return
	}
	// RequireRole is an exact-match allowlist, so viewer+ lists every role.
	mux.Handle(pattern, withMiddleware(http.HandlerFunc(s.handlePLCStatus),
		authMiddleware(s.authTokens),
		auth.RequireRole(auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin)))
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
