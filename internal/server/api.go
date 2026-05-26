package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
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

func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tags/current", s.handleCurrentTags)
	mux.HandleFunc("GET /api/ws/tags", s.handleTagsWebSocket)
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
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	sub := &tagSubscriber{
		plc: r.URL.Query().Get("plc"),
		tag: r.URL.Query().Get("tag"),
		ch:  make(chan plc.TagUpdate, 16),
	}
	s.tagHub.register(sub)
	defer s.tagHub.unregister(sub)

	if err := wsjson.Write(r.Context(), conn, struct {
		Type string `json:"type"`
	}{Type: "subscribed"}); err != nil {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case update := <-sub.ch:
			msg := struct {
				Type      string    `json:"type"`
				PLC       string    `json:"plc"`
				Tag       string    `json:"tag"`
				Value     any       `json:"value"`
				Timestamp time.Time `json:"timestamp"`
			}{Type: "tag_update", PLC: update.PLCName, Tag: update.Tag, Value: update.Value, Timestamp: update.Timestamp}
			if err := wsjson.Write(r.Context(), conn, msg); err != nil {
				return
			}
		}
	}
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
