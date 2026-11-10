// Package writeguard_test — unit tests for the write enforcement core.
// Follows strict TDD: RED tests written first, referencing types and functions
// that do not exist yet.
//
// Requirements: TWA-ENFORCE-2.1, TWA-ENFORCE-2.2, TWA-ENFORCE-2.3 (guard unit tests).
package writeguard_test

import (
	"context"
	"testing"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// ─── Test doubles ───────────────────────────────────────────────────────────

// fakeTagReadable is a test double for TagReadable.
// It returns predefined Writable, DCMDEnabled and OPCUAEnabled values per
// (plc, tag).
type fakeTagReadable struct {
	tags map[string]writeguard.TagMeta // key: "plc/tag"
}

func (f *fakeTagReadable) TagMeta(ctx context.Context, plc, tag string) (writeguard.TagMeta, bool) {
	if f.tags == nil {
		return writeguard.TagMeta{}, false
	}
	meta, ok := f.tags[plc+"/"+tag]
	return meta, ok
}

// fakeACLReader is a test double for ACLReader.
// It records calls so tests can assert that CanWrite was (or was not) invoked.
type fakeACLReader struct {
	allow bool
	calls int
}

func (f *fakeACLReader) CanWrite(ctx context.Context, role, plc, tag string) (bool, error) {
	f.calls++
	return f.allow, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newGuard(tags *fakeTagReadable, acl *fakeACLReader) *writeguard.Guard {
	return writeguard.NewGuard(tags, acl)
}

func operatorActor() writeguard.Actor {
	return writeguard.Actor{Username: "alice", Role: auth.RoleOperator}
}

func adminActor() writeguard.Actor {
	return writeguard.Actor{Username: "root", Role: auth.RoleAdmin}
}

// ─── TWA-ENFORCE-2.1: HTTP path — master switch ──────────────────────────────

// TestAuthorizeHTTP_MasterSwitchOff_DeniesAdmin verifies that when Writable=false,
// even admin is denied and aclReader is NOT called. (TWA-ENFORCE-2.1)
func TestAuthorizeHTTP_MasterSwitchOff_DeniesAdmin(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Emergency.Stop": {Writable: false, DCMDEnabled: false},
		},
	}
	acl := &fakeACLReader{allow: true} // would allow — but must never be called

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeHTTP(context.Background(), adminActor(), "Silo-1", "Emergency.Stop", true)

	if decision.Allowed {
		t.Error("expected Allowed=false when Writable=false, got true")
	}
	if decision.Reason != "tag not writable" {
		t.Errorf("expected Reason %q, got %q", "tag not writable", decision.Reason)
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader.CanWrite NOT called when Writable=false, got %d calls", acl.calls)
	}
}

// TestAuthorizeHTTP_WritableTrue_NoACLRow_Denies verifies that when Writable=true
// but no ACL row exists (CanWrite returns false), the result is deny. (TWA-ENFORCE-2.1)
func TestAuthorizeHTTP_WritableTrue_NoACLRow_Denies(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, DCMDEnabled: false},
		},
	}
	acl := &fakeACLReader{allow: false}

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeHTTP(context.Background(), operatorActor(), "Silo-1", "Feed.Rate", 2.5)

	if decision.Allowed {
		t.Error("expected Allowed=false when no ACL row, got true")
	}
	if decision.Reason != "acl deny" {
		t.Errorf("expected Reason %q, got %q", "acl deny", decision.Reason)
	}
	if acl.calls == 0 {
		t.Error("expected aclReader.CanWrite called when Writable=true")
	}
}

// TestAuthorizeHTTP_BothLayersPass_Allows verifies the happy path: Writable=true
// + ACL allow → Allowed=true. (TWA-ENFORCE-2.1)
func TestAuthorizeHTTP_BothLayersPass_Allows(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, DCMDEnabled: false},
		},
	}
	acl := &fakeACLReader{allow: true}

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeHTTP(context.Background(), operatorActor(), "Silo-1", "Feed.Rate", 2.5)

	if !decision.Allowed {
		t.Errorf("expected Allowed=true, got false (reason=%q)", decision.Reason)
	}
}

// TestAuthorizeHTTP_EmptyACL_Denies verifies deny-by-default when ACL is empty.
// (TWA-ENFORCE-2.2)
func TestAuthorizeHTTP_EmptyACL_Denies(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, DCMDEnabled: false},
		},
	}
	acl := &fakeACLReader{allow: false}

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeHTTP(context.Background(), adminActor(), "Silo-1", "Feed.Rate", 1.0)

	if decision.Allowed {
		t.Error("expected Allowed=false with empty ACL, got true")
	}
	if decision.Reason != "acl deny" {
		t.Errorf("expected Reason %q, got %q", "acl deny", decision.Reason)
	}
}

