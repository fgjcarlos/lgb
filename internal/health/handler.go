// Package health provides the HTTP health-check handler for the LGB gateway.
//
// The handler returns 200 OK with body {"status":"ok"} and Content-Type:
// application/json. It is mounted at /health by internal/server.
//
// Requirements: MVP-FND-1.3. Design: §11.
package health

import (
	"encoding/json"
	"net/http"
)

type healthResponse struct {
	Status string `json:"status"`
}

// Handler returns an http.Handler that always responds 200 {"status":"ok"}.
func Handler() http.Handler {
	resp, _ := json.Marshal(healthResponse{Status: "ok"})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	})
}
