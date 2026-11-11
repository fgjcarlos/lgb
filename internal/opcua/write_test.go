// write_test.go — behaviour tests for the OPC UA write surface.
//
// The tests drive the exported seam (Server.HandleWriteRequest) so they assert
// real behaviour without a live OPC UA session: the returned UA status code,
// whether the PLC writer was called, and the audit event that was emitted.
//
// Requirements: TWA-OPCUA-3.3, TWA-ENFORCE-2.1, TWA-AUDIT-4.1.
package opcua

import (
	"context"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	gopcua "github.com/gopcua/opcua"
	opcserver "github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// ─── Test doubles ───────────────────────────────────────────────────────────

// fakeTagMeta is a writeguard.TagReadable double. It returns the per-tag
// metadata the write guard reads.
type fakeTagMeta struct {
	tags map[string]writeguard.TagMeta // key: "plc/tag"
}

func (f *fakeTagMeta) TagMeta(_ context.Context, plcName, tag string) (writeguard.TagMeta, bool) {
	meta, ok := f.tags[plcName+"/"+tag]
	return meta, ok
}

// countingACL is a writeguard.ACLReader double that records every call.
// The OPC UA path must never consult it.
type countingACL struct {
	allow bool
	calls int
}

func (c *countingACL) CanWrite(_ context.Context, _, _, _ string) (bool, error) {
	c.calls++
	return c.allow, nil
}

// writerCall records one WriteTag invocation.
type writerCall struct {
	plc string
	tag string
	val any
}

// recordingWriter is a writeguard.TagWriter double.
type recordingWriter struct {
	mu    sync.Mutex
	calls []writerCall
	err   error
}

func (w *recordingWriter) WriteTag(plcName, tag string, val any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writerCall{plc: plcName, tag: tag, val: val})
	return w.err
}

func (w *recordingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

func (w *recordingWriter) last() (writerCall, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.calls) == 0 {
		return writerCall{}, false
	}
	return w.calls[len(w.calls)-1], true
}

// recordingAudit is a writeguard.AuditSink double.
type recordingAudit struct {
	mu     sync.Mutex
	events []auth.AuditEvent
}

func (a *recordingAudit) Log(ev auth.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, ev)
	return nil
}

func (a *recordingAudit) all() []auth.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]auth.AuditEvent, len(a.events))
	copy(out, a.events)
	return out
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// writeTestConfig returns a config with one PLC ("Silo-1") holding two tags:
// "Feed.Rate" (Float) and "Emergency.Stop" (Boolean).
func writeTestConfig() *config.Config {
	return &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0, SecurityMode: "None"},
		PLCs: []config.PLC{{
			Name: "Silo-1",
			Tags: []config.TagDef{
				{Name: "Feed.Rate", Type: "Float"},
				{Name: "Emergency.Stop", Type: "Boolean"},
			},
		}},
	}
}

// writeTestTagSource returns a tag source serving a live value for Feed.Rate.
func writeTestTagSource() TagSource {
	return &mockTagSource{
		tags: map[string]map[string]plc.TagValue{
			"Silo-1": {
				"Feed.Rate": {Value: float32(11.5), Quality: "good"},
			},
		},
	}
}

// sharedOPCUAServer returns a single gopcua server instance shared by the
// write tests. Creating a gopcua server imports the standard OPC UA node set,
// which takes seconds — sharing one instance keeps the package tests fast.
// Each test still gets its own namespaces (they are appended to the shared
// server) and its own Server struct.
var (
	sharedServerOnce sync.Once
	sharedServer     *opcserver.Server
)

func sharedOPCUAServer() *opcserver.Server {
	sharedServerOnce.Do(func() {
		sharedServer = opcserver.New(opcserver.EndPoint("127.0.0.1", 0))
	})
	return sharedServer
}

// newWriteTestServer builds a Server with a populated address space but no live
// OPC UA session. The gopcua server instance is created so the namespaces can
// be built exactly as they are at runtime.
func newWriteTestServer(t *testing.T, cfg *config.Config, opts Options) *Server {
	t.Helper()
	srv := New(cfg, writeTestTagSource(), nil, opts)
	srv.srv = sharedOPCUAServer()
	srv.populateAddressSpace()
	return srv
}

