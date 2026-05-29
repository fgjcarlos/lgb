package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
)

// ─── Fake PLCManager for PLC handler tests ───────────────────────────────────

// fakePLCManager records Reload invocations for assertion in tests.
type fakePLCManager struct {
	mu          sync.Mutex
	reloadCount int
	reloadErr   error
}

func (f *fakePLCManager) Start(_ context.Context) error { return nil }
func (f *fakePLCManager) Stop() error                   { return nil }
func (f *fakePLCManager) Reload(_ context.Context, _ *config.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloadCount++
	return f.reloadErr
}
func (f *fakePLCManager) ReloadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloadCount
}

// ─── Test server helpers ──────────────────────────────────────────────────────

// newPLCTestServer returns a running test server with an in-memory plcStore,
// a fake PLCManager, and a real TokenService. No audit logger is wired — use
// newPLCTestServerAuditing when a test needs to assert audit events.
func newPLCTestServer(t *testing.T) (
	store *plcstore.Store,
	mgr *fakePLCManager,
	tokens *auth.TokenService,
	baseURL string,
	stop func(),
) {
	store, mgr, tokens, baseURL, _, stop = newPLCTestServerAuditing(t, false)
	return
}

// newPLCTestServerAuditing is like newPLCTestServer but, when withAudit is
// true, wires a real auth.AuditLogger pointing at a temp dir and returns that
// dir so the test can read events.jsonl and assert the recorded events.
// auditDir is "" when withAudit is false.
func newPLCTestServerAuditing(t *testing.T, withAudit bool) (
	store *plcstore.Store,
	mgr *fakePLCManager,
	tokens *auth.TokenService,
	baseURL string,
	auditDir string,
	stop func(),
) {
	t.Helper()
	ctx := context.Background()
	store, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open plcstore: %v", err)
	}

	tokens = auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	mgr = &fakePLCManager{}

	opts := Opts{
		AuthTokens: tokens,
		PLCStore:   store,
		PLCMgr:     mgr,
	}

	if withAudit {
		auditDir = t.TempDir()
		real, err := auth.OpenAuditLogger(auditDir)
		if err != nil {
			t.Fatalf("open audit logger: %v", err)
		}
		t.Cleanup(func() { _ = real.Close() })
		opts.AuditLog = real
	}

	_, url, stopSrv := startAPITestServerWithPLCStore(t, mgr, opts, store)
	baseURL = url
	stop = func() {
		stopSrv()
		_ = store.Close()
	}
	return
}

// readAuditActions reads events.jsonl from dir and returns the (action, detail)
// pairs in order. Fails the test if the file cannot be read.
func readAuditActions(t *testing.T, dir string) []auth.AuditEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	var events []auth.AuditEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev auth.AuditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal audit line %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

// startAPITestServerWithPLCStore creates a test server with PLCStore wired.
func startAPITestServerWithPLCStore(
	t *testing.T,
	mgr PLCManager,
	opts Opts,
	store *plcstore.Store,
) (*Server, string, func()) {
	t.Helper()
	opts.PLCMgr = mgr
	opts.PLCStore = store
	return startAPITestServerWithOpts(t, mgr, opts)
}

// ─── GET /api/plcs ────────────────────────────────────────────────────────────

// TestHandleListPLCs_EmptyReturnsEmptyArray verifies that an empty store
// returns {"data":[]} (never null). (PCS-API-2.1)
func TestHandleListPLCs_EmptyReturnsEmptyArray(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	_ = store

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 0 {
		t.Errorf("expected empty data array, got %d items", len(body.Data))
	}
}

// TestHandleListPLCs_TwoPLCsReturnsBoth verifies the list returns seeded PLCs.
// (PCS-API-2.1)
func TestHandleListPLCs_TwoPLCsReturnsBoth(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "alpha", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})
	_ = store.Create(ctx, config.PLC{Name: "beta", Address: "10.0.0.2:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("expected 2 PLCs, got %d", len(body.Data))
	}
}

