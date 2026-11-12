package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/aclstore"
	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// Compile-time guard: plcstoreTagReader must implement writeguard.TagReadable.
var _ writeguard.TagReadable = (*plcstoreTagReader)(nil)

// ─── Write-capable PLCManager double ─────────────────────────────────────────

// writePLCManager implements PLCManager and records WriteTag calls.
type writePLCManager struct {
	fakePLCManager
	mu            sync.Mutex
	writeTagCalls []writeTagCall
	writeTagErr   error
}

type writeTagCall struct {
	PLC string
	Tag string
	Val any
}

func (m *writePLCManager) WriteTag(plcName, tag string, val any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeTagCalls = append(m.writeTagCalls, writeTagCall{PLC: plcName, Tag: tag, Val: val})
	return m.writeTagErr
}

func (m *writePLCManager) lastWrite() (writeTagCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.writeTagCalls) == 0 {
		return writeTagCall{}, false
	}
	return m.writeTagCalls[len(m.writeTagCalls)-1], true
}

// ─── Test server builder ──────────────────────────────────────────────────────

// writeTestServer groups all dependencies for write endpoint tests.
type writeTestServer struct {
	plcStore *plcstore.Store
	aclStore *aclstore.Store
	guard    *writeguard.Guard
	mgr      *writePLCManager
	tokens   *auth.TokenService
	baseURL  string
	auditDir string
	stop     func()
}

// newWriteTestServer creates a running server with write ACL wired.
// It seeds the plcstore with PLC "Silo-1" containing two tags:
//   - "Feed.Rate"    (Writable=true)
//   - "Emergency.Stop" (Writable=false)
func newWriteTestServer(t *testing.T) *writeTestServer {
	t.Helper()
	ctx := context.Background()

	pStore, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}

	aStore, err := aclstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("aclstore.Open: %v", err)
	}

	// Seed PLC with two tags.
	if err := pStore.Create(ctx, config.PLC{
		Name:          "Silo-1",
		Address:       "127.0.0.1:44818",
		ScanRate:      "1s",
		SocketTimeout: "5s",
		Tags: []config.TagDef{
			{Name: "Feed.Rate", Type: "Float", Writable: true},
			{Name: "Emergency.Stop", Type: "Boolean", Writable: false},
		},
	}); err != nil {
		t.Fatalf("seed PLC: %v", err)
	}

	tr := &plcstoreTagReader{store: pStore}
	guard := writeguard.NewGuard(tr, aStore)

	mgr := &writePLCManager{}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)

	auditDir := t.TempDir()
	auditLogger, err := auth.OpenAuditLogger(auditDir)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}
	t.Cleanup(func() { _ = auditLogger.Close() })

	opts := Opts{
		AuthTokens: tokens,
		PLCStore:   pStore,
		PLCMgr:     mgr,
		ACLStore:   aStore,
		WriteGuard: guard,
		AuditLog:   auditLogger,
	}

	_, url, stopSrv := startAPITestServerWithOpts(t, mgr, opts)

	stop := func() {
		stopSrv()
		_ = pStore.Close()
		_ = aStore.Close()
	}

	return &writeTestServer{
		plcStore: pStore,
		aclStore: aStore,
		guard:    guard,
		mgr:      mgr,
		tokens:   tokens,
		baseURL:  url,
		auditDir: auditDir,
		stop:     stop,
	}
}

// operatorToken returns a token for "op1" with operator role.
func (s *writeTestServer) operatorToken(t *testing.T) string {
	t.Helper()
	tok, err := s.tokens.Issue(10, "op1", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue operator token: %v", err)
	}
	return tok
}