// guardedOptions builds the standard test options: a guard over the given
// metadata, a recording writer and a recording audit sink.
func guardedOptions(meta *fakeTagMeta, acl *countingACL, writer *recordingWriter, audit *recordingAudit) Options {
	return Options{
		Guard:  writeguard.NewGuard(meta, acl),
		Writer: writer,
		Audit:  audit,
		Tags:   meta,
	}
}

// writeRequestFor builds a single-node Write request for the given tag.
func writeRequestFor(srv *Server, plcName, tag string, value any) *ua.WriteRequest {
	ns := srv.namespaces[plcName]
	return &ua.WriteRequest{
		RequestHeader: &ua.RequestHeader{RequestHandle: 7},
		NodesToWrite: []*ua.WriteValue{{
			NodeID:      ua.NewStringNodeID(ns.ID(), tag),
			AttributeID: ua.AttributeIDValue,
			Value:       opcserver.DataValueFromValue(value),
		}},
	}
}

// ─── TWA-OPCUA-3.3 scenario (a): allow ───────────────────────────────────────

// TestHandleWriteRequest_Allowed_WritesThroughGuardAndAudits verifies that a
// write to a tag with Writable=true and OPCUAEnabled=true reaches the PLC
// writer, is answered with ua.StatusOK and emits exactly one audit event with
// outcome=allow and source=opcua. The ACL is never consulted.
// (TWA-OPCUA-3.3 scenario a, TWA-ENFORCE-2.1, TWA-AUDIT-4.1)
func TestHandleWriteRequest_Allowed_WritesThroughGuardAndAudits(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 3.5))

	if len(resp.Results) != 1 {
		t.Fatalf("Results len = %d; want 1", len(resp.Results))
	}
	if resp.Results[0] != ua.StatusOK {
		t.Errorf("status = %v; want %v (StatusOK)", resp.Results[0], ua.StatusOK)
	}

	if writer.count() != 1 {
		t.Fatalf("expected exactly 1 WriteTag call, got %d", writer.count())
	}
	call, _ := writer.last()
	if call.plc != "Silo-1" || call.tag != "Feed.Rate" {
		t.Errorf("WriteTag call = %+v; want plc=Silo-1 tag=Feed.Rate", call)
	}
	// The value is converted to the tag's declared type (Float → float32).
	if got, ok := call.val.(float32); !ok || got != float32(3.5) {
		t.Errorf("WriteTag value = %#v; want float32(3.5)", call.val)
	}

	if acl.calls != 0 {
		t.Errorf("expected aclStore.CanWrite NEVER called on the OPC UA path, got %d calls", acl.calls)
	}

	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev.Action != "tag.write" {
		t.Errorf("Action = %q; want tag.write", ev.Action)
	}
	if ev.Username != "" {
		t.Errorf("Username = %q; want empty (OPC UA carries no authenticated principal)", ev.Username)
	}
	for _, want := range []string{"plc=Silo-1", "tag=Feed.Rate", "outcome=allow", "source=opcua"} {
		if !strings.Contains(ev.Detail, want) {
			t.Errorf("Detail = %q; want it to contain %q", ev.Detail, want)
		}
	}
}

// ─── TWA-OPCUA-3.3 scenario (b): opt-in missing → deny ───────────────────────