// TestHandleListPLCs_Unauthed returns 401. (PCS-API-2.1)
func TestHandleListPLCs_Unauthed(t *testing.T) {
	_, _, _, baseURL, stop := newPLCTestServer(t)
	defer stop()

	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs", "", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ─── GET /api/plcs/{name} ────────────────────────────────────────────────────

// TestHandleGetPLC_Found returns 200 with the PLC. (PCS-API-2.2)
func TestHandleGetPLC_Found(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs/line1", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Name != "line1" {
		t.Errorf("expected name 'line1', got %q", body.Data.Name)
	}
}

// TestHandleGetPLC_Missing returns 404. (PCS-API-2.2)
func TestHandleGetPLC_Missing(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs/nonexistent", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "plc_not_found")
}

// ─── POST /api/plcs ──────────────────────────────────────────────────────────

// TestHandleCreatePLC_Admin201 creates a PLC as admin and asserts 201 + Reload called.
// (PCS-API-2.3, PCS-AUDIT-4.1)
func TestHandleCreatePLC_Admin201(t *testing.T) {
	_, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"line1","address":"10.0.0.1:44818","slot":0,"socket_timeout":"5s","scan_rate":"1s","keep_alive":false,"path":"","tags":[]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	if mgr.ReloadCount() < 1 {
		t.Error("expected Reload to be called after create")
	}
}

// TestHandleCreatePLC_ViewerGets403 verifies viewer role is rejected. (PCS-API-2.3)
func TestHandleCreatePLC_ViewerGets403(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(2, "viewer", auth.RoleViewer)
	body := `{"name":"line1","address":"10.0.0.1:44818","slot":0,"socket_timeout":"5s","scan_rate":"1s"}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// TestHandleCreatePLC_BadBody400 returns 400 for invalid JSON. (PCS-API-2.3)
func TestHandleCreatePLC_BadBody400(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", `{not json`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestHandleCreatePLC_InvalidPLC400 returns 400 with invalid_plc on validation failure.
// (PCS-API-2.3, PCS-STORE-1.7)
func TestHandleCreatePLC_InvalidPLC400(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	// Missing address is invalid
	body := `{"name":"line1","address":"","slot":0,"scan_rate":"1s","socket_timeout":"5s"}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "invalid_plc")
}

// TestHandleCreatePLC_Duplicate409 returns 409 when name already exists. (PCS-API-2.3)
func TestHandleCreatePLC_Duplicate409(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"line1","address":"10.0.0.2:44818","slot":0,"socket_timeout":"5s","scan_rate":"1s","tags":[]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "duplicate_plc")
}

// ─── PUT /api/plcs/{name} ────────────────────────────────────────────────────

// TestHandleUpdatePLC_Admin200 updates a PLC and asserts 200 + Reload called.
// (PCS-API-2.4, PCS-AUDIT-4.1)
func TestHandleUpdatePLC_Admin200(t *testing.T) {
	store, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"line1","address":"10.0.0.9:44818","slot":0,"socket_timeout":"5s","scan_rate":"2s","keep_alive":false,"path":"","tags":[]}`
	resp := doRequest(t, http.MethodPut, baseURL+"/api/plcs/line1", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if mgr.ReloadCount() < 1 {
		t.Error("expected Reload to be called after update")
	}
}

// TestHandleUpdatePLC_ViewerGets403 verifies viewer role is rejected. (PCS-API-2.4)
func TestHandleUpdatePLC_ViewerGets403(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(2, "viewer", auth.RoleViewer)
	body := `{"name":"line1","address":"10.0.0.9:44818","slot":0,"socket_timeout":"5s","scan_rate":"2s"}`
	resp := doRequest(t, http.MethodPut, baseURL+"/api/plcs/line1", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// TestHandleUpdatePLC_Missing404 returns 404 for an unknown PLC. (PCS-API-2.4)
func TestHandleUpdatePLC_Missing404(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"ghost","address":"10.0.0.9:44818","slot":0,"socket_timeout":"5s","scan_rate":"2s","tags":[]}`
	resp := doRequest(t, http.MethodPut, baseURL+"/api/plcs/ghost", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "plc_not_found")
}

// ─── DELETE /api/plcs/{name} ─────────────────────────────────────────────────

