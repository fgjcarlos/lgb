package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/fgjcarlos/lgb/internal/historian"
)

// sampleResponse is the API representation of a historian.Sample.
type sampleResponse struct {
	PLCName   string    `json:"plc_name"`
	Tag       string    `json:"tag"`
	Timestamp time.Time `json:"timestamp"`
	Value     any       `json:"value"`
	Quality   string    `json:"quality"`
}

// handleHistorianQuery serves GET /api/historian/query.
// Required query params: tag
// Optional query params: plc, from (RFC3339), to (RFC3339), limit (1–1000, default 100)
func (s *Server) handleHistorianQuery(w http.ResponseWriter, r *http.Request) {
	if s.histStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "historian not configured")
		return
	}

	q := r.URL.Query()

	tag := q.Get("tag")
	if tag == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "tag parameter is required")
		return
	}

	plcName := q.Get("plc")

	var from, to time.Time
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "from must be an RFC3339 timestamp")
			return
		}
		from = t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "to must be an RFC3339 timestamp")
			return
		}
		to = t
	}

	limit := 100
	if raw := q.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "limit must be an integer between 1 and 1000")
			return
		}
		limit = parsed
	}

	samples, err := s.histStore.Query(r.Context(), historian.Query{
		PLCName: plcName,
		Tag:     tag,
		From:    from,
		To:      to,
		Limit:   limit,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "historian query failed")
		return
	}

	// Always return an array, never null.
	data := make([]sampleResponse, 0, len(samples))
	for _, s := range samples {
		data = append(data, sampleResponse{
			PLCName:   s.PLCName,
			Tag:       s.Tag,
			Timestamp: s.Timestamp,
			Value:     s.Value,
			Quality:   s.Quality,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		Data       []sampleResponse   `json:"data"`
		Pagination paginationResponse `json:"pagination"`
	}{
		Data:       data,
		Pagination: paginationResponse{Limit: limit, Offset: 0, Count: len(data)},
	})
}

// retentionResponse is the effective historian retention (#85): the global
// default plus, per configured PLC and tag, the days applied and the level
// ("tag", "plc" or "global") that set them.
type retentionResponse struct {
	DefaultDays int            `json:"default_days"`
	PLCs        []plcRetention `json:"plcs"`
}

type plcRetention struct {
	Name          string         `json:"name"`
	RetentionDays int            `json:"retention_days"`
	Source        string         `json:"source"`
	Tags          []tagRetention `json:"tags"`
}

type tagRetention struct {
	Name          string `json:"name"`
	RetentionDays int    `json:"retention_days"`
	Source        string `json:"source"`
}

// handleHistorianRetention serves GET /api/historian/retention (#85). PLCs come
// from the PLC store (the startup config without one), as the retention loop
// reads them.
func (s *Server) handleHistorianRetention(w http.ResponseWriter, r *http.Request) {
	if s.histStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "historian not configured")
		return
	}
	plcs := s.cfg.PLCs
	if s.plcStore != nil {
		list, err := s.plcStore.List(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to load PLCs")
			return
		}
		plcs = list
	}

	policy := historian.NewRetentionPolicy(s.cfg.Historian.RetentionDays, plcs)
	resp := retentionResponse{DefaultDays: policy.DefaultDays, PLCs: make([]plcRetention, 0, len(plcs))}
	for _, p := range plcs {
		pr := plcRetention{Name: p.Name, RetentionDays: policy.DefaultDays, Source: historian.RetentionSourceGlobal, Tags: make([]tagRetention, 0, len(p.Tags))}
		if days, ok := policy.PLCDays[p.Name]; ok {
			pr.RetentionDays, pr.Source = days, historian.RetentionSourcePLC
		}
		for _, tag := range p.Tags {
			days, source := policy.Effective(p.Name, tag.Name)
			pr.Tags = append(pr.Tags, tagRetention{Name: tag.Name, RetentionDays: days, Source: source})
		}
		resp.PLCs = append(resp.PLCs, pr)
	}
	writeJSON(w, http.StatusOK, struct {
		Data retentionResponse `json:"data"`
	}{Data: resp})
}