// TestHandleWriteRequest_NotOptedIn_DeniedAndAudited verifies that a tag with
// OPCUAEnabled=false is denied with ua.StatusBadUserAccessDenied, that the PLC
// writer is NOT called and that the deny audit carries reason="opcua not
// enabled" and source=opcua. The ACL is never consulted.
// (TWA-OPCUA-3.3 scenario b, TWA-ENFORCE-2.1, TWA-AUDIT-4.1)
func TestHandleWriteRequest_NotOptedIn_DeniedAndAudited(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: false},
	}}
	acl := &countingACL{allow: true} // would allow HTTP — must be irrelevant here
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 3.5))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadUserAccessDenied {
		t.Fatalf("status = %v; want %v (StatusBadUserAccessDenied)", resp.Results, ua.StatusBadUserAccessDenied)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}
	if acl.calls != 0 {
		t.Errorf("expected aclStore.CanWrite NEVER called on the OPC UA path, got %d calls", acl.calls)
	}

	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	ev := events[0]
	for _, want := range []string{"outcome=deny", "source=opcua", "reason=opcua not enabled"} {
		if !strings.Contains(ev.Detail, want) {
			t.Errorf("Detail = %q; want it to contain %q", ev.Detail, want)
		}
	}
	if ev.Username != "" {
		t.Errorf("Username = %q; want empty (no actor on the OPC UA path)", ev.Username)
	}
}

// ─── TWA-OPCUA-3.3 scenario (c): master switch off → deny ────────────────────

// TestHandleWriteRequest_NotWritable_DeniedAndAudited verifies that a tag with
// Writable=false is denied even when OPCUAEnabled=true, with reason
// "tag not writable". (TWA-OPCUA-3.3 scenario c, TWA-ENFORCE-2.1)
func TestHandleWriteRequest_NotWritable_DeniedAndAudited(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Emergency.Stop": {Writable: false, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: true}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Emergency.Stop", true))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadUserAccessDenied {
		t.Fatalf("status = %v; want %v (StatusBadUserAccessDenied)", resp.Results, ua.StatusBadUserAccessDenied)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}

	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	if !strings.Contains(events[0].Detail, "reason=tag not writable") {
		t.Errorf("Detail = %q; want it to contain %q", events[0].Detail, "reason=tag not writable")
	}
}

// ─── TWA-OPCUA-3.3 scenario (e): unknown node id ─────────────────────────────

// TestHandleWriteRequest_UnknownNode_Rejected verifies that a write to a node
// id that is not a tag node is rejected with ua.StatusBadNodeIDUnknown and that
// the PLC writer is NOT called.
// (TWA-OPCUA-3.3 scenario e)
func TestHandleWriteRequest_UnknownNode_Rejected(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: true}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	req := &ua.WriteRequest{
		RequestHeader: &ua.RequestHeader{RequestHandle: 7},
		NodesToWrite: []*ua.WriteValue{{
			NodeID:      ua.NewStringNodeID(99, "NotATag"),
			AttributeID: ua.AttributeIDValue,
			Value:       opcserver.DataValueFromValue(1.0),
		}},
	}
	resp := srv.HandleWriteRequest(req)

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadNodeIDUnknown {
		t.Fatalf("status = %v; want %v (StatusBadNodeIDUnknown)", resp.Results, ua.StatusBadNodeIDUnknown)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}
}

// TestHandleWriteRequest_UnknownTagInKnownNamespace_Rejected verifies that a
// write to a string node id that does not exist in a known namespace is
// rejected with ua.StatusBadNodeIDUnknown.
// (TWA-OPCUA-3.3 scenario e)
func TestHandleWriteRequest_UnknownTagInKnownNamespace_Rejected(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: true}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	ns := srv.namespaces["Silo-1"]
	req := &ua.WriteRequest{
		RequestHeader: &ua.RequestHeader{RequestHandle: 7},
		NodesToWrite: []*ua.WriteValue{{
			NodeID:      ua.NewStringNodeID(ns.ID(), "Ghost.Tag"),
			AttributeID: ua.AttributeIDValue,
			Value:       opcserver.DataValueFromValue(1.0),
		}},
	}
	resp := srv.HandleWriteRequest(req)

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadNodeIDUnknown {
		t.Fatalf("status = %v; want %v (StatusBadNodeIDUnknown)", resp.Results, ua.StatusBadNodeIDUnknown)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}
}

