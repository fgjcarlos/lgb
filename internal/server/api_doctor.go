package server

import (
	"net/http"

	"github.com/fgjcarlos/lgb/internal/doctor"
)

// checkResult is the API representation of a single doctor check outcome.
type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// handleDoctor serves GET /api/doctor.
// Runs all registered checks, aggregates the overall status, and returns the
// full result set as JSON.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	results := make([]checkResult, 0, len(s.checks))
	overall := doctor.StatusPass

	for _, c := range s.checks {
		res := c.Run(ctx)
		results = append(results, checkResult{
			Name:    res.Name,
			Status:  res.Status.String(),
			Message: res.Message,
		})
		if res.Status > overall {
			overall = res.Status
		}
	}

	// Clamp overall to the meaningful output values: pass, warn, fail.
	// StatusInfo (0) < StatusPass (1) — if somehow a check returns info, treat as pass.
	overallStr := "pass"
	switch {
	case overall >= doctor.StatusFail:
		overallStr = "fail"
	case overall >= doctor.StatusWarn:
		overallStr = "warn"
	}

	writeJSON(w, http.StatusOK, struct {
		Checks  []checkResult `json:"checks"`
		Overall string        `json:"overall"`
	}{
		Checks:  results,
		Overall: overallStr,
	})
}