// ─── TWA-ENFORCE-2.1: DCMD path ──────────────────────────────────────────────

// TestAuthorizeDCMD_WritableFalse_Denies verifies that Writable=false denies DCMD
// regardless of DCMDEnabled. (TWA-ENFORCE-2.1)
func TestAuthorizeDCMD_WritableFalse_Denies(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Emergency.Stop": {Writable: false, DCMDEnabled: true},
		},
	}
	acl := &fakeACLReader{allow: true} // must never be called for DCMD

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeDCMD(context.Background(), "Silo-1", "Emergency.Stop")

	if decision.Allowed {
		t.Error("expected Allowed=false when Writable=false for DCMD, got true")
	}
	if decision.Reason != "tag not writable" {
		t.Errorf("expected Reason %q, got %q", "tag not writable", decision.Reason)
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called for DCMD path, got %d calls", acl.calls)
	}
}

// TestAuthorizeDCMD_DCMDEnabledFalse_Denies verifies that DCMDEnabled=false denies
// even when Writable=true and an ACL row would allow HTTP. (TWA-ENFORCE-2.1)
func TestAuthorizeDCMD_DCMDEnabledFalse_Denies(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, DCMDEnabled: false},
		},
	}
	acl := &fakeACLReader{allow: true} // HTTP would allow — DCMD must ignore

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeDCMD(context.Background(), "Silo-1", "Feed.Rate")

	if decision.Allowed {
		t.Error("expected Allowed=false when DCMDEnabled=false, got true")
	}
	if decision.Reason != "dcmd not enabled" {
		t.Errorf("expected Reason %q, got %q", "dcmd not enabled", decision.Reason)
	}
	// Confirm ACL was never consulted.
	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called for DCMD path, got %d calls", acl.calls)
	}
}

// TestAuthorizeDCMD_BothTrue_Allows verifies the DCMD happy path.
// (TWA-ENFORCE-2.1)
func TestAuthorizeDCMD_BothTrue_Allows(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, DCMDEnabled: true},
		},
	}
	acl := &fakeACLReader{allow: false} // irrelevant for DCMD

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeDCMD(context.Background(), "Silo-1", "Feed.Rate")

	if !decision.Allowed {
		t.Errorf("expected Allowed=true when both flags are true, got false (reason=%q)", decision.Reason)
	}
}

// ─── TWA-ENFORCE-2.1: OPC UA path ─────────────────────────────────────────────

// TestAuthorizeOPCUA_WritableFalse_Denies verifies that Writable=false denies an
// OPC UA write regardless of OPCUAEnabled, and that the ACL reader is NEVER
// consulted on this path. (TWA-ENFORCE-2.1, TWA-OPCUA-3.3 scenario c)
func TestAuthorizeOPCUA_WritableFalse_Denies(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Emergency.Stop": {Writable: false, OPCUAEnabled: true},
		},
	}
	acl := &fakeACLReader{allow: true} // must never be called for OPC UA

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeOPCUA(context.Background(), "Silo-1", "Emergency.Stop")

	if decision.Allowed {
		t.Error("expected Allowed=false when Writable=false for OPC UA, got true")
	}
	if decision.Reason != "tag not writable" {
		t.Errorf("expected Reason %q, got %q", "tag not writable", decision.Reason)
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called for OPC UA path, got %d calls", acl.calls)
	}
}

// TestAuthorizeOPCUA_NotEnabled_Denies verifies that OPCUAEnabled=false denies an
// OPC UA write even when Writable=true and an ACL row would allow HTTP. The ACL
// reader MUST NOT be called. (TWA-ENFORCE-2.1, TWA-OPCUA-3.3 scenario b)
func TestAuthorizeOPCUA_NotEnabled_Denies(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: false},
		},
	}
	acl := &fakeACLReader{allow: true} // HTTP would allow — OPC UA must ignore it

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeOPCUA(context.Background(), "Silo-1", "Feed.Rate")

	if decision.Allowed {
		t.Error("expected Allowed=false when OPCUAEnabled=false, got true")
	}
	if decision.Reason != "opcua not enabled" {
		t.Errorf("expected Reason %q, got %q", "opcua not enabled", decision.Reason)
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called for OPC UA path, got %d calls", acl.calls)
	}
}

// TestAuthorizeOPCUA_BothTrue_Allows verifies the OPC UA happy path:
// Writable=true + OPCUAEnabled=true → allow. (TWA-ENFORCE-2.1)
func TestAuthorizeOPCUA_BothTrue_Allows(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate": {Writable: true, OPCUAEnabled: true},
		},
	}
	acl := &fakeACLReader{allow: false} // irrelevant for OPC UA

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeOPCUA(context.Background(), "Silo-1", "Feed.Rate")

	if !decision.Allowed {
		t.Errorf("expected Allowed=true when both flags are true, got false (reason=%q)", decision.Reason)
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called for OPC UA path, got %d calls", acl.calls)
	}
}