// TestHandleWriteRequest_BadAttributeID_Rejected verifies that a write to an
// attribute other than Value is rejected with ua.StatusBadAttributeIDInvalid
// and that the PLC writer is NOT called. (TWA-OPCUA-3.3 step 1)
func TestHandleWriteRequest_BadAttributeID_Rejected(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: true}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	ns := srv.namespaces["Silo-1"]
	req := &ua.WriteRequest{
		RequestHeader: &ua.RequestHeader{RequestHandle: 7},
		NodesToWrite: []*ua.WriteValue{{
			NodeID:      ua.NewStringNodeID(ns.ID(), "Feed.Rate"),
			AttributeID: ua.AttributeIDDisplayName,
			Value:       opcserver.DataValueFromValue("nope"),
		}},
	}
	resp := srv.HandleWriteRequest(req)

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadAttributeIDInvalid {
		t.Fatalf("status = %v; want %v (StatusBadAttributeIDInvalid)", resp.Results, ua.StatusBadAttributeIDInvalid)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}
}

// ─── TWA-OPCUA-3.3 scenario (f): value type mismatch ─────────────────────────

// TestHandleWriteRequest_TypeMismatch_DeniedAndAudited verifies that a value
// that does not match the tag's declared type is rejected with
// ua.StatusBadTypeMismatch, audited as a deny and never reaches the PLC.
// (TWA-OPCUA-3.3 scenario f)
func TestHandleWriteRequest_TypeMismatch_DeniedAndAudited(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: true}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	// Feed.Rate is declared Float — a string is not convertible.
	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", "not-a-number"))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadTypeMismatch {
		t.Fatalf("status = %v; want %v (StatusBadTypeMismatch)", resp.Results, ua.StatusBadTypeMismatch)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}

	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	for _, want := range []string{"outcome=deny", "source=opcua", "reason=value type mismatch"} {
		if !strings.Contains(events[0].Detail, want) {
			t.Errorf("Detail = %q; want it to contain %q", events[0].Detail, want)
		}
	}
}

// TestHandleWriteRequest_ConversionToDeclaredType verifies that the value is
// converted to the tag's declared type before it reaches the PLC writer.
// (TWA-OPCUA-3.3 step 2)
func TestHandleWriteRequest_ConversionToDeclaredType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		declared string
		written  any
		want     any
	}{
		{"Boolean", true, true},
		{"Int8", int8(7), int8(7)},
		{"Int16", int16(300), int16(300)},
		{"Int32", int32(70000), int32(70000)},
		{"Int64", int64(5000000000), int64(5000000000)},
		{"UInt8", uint8(200), uint8(200)},
		{"UInt16", uint16(60000), uint16(60000)},
		{"UInt32", uint32(4000000000), uint32(4000000000)},
		{"UInt64", uint64(1 << 40), uint64(1 << 40)},
		{"UInt64", ^uint64(0), ^uint64(0)},
		{"Float", float64(3.5), float32(3.5)},
		{"Double", float64(3.5), float64(3.5)},
		{"String", "hello", "hello"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.declared, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{
				OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0, SecurityMode: "None"},
				PLCs: []config.PLC{{
					Name: "Silo-1",
					Tags: []config.TagDef{{Name: "T", Type: tc.declared}},
				}},
			}
			meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
				"Silo-1/T": {Writable: true, OPCUAEnabled: true},
			}}
			acl := &countingACL{allow: false}
			writer := &recordingWriter{}
			audit := &recordingAudit{}

			srv := newWriteTestServer(t, cfg, guardedOptions(meta, acl, writer, audit))
			resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "T", tc.written))

			if len(resp.Results) != 1 || resp.Results[0] != ua.StatusOK {
				t.Fatalf("status = %v; want %v (StatusOK)", resp.Results, ua.StatusOK)
			}
			call, ok := writer.last()
			if !ok {
				t.Fatal("expected WriteTag to be called")
			}
			if fmt.Sprintf("%T", call.val) != fmt.Sprintf("%T", tc.want) {
				t.Fatalf("WriteTag value type = %T; want %T", call.val, tc.want)
			}
			if call.val != tc.want {
				t.Errorf("WriteTag value = %v; want %v", call.val, tc.want)
			}
		})
	}
}