// adminToken returns a token for "root" with admin role.
func (s *writeTestServer) adminToken(t *testing.T) string {
	t.Helper()
	tok, err := s.tokens.Issue(1, "root", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	return tok
}

// viewerToken returns a token for "viewer1" with viewer role.
func (s *writeTestServer) viewerToken(t *testing.T) string {
	t.Helper()
	tok, err := s.tokens.Issue(20, "viewer1", auth.RoleViewer)
	if err != nil {
		t.Fatalf("issue viewer token: %v", err)
	}
	return tok
}

// grantOperatorWrite seeds an ACL row allowing operator to write Feed.Rate on Silo-1.
func (s *writeTestServer) grantOperatorWrite(t *testing.T) {
	t.Helper()
	if err := s.aclStore.CreateRule(context.Background(), aclstore.ACLRule{
		Role: "operator", PLC: "Silo-1", Tag: "Feed.Rate", AllowWrite: true,
	}); err != nil {
		t.Fatalf("create ACL rule: %v", err)
	}
}

// readAuditEventsWrite reads the audit log from the test server's auditDir.
func (s *writeTestServer) readAuditEvents(t *testing.T) []auth.AuditEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.auditDir, "events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read audit: %v", err)
	}
	var events []auth.AuditEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev auth.AuditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal audit: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func writeURL(base, plc, tag string) string {
	return fmt.Sprintf("%s/api/plcs/%s/tags/%s/write", base, plc, tag)
}

// ─── TWA-HTTP-3.1: Authorized write succeeds (200) ──────────────────────────

// TestHandleWriteTag_AuthorizedOperator_200 verifies that an operator with an ACL
// rule gets 200 and the write is delegated to the PLC manager.
// (TWA-HTTP-3.1, TWA-ENFORCE-2.1, TWA-AUDIT-4.1)
func TestHandleWriteTag_AuthorizedOperator_200(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()
	ts.grantOperatorWrite(t)

	tok := ts.operatorToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"),
		`{"value":2.5}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Driver must have been called.
	call, ok := ts.mgr.lastWrite()
	if !ok {
		t.Fatal("expected Manager.WriteTag to be called, but it was not")
	}
	if call.PLC != "Silo-1" || call.Tag != "Feed.Rate" {
		t.Errorf("unexpected write call: %+v", call)
	}
}

// ─── TWA-HTTP-3.1: ACL deny → 403 write_denied ──────────────────────────────

// TestHandleWriteTag_ACLDeny_403 verifies that when no ACL row exists for the
// actor's role, the response is 403 with code write_denied.
// (TWA-HTTP-3.1, TWA-ENFORCE-2.2)
func TestHandleWriteTag_ACLDeny_403(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()
	// No ACL rule seeded → deny-by-default.

	tok := ts.viewerToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"),
		`{"value":1.0}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "write_denied")
}

// TestHandleWriteTag_MasterSwitchOff_403 verifies that a write to a Writable=false
// tag returns 403 even with an admin token.
// (TWA-HTTP-3.1, TWA-ENFORCE-2.1)
func TestHandleWriteTag_MasterSwitchOff_403(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()

	tok := ts.adminToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Emergency.Stop"),
		`{"value":true}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "write_denied")
}

// ─── TWA-ENFORCE-2.3: Unknown tag → 404 tag_not_found ───────────────────────

// TestHandleWriteTag_UnknownTag_404 verifies that writing to a non-existent tag
// returns 404 with code tag_not_found.
// (TWA-ENFORCE-2.3, TWA-HTTP-3.1)
func TestHandleWriteTag_UnknownTag_404(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()

	tok := ts.adminToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Ghost.Tag"),
		`{"value":1}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "tag_not_found")

	// An unknown tag is rejected before authorization: no audit event (#62).
	if events := ts.readAuditEvents(t); len(events) != 0 {
		t.Errorf("audit events on unknown tag = %d; want 0: %+v", len(events), events)
	}
}

// spyACLReader is a writeguard.ACLReader that counts lookups before delegating.
type spyACLReader struct {
	mu    sync.Mutex
	calls int
	next  writeguard.ACLReader
}

func (s *spyACLReader) CanWrite(ctx context.Context, role, plc, tag string) (bool, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.next.CanWrite(ctx, role, plc, tag)
}

