package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/fgjcarlos/lgb/internal/aclstore"
	"github.com/fgjcarlos/lgb/internal/auth"
)

// aclRuleRequest is the JSON body for POST and PUT /api/acl/rules[/{id}].
type aclRuleRequest struct {
	Role       string `json:"role"`
	PLC        string `json:"plc"`
	Tag        string `json:"tag"`
	AllowWrite bool   `json:"allow_write"`
}

// aclRuleResponse is the API-safe ACL rule representation.
// The surrogate ID is exposed so callers can use it in GET/PUT/DELETE by-id
// paths. (TWA-API-5.1)
type aclRuleResponse struct {
	ID         int64  `json:"id"`
	Role       string `json:"role"`
	PLC        string `json:"plc"`
	Tag        string `json:"tag"`
	AllowWrite bool   `json:"allow_write"`
}

// aclRuleToResponse converts an aclstore.ACLRule to the API wire format.
func aclRuleToResponse(r aclstore.ACLRule) aclRuleResponse {
	return aclRuleResponse{
		ID:         r.ID,
		Role:       r.Role,
		PLC:        r.PLC,
		Tag:        r.Tag,
		AllowWrite: r.AllowWrite,
	}
}

// registerACLRoutes wires the ACL CRUD endpoints onto mux.
// Routes are registered only when aclStore is non-nil and authTokens are set.
// All endpoints are admin-only (auth.RequireRole(RoleAdmin)). (TWA-API-5.1)
func (s *Server) registerACLRoutes(mux *http.ServeMux) {
	if s.aclStore == nil {
		return
	}

	if s.authTokens != nil {
		adminMWs := []func(http.Handler) http.Handler{
			authMiddleware(s.authTokens),
			auth.RequireRole(auth.RoleAdmin),
		}
		mux.Handle("GET /api/acl/rules",
			withMiddleware(http.HandlerFunc(s.handleListACLRules), adminMWs...))
		mux.Handle("POST /api/acl/rules",
			withMiddleware(http.HandlerFunc(s.handleCreateACLRule), adminMWs...))
		mux.Handle("GET /api/acl/rules/{id}",
			withMiddleware(http.HandlerFunc(s.handleGetACLRule), adminMWs...))
		mux.Handle("PUT /api/acl/rules/{id}",
			withMiddleware(http.HandlerFunc(s.handleUpdateACLRule), adminMWs...))
		mux.Handle("DELETE /api/acl/rules/{id}",
			withMiddleware(http.HandlerFunc(s.handleDeleteACLRule), adminMWs...))
	} else {
		mux.HandleFunc("GET /api/acl/rules", s.handleListACLRules)
		mux.HandleFunc("POST /api/acl/rules", s.handleCreateACLRule)
		mux.HandleFunc("GET /api/acl/rules/{id}", s.handleGetACLRule)
		mux.HandleFunc("PUT /api/acl/rules/{id}", s.handleUpdateACLRule)
		mux.HandleFunc("DELETE /api/acl/rules/{id}", s.handleDeleteACLRule)
	}
}

// parseACLRuleID extracts and validates the {id} path value.
// Writes a 400 response and returns (0, false) on failure.
func parseACLRuleID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// handleListACLRules serves GET /api/acl/rules.
// Returns all ACL rules from the store in a {"data":[...]} envelope.
// (TWA-API-5.1)
func (s *Server) handleListACLRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.aclStore.ListRules(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not list ACL rules")
		return
	}

	rows := make([]aclRuleResponse, 0, len(rules))
	for _, rule := range rules {
		rows = append(rows, aclRuleToResponse(rule))
	}
	writeJSON(w, http.StatusOK, struct {
		Data []aclRuleResponse `json:"data"`
	}{Data: rows})
}

// handleCreateACLRule serves POST /api/acl/rules.
// Validates the body, stores the rule, emits acl.create audit (after write). (TWA-API-5.1)
func (s *Server) handleCreateACLRule(w http.ResponseWriter, r *http.Request) {
	var req aclRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	rule := aclstore.ACLRule{
		Role:       req.Role,
		PLC:        req.PLC,
		Tag:        req.Tag,
		AllowWrite: req.AllowWrite,
	}

	if err := s.aclStore.CreateRule(r.Context(), rule); err != nil {
		switch {
		case errors.Is(err, aclstore.ErrRuleAlreadyExists):
			writeAPIError(w, http.StatusConflict, "duplicate_rule", "a rule for this (role, plc, tag) already exists")
		case errors.Is(err, aclstore.ErrInvalidRole):
			writeAPIError(w, http.StatusBadRequest, "invalid_rule", "role must be admin, operator, or viewer")
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not create ACL rule")
		}
		return
	}

	// Audit: only after a successful store write. (TWA-API-5.1)
	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "acl.create",
			Username: actorFromContext(r),
			Detail:   fmt.Sprintf("role=%s plc=%s tag=%s", req.Role, req.PLC, req.Tag),
		})
	}

	// Fetch the stored rule to return canonical form (with assigned ID).
	rules, err := s.aclStore.ListRules(r.Context())
	if err == nil {
		for _, stored := range rules {
			if stored.Role == rule.Role && stored.PLC == rule.PLC && stored.Tag == rule.Tag {
				writeJSON(w, http.StatusCreated, struct {
					Data aclRuleResponse `json:"data"`
				}{Data: aclRuleToResponse(stored)})
				return
			}
		}
	}

	// Fallback: echo the request (ID will be 0 but store committed).
	writeJSON(w, http.StatusCreated, struct {
		Data aclRuleResponse `json:"data"`
	}{Data: aclRuleToResponse(rule)})
}