// ─── TWA-OPCUA-3.3 step 6: PLC write error ──────────────────────────────────

// TestHandleWriteRequest_WriteError_InternalErrorAndAudited verifies that a
// PLC write failure is answered with ua.StatusBadInternalError and audited as
// a deny with the reason "write error: ...". (TWA-OPCUA-3.3 step 6)
func TestHandleWriteRequest_WriteError_InternalErrorAndAudited(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{err: fmt.Errorf("boom")}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 1.0))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadInternalError {
		t.Fatalf("status = %v; want %v (StatusBadInternalError)", resp.Results, ua.StatusBadInternalError)
	}

	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	for _, want := range []string{"outcome=deny", "source=opcua", "reason=write error: boom"} {
		if !strings.Contains(events[0].Detail, want) {
			t.Errorf("Detail = %q; want it to contain %q", events[0].Detail, want)
		}
	}
}

// ─── S7 / TWA-OPCUA-3.3: deny-by-default without a guard or writer ──────────

// TestHandleWriteRequest_NoGuard_Denied verifies that a server built without
// a write guard denies every write with ua.StatusBadUserAccessDenied and
// audits the deny. (TWA-OPCUA-3.3, S7)
func TestHandleWriteRequest_NoGuard_Denied(t *testing.T) {
	t.Parallel()
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), Options{Writer: writer, Audit: audit})

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 3.5))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadUserAccessDenied {
		t.Fatalf("status = %v; want %v (StatusBadUserAccessDenied)", resp.Results, ua.StatusBadUserAccessDenied)
	}
	if writer.count() != 0 {
		t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
	}
	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	if !strings.Contains(events[0].Detail, "outcome=deny") || !strings.Contains(events[0].Detail, "source=opcua") {
		t.Errorf("Detail = %q; want outcome=deny and source=opcua", events[0].Detail)
	}
}

// TestHandleWriteRequest_NoWriter_Denied verifies that a server built without
// a PLC writer denies every write with ua.StatusBadUserAccessDenied.
// (TWA-OPCUA-3.3, S7)
func TestHandleWriteRequest_NoWriter_Denied(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), Options{
		Guard: writeguard.NewGuard(meta, acl),
		Audit: audit,
	})

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 3.5))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadUserAccessDenied {
		t.Fatalf("status = %v; want %v (StatusBadUserAccessDenied)", resp.Results, ua.StatusBadUserAccessDenied)
	}
	events := audit.all()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}
	if !strings.Contains(events[0].Detail, "outcome=deny") {
		t.Errorf("Detail = %q; want outcome=deny", events[0].Detail)
	}
}

// TestHandleWriteRequest_NilAuditSink_NoPanic verifies that the audit sink is
// optional: with no sink configured the write still succeeds and nothing
// panics. (TWA-AUDIT-4.1 — audit calls must be nil-safe)
func TestHandleWriteRequest_NilAuditSink_NoPanic(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{}

	srv := newWriteTestServer(t, writeTestConfig(), Options{
		Guard:  writeguard.NewGuard(meta, acl),
		Writer: writer,
	})

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 3.5))
	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusOK {
		t.Fatalf("status = %v; want %v (StatusOK)", resp.Results, ua.StatusOK)
	}
	if writer.count() != 1 {
		t.Errorf("expected 1 WriteTag call, got %d", writer.count())
	}
}

// ─── TWA-OPCUA-3.3 scenario (g): the read path is never corrupted ───────────