func (s *spyACLReader) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestHandleWriteTag_UnknownTag_SkipsACLAndAudit triangulates the unknown-tag
// path (#62 SUGGESTION-1): even for an operator holding an ACL grant on the PLC,
// a write to a non-existent tag returns 404 tag_not_found without consulting
// the ACL and without emitting an audit event. A control write to a known tag
// proves the spy and the audit sink are live. (TWA-ENFORCE-2.3)
func TestHandleWriteTag_UnknownTag_SkipsACLAndAudit(t *testing.T) {
	ctx := context.Background()

	pStore, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	defer pStore.Close()
	if err := pStore.Create(ctx, config.PLC{
		Name:          "Silo-1",
		Address:       "127.0.0.1:44818",
		ScanRate:      "1s",
		SocketTimeout: "5s",
		Tags:          []config.TagDef{{Name: "Feed.Rate", Type: "Float", Writable: true}},
	}); err != nil {
		t.Fatalf("seed PLC: %v", err)
	}

	aStore, err := aclstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("aclstore.Open: %v", err)
	}
	defer aStore.Close()
	for _, tag := range []string{"Feed.Rate", "Ghost.Tag"} {
		if err := aStore.CreateRule(ctx, aclstore.ACLRule{
			Role: "operator", PLC: "Silo-1", Tag: tag, AllowWrite: true,
		}); err != nil {
			t.Fatalf("create ACL rule: %v", err)
		}
	}
	spy := &spyACLReader{next: aStore}

	auditDir := t.TempDir()
	auditLogger, err := auth.OpenAuditLogger(auditDir)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}
	defer auditLogger.Close()

	mgr := &writePLCManager{}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, url, stopSrv := startAPITestServerWithOpts(t, mgr, Opts{
		AuthTokens: tokens,
		PLCStore:   pStore,
		PLCMgr:     mgr,
		ACLStore:   aStore,
		AuditLog:   auditLogger,
		WriteGuard: writeguard.NewGuard(NewPLCStoreTagReader(pStore), spy),
	})
	defer stopSrv()

	tok, err := tokens.Issue(10, "op1", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue operator token: %v", err)
	}
	auditLines := func() int {
		data, err := os.ReadFile(filepath.Join(auditDir, "events.jsonl"))
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatalf("read audit: %v", err)
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			return 0
		}
		return len(strings.Split(trimmed, "\n"))
	}

	resp := doRequest(t, http.MethodPost, writeURL(url, "Silo-1", "Ghost.Tag"), `{"value":1}`, tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "tag_not_found")
	if n := spy.callCount(); n != 0 {
		t.Errorf("ACL lookups on unknown tag = %d; want 0", n)
	}
	if n := auditLines(); n != 0 {
		t.Errorf("audit events on unknown tag = %d; want 0", n)
	}
	if _, ok := mgr.lastWrite(); ok {
		t.Error("Manager.WriteTag called for an unknown tag")
	}

	// Control: a known tag goes through the ACL and is audited.
	ok := doRequest(t, http.MethodPost, writeURL(url, "Silo-1", "Feed.Rate"), `{"value":2.5}`, tok)
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("control write: expected 200, got %d", ok.StatusCode)
	}
	if n := spy.callCount(); n != 1 {
		t.Errorf("ACL lookups after control write = %d; want 1", n)
	}
	if n := auditLines(); n != 1 {
		t.Errorf("audit events after control write = %d; want 1", n)
	}
}

// ─── TWA-HTTP-3.1: Unauthenticated → 401 ────────────────────────────────────

// TestHandleWriteTag_NoToken_401 verifies that missing Authorization returns 401.
// (TWA-HTTP-3.1)
func TestHandleWriteTag_NoToken_401(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()

	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"),
		`{"value":2.5}`, "") // no token
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ─── TWA-HTTP-3.1: Bad body → 400 ───────────────────────────────────────────

// TestHandleWriteTag_BadBody_400 verifies that a malformed JSON body returns 400.
// (TWA-HTTP-3.1)
func TestHandleWriteTag_BadBody_400(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()

	tok := ts.adminToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"),
		`{not json`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "bad_request")
}

// ─── TWA-AUDIT-4.1: Audit events ────────────────────────────────────────────

