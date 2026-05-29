package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
)

// plcTagResponse is the API-safe tag representation.
type plcTagResponse struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Writable bool   `json:"writable"`
}

// plcResponse is the API-safe PLC representation.
type plcResponse struct {
	Name          string           `json:"name"`
	Address       string           `json:"address"`
	Slot          int              `json:"slot"`
	SocketTimeout string           `json:"socket_timeout"`
	ScanRate      string           `json:"scan_rate"`
	KeepAlive     bool             `json:"keep_alive"`
	Path          string           `json:"path"`
	Tags          []plcTagResponse `json:"tags"`
}

// plcToResponse converts a config.PLC to the API wire format.
func plcToResponse(p config.PLC) plcResponse {
	tags := make([]plcTagResponse, 0, len(p.Tags))
	for _, t := range p.Tags {
		tags = append(tags, plcTagResponse{Name: t.Name, Type: t.Type, Writable: t.Writable})
	}
	return plcResponse{
		Name:          p.Name,
		Address:       p.Address,
		Slot:          p.Slot,
		SocketTimeout: p.SocketTimeout,
		ScanRate:      p.ScanRate,
		KeepAlive:     p.KeepAlive,
		Path:          p.Path,
		Tags:          tags,
	}
}

// decodePLCRequest decodes the JSON body into a config.PLC.
func decodePLCRequest(r *http.Request) (config.PLC, error) {
	var req struct {
		Name          string           `json:"name"`
		Address       string           `json:"address"`
		Slot          int              `json:"slot"`
		SocketTimeout string           `json:"socket_timeout"`
		ScanRate      string           `json:"scan_rate"`
		KeepAlive     bool             `json:"keep_alive"`
		Path          string           `json:"path"`
		Tags          []plcTagResponse `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return config.PLC{}, err
	}
	tags := make([]config.TagDef, 0, len(req.Tags))
	for _, t := range req.Tags {
		tags = append(tags, config.TagDef{Name: t.Name, Type: t.Type, Writable: t.Writable})
	}
	return config.PLC{
		Name:          req.Name,
		Address:       req.Address,
		Slot:          req.Slot,
		SocketTimeout: req.SocketTimeout,
		ScanRate:      req.ScanRate,
		KeepAlive:     req.KeepAlive,
		Path:          req.Path,
		Tags:          tags,
	}, nil
}

// actorFromContext extracts the username from the auth claims in ctx, or
// returns "unknown" if the context carries no claims.
func actorFromContext(r *http.Request) string {
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
		return claims.Username
	}
	return "unknown"
}

// reloadPLCsFromStore lists PLCs from the store, builds a *config.Config with
// those PLCs, and calls plcMgr.Reload. A reload error is logged WARN but does
// NOT fail the HTTP response — the store write has already committed.
func (s *Server) reloadPLCsFromStore(r *http.Request) {
	if s.plcMgr == nil {
		return
	}
	plcs, err := s.plcStore.List(r.Context())
	if err != nil {
		s.log.Warn("plc store list failed during reload", "err", err)
		return
	}
	cfg := &config.Config{PLCs: plcs}
	if err := s.plcMgr.Reload(r.Context(), cfg); err != nil {
		s.log.Warn("plc manager reload error", "component", "api-plcs", "err", err)
	}
}

// handleListPLCs serves GET /api/plcs.
// Returns all PLCs from the store in a {"data":[...]} envelope.
func (s *Server) handleListPLCs(w http.ResponseWriter, r *http.Request) {
	plcs, err := s.plcStore.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not list PLCs")
		return
	}

	rows := make([]plcResponse, 0, len(plcs))
	for _, p := range plcs {
		rows = append(rows, plcToResponse(p))
	}
	writeJSON(w, http.StatusOK, struct {
		Data []plcResponse `json:"data"`
	}{Data: rows})
}

// handleGetPLC serves GET /api/plcs/{name}.
// Returns a single PLC by name; 404 if not found.
func (s *Server) handleGetPLC(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	p, err := s.plcStore.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, plcstore.ErrPLCNotFound) {
			writeAPIError(w, http.StatusNotFound, "plc_not_found", "PLC not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not get PLC")
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Data plcResponse `json:"data"`
	}{Data: plcToResponse(p)})
}

// handleCreatePLC serves POST /api/plcs.
// Validates the body, stores the PLC, audits, and reloads the manager.
func (s *Server) handleCreatePLC(w http.ResponseWriter, r *http.Request) {
	p, err := decodePLCRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	if err := config.ValidatePLC(p); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_plc", err.Error())
		return
	}

	if err := s.plcStore.Create(r.Context(), p); err != nil {
		if errors.Is(err, plcstore.ErrPLCAlreadyExists) {
			writeAPIError(w, http.StatusConflict, "duplicate_plc", "a PLC with that name already exists")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not create PLC")
		return
	}

	// Audit: only after a successful store write.
	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "plc.create",
			Username: actorFromContext(r),
			Detail:   p.Name,
		})
	}

	// Reload is best-effort; errors are logged but do not fail the response.
	s.reloadPLCsFromStore(r)

	// Fetch the stored PLC to return it (ensures we echo the canonical form).
	stored, err := s.plcStore.Get(r.Context(), p.Name)
	if err != nil {
		// Store committed; fall back to echoing the request body.
		stored = p
	}

	writeJSON(w, http.StatusCreated, struct {
		Data plcResponse `json:"data"`
	}{Data: plcToResponse(stored)})
}

// handleUpdatePLC serves PUT /api/plcs/{name}.
// Validates the body, updates the PLC, audits, and reloads the manager.
func (s *Server) handleUpdatePLC(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	p, err := decodePLCRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	if err := config.ValidatePLC(p); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_plc", err.Error())
		return
	}

	if err := s.plcStore.Update(r.Context(), name, p); err != nil {
		switch {
		case errors.Is(err, plcstore.ErrPLCNotFound):
			writeAPIError(w, http.StatusNotFound, "plc_not_found", "PLC not found")
		case errors.Is(err, plcstore.ErrPLCAlreadyExists):
			writeAPIError(w, http.StatusConflict, "duplicate_plc", "another PLC already has that name")
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update PLC")
		}
		return
	}

	// Audit: only after a successful store write.
	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "plc.update",
			Username: actorFromContext(r),
			Detail:   name,
		})
	}

	s.reloadPLCsFromStore(r)

	// Fetch the updated PLC to return the canonical form.
	stored, err := s.plcStore.Get(r.Context(), p.Name)
	if err != nil {
		stored = p
	}

	writeJSON(w, http.StatusOK, struct {
		Data plcResponse `json:"data"`
	}{Data: plcToResponse(stored)})
}

// handleDeletePLC serves DELETE /api/plcs/{name}.
// Deletes the PLC, audits, and reloads the manager.
func (s *Server) handleDeletePLC(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if err := s.plcStore.Delete(r.Context(), name); err != nil {
		if errors.Is(err, plcstore.ErrPLCNotFound) {
			writeAPIError(w, http.StatusNotFound, "plc_not_found", "PLC not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not delete PLC")
		return
	}

	// Audit: only after a successful store write.
	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "plc.delete",
			Username: actorFromContext(r),
			Detail:   name,
		})
	}

	s.reloadPLCsFromStore(r)

	w.WriteHeader(http.StatusNoContent)
}