// TestHandleWriteRequest_DoesNotMutateReadPath verifies that a write never
// replaces the node's value: the node keeps serving the live tag value after
// the write, and the value was applied through the PLC driver instead.
// (TWA-OPCUA-3.3 scenario g, S9)
func TestHandleWriteRequest_DoesNotMutateReadPath(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 99.0))
	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusOK {
		t.Fatalf("status = %v; want %v (StatusOK)", resp.Results, ua.StatusOK)
	}

	// The node must still serve the live tag value (11.5), not the written 99.
	ns := srv.namespaces["Silo-1"]
	dv := ns.Attribute(ua.NewStringNodeID(ns.ID(), "Feed.Rate"), ua.AttributeIDValue)
	if dv == nil || dv.Value == nil {
		t.Fatal("node value is nil after the write")
	}
	got, ok := dv.Value.Value().(float32)
	if !ok {
		t.Fatalf("node value type = %T; want float32", dv.Value.Value())
	}
	if got != float32(11.5) {
		t.Errorf("node value = %v; want 11.5 (the live tag value — the write must not replace the value callback)", got)
	}

	// The write went through the PLC driver.
	if writer.count() != 1 {
		t.Errorf("expected 1 WriteTag call, got %d", writer.count())
	}
}

// ─── AccessLevel ────────────────────────────────────────────────────────────