// TestHandleWriteTag_Deny_EmitsAuditEventWithSourceHTTP verifies that a denied
// HTTP write still emits an audit event with source="http". (TWA-AUDIT-4.1)
func TestHandleWriteTag_Deny_EmitsAuditEventWithSourceHTTP(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()
	// No ACL rule → deny

	tok := ts.viewerToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"),
		`{"value":1.0}`, tok)
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}

	events := ts.readAuditEvents(t)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event on deny, got 0")
	}
	ev := events[0]
	if ev.Action != "tag.write" {
		t.Errorf("expected action 'tag.write', got %q", ev.Action)
	}
	if !strings.Contains(ev.Detail, `source=http`) {
		t.Errorf("expected Detail to contain 'source=http', got %q", ev.Detail)
	}
	if !strings.Contains(ev.Detail, `outcome=deny`) {
		t.Errorf("expected Detail to contain 'outcome=deny', got %q", ev.Detail)
	}
}

// TestHandleWriteTag_Allow_EmitsAuditEventWithSourceHTTP verifies that an allowed
// HTTP write emits an audit event with source="http" and outcome="allow". (TWA-AUDIT-4.1)
func TestHandleWriteTag_Allow_EmitsAuditEventWithSourceHTTP(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()
	ts.grantOperatorWrite(t)

	tok := ts.operatorToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"),
		`{"value":3.14}`, tok)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	events := ts.readAuditEvents(t)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event on allow, got 0")
	}
	ev := events[0]
	if ev.Action != "tag.write" {
		t.Errorf("expected action 'tag.write', got %q", ev.Action)
	}
	if !strings.Contains(ev.Detail, `source=http`) {
		t.Errorf("expected Detail to contain 'source=http', got %q", ev.Detail)
	}
	if !strings.Contains(ev.Detail, `outcome=allow`) {
		t.Errorf("expected Detail to contain 'outcome=allow', got %q", ev.Detail)
	}
	if ev.Username != "op1" {
		t.Errorf("expected Username 'op1', got %q", ev.Username)
	}
}

// ─── T6: exported store→TagReadable adapter and guard wiring ─────────────────

// TestNewPLCStoreTagReader_ExposesPerTagFlags verifies that the exported
// store→writeguard.TagReadable adapter reports every per-tag safety switch,
// including the OPC UA opt-in, so cmd can build the shared write guard without
// duplicating the adapter. (PCS-CFG-5.2, PCS-CFG-5.3, T6)
func TestNewPLCStoreTagReader_ExposesPerTagFlags(t *testing.T) {
	ctx := context.Background()
	store, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	defer store.Close()

	if err := store.Create(ctx, config.PLC{
		Name:          "Silo-1",
		Address:       "127.0.0.1:44818",
		ScanRate:      "1s",
		SocketTimeout: "5s",
		Tags: []config.TagDef{
			{Name: "Feed.Rate", Type: "Float", Writable: true, DCMDEnabled: true, OPCUAEnabled: true},
			{Name: "Motor.Speed", Type: "Float", Writable: false},
		},
	}); err != nil {
		t.Fatalf("seed PLC: %v", err)
	}

	reader := NewPLCStoreTagReader(store)

	meta, ok := reader.TagMeta(ctx, "Silo-1", "Feed.Rate")
	if !ok {
		t.Fatal("TagMeta returned ok=false for a known tag")
	}
	if !meta.Writable {
		t.Error("meta.Writable = false; want true")
	}
	if !meta.DCMDEnabled {
		t.Error("meta.DCMDEnabled = false; want true")
	}
	if !meta.OPCUAEnabled {
		t.Error("meta.OPCUAEnabled = false; want true")
	}

	meta, ok = reader.TagMeta(ctx, "Silo-1", "Motor.Speed")
	if !ok {
		t.Fatal("TagMeta returned ok=false for a known tag")
	}
	if meta.Writable || meta.DCMDEnabled || meta.OPCUAEnabled {
		t.Errorf("meta = %+v; want all flags false", meta)
	}

	if _, ok := reader.TagMeta(ctx, "Silo-1", "Ghost.Tag"); ok {
		t.Error("TagMeta returned ok=true for an unknown tag")
	}
}

