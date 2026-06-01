// Package writeguard implements the write enforcement core for the LGB gateway.
//
// It is the single authority that decides whether a write attempt is allowed,
// regardless of the write surface (HTTP or DCMD). It deliberately sits above
// internal/plc and internal/aclstore to avoid import cycles.
//
// Requirements: TWA-ENFORCE-2.1, TWA-ENFORCE-2.2 (HTTP path).
// TWA-ENFORCE-2.1 (DCMD path wired here; OnCommand wired in PR3).
package writeguard

import (
	"context"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// Source identifies which write surface initiated the request.
// It is load-bearing: it selects which enforcement gate runs.
type Source int

const (
	// SourceHTTP is a write via the REST endpoint. Requires Writable=true AND
	// an ACL row allowing the caller's role.
	SourceHTTP Source = iota

	// SourceDCMD is a write via a Sparkplug DCMD metric. Requires Writable=true
	// AND DCMDEnabled=true. No ACL lookup, no identity check.
	SourceDCMD
)

// TagMeta carries the per-tag safety switches read from plcstore.
type TagMeta struct {
	// Writable is the master safety switch. When false, no write surface is
	// allowed to write this tag — including admin via HTTP.
	Writable bool

	// DCMDEnabled is the per-tag DCMD opt-in. Only relevant for SourceDCMD.
	DCMDEnabled bool
}

// Actor carries the identity of the HTTP caller.
// For DCMD writes there is no actor — MQTT carries no identity.
type Actor struct {
	Username string
	Role     auth.Role
}

// Decision is the result of an authorization check.
type Decision struct {
	Allowed bool
	// Reason is set on deny and describes why; used for audit Detail.
	// Values: "tag not writable" | "acl deny" | "dcmd not enabled" | ""
	Reason string
}

// TagReadable reads per-tag metadata from the PLC config store.
// The implementation is plcstore.Store; tests inject a fake.
type TagReadable interface {
	TagMeta(ctx context.Context, plc, tag string) (TagMeta, bool)
}

// ACLReader checks whether a role is allowed to write a given (plc, tag) pair.
// The implementation is *aclstore.Store; tests inject a fake.
type ACLReader interface {
	CanWrite(ctx context.Context, role, plc, tag string) (bool, error)
}

// TagWriter writes a value to a named PLC tag.
// The implementation is *plc.Manager; tests inject a fake.
type TagWriter interface {
	WriteTag(plcName, tag string, val any) error
}

// AuditSink records audit events. The implementation is *auth.AuditLogger;
// tests inject a fake or nil.
type AuditSink interface {
	Log(event auth.AuditEvent) error
}

// Guard is the write enforcement core. It is safe for concurrent use.
// All fields are read-only after construction.
type Guard struct {
	tags TagReadable
	acl  ACLReader
}

// NewGuard constructs a Guard with the given dependencies.
// tags and acl must not be nil.
func NewGuard(tags TagReadable, acl ACLReader) *Guard {
	return &Guard{tags: tags, acl: acl}
}

// AuthorizeHTTP enforces the HTTP write gate for the given actor, plc, and tag.
//
// Gate 1 — master switch: if tag.Writable == false, deny immediately.
//
//	No ACL lookup is performed. This applies to every role including admin.
//
// Gate 2 — ACL: call aclStore.CanWrite(role, plc, tag).
//
//	If false (no row or allow_write=0), deny.
//
// Returns Decision{Allowed: true} only when both layers pass.
// Deny-by-default: unknown tag (TagMeta not found) returns deny.
//
// Requirements: TWA-ENFORCE-2.1, TWA-ENFORCE-2.2.
func (g *Guard) AuthorizeHTTP(ctx context.Context, actor Actor, plcName, tagName string, val any) Decision {
	meta, ok := g.tags.TagMeta(ctx, plcName, tagName)
	if !ok || !meta.Writable {
		return Decision{Allowed: false, Reason: "tag not writable"}
	}

	allowed, err := g.acl.CanWrite(ctx, string(actor.Role), plcName, tagName)
	if err != nil || !allowed {
		return Decision{Allowed: false, Reason: "acl deny"}
	}

	return Decision{Allowed: true}
}

// AuthorizeDCMD enforces the DCMD write gate. There is NO actor, NO role, and
// NO ACL lookup for this path — MQTT carries no identity.
//
// Gate 1 — master switch: if tag.Writable == false, deny immediately.
// Gate 2 — DCMD flag: if tag.DCMDEnabled == false, deny.
//
// aclStore is NEVER consulted for this path. An operator-level HTTP ACL row
// granting write on a tag does NOT enable DCMD writes.
//
// Requirements: TWA-ENFORCE-2.1 (DCMD scenarios).
func (g *Guard) AuthorizeDCMD(ctx context.Context, plcName, tagName string) Decision {
	meta, ok := g.tags.TagMeta(ctx, plcName, tagName)
	if !ok || !meta.Writable {
		return Decision{Allowed: false, Reason: "tag not writable"}
	}

	if !meta.DCMDEnabled {
		return Decision{Allowed: false, Reason: "dcmd not enabled"}
	}

	return Decision{Allowed: true}
}