// TestAccessLevel_MirrorsServerPolicy verifies that each tag node exposes an
// AccessLevel attribute: CurrentRead|CurrentWrite when the tag is OPC UA
// writable and CurrentRead alone otherwise. The level is derived from the same
// metadata the guard reads. (TWA-OPCUA-3.3 AccessLevel requirement)
func TestAccessLevel_MirrorsServerPolicy(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate":      {Writable: true, OPCUAEnabled: true},
		"Silo-1/Emergency.Stop": {Writable: false, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	srv := newWriteTestServer(t, writeTestConfig(), guardedOptions(meta, acl, writer, audit))
	ns := srv.namespaces["Silo-1"]

	accessLevelOf := func(tag string) uint8 {
		t.Helper()
		dv := ns.Attribute(ua.NewStringNodeID(ns.ID(), tag), ua.AttributeIDAccessLevel)
		if dv == nil || dv.Value == nil {
			t.Fatalf("AccessLevel attribute missing for tag %q", tag)
		}
		lvl, ok := dv.Value.Value().(uint8)
		if !ok {
			t.Fatalf("AccessLevel type = %T; want uint8", dv.Value.Value())
		}
		return lvl
	}

	if got, want := accessLevelOf("Feed.Rate"), uint8(ua.AccessLevelTypeCurrentRead|ua.AccessLevelTypeCurrentWrite); got != want {
		t.Errorf("AccessLevel(Feed.Rate) = %d; want %d (CurrentRead|CurrentWrite)", got, want)
	}
	if got, want := accessLevelOf("Emergency.Stop"), uint8(ua.AccessLevelTypeCurrentRead); got != want {
		t.Errorf("AccessLevel(Emergency.Stop) = %d; want %d (CurrentRead only)", got, want)
	}
}

// ─── PRESERVE: the read-only server keeps working ───────────────────────────

// TestPreserve_ReadOnlyServer_ServesValues verifies that a server built
// without any write dependency still serves the live tag values and that its
// nodes are read-only. (TWA-OPCUA-3.3 — the read-only server must keep working)
func TestPreserve_ReadOnlyServer_ServesValues(t *testing.T) {
	t.Parallel()
	srv := newWriteTestServer(t, writeTestConfig(), Options{})

	dv := srv.DataValueForTag("Silo-1", "Feed.Rate")
	if dv == nil || dv.Value == nil {
		t.Fatal("DataValueForTag returned no value for Feed.Rate")
	}
	if got, ok := dv.Value.Value().(float32); !ok || got != float32(11.5) {
		t.Errorf("DataValueForTag = %v; want 11.5", dv.Value.Value())
	}

	ns := srv.namespaces["Silo-1"]
	nodeDV := ns.Attribute(ua.NewStringNodeID(ns.ID(), "Feed.Rate"), ua.AttributeIDValue)
	if nodeDV == nil || nodeDV.Value == nil {
		t.Fatal("node value is nil")
	}
	if got, ok := nodeDV.Value.Value().(float32); !ok || got != float32(11.5) {
		t.Errorf("node value = %v; want 11.5", nodeDV.Value.Value())
	}

	// Without a guard every node is read-only for clients.
	levelDV := ns.Attribute(ua.NewStringNodeID(ns.ID(), "Feed.Rate"), ua.AttributeIDAccessLevel)
	if levelDV == nil || levelDV.Value == nil {
		t.Fatal("AccessLevel attribute missing")
	}
	if got, want := levelDV.Value.Value().(uint8), uint8(ua.AccessLevelTypeCurrentRead); got != want {
		t.Errorf("AccessLevel = %d; want %d (CurrentRead only on a read-only server)", got, want)
	}
}

// ─── Live session: the gateway's Write handler replaces gopcua's default ─────

// dialOPCUA waits until the OPC UA endpoint accepts TCP connections and
// returns the probe connection.
func dialOPCUA(t *testing.T, endpoint string) net.Conn {
	t.Helper()
	addr := strings.TrimPrefix(endpoint, "opc.tcp://")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("opc ua endpoint %s never accepted a connection", endpoint)
	return nil
}

// newOPCUAClient connects a gopcua client to the endpoint.
func newOPCUAClient(t *testing.T, endpoint string) *gopcua.Client {
	t.Helper()
	cl, err := gopcua.NewClient(endpoint,
		gopcua.SecurityMode(ua.MessageSecurityModeNone),
		gopcua.SecurityPolicy(ua.SecurityPolicyURINone),
	)
	if err != nil {
		t.Fatalf("create opc ua client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := cl.Connect(ctx); err != nil {
		t.Fatalf("connect opc ua client: %v", err)
	}
	return cl
}

// TestLiveSession_WriteReachesPLCAndKeepsReadPath verifies end-to-end that the
// gateway's Write handler is the one serving writes (gopcua's default Write
// service would replace the node's value callback and never reach the PLC) and
// that a write does not corrupt the read path.
//
// gopcua registers its default Write service inside Server.Start and
// RegisterHandler does not override an existing handler, so the gateway MUST
// register its handler before the server starts.
// (TWA-OPCUA-3.3, S9)
func TestLiveSession_WriteReachesPLCAndKeepsReadPath(t *testing.T) {
	if testing.Short() {
		t.Skip("live OPC UA session test skipped in -short mode")
	}
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{}
	audit := &recordingAudit{}

	cfg := writeTestConfig()
	cfg.OPCUA.Port = freeTCPPort(t)

	srv := New(cfg, writeTestTagSource(), nil, guardedOptions(meta, acl, writer, audit))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The gopcua server starts listening inside Start and returns; wait until
	// the endpoint accepts connections.
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	endpoint := fmt.Sprintf("opc.tcp://127.0.0.1:%d", cfg.OPCUA.Port)
	conn := dialOPCUA(t, endpoint)
	_ = conn.Close()

	cl := newOPCUAClient(t, endpoint)
	defer func() { _ = cl.Close(context.Background()) }()

	nsID := srv.namespaces["Silo-1"].ID()
	nodeID := ua.NewStringNodeID(nsID, "Feed.Rate")

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer writeCancel()
	resp, err := cl.Write(writeCtx, &ua.WriteRequest{
		RequestHeader: &ua.RequestHeader{RequestHandle: 1},
		NodesToWrite: []*ua.WriteValue{{
			NodeID:      nodeID,
			AttributeID: ua.AttributeIDValue,
			Value:       opcserver.DataValueFromValue(42.0),
		}},
	})
	if err != nil {
		t.Fatalf("client write failed: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusOK {
		t.Fatalf("status = %v; want %v (StatusOK)", resp.Results, ua.StatusOK)
	}
	if writer.count() != 1 {
		t.Fatalf("expected exactly 1 WriteTag call (the gateway handler served the write), got %d", writer.count())
	}

	// The read path still serves the live value — the write did not replace it.
	readResp, err := cl.Read(context.Background(), &ua.ReadRequest{
		RequestHeader: &ua.RequestHeader{RequestHandle: 2},
		NodesToRead: []*ua.ReadValueID{{
			NodeID:      nodeID,
			AttributeID: ua.AttributeIDValue,
		}},
	})
	if err != nil {
		t.Fatalf("client read failed: %v", err)
	}
	if len(readResp.Results) != 1 {
		t.Fatalf("read results len = %d; want 1", len(readResp.Results))
	}
	if readResp.Results[0].Value == nil {
		t.Fatal("read returned no value")
	}
	got, ok := readResp.Results[0].Value.Value().(float32)
	if !ok {
		t.Fatalf("read value type = %T; want float32", readResp.Results[0].Value.Value())
	}
	if got != float32(11.5) {
		t.Errorf("read value = %v; want 11.5 (the live tag value)", got)
	}
}

// ─── Bounded correction: numeric range and audit durability ──────────────────

// failingAudit is an AuditSink whose Log always fails, standing in for an
// audit sink that cannot persist its event (a full disk, a closed file).
type failingAudit struct{}

func (failingAudit) Log(auth.AuditEvent) error {
	return fmt.Errorf("audit sink unavailable")
}

// TestHandleWriteRequest_OutOfRangeNumber_DeniedAndAudited verifies that a
// numeric value outside the declared type's range is denied with
// ua.StatusBadTypeMismatch and never reaches the PLC.
//
// Two overflow shapes are covered: 2^63 is not representable as an int64 but
// compares equal to float64(math.MaxInt64), and 1e100 narrows to +Inf as a
// float32. Either one must be refused rather than written as a wrapped or
// infinite setpoint.
func TestHandleWriteRequest_OutOfRangeNumber_DeniedAndAudited(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		declared string
		written  any
	}{
		{"int64 overflow", "Int64", float64(math.Exp2(63))},
		{"float32 overflow", "Float", 1e100},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{
				OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0, SecurityMode: "None"},
				PLCs: []config.PLC{{
					Name: "Silo-1",
					Tags: []config.TagDef{{Name: "T", Type: tc.declared}},
				}},
			}
			meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
				"Silo-1/T": {Writable: true, OPCUAEnabled: true},
			}}
			acl := &countingACL{allow: false}
			writer := &recordingWriter{}
			audit := &recordingAudit{}

			srv := newWriteTestServer(t, cfg, guardedOptions(meta, acl, writer, audit))
			resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "T", tc.written))

			if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadTypeMismatch {
				t.Fatalf("status = %v; want %v (StatusBadTypeMismatch)", resp.Results, ua.StatusBadTypeMismatch)
			}
			if writer.count() != 0 {
				t.Errorf("expected WriteTag NOT called, got %d calls", writer.count())
			}
			events := audit.all()
			if len(events) != 1 {
				t.Fatalf("expected exactly 1 audit event, got %d", len(events))
			}
			if !strings.Contains(events[0].Detail, "outcome=deny") {
				t.Errorf("Detail = %q; want outcome=deny", events[0].Detail)
			}
		})
	}
}

