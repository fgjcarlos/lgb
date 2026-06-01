package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/plcstore"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// plcstoreTagReader adapts *plcstore.Store to writeguard.TagReadable.
// It reads Writable from the plc_tags table. DCMDEnabled is always false
// until PR3-pre adds the dcmd_enabled column to plcstore.
type plcstoreTagReader struct {
	store *plcstore.Store
}

func (r *plcstoreTagReader) TagMeta(ctx context.Context, plcName, tagName string) (writeguard.TagMeta, bool) {
	p, err := r.store.Get(ctx, plcName)
	if err != nil {
		return writeguard.TagMeta{}, false
	}
	for _, t := range p.Tags {
		if t.Name == tagName {
			// DCMDEnabled is PR3-pre; defaults to false (deny-by-default) until
			// the dcmd_enabled column and TagDef.DCMDEnabled field are added.
			return writeguard.TagMeta{Writable: t.Writable, DCMDEnabled: false}, true
		}
	}
	return writeguard.TagMeta{}, false
}

// writeTagRequest is the JSON body for POST .../write.
type writeTagRequest struct {
	Value any `json:"value"`
}

// registerWriteRoutes wires the write endpoint onto mux.
// It is only registered when both plcStore and writeGuard are non-nil.
// All roles may attempt a write; the Guard decides the outcome.
// (TWA-HTTP-3.1)
func (s *Server) registerWriteRoutes(mux *http.ServeMux) {
	if s.plcStore == nil || s.writeGuard == nil {
		return
	}
	if s.authTokens != nil {
		mws := []func(http.Handler) http.Handler{
			authMiddleware(s.authTokens),
			auth.RequireRole(auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin),
		}
		mux.Handle("POST /api/plcs/{plc}/tags/{tag}/write",
			withMiddleware(http.HandlerFunc(s.handleWriteTag), mws...))
	} else {
		mux.HandleFunc("POST /api/plcs/{plc}/tags/{tag}/write", s.handleWriteTag)
	}
}

// handleWriteTag serves POST /api/plcs/{plc}/tags/{tag}/write.
//
// Flow:
//  1. Decode body → extract value; 400 on malformed.
//  2. Look up tag in plcStore; 404 tag_not_found if absent.
//  3. Extract Claims → actor; call guard.AuthorizeHTTP.
//  4. On deny: emit audit deny event, return 403 write_denied.
//  5. On allow: call plcMgr.WriteTag; emit audit allow event, return 200.
//
// Audit MUST be emitted BEFORE the handler returns on both allow and deny.
// (TWA-HTTP-3.1, TWA-AUDIT-4.1)
func (s *Server) handleWriteTag(w http.ResponseWriter, r *http.Request) {
	plcName := r.PathValue("plc")
	tagName := r.PathValue("tag")

	// Step 1 — decode body.
	var req writeTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	// Step 2 — verify tag exists in plcStore. (TWA-ENFORCE-2.3)
	plc, err := s.plcStore.Get(r.Context(), plcName)
	if err != nil {
		if errors.Is(err, plcstore.ErrPLCNotFound) {
			writeAPIError(w, http.StatusNotFound, "tag_not_found", "PLC or tag not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not look up PLC")
		return
	}
	tagFound := false
	for _, t := range plc.Tags {
		if t.Name == tagName {
			tagFound = true
			break
		}
	}
	if !tagFound {
		writeAPIError(w, http.StatusNotFound, "tag_not_found", "tag not found")
		return
	}

	// Step 3 — extract actor from JWT claims.
	actor := writeguard.Actor{}
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
		actor.Username = claims.Username
		actor.Role = claims.Role
	}

	// Authorize — gate 1 (master switch) + gate 2 (ACL).
	decision := s.writeGuard.AuthorizeHTTP(r.Context(), actor, plcName, tagName, req.Value)

	if !decision.Allowed {
		// Step 4 — deny path: audit THEN respond.
		s.emitWriteAudit(r, plcName, tagName, req.Value, "deny", decision.Reason, "http", actor.Username)
		writeAPIError(w, http.StatusForbidden, "write_denied", "write not permitted")
		return
	}

	// Step 5 — allow path: write then audit then respond.
	if s.plcMgr != nil {
		if err := s.plcMgr.WriteTag(plcName, tagName, req.Value); err != nil {
			s.emitWriteAudit(r, plcName, tagName, req.Value, "deny", fmt.Sprintf("write error: %v", err), "http", actor.Username)
			writeAPIError(w, http.StatusInternalServerError, "write_error", "write failed")
			return
		}
	}
	s.emitWriteAudit(r, plcName, tagName, req.Value, "allow", "", "http", actor.Username)
	w.WriteHeader(http.StatusOK)
}

// emitWriteAudit emits a tag.write audit event. It is nil-safe — if auditLog is
// nil, the call is a no-op. Audit fires synchronously before the caller returns.
// (TWA-AUDIT-4.1)
func (s *Server) emitWriteAudit(r *http.Request, plcName, tagName string, value any, outcome, reason, source, username string) {
	if s.auditLog == nil {
		return
	}
	detail := fmt.Sprintf("plc=%s tag=%s value=%v outcome=%s source=%s", plcName, tagName, value, outcome, source)
	if reason != "" {
		detail += fmt.Sprintf(" reason=%s", reason)
	}
	_ = s.auditLog.Log(auth.AuditEvent{
		Action:   "tag.write",
		Username: username,
		Detail:   detail,
	})
}
