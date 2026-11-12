// write.go — the OPC UA write surface.
//
// The gateway installs its own OPC UA Write handler on the gopcua server.
// gopcua's default Write service calls ns.SetAttribute, which REPLACES the
// node's value callback: an OPC UA client write would be silently accepted,
// break the read path of that tag and never reach the PLC. The handler here
// never mutates the address space — a write either goes through the shared
// write guard to the PLC, or it is rejected with a UA status code.
//
// Trust model: the gateway's OPC UA server runs anonymous user tokens, so a
// write carries no authenticated principal. Authorization is therefore a
// per-tag opt-in (opcua_write_enabled) evaluated by the shared write guard,
// never an identity check and never an ACL lookup.
//
// Requirements: TWA-OPCUA-3.3, TWA-ENFORCE-2.1, TWA-AUDIT-4.1.
package opcua

import (
	"context"
	"fmt"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/tagvalue"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// Options carries the optional dependencies of the OPC UA write surface.
//
// Every field is optional. A server built without a Guard or without a Writer
// is read-only: every write attempt is denied with ua.StatusBadUserAccessDenied
// and audited as a deny.
type Options struct {
	// Guard is the shared write enforcement core. When nil every write is
	// denied (deny-by-default).
	Guard *writeguard.Guard

	// Writer writes the value to the PLC. When nil every write is denied.
	Writer writeguard.TagWriter

	// Audit receives exactly one event per write attempt. A nil sink denies
	// every write: without a sink no attempt can be accounted for.
	Audit writeguard.AuditSink

	// Tags exposes the per-tag metadata used to derive the node AccessLevel
	// attribute. It is the same metadata the guard reads (the PLC config
	// store), so the client-visible access level matches the server policy.
	// When nil, the server's own configuration is used instead.
	Tags writeguard.TagReadable
}

// handleWriteRequest is the gopcua service handler for the Write service.
//
// It MUST be registered before srv.Start(ctx): gopcua registers its default
// Write service inside Server.Start and RegisterHandler does NOT override an
// already-registered handler.
//
// (TWA-OPCUA-3.3)
func (s *Server) handleWriteRequest(_ *uasc.SecureChannel, r ua.Request, _ uint32) (ua.Response, error) {
	req, ok := r.(*ua.WriteRequest)
	if !ok {
		return nil, ua.StatusBadRequestTypeInvalid
	}
	return s.HandleWriteRequest(req), nil
}

// HandleWriteRequest processes an OPC UA Write request and returns the
// response. It is the testable seam of the write surface: it takes a
// *ua.WriteRequest and returns a *ua.WriteResponse, so the behaviour can be
// asserted without a live OPC UA session.
//
// The handler NEVER mutates the address space. Every node in the request is
// one write attempt and produces exactly one UA status code and exactly one
// audit event.
//
// (TWA-OPCUA-3.3, TWA-AUDIT-4.1)
func (s *Server) HandleWriteRequest(req *ua.WriteRequest) *ua.WriteResponse {
	results := make([]ua.StatusCode, 0)
	if req != nil {
		results = make([]ua.StatusCode, len(req.NodesToWrite))
		for i, wv := range req.NodesToWrite {
			results[i] = s.handleWriteValue(wv)
		}
	}

	var handle uint32
	if req != nil && req.RequestHeader != nil {
		handle = req.RequestHeader.RequestHandle
	}

	return &ua.WriteResponse{
		ResponseHeader: &ua.ResponseHeader{
			Timestamp:          time.Now(),
			RequestHandle:      handle,
			ServiceResult:      ua.StatusOK,
			ServiceDiagnostics: &ua.DiagnosticInfo{},
			StringTable:        []string{},
			AdditionalHeader:   ua.NewExtensionObject(nil),
		},
		Results:         results,
		DiagnosticInfos: []*ua.DiagnosticInfo{},
	}
}

// handleWriteValue processes a single WriteValue and returns its status code.
//
// Order of checks (TWA-OPCUA-3.3):
//  1. unknown node id           → ua.StatusBadNodeIDUnknown
//  2. attribute != Value        → ua.StatusBadAttributeIDInvalid
//  3. no guard, writer or audit → deny (deny-by-default)
//  4. value not convertible     → ua.StatusBadTypeMismatch
//  5. guard denies              → ua.StatusBadUserAccessDenied
//  6. audit not persisted       → ua.StatusBadInternalError (no PLC write)
//  7. PLC write error           → ua.StatusBadInternalError
//  8. otherwise                 → ua.StatusOK
func (s *Server) handleWriteValue(wv *ua.WriteValue) ua.StatusCode {
	if wv == nil || wv.NodeID == nil {
		_ = s.emitWriteAudit("", "", nil, "deny", "unknown node id")
		return ua.StatusBadNodeIDUnknown
	}

	// (2) Resolve the node id to its (plc, tag) pair and declared type.
	plcName, tagName, declaredType, ok := s.resolveTag(wv.NodeID)
	if !ok {
		_ = s.emitWriteAudit("", "", nil, "deny", "unknown node id")
		return ua.StatusBadNodeIDUnknown
	}

	// (3) Only the Value attribute may be written.
	if wv.AttributeID != ua.AttributeIDValue {
		_ = s.emitWriteAudit(plcName, tagName, nil, "deny", "invalid attribute id")
		return ua.StatusBadAttributeIDInvalid
	}

	// (4) Deny-by-default: without a guard, a writer or an audit sink nothing
	// may be written. A write that cannot be audited must not reach the PLC.
	// Resolve the node first so unknown identifiers still receive the precise
	// OPC UA status required by the Write service contract.
	if s.guard == nil || s.writer == nil || s.audit == nil {
		_ = s.emitWriteAudit(plcName, tagName, variantValue(wv.Value), "deny", "opcua write surface not configured")
		return ua.StatusBadUserAccessDenied
	}

	// (5) Convert the OPC UA variant value to the tag's declared type.
	raw := variantValue(wv.Value)
	val, ok := convertValue(raw, declaredType)
	if !ok {
		_ = s.emitWriteAudit(plcName, tagName, raw, "deny", "value type mismatch")
		return ua.StatusBadTypeMismatch
	}

	// (6) Authorize through the shared write guard — no actor, no role, no ACL.
	decision := s.guard.AuthorizeOPCUA(context.Background(), plcName, tagName)
	if !decision.Allowed {
		_ = s.emitWriteAudit(plcName, tagName, val, "deny", decision.Reason)
		return ua.StatusBadUserAccessDenied
	}

	// (7) Account for the attempt BEFORE the write. The audit event is
	// persisted first, so a physical write always carries its audit trail: an
	// event that cannot be persisted stops the write instead of leaving an
	// untraceable setpoint behind.
	if err := s.emitWriteAudit(plcName, tagName, val, "allow", ""); err != nil {
		return ua.StatusBadInternalError
	}

	// (8) Write through the PLC driver. The attempt already carries its one
	// audit event, so a driver failure is reported to the client without a
	// second event.
	if err := s.writer.WriteTag(plcName, tagName, val); err != nil {
		return ua.StatusBadInternalError
	}
	return ua.StatusOK
}

// resolveTag maps a node id to its (plc, tag) pair and declared type.
//
// The address space holds one namespace per PLC and one variable node per tag
// (ua.NewStringNodeID(ns.ID(), tag.Name)), so the namespace id identifies the
// PLC and the node's string identifier is the tag name. Namespace URIs are
// never parsed at write time.
//
// (TWA-OPCUA-3.3 step 1)
func (s *Server) resolveTag(nodeID *ua.NodeID) (plcName, tagName, declaredType string, ok bool) {
	if nodeID == nil {
		return "", "", "", false
	}
	plcName, ok = s.nsPLC[nodeID.Namespace()]
	if !ok {
		return "", "", "", false
	}
	tagName = nodeID.StringID()
	if tagName == "" {
		return "", "", "", false
	}
	declaredType, ok = s.tagTypes[tagKey{plc: plcName, tag: tagName}]
	if !ok {
		return "", "", "", false
	}
	return plcName, tagName, declaredType, true
}

// emitWriteAudit emits exactly one tag.write audit event for an OPC UA write
// attempt and reports whether the event was persisted.
//
// It is nil-safe: with no audit sink configured the call is a no-op that
// reports success. The write surface denies writes without a sink, so this
// path only serves the deny branches.
//
// The OPC UA surface carries no authenticated principal, so the actor username
// is always empty. The detail uses the same flat key=value format as the HTTP
// and DCMD paths, with source=opcua.
//
// (TWA-AUDIT-4.1)
func (s *Server) emitWriteAudit(plcName, tagName string, value any, outcome, reason string) error {
	if s.audit == nil {
		return nil
	}
	detail := fmt.Sprintf("plc=%s tag=%s value=%v outcome=%s source=opcua", plcName, tagName, value, outcome)
	if reason != "" {
		detail += fmt.Sprintf(" reason=%s", reason)
	}
	return s.audit.Log(auth.AuditEvent{
		Action:   "tag.write",
		Username: "",
		Detail:   detail,
	})
}

// variantValue extracts the Go value carried by an OPC UA data value.
// A nil value yields nil.
func variantValue(dv *ua.DataValue) any {
	if dv == nil || dv.Value == nil {
		return nil
	}
	return dv.Value.Value()
}

// convertValue converts an OPC UA variant value to the tag's declared type.
//
// It delegates to tagvalue.Coerce, the conversion shared with the HTTP write
// endpoint: the returned Go type is the exact type the PLC driver expects for
// that declared type (e.g. Float → float32, Double → float64).
//
// ok is false when the value cannot be converted — the caller then denies the
// write with ua.StatusBadTypeMismatch and the value never reaches the PLC.
//
// (TWA-OPCUA-3.3 step 2)
func convertValue(raw any, declaredType string) (any, bool) {
	val, err := tagvalue.Coerce(raw, declaredType)
	return val, err == nil
}
