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
// It returns predefined Writable and DCMDEnabled values per (plc, tag).
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
	allow    bool
	calls    int
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
