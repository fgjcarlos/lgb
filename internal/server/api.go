package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/plc"
)

type tagSnapshotProvider interface {
	CurrentSnapshot() map[string]map[string]plc.TagValue
}

type tagResponse struct {
	PLC       string    `json:"plc"`
	Tag       string    `json:"tag"`
	Value     any       `json:"value"`
	Quality   string    `json:"quality"`
	Timestamp time.Time `json:"timestamp"`
}

type paginationResponse struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// withMiddleware wraps h with the provided middleware functions. Middleware is
// applied left-to-right, so the first element in mws is the outermost wrapper
// and therefore runs first when a request arrives.
//
//	withMiddleware(h, mw1, mw2)  →  mw1(mw2(h))
func withMiddleware(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	// Apply in reverse so that mws[0] ends up as the outermost layer.
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tags/current", s.handleCurrentTags)
	mux.HandleFunc("GET /api/ws/tags", s.handleTagsWebSocket)

	// Auth endpoints — login is public; refresh requires a valid token.
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	if s.authTokens != nil {
		mux.Handle("POST /api/auth/refresh",
			withMiddleware(http.HandlerFunc(s.handleRefresh), authMiddleware(s.authTokens)))
	} else {
		mux.HandleFunc("POST /api/auth/refresh", s.handleRefresh)
	}

	// User CRUD endpoints — admin only.
	if s.userStore != nil && s.authTokens != nil {
		adminMWs := []func(http.Handler) http.Handler{
			authMiddleware(s.authTokens),
			auth.RequireRole(auth.RoleAdmin),
		}
		mux.Handle("GET /api/users",
			withMiddleware(http.HandlerFunc(s.handleListUsers), adminMWs...))
		mux.Handle("POST /api/users",
			withMiddleware(http.HandlerFunc(s.handleCreateUser), adminMWs...))
		mux.Handle("GET /api/users/{id}",
			withMiddleware(http.HandlerFunc(s.handleGetUser), adminMWs...))
		mux.Handle("PUT /api/users/{id}/role",
			withMiddleware(http.HandlerFunc(s.handleUpdateUserRole), adminMWs...))
		mux.Handle("PUT /api/users/{id}/password",
			withMiddleware(http.HandlerFunc(s.handleUpdateUserPassword), adminMWs...))
		mux.Handle("DELETE /api/users/{id}",
			withMiddleware(http.HandlerFunc(s.handleDeleteUser), adminMWs...))
	}
}

// authMiddleware is a thin adapter that converts auth.Middleware to the
// func(http.Handler) http.Handler signature expected by withMiddleware.
func authMiddleware(ts *auth.TokenService) func(http.Handler) http.Handler {
	return auth.Middleware(ts)
}

// PublishTagUpdate pushes a PLC tag update to realtime API subscribers.
func (s *Server) PublishTagUpdate(update plc.TagUpdate) {
	if s.tagHub != nil {
		s.tagHub.publish(update)
	}
}

func (s *Server) handleCurrentTags(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r)
	if !ok {
		return
	}
	provider, ok := s.plcMgr.(tagSnapshotProvider)
	if !ok || provider == nil {
		writeJSON(w, http.StatusOK, struct {
			Data       []tagResponse      `json:"data"`
			Pagination paginationResponse `json:"pagination"`
		}{Data: []tagResponse{}, Pagination: paginationResponse{Limit: limit, Offset: offset, Count: 0}})
		return
	}

	rows := flattenSnapshot(provider.CurrentSnapshot())
	count := len(rows)
	start := offset
	if start > count {
		start = count
	}
	end := start + limit
	if end > count {
		end = count
	}
	writeJSON(w, http.StatusOK, struct {
		Data       []tagResponse      `json:"data"`
		Pagination paginationResponse `json:"pagination"`
	}{Data: rows[start:end], Pagination: paginationResponse{Limit: limit, Offset: offset, Count: count}})
}