// TestHandleDeletePLC_Admin204 deletes a PLC and asserts 204 + Reload called.
// (PCS-API-2.5, PCS-AUDIT-4.1)
func TestHandleDeletePLC_Admin204(t *testing.T) {
	store, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodDelete, baseURL+"/api/plcs/line1", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	if mgr.ReloadCount() < 1 {
		t.Error("expected Reload to be called after delete")
	}
}

// TestHandleDeletePLC_ViewerGets403 verifies viewer role is rejected. (PCS-API-2.5)
func TestHandleDeletePLC_ViewerGets403(t *testing.T) {
	store, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	ctx := context.Background()

	_ = store.Create(ctx, config.PLC{Name: "line1", Address: "10.0.0.1:44818", ScanRate: "1s", SocketTimeout: "5s"})

	tok, _ := tokens.Issue(2, "viewer", auth.RoleViewer)
	resp := doRequest(t, http.MethodDelete, baseURL+"/api/plcs/line1", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// TestHandleDeletePLC_Missing404 returns 404 for an unknown PLC. (PCS-API-2.5)
func TestHandleDeletePLC_Missing404(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	resp := doRequest(t, http.MethodDelete, baseURL+"/api/plcs/ghost", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "plc_not_found")
}

// ─── PCS-RELOAD-3.1: empty manager handles Reload without panic ───────────────

// TestHandleCreatePLC_EmptyManager_ReloadNoOp verifies that posting to an empty
// manager does not panic and Reload returns nil. (PCS-RELOAD-3.1)
func TestHandleCreatePLC_EmptyManager_ReloadNoOp(t *testing.T) {
	store, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()
	_ = store

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"newplc","address":"10.0.0.5:44818","slot":0,"socket_timeout":"5s","scan_rate":"1s","tags":[]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if mgr.ReloadCount() < 1 {
		t.Error("expected Reload to be called even with an empty manager")
	}
}

// ─── PCS-AUDIT-4.1: audit NOT emitted on validation failure ─────────────────

// TestHandleCreatePLC_ValidationFail_NoAuditEmit verifies that a failed POST
// (validation error) does not emit an audit event. (PCS-AUDIT-4.1)
func TestHandleCreatePLC_ValidationFail_NoAuditEmit(t *testing.T) {
	// We use a separate store-level audit event counter: since Server holds
	// *auth.AuditLogger directly, we open a real one and count via file size.
	// Actually, the simplest assertion here is structural: if status is 400,
	// no audit path was reached because of early return. We verify indirectly
	// via Reload not being called (audit is only called after store writes, and
	// Reload also only happens after store writes).
	_, mgr, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	// Empty address triggers ValidatePLC failure.
	body := `{"name":"bad","address":"","slot":0,"scan_rate":"1s","socket_timeout":"5s"}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on validation failure, got %d", resp.StatusCode)
	}

	// Reload must NOT have been called (store was never written).
	if mgr.ReloadCount() != 0 {
		t.Errorf("expected Reload count to be 0 on validation failure, got %d", mgr.ReloadCount())
	}
}

// ─── Full round-trip: response body includes PLC fields ──────────────────────

// TestHandleCreatePLC_ResponseBody verifies the created PLC fields are echoed back.
// (PCS-API-2.3)
func TestHandleCreatePLC_ResponseBody(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(1, "admin", auth.RoleAdmin)
	body := `{"name":"line1","address":"10.0.0.1:44818","slot":2,"socket_timeout":"5s","scan_rate":"1s","keep_alive":true,"path":"/device","tags":[{"name":"Temp","type":"Float","writable":false}]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Name    string `json:"name"`
			Address string `json:"address"`
			Slot    int    `json:"slot"`
			Tags    []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"tags"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Data.Name != "line1" {
		t.Errorf("expected name 'line1', got %q", result.Data.Name)
	}
	if result.Data.Address != "10.0.0.1:44818" {
		t.Errorf("expected address '10.0.0.1:44818', got %q", result.Data.Address)
	}
	if result.Data.Slot != 2 {
		t.Errorf("expected slot 2, got %d", result.Data.Slot)
	}
	if len(result.Data.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(result.Data.Tags))
	}
	if result.Data.Tags[0].Name != "Temp" {
		t.Errorf("expected tag name 'Temp', got %q", result.Data.Tags[0].Name)
	}
}

// ─── GET access (viewer+) ──────────────────────────────────────────────────────

// TestHandleListPLCs_ViewerGets200 verifies that a viewer (read-only) role can
// LIST PLCs — reads are viewer+, consistent with GET /api/config/mappings.
// (PCS-API-2.1)
func TestHandleListPLCs_ViewerGets200(t *testing.T) {
	_, _, tokens, baseURL, stop := newPLCTestServer(t)
	defer stop()

	tok, _ := tokens.Issue(2, "viewer", auth.RoleViewer)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/plcs", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for viewer reading PLC list, got %d", resp.StatusCode)
	}
}