// fakeTagMetaReader is a writeguard.TagReadable double used to prove that an
// injected guard takes precedence over the auto-constructed one.
type fakeTagMetaReader struct {
	meta writeguard.TagMeta
}

func (f *fakeTagMetaReader) TagMeta(_ context.Context, _, _ string) (writeguard.TagMeta, bool) {
	return f.meta, true
}

// TestNew_InjectedWriteGuardIsUsed verifies that server.New uses the injected
// write guard instead of auto-constructing one from the stores: the HTTP write
// path authorizes against the injected guard's metadata even though the store
// says the tag is not writable. (T6, TWA-ENFORCE-2.1)
func TestNew_InjectedWriteGuardIsUsed(t *testing.T) {
	ctx := context.Background()

	pStore, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	defer pStore.Close()

	aStore, err := aclstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("aclstore.Open: %v", err)
	}
	defer aStore.Close()

	// The store says Feed.Rate is NOT writable…
	if err := pStore.Create(ctx, config.PLC{
		Name:          "Silo-1",
		Address:       "127.0.0.1:44818",
		ScanRate:      "1s",
		SocketTimeout: "5s",
		Tags: []config.TagDef{
			{Name: "Feed.Rate", Type: "Float", Writable: false},
		},
	}); err != nil {
		t.Fatalf("seed PLC: %v", err)
	}

	// …but the injected guard says it is.
	injected := writeguard.NewGuard(&fakeTagMetaReader{meta: writeguard.TagMeta{Writable: true}}, aStore)

	if err := aStore.CreateRule(ctx, aclstore.ACLRule{
		Role: "operator", PLC: "Silo-1", Tag: "Feed.Rate", AllowWrite: true,
	}); err != nil {
		t.Fatalf("create ACL rule: %v", err)
	}

	mgr := &writePLCManager{}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)

	opts := Opts{
		AuthTokens: tokens,
		PLCStore:   pStore,
		PLCMgr:     mgr,
		ACLStore:   aStore,
		WriteGuard: injected,
	}
	_, url, stopSrv := startAPITestServerWithOpts(t, mgr, opts)
	defer stopSrv()

	tok, err := tokens.Issue(10, "op1", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue operator token: %v", err)
	}

	resp := doRequest(t, http.MethodPost, writeURL(url, "Silo-1", "Feed.Rate"), `{"value":2.5}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (the injected guard must be used), got %d", resp.StatusCode)
	}
	if call, ok := mgr.lastWrite(); !ok {
		t.Fatal("expected Manager.WriteTag to be called")
	} else if call.Tag != "Feed.Rate" {
		t.Errorf("unexpected write call: %+v", call)
	}
}

// ─── PRESERVE: the HTTP write endpoint is unchanged ──────────────────────────

// TestPreserve_HandleWriteTag_HTTPPathUnchanged pins the HTTP write behaviour
// after the OPC UA surface was added: an operator with an ACL rule gets 200,
// the PLC manager is called and the audit event carries source=http.
// (TWA-HTTP-3.1, TWA-AUDIT-4.1 — PRESERVE)
func TestPreserve_HandleWriteTag_HTTPPathUnchanged(t *testing.T) {
	ts := newWriteTestServer(t)
	defer ts.stop()
	ts.grantOperatorWrite(t)

	tok := ts.operatorToken(t)
	resp := doRequest(t, http.MethodPost, writeURL(ts.baseURL, "Silo-1", "Feed.Rate"), `{"value":2.5}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if call, ok := ts.mgr.lastWrite(); !ok {
		t.Fatal("expected Manager.WriteTag to be called")
	} else if call.Val != float32(2.5) {
		// Feed.Rate is declared Float, so the value reaches the driver as
		// float32 (issue #61: coerced to the declared type).
		t.Errorf("written value = %#v; want float32(2.5)", call.Val)
	}

	events := ts.readAuditEvents(t)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event, got 0")
	}
	if !strings.Contains(events[0].Detail, "source=http") {
		t.Errorf("Detail = %q; want it to contain source=http", events[0].Detail)
	}
	if events[0].Username != "op1" {
		t.Errorf("Username = %q; want op1", events[0].Username)
	}
}
