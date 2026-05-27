package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fgjcarlos/lgb/internal/doctor"
)

// mockCheck is a simple doctor.Check implementation for tests.
type mockCheck struct {
	name   string
	result doctor.Result
}

func (m *mockCheck) Name() string { return m.name }
func (m *mockCheck) Run(_ context.Context) doctor.Result {
	return m.result
}

// newDoctorTestServer builds a *Server with the given checks.
func newDoctorTestServer(t *testing.T, checks []doctor.Check) (string, func()) {
	t.Helper()
	_, baseURL, stopSrv := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{Checks: checks})
	return baseURL, stopSrv
}

// ─── GET /api/doctor ─────────────────────────────────────────────────────────

func TestHandleDoctor_AllPass200(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "db", result: doctor.Result{Name: "db", Status: doctor.StatusPass, Message: "ok"}},
		&mockCheck{name: "disk", result: doctor.Result{Name: "disk", Status: doctor.StatusPass, Message: "ok"}},
	}
	baseURL, stop := newDoctorTestServer(t, checks)
	defer stop()

	resp, err := http.Get(baseURL + "/api/doctor")
	if err != nil {
		t.Fatalf("GET /api/doctor: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
		Overall string `json:"overall"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Overall != "pass" {
		t.Errorf("expected overall=pass, got %q", body.Overall)
	}
	if len(body.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(body.Checks))
	}
}

func TestHandleDoctor_OneWarn_OverallWarn(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "db", result: doctor.Result{Name: "db", Status: doctor.StatusPass, Message: "ok"}},
		&mockCheck{name: "disk", result: doctor.Result{Name: "disk", Status: doctor.StatusWarn, Message: "low space"}},
	}
	baseURL, stop := newDoctorTestServer(t, checks)
	defer stop()

	resp, err := http.Get(baseURL + "/api/doctor")
	if err != nil {
		t.Fatalf("GET /api/doctor: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Overall string `json:"overall"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Overall != "warn" {
		t.Errorf("expected overall=warn, got %q", body.Overall)
	}
}

func TestHandleDoctor_OneFail_OverallFail(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "db", result: doctor.Result{Name: "db", Status: doctor.StatusPass, Message: "ok"}},
		&mockCheck{name: "conn", result: doctor.Result{Name: "conn", Status: doctor.StatusFail, Message: "unreachable"}},
	}
	baseURL, stop := newDoctorTestServer(t, checks)
	defer stop()

	resp, err := http.Get(baseURL + "/api/doctor")
	if err != nil {
		t.Fatalf("GET /api/doctor: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Overall string `json:"overall"`
		Checks  []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Overall != "fail" {
		t.Errorf("expected overall=fail, got %q", body.Overall)
	}
	// Verify JSON shape includes name, status, message fields.
	if len(body.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(body.Checks))
	}
	for _, c := range body.Checks {
		if c.Name == "" {
			t.Error("check missing name field")
		}
		if c.Status == "" {
			t.Error("check missing status field")
		}
	}
}