// TestHandleWriteRequest_AuditFailure_NotReportedAsSuccess verifies that a
// write whose audit event cannot be persisted is not reported as a success.
// The PLC write already happened, so the handler answers with
// ua.StatusBadInternalError: the client learns that the attempt could not be
// accounted for instead of receiving a false StatusOK.
func TestHandleWriteRequest_AuditFailure_NotReportedAsSuccess(t *testing.T) {
	t.Parallel()
	meta := &fakeTagMeta{tags: map[string]writeguard.TagMeta{
		"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
	}}
	acl := &countingACL{allow: false}
	writer := &recordingWriter{}

	srv := newWriteTestServer(t, writeTestConfig(), Options{
		Guard:  writeguard.NewGuard(meta, acl),
		Writer: writer,
		Audit:  failingAudit{},
	})

	resp := srv.HandleWriteRequest(writeRequestFor(srv, "Silo-1", "Feed.Rate", 3.5))

	if len(resp.Results) != 1 || resp.Results[0] != ua.StatusBadInternalError {
		t.Fatalf("status = %v; want %v (StatusBadInternalError)", resp.Results, ua.StatusBadInternalError)
	}
	if writer.count() != 1 {
		t.Errorf("expected 1 WriteTag call, got %d", writer.count())
	}
}
