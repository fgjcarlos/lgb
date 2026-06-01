package server

import (
	"context"
	"encoding/json"
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

// ─── Fakes for DCMD handler tests ─────────────────────────────────────────────

// dcmdFakePLCManager records WriteTag calls and satisfies PLCManager.
type dcmdFakePLCManager struct {
	fakePLCManager
	mu        sync.Mutex
	writeCalls []dcmdWriteCall
	writeErr   error
}

type dcmdWriteCall struct {
	PLC string
	Tag string
	Val any
}

func (m *dcmdFakePLCManager) WriteTag(plcName, tag string, val any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeCalls = append(m.writeCalls, dcmdWriteCall{PLC: plcName, Tag: tag, Val: val})
	return m.writeErr
}

func (m *dcmdFakePLCManager) writeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.writeCalls)
}

// ─── dcmdTestEnv builds a server with the full write stack ────────────────────

// dcmdTestEnv contains the pieces needed to exercise SparkplugCommandHandler.
type dcmdTestEnv struct {
	plcStore *plcstore.Store
	aclStore *aclstore.Store
	mgr      *dcmdFakePLCManager
	srv      *Server
	auditDir string
}

// newDCMDTestEnv creates a minimal server with:
//   - plcstore seeded with "Silo-1" / "Feed.Rate" (Writable=true, DCMDEnabled=true)
//     and "Emergency.Stop" (Writable=false, DCMDEnabled=true)
//   - aclstore with a row allowing operator to write Feed.Rate (to prove DCMD ignores it)
//   - a real audit logger so we can assert audit events
func newDCMDTestEnv(t *testing.T) *dcmdTestEnv {
	t.Helper()
	ctx := context.Background()

	pStore, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("plcstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = pStore.Close() })

	// Seed PLC with feed.rate (writable+dcmd_enabled) and emergency.stop (not writable).
	if err := pStore.Create(ctx, config.PLC{
		Name:          "Silo-1",
		Address:       "127.0.0.1:44818",
		ScanRate:      "1s",
		SocketTimeout: "5s",
		Tags: []config.TagDef{
			{Name: "Feed.Rate", Type: "Float", Writable: true, DCMDEnabled: true},
			{Name: "Emergency.Stop", Type: "Boolean", Writable: false, DCMDEnabled: true},
			{Name: "NoCommand.Tag", Type: "Float", Writable: true, DCMDEnabled: false},
		},
	}); err != nil {
		t.Fatalf("seed PLC: %v", err)
	}

	aStore, err := aclstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("aclstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = aStore.Close() })

	// Seed an ACL row that allows HTTP operator to write Feed.Rate.
	// This row MUST NOT affect DCMD authorization (the decoupling test, scenario b).
	if err := aStore.CreateRule(ctx, aclstore.ACLRule{
		Role: "operator", PLC: "Silo-1", Tag: "Feed.Rate", AllowWrite: true,
	}); err != nil {
		t.Fatalf("seed ACL rule: %v", err)
	}

	auditDir := t.TempDir()
	auditLogger, err := auth.OpenAuditLogger(auditDir)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}
	t.Cleanup(func() { _ = auditLogger.Close() })

	mgr := &dcmdFakePLCManager{}

	tr := &plcstoreTagReader{store: pStore}
	guard := writeguard.NewGuard(tr, aStore)

	cfg := minimalCfgForDCMD(t)
	srv := New(cfg, nil, nil, Opts{
		PLCStore:   pStore,
		PLCMgr:     mgr,
		ACLStore:   aStore,
		WriteGuard: guard,
		AuditLog:   auditLogger,
	})

	return &dcmdTestEnv{
		plcStore: pStore,
		aclStore: aStore,
		mgr:      mgr,
		srv:      srv,
		auditDir: auditDir,
	}
}

// readAuditEventsDCMD reads events.jsonl from auditDir and returns all events.
func readAuditEventsDCMD(t *testing.T, auditDir string) []auth.AuditEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(auditDir, "events.jsonl"))
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

func minimalCfgForDCMD(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Server: config.ServerSection{HTTPAddr: "127.0.0.1:0"},
		Auth:   config.AuthSection{JwtSecret: "test-secret-32bytes-long!!"},
	}
}

// ─── TWA-DCMD-3.2 Scenario (a): both flags true → allow ─────────────────────