// ─── Audit events (PCS-AUDIT-4.1) ───────────────────────────────────────────────

// TestHandleCreatePLC_AuditsCreate verifies a successful create writes a
// plc.create audit event carrying the actor username and the PLC name.
func TestHandleCreatePLC_AuditsCreate(t *testing.T) {
	_, _, tokens, baseURL, auditDir, stop := newPLCTestServerAuditing(t, true)
	defer stop()

	tok, _ := tokens.Issue(1, "alice", auth.RoleAdmin)
	body := `{"name":"line1","address":"10.0.0.1:44818","slot":0,"socket_timeout":"5s","scan_rate":"1s","keep_alive":false,"path":"","tags":[]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	events := readAuditActions(t, auditDir)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	if events[0].Action != "plc.create" {
		t.Errorf("expected action 'plc.create', got %q", events[0].Action)
	}
	if events[0].Username != "alice" {
		t.Errorf("expected actor 'alice', got %q", events[0].Username)
	}
	if events[0].Detail != "line1" {
		t.Errorf("expected detail (PLC name) 'line1', got %q", events[0].Detail)
	}
}

// TestHandleDeletePLC_AuditsDelete verifies a successful delete writes a
// plc.delete audit event for the targeted PLC.
func TestHandleDeletePLC_AuditsDelete(t *testing.T) {
	_, _, tokens, baseURL, auditDir, stop := newPLCTestServerAuditing(t, true)
	defer stop()

	tok, _ := tokens.Issue(1, "alice", auth.RoleAdmin)
	body := `{"name":"line1","address":"10.0.0.1:44818","slot":0,"socket_timeout":"5s","scan_rate":"1s","keep_alive":false,"path":"","tags":[]}`
	doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok).Body.Close()

	resp := doRequest(t, http.MethodDelete, baseURL+"/api/plcs/line1", "", tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	events := readAuditActions(t, auditDir)
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events (create + delete), got %d", len(events))
	}
	last := events[len(events)-1]
	if last.Action != "plc.delete" {
		t.Errorf("expected action 'plc.delete', got %q", last.Action)
	}
	if last.Detail != "line1" {
		t.Errorf("expected detail 'line1', got %q", last.Detail)
	}
}

// TestHandleCreatePLC_ValidationFail_NoAudit verifies that a rejected (invalid)
// create writes NO audit event — failures must not be recorded as actions.
func TestHandleCreatePLC_ValidationFail_NoAudit(t *testing.T) {
	_, _, tokens, baseURL, auditDir, stop := newPLCTestServerAuditing(t, true)
	defer stop()

	tok, _ := tokens.Issue(1, "alice", auth.RoleAdmin)
	// Empty address → ValidatePLC rejects with 400 before any store write.
	body := `{"name":"bad","address":"","slot":0,"socket_timeout":"5s","scan_rate":"1s","keep_alive":false,"path":"","tags":[]}`
	resp := doRequest(t, http.MethodPost, baseURL+"/api/plcs", body, tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// No events.jsonl write should have occurred. The file may not exist at all.
	if data, err := os.ReadFile(filepath.Join(auditDir, "events.jsonl")); err == nil {
		if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			t.Errorf("expected no audit events on validation failure, got: %q", trimmed)
		}
	}
}

// compile-time guard: bytes import used by doRequest (defined in api_users_test.go).
var _ = bytes.NewBuffer
