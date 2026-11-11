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
	"math"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uasc"

	"github.com/fgjcarlos/lgb/internal/auth"
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

	// Audit receives exactly one event per write attempt. When nil the audit
	// calls are no-ops (the audit sink is nil-safe everywhere else too).
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
//  1. unknown node id        → ua.StatusBadNodeIDUnknown
//  2. attribute != Value     → ua.StatusBadAttributeIDInvalid
//  3. no guard or no writer  → deny (deny-by-default)
//  4. value not convertible  → ua.StatusBadTypeMismatch
//  5. guard denies           → ua.StatusBadUserAccessDenied
//  6. PLC write error        → ua.StatusBadInternalError
//  7. otherwise              → ua.StatusOK
func (s *Server) handleWriteValue(wv *ua.WriteValue) ua.StatusCode {
	if wv == nil || wv.NodeID == nil {
		s.emitWriteAudit("", "", nil, "deny", "unknown node id")
		return ua.StatusBadNodeIDUnknown
	}

	// (2) Resolve the node id to its (plc, tag) pair and declared type.
	plcName, tagName, declaredType, ok := s.resolveTag(wv.NodeID)
	if !ok {
		s.emitWriteAudit("", "", nil, "deny", "unknown node id")
		return ua.StatusBadNodeIDUnknown
	}

	// (3) Only the Value attribute may be written.
	if wv.AttributeID != ua.AttributeIDValue {
		s.emitWriteAudit(plcName, tagName, nil, "deny", "invalid attribute id")
		return ua.StatusBadAttributeIDInvalid
	}

	// (4) Deny-by-default: without a guard or a writer nothing may be written.
	// Resolve the node first so unknown identifiers still receive the precise
	// OPC UA status required by the Write service contract.
	if s.guard == nil || s.writer == nil {
		s.emitWriteAudit(plcName, tagName, variantValue(wv.Value), "deny", "opcua write surface not configured")
		return ua.StatusBadUserAccessDenied
	}

	// (5) Convert the OPC UA variant value to the tag's declared type.
	raw := variantValue(wv.Value)
	val, ok := convertValue(raw, declaredType)
	if !ok {
		s.emitWriteAudit(plcName, tagName, raw, "deny", "value type mismatch")
		return ua.StatusBadTypeMismatch
	}

	// (6) Authorize through the shared write guard — no actor, no role, no ACL.
	decision := s.guard.AuthorizeOPCUA(context.Background(), plcName, tagName)
	if !decision.Allowed {
		s.emitWriteAudit(plcName, tagName, val, "deny", decision.Reason)
		return ua.StatusBadUserAccessDenied
	}

	// (7) Write through the PLC driver.
	if err := s.writer.WriteTag(plcName, tagName, val); err != nil {
		s.emitWriteAudit(plcName, tagName, val, "deny", fmt.Sprintf("write error: %v", err))
		return ua.StatusBadInternalError
	}

	// (8) Allow.
	s.emitWriteAudit(plcName, tagName, val, "allow", "")
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
// attempt. It is nil-safe: with no audit sink configured the call is a no-op.
//
// The OPC UA surface carries no authenticated principal, so the actor username
// is always empty. The detail uses the same flat key=value format as the HTTP
// and DCMD paths, with source=opcua.
//
// (TWA-AUDIT-4.1)
func (s *Server) emitWriteAudit(plcName, tagName string, value any, outcome, reason string) {
	if s.audit == nil {
		return
	}
	detail := fmt.Sprintf("plc=%s tag=%s value=%v outcome=%s source=opcua", plcName, tagName, value, outcome)
	if reason != "" {
		detail += fmt.Sprintf(" reason=%s", reason)
	}
	_ = s.audit.Log(auth.AuditEvent{
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
// The supported declared types are the ones internal/config validates:
// Boolean, Int8..UInt64, Float, Double and String. The returned Go type is the
// exact type the PLC driver expects for that declared type (e.g. Float →
// float32, Double → float64).
//
// ok is false when the value cannot be converted — the caller then denies the
// write with ua.StatusBadTypeMismatch and the value never reaches the PLC.
//
// (TWA-OPCUA-3.3 step 2)
func convertValue(raw any, declaredType string) (any, bool) {
	switch declaredType {
	case "Boolean":
		b, ok := raw.(bool)
		return b, ok
	case "Int8":
		i, ok := toInt64(raw)
		if !ok || i < math.MinInt8 || i > math.MaxInt8 {
			return nil, false
		}
		return int8(i), true
	case "Int16":
		i, ok := toInt64(raw)
		if !ok || i < math.MinInt16 || i > math.MaxInt16 {
			return nil, false
		}
		return int16(i), true
	case "Int32":
		i, ok := toInt64(raw)
		if !ok || i < math.MinInt32 || i > math.MaxInt32 {
			return nil, false
		}
		return int32(i), true
	case "Int64":
		i, ok := toInt64(raw)
		if !ok {
			return nil, false
		}
		return i, true
	case "UInt8":
		i, ok := toInt64(raw)
		if !ok || i < 0 || i > math.MaxUint8 {
			return nil, false
		}
		return uint8(i), true
	case "UInt16":
		i, ok := toInt64(raw)
		if !ok || i < 0 || i > math.MaxUint16 {
			return nil, false
		}
		return uint16(i), true
	case "UInt32":
		i, ok := toInt64(raw)
		if !ok || i < 0 || i > math.MaxUint32 {
			return nil, false
		}
		return uint32(i), true
	case "UInt64":
		i, ok := toUint64(raw)
		if !ok {
			return nil, false
		}
		return i, true
	case "Float":
		f, ok := toFloat64(raw)
		if !ok {
			return nil, false
		}
		return float32(f), true
	case "Double":
		f, ok := toFloat64(raw)
		if !ok {
			return nil, false
		}
		return f, true
	case "String":
		s, ok := raw.(string)
		return s, ok
	default:
		// Unknown declared type — the value cannot be converted safely.
		return nil, false
	}
}

// toInt64 converts a numeric OPC UA value to an int64. Floating point values
// are accepted only when they are integral and in range.
func toInt64(raw any) (int64, bool) {
	switch v := raw.(type) {
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	case float32:
		return floatToInt64(float64(v))
	case float64:
		return floatToInt64(v)
	default:
		return 0, false
	}
}

// floatToInt64 converts an integral float to an int64.
func floatToInt64(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f != math.Trunc(f) {
		return 0, false
	}
	if f < math.MinInt64 || f > math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}

// toUint64 converts a non-negative integral OPC UA value to a uint64 without
// narrowing the full uint64 input range through int64.
func toUint64(raw any) (uint64, bool) {
	switch v := raw.(type) {
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case int8:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int16:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int32:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case float32:
		return floatToUint64(float64(v))
	case float64:
		return floatToUint64(v)
	default:
		return 0, false
	}
}

// floatToUint64 converts an integral float in [0, 2^64) to uint64. The upper
// bound is exclusive because float64 cannot represent math.MaxUint64 exactly.
func floatToUint64(f float64) (uint64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f != math.Trunc(f) || f >= math.Exp2(64) {
		return 0, false
	}
	return uint64(f), true
}

// toFloat64 converts a numeric OPC UA value to a float64.
func toFloat64(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}