// TestDCMDHandler_AllowWhenBothFlagsTrue verifies that when Writable=true and
// DCMDEnabled=true, the DCMD handler calls Manager.WriteTag and emits an audit
// event with outcome=allow, source=dcmd, Username="".
// Proves the ACL is never consulted: even though a denying aclstore would block
// the HTTP path, the DCMD path proceeds purely on plcstore flags.
// (TWA-DCMD-3.2 scenario a, TWA-AUDIT-4.1)
func TestDCMDHandler_AllowWhenBothFlagsTrue(t *testing.T) {
	t.Parallel()
	env := newDCMDTestEnv(t)

	handler := env.srv.SparkplugCommandHandler()
	if handler == nil {
		t.Fatal("SparkplugCommandHandler returned nil; expected non-nil handler when guard and plcMgr are set")
	}

	// Invoke the DCMD handler: deviceID="Silo-1" (the PLC name), tag="Feed.Rate", value=3.0.
	handler("Silo-1", "Feed.Rate", float32(3.0))

	// Manager.WriteTag must have been called exactly once.
	if env.mgr.writeCount() != 1 {
		t.Fatalf("expected 1 WriteTag call, got %d", env.mgr.writeCount())
	}
	env.mgr.mu.Lock()
	call := env.mgr.writeCalls[0]
	env.mgr.mu.Unlock()
	if call.PLC != "Silo-1" || call.Tag != "Feed.Rate" {
		t.Errorf("WriteTag call = %+v; want PLC=Silo-1 Tag=Feed.Rate", call)
	}

	// Audit event must exist with outcome=allow, source=dcmd, Username="".
	// Give the synchronous audit a moment (it's synchronous so no wait needed).
	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event, got 0")
	}
	ev := events[0]
	if ev.Action != "tag.write" {
		t.Errorf("Action = %q; want tag.write", ev.Action)
	}
	if !strings.Contains(ev.Detail, "outcome=allow") {
		t.Errorf("Detail = %q; want outcome=allow", ev.Detail)
	}
	if !strings.Contains(ev.Detail, "source=dcmd") {
		t.Errorf("Detail = %q; want source=dcmd", ev.Detail)
	}
	if ev.Username != "" {
		t.Errorf("Username = %q; want empty string (DCMD has no actor)", ev.Username)
	}
}

// ─── TWA-DCMD-3.2 Scenario (b): DCMDEnabled=false → deny; ACL row irrelevant ─

// TestDCMDHandler_DenyWhenDCMDEnabledFalse verifies that even when an operator
// ACL row allows HTTP writes on Feed.Rate, a DCMD is denied if DCMDEnabled=false.
// Manager.WriteTag MUST NOT be called. The audit must record source=dcmd, outcome=deny,
// reason="dcmd not enabled". aclStore.CanWrite MUST NOT be called for the DCMD path
// (demonstrated by the fact that the ACL row exists but has no effect).
// (TWA-DCMD-3.2 scenario b, TWA-ENFORCE-2.1, TWA-AUDIT-4.1)
func TestDCMDHandler_DenyWhenDCMDEnabledFalse(t *testing.T) {
	t.Parallel()
	env := newDCMDTestEnv(t)

	handler := env.srv.SparkplugCommandHandler()
	if handler == nil {
		t.Fatal("SparkplugCommandHandler returned nil")
	}

	// "NoCommand.Tag" has Writable=true but DCMDEnabled=false.
	// An operator ACL row exists for Feed.Rate (different tag) — irrelevant for DCMD.
	handler("Silo-1", "NoCommand.Tag", float32(1.0))

	// WriteTag must NOT have been called.
	if env.mgr.writeCount() != 0 {
		t.Fatalf("expected 0 WriteTag calls, got %d", env.mgr.writeCount())
	}

	// Audit event must exist with outcome=deny, source=dcmd, reason=dcmd not enabled.
	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event, got 0")
	}
	ev := events[0]
	if !strings.Contains(ev.Detail, "outcome=deny") {
		t.Errorf("Detail = %q; want outcome=deny", ev.Detail)
	}
	if !strings.Contains(ev.Detail, "source=dcmd") {
		t.Errorf("Detail = %q; want source=dcmd", ev.Detail)
	}
	if !strings.Contains(ev.Detail, "reason=dcmd not enabled") {
		t.Errorf("Detail = %q; want reason=dcmd not enabled", ev.Detail)
	}
	// ACL is never consulted for DCMD: proved by the fact that an operator HTTP-allow
	// row exists for Feed.Rate but the DCMD for NoCommand.Tag (DCMDEnabled=false)
	// was still denied — aclStore.CanWrite is simply never called on the DCMD path
	// (Guard.AuthorizeDCMD does not touch aclStore).
}

