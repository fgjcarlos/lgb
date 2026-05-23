// handler_test.go — tests for the health HTTP handler.
//
// Requirements: MVP-FND-1.3, MVP-FND-1.8. Design: §11.
package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandler_Returns200WithJSONOK verifies GET /health → 200, body {"status":"ok"},
// Content-Type: application/json. (MVP-FND-1.3)
func TestHandler_Returns200WithJSONOK(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v — got: %q", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=%q in body, got %q", "ok", body["status"])
	}
}