func (s *Server) handleTagsWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.authTokens != nil && !s.authorizeAPIRequest(w, r) {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	sub := &tagSubscriber{ch: make(chan plc.TagUpdate, 16)}
	sub.setFilter(r.URL.Query().Get("plc"), r.URL.Query().Get("tag"))
	s.tagHub.register(sub)
	defer s.tagHub.unregister(sub)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	writeErr := make(chan error, 1)
	var writeMu sync.Mutex
	writeJSONMessage := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return wsjson.Write(ctx, conn, value)
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-sub.ch:
				if !ok {
					return
				}
				msg := struct {
					Type      string    `json:"type"`
					PLC       string    `json:"plc"`
					Tag       string    `json:"tag"`
					Value     any       `json:"value"`
					Timestamp time.Time `json:"timestamp"`
				}{Type: "tag_update", PLC: update.PLCName, Tag: update.Tag, Value: update.Value, Timestamp: update.Timestamp}
				if err := writeJSONMessage(msg); err != nil {
					cancel()
					writeErr <- err
					return
				}
			case <-ticker.C:
				if err := writeJSONMessage(struct {
					Type string `json:"type"`
				}{Type: "ping"}); err != nil {
					cancel()
					writeErr <- err
					return
				}
			}
		}
	}()

	if err := writeJSONMessage(struct {
		Type string `json:"type"`
		PLC  string `json:"plc,omitempty"`
		Tag  string `json:"tag,omitempty"`
	}{Type: "subscribed", PLC: r.URL.Query().Get("plc"), Tag: r.URL.Query().Get("tag")}); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-writeErr:
			return
		default:
		}

		var msg tagWSClientMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		switch msg.Type {
		case "subscribe":
			sub.setFilter(msg.PLC, msg.Tag)
			if err := writeJSONMessage(struct {
				Type string `json:"type"`
				PLC  string `json:"plc,omitempty"`
				Tag  string `json:"tag,omitempty"`
			}{Type: "subscribed", PLC: msg.PLC, Tag: msg.Tag}); err != nil {
				return
			}
		case "unsubscribe":
			sub.unsubscribe()
			if err := writeJSONMessage(struct {
				Type string `json:"type"`
			}{Type: "unsubscribed"}); err != nil {
				return
			}
		case "ping":
			if err := writeJSONMessage(struct {
				Type string `json:"type"`
			}{Type: "pong"}); err != nil {
				return
			}
		default:
			if err := writeJSONMessage(struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
			}{Type: "error", Code: "bad_message", Message: "type must be subscribe, unsubscribe, or ping"}); err != nil {
				return
			}
		}
	}
}

type tagWSClientMessage struct {
	Type string `json:"type"`
	PLC  string `json:"plc,omitempty"`
	Tag  string `json:"tag,omitempty"`
}

func (s *Server) authorizeAPIRequest(w http.ResponseWriter, r *http.Request) bool {
	token := extractBearerOrQueryToken(r)
	if token == "" {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authorization token")
		return false
	}
	if _, err := s.authTokens.Validate(token); err != nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
		return false
	}
	return true
}

func extractBearerOrQueryToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return r.URL.Query().Get("token")
}

func parsePagination(w http.ResponseWriter, r *http.Request) (limit int, offset int, ok bool) {
	limit = 100
	offset = 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "limit must be an integer between 1 and 1000")
			return 0, 0, false
		}
		limit = parsed
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "offset must be a non-negative integer")
			return 0, 0, false
		}
		offset = parsed
	}
	return limit, offset, true
}

func flattenSnapshot(snapshot map[string]map[string]plc.TagValue) []tagResponse {
	rows := make([]tagResponse, 0)
	for plcName, tags := range snapshot {
		for tagName, value := range tags {
			rows = append(rows, tagResponse{PLC: plcName, Tag: tagName, Value: value.Value, Quality: value.Quality, Timestamp: value.Timestamp})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PLC == rows[j].PLC {
			return rows[i].Tag < rows[j].Tag
		}
		return rows[i].PLC < rows[j].PLC
	})
	return rows
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorResponse{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = fmt.Fprintf(w, `{"error":{"code":"internal_error","message":%q}}`, err.Error())
	}
}