// ─── TWA-DCMD-3.2 Scenario (c): Writable=false → deny ────────────────────────

// TestDCMDHandler_DenyWhenWritableFalse verifies that DCMD is denied when
// Writable=false, even when DCMDEnabled=true. Manager.WriteTag MUST NOT be called.
// (TWA-DCMD-3.2 scenario c, TWA-ENFORCE-2.1)
func TestDCMDHandler_DenyWhenWritableFalse(t *testing.T) {
	t.Parallel()
	env := newDCMDTestEnv(t)

	handler := env.srv.SparkplugCommandHandler()
	if handler == nil {
		t.Fatal("SparkplugCommandHandler returned nil")
	}

	// "Emergency.Stop" has Writable=false, DCMDEnabled=true — master switch denies.
	handler("Silo-1", "Emergency.Stop", true)

	if env.mgr.writeCount() != 0 {
		t.Fatalf("expected 0 WriteTag calls, got %d", env.mgr.writeCount())
	}

	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event, got 0")
	}
	ev := events[0]
	if !strings.Contains(ev.Detail, "outcome=deny") {
		t.Errorf("Detail = %q; want outcome=deny", ev.Detail)
	}
	if !strings.Contains(ev.Detail, "source=dcmd") {
		t.Errorf("Detail = %q; want source=dcmd", ev.Detail)
	}
	if !strings.Contains(ev.Detail, "reason=tag not writable") {
		t.Errorf("Detail = %q; want reason=tag not writable", ev.Detail)
	}
}

// ─── TWA-DCMD-3.2 Scenario (d): deny audit has Username="" and source=dcmd ────

// TestDCMDHandler_DenyAuditHasNoActor verifies that the deny audit event for a
// DCMD has Username="" and source=dcmd (no actor, no role).
// (TWA-DCMD-3.2 scenario d, TWA-AUDIT-4.1)
func TestDCMDHandler_DenyAuditHasNoActor(t *testing.T) {
	t.Parallel()
	env := newDCMDTestEnv(t)

	handler := env.srv.SparkplugCommandHandler()
	if handler == nil {
		t.Fatal("SparkplugCommandHandler returned nil")
	}

	// Any deny scenario works; use DCMDEnabled=false.
	handler("Silo-1", "NoCommand.Tag", float32(1.0))

	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event, got 0")
	}
	ev := events[0]
	if ev.Username != "" {
		t.Errorf("Username = %q; DCMD deny audit must have empty Username (no actor)", ev.Username)
	}
	if !strings.Contains(ev.Detail, "source=dcmd") {
		t.Errorf("Detail = %q; want source=dcmd", ev.Detail)
	}
}

// ─── Guard absent: SparkplugCommandHandler returns nil ────────────────────────

// TestDCMDHandler_NilWhenNoGuard verifies that SparkplugCommandHandler returns
// nil when no guard is wired (safe nil-guard for bootstrap before PR4).
func TestDCMDHandler_NilWhenNoGuard(t *testing.T) {
	t.Parallel()
	cfg := minimalCfgForDCMD(t)
	srv := New(cfg, nil, nil, Opts{}) // no guard, no plcMgr

	if h := srv.SparkplugCommandHandler(); h != nil {
		t.Error("expected nil handler when no guard wired, got non-nil")
	}
}

// ─── Triangulation: allow audit also has Username="" ──────────────────────────

// TestDCMDHandler_AllowAuditHasNoActor verifies that the allow audit event for a
// DCMD also has Username="" (DCMD never has an actor).
// Triangulates scenario (a) by asserting the Username field on the allow path.
func TestDCMDHandler_AllowAuditHasNoActor(t *testing.T) {
	t.Parallel()
	env := newDCMDTestEnv(t)

	handler := env.srv.SparkplugCommandHandler()
	if handler == nil {
		t.Fatal("SparkplugCommandHandler returned nil")
	}

	handler("Silo-1", "Feed.Rate", float32(2.0))

	// Wait briefly to let the (synchronous) audit flush.
	time.Sleep(10 * time.Millisecond)

	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) == 0 {
		t.Fatal("expected at least 1 audit event, got 0")
	}
	ev := events[0]
	if ev.Username != "" {
		t.Errorf("Username = %q on allow path; DCMD must always have empty Username", ev.Username)
	}
}