// TestAuthorizeOPCUA_UnknownTag_Denies verifies deny-by-default: an unknown tag
// is denied, exactly like the HTTP and DCMD paths. (TWA-ENFORCE-2.1)
func TestAuthorizeOPCUA_UnknownTag_Denies(t *testing.T) {
	tags := &fakeTagReadable{tags: map[string]writeguard.TagMeta{}}
	acl := &fakeACLReader{allow: true}

	guard := newGuard(tags, acl)
	decision := guard.AuthorizeOPCUA(context.Background(), "Silo-1", "Ghost.Tag")

	if decision.Allowed {
		t.Error("expected Allowed=false for unknown tag, got true")
	}
	if decision.Reason != "tag not writable" {
		t.Errorf("expected Reason %q, got %q", "tag not writable", decision.Reason)
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called for OPC UA path, got %d calls", acl.calls)
	}
}

// ─── PRESERVE: HTTP and DCMD semantics are unchanged ─────────────────────────

// TestPreserve_AuthorizeHTTP_SemanticsUnchanged pins the HTTP enforcement order
// after the OPC UA gate was added: the master switch denies before any ACL
// lookup, and an ACL deny denies. (TWA-ENFORCE-2.1 — PRESERVE)
func TestPreserve_AuthorizeHTTP_SemanticsUnchanged(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate":     {Writable: true, DCMDEnabled: false, OPCUAEnabled: true},
			"Silo-1/Emergency.Stp": {Writable: false, DCMDEnabled: true, OPCUAEnabled: true},
		},
	}
	acl := &fakeACLReader{allow: true}
	guard := newGuard(tags, acl)

	// Writable=false → deny before the ACL is consulted.
	d := guard.AuthorizeHTTP(context.Background(), adminActor(), "Silo-1", "Emergency.Stp", 1.0)
	if d.Allowed || d.Reason != "tag not writable" {
		t.Errorf("HTTP master switch: got %+v; want deny with reason %q", d, "tag not writable")
	}
	if acl.calls != 0 {
		t.Errorf("expected aclReader NOT called when Writable=false, got %d calls", acl.calls)
	}

	// Writable=true + ACL allow → allow.
	d = guard.AuthorizeHTTP(context.Background(), operatorActor(), "Silo-1", "Feed.Rate", 1.0)
	if !d.Allowed {
		t.Errorf("HTTP happy path: got %+v; want allow", d)
	}
	if acl.calls != 1 {
		t.Errorf("expected aclReader called exactly once on the HTTP path, got %d calls", acl.calls)
	}
}

// TestPreserve_AuthorizeDCMD_SemanticsUnchanged pins the DCMD enforcement order
// after the OPC UA gate was added: the master switch denies first, the DCMD flag
// denies second, and both flags true allow — never touching the ACL.
// (TWA-ENFORCE-2.1 — PRESERVE)
func TestPreserve_AuthorizeDCMD_SemanticsUnchanged(t *testing.T) {
	tags := &fakeTagReadable{
		tags: map[string]writeguard.TagMeta{
			"Silo-1/Feed.Rate":      {Writable: true, DCMDEnabled: true, OPCUAEnabled: false},
			"Silo-1/Emergency.Stp2": {Writable: false, DCMDEnabled: true, OPCUAEnabled: true},
			"Silo-1/NoCommand.Tag":  {Writable: true, DCMDEnabled: false, OPCUAEnabled: true},
		},
	}
	acl := &fakeACLReader{allow: true}
	guard := newGuard(tags, acl)

	// Master switch wins over the DCMD flag.
	d := guard.AuthorizeDCMD(context.Background(), "Silo-1", "Emergency.Stp2")
	if d.Allowed || d.Reason != "tag not writable" {
		t.Errorf("DCMD master switch: got %+v; want deny with reason %q", d, "tag not writable")
	}

	// DCMD flag denies when Writable=true.
	d = guard.AuthorizeDCMD(context.Background(), "Silo-1", "NoCommand.Tag")
	if d.Allowed || d.Reason != "dcmd not enabled" {
		t.Errorf("DCMD flag: got %+v; want deny with reason %q", d, "dcmd not enabled")
	}

	// Both flags true → allow.
	d = guard.AuthorizeDCMD(context.Background(), "Silo-1", "Feed.Rate")
	if !d.Allowed {
		t.Errorf("DCMD happy path: got %+v; want allow", d)
	}

	if acl.calls != 0 {
		t.Errorf("expected aclReader NEVER called on the DCMD path, got %d calls", acl.calls)
	}
}