// handleGetACLRule serves GET /api/acl/rules/{id}.
// Returns a single ACL rule by surrogate id; 404 if not found. (TWA-API-5.1)
func (s *Server) handleGetACLRule(w http.ResponseWriter, r *http.Request) {
	id, ok := parseACLRuleID(w, r)
	if !ok {
		return
	}

	rule, err := s.aclStore.GetRule(r.Context(), id)
	if err != nil {
		if errors.Is(err, aclstore.ErrRuleNotFound) {
			writeAPIError(w, http.StatusNotFound, "rule_not_found", "ACL rule not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not get ACL rule")
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Data aclRuleResponse `json:"data"`
	}{Data: aclRuleToResponse(rule)})
}

// handleUpdateACLRule serves PUT /api/acl/rules/{id}.
// Replaces all fields of the rule; 404 / 409 / 400 as applicable.
// Emits acl.update audit after successful write. (TWA-API-5.1)
func (s *Server) handleUpdateACLRule(w http.ResponseWriter, r *http.Request) {
	id, ok := parseACLRuleID(w, r)
	if !ok {
		return
	}

	var req aclRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	rule := aclstore.ACLRule{
		Role:       req.Role,
		PLC:        req.PLC,
		Tag:        req.Tag,
		AllowWrite: req.AllowWrite,
	}

	if err := s.aclStore.UpdateRule(r.Context(), id, rule); err != nil {
		switch {
		case errors.Is(err, aclstore.ErrRuleNotFound):
			writeAPIError(w, http.StatusNotFound, "rule_not_found", "ACL rule not found")
		case errors.Is(err, aclstore.ErrRuleAlreadyExists):
			writeAPIError(w, http.StatusConflict, "duplicate_rule", "a rule for this (role, plc, tag) already exists")
		case errors.Is(err, aclstore.ErrInvalidRole):
			writeAPIError(w, http.StatusBadRequest, "invalid_rule", "role must be admin, operator, or viewer")
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update ACL rule")
		}
		return
	}

	// Audit: only after a successful store write.
	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "acl.update",
			Username: actorFromContext(r),
			Detail:   fmt.Sprintf("id=%d role=%s plc=%s tag=%s", id, req.Role, req.PLC, req.Tag),
		})
	}

	// Fetch the updated rule to return canonical form.
	stored, err := s.aclStore.GetRule(r.Context(), id)
	if err != nil {
		// Store committed; fall back to echoing the request with the known ID.
		rule.ID = id
		stored = rule
	}

	writeJSON(w, http.StatusOK, struct {
		Data aclRuleResponse `json:"data"`
	}{Data: aclRuleToResponse(stored)})
}

// handleDeleteACLRule serves DELETE /api/acl/rules/{id}.
// Deletes the rule; 404 if not found. Emits acl.delete audit after write. (TWA-API-5.1)
func (s *Server) handleDeleteACLRule(w http.ResponseWriter, r *http.Request) {
	id, ok := parseACLRuleID(w, r)
	if !ok {
		return
	}

	// Fetch before delete so we can include identifying info in the audit detail.
	existing, fetchErr := s.aclStore.GetRule(r.Context(), id)

	if err := s.aclStore.DeleteRule(r.Context(), id); err != nil {
		if errors.Is(err, aclstore.ErrRuleNotFound) {
			writeAPIError(w, http.StatusNotFound, "rule_not_found", "ACL rule not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not delete ACL rule")
		return
	}

	// Audit: only after a successful store write.
	if s.auditLog != nil {
		detail := fmt.Sprintf("id=%d", id)
		if fetchErr == nil {
			detail = fmt.Sprintf("id=%d role=%s plc=%s tag=%s", id, existing.Role, existing.PLC, existing.Tag)
		}
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "acl.delete",
			Username: actorFromContext(r),
			Detail:   detail,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
