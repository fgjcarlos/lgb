package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/backup"
)

// newBackupTestServer builds a Server wired with a real *backup.Manager backed
// by a configurable fake restic binary.  Returns (baseURL, stop).
func newBackupTestServer(t *testing.T, fakeRestic string) (string, func()) {
	t.Helper()
	runner := backup.NewRunner(fakeRestic)
	mgr := backup.NewManager(runner, []backup.Repository{
		{URL: filepath.Join(t.TempDir(), "repo"), Password: "pw"},
	})
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{BkpMgr: mgr})
	return baseURL, stop
}

// writeFakeRestic writes an executable shell script at path and returns it.
func writeFakeRestic(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "restic")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
	return path
}

// ─── POST /api/backup/trigger ────────────────────────────────────────────────

func TestHandleBackupTrigger_FirstTrigger202(t *testing.T) {
	fake := writeFakeRestic(t, "#!/bin/sh\nprintf '{\"message_type\":\"summary\",\"snapshot_id\":\"abc\"}\n'")
	baseURL, stop := newBackupTestServer(t, fake)
	defer stop()

	resp, err := http.Post(baseURL+"/api/backup/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "started" {
		t.Errorf("expected status=%q, got %q", "started", body.Status)
	}
}

func TestHandleBackupTrigger_ConcurrentTrigger409(t *testing.T) {
	// This fake restic sleeps briefly so the goroutine stays "running" long
	// enough for the second request to arrive while status is still "running".
	fake := writeFakeRestic(t, "#!/bin/sh\nsleep 2\nprintf '{\"message_type\":\"summary\",\"snapshot_id\":\"abc\"}\n'")
	baseURL, stop := newBackupTestServer(t, fake)
	defer stop()

	// First trigger — should be accepted and set status to running.
	resp1, err := http.Post(baseURL+"/api/backup/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger 1: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first trigger: expected 202, got %d", resp1.StatusCode)
	}

	// Give the goroutine a moment to set status = running.
	time.Sleep(50 * time.Millisecond)

	// Second trigger — backup is still running, must get 409.
	resp2, err := http.Post(baseURL+"/api/backup/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger 2: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second trigger: expected 409, got %d", resp2.StatusCode)
	}
	assertHTTPErrorCode(t, resp2, "conflict")
}

// ─── GET /api/backup/status ──────────────────────────────────────────────────

func TestHandleBackupStatus_Idle200(t *testing.T) {
	fake := writeFakeRestic(t, "#!/bin/sh\nprintf '{}\n'")
	baseURL, stop := newBackupTestServer(t, fake)
	defer stop()

	resp, err := http.Get(baseURL + "/api/backup/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Status  string  `json:"status"`
		LastRun *string `json:"last_run"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "idle" {
		t.Errorf("expected status=idle, got %q", body.Status)
	}
	if body.LastRun != nil {
		t.Errorf("expected last_run=null before first backup, got %v", *body.LastRun)
	}
}

func TestHandleBackupStatus_RunningAfterTrigger(t *testing.T) {
	// Slow fake so the backup goroutine keeps running during status check.
	fake := writeFakeRestic(t, "#!/bin/sh\nsleep 2\nprintf '{\"message_type\":\"summary\"}\n'")
	baseURL, stop := newBackupTestServer(t, fake)
	defer stop()

	// Trigger backup.
	resp1, err := http.Post(baseURL+"/api/backup/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger: %v", err)
	}
	resp1.Body.Close()

	// Give goroutine time to set status = running.
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(baseURL + "/api/backup/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "running" {
		t.Errorf("expected status=running, got %q", body.Status)
	}
}

// ─── GET /api/backup/snapshots ───────────────────────────────────────────────

func TestHandleBackupSnapshots_TwoSnapshots200(t *testing.T) {
	fake := writeFakeRestic(t, `#!/bin/sh
printf '[{"short_id":"abc","time":"2026-05-26T10:00:00Z","hostname":"h1","paths":["/data"]},{"short_id":"def","time":"2026-05-27T10:00:00Z","hostname":"h2","paths":["/etc"]}]\n'
`)
	baseURL, stop := newBackupTestServer(t, fake)
	defer stop()

	resp, err := http.Get(baseURL + "/api/backup/snapshots")
	if err != nil {
		t.Fatalf("GET snapshots: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(body.Data))
	}
	if body.Data[0].ID != "abc" {
		t.Errorf("snap[0].id = %q; want %q", body.Data[0].ID, "abc")
	}
	if body.Data[1].ID != "def" {
		t.Errorf("snap[1].id = %q; want %q", body.Data[1].ID, "def")
	}
}

func TestHandleBackupSnapshots_EmptyArray200(t *testing.T) {
	fake := writeFakeRestic(t, "#!/bin/sh\nprintf '[]\n'")
	baseURL, stop := newBackupTestServer(t, fake)
	defer stop()

	resp, err := http.Get(baseURL + "/api/backup/snapshots")
	if err != nil {
		t.Fatalf("GET snapshots: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data == nil {
		t.Error("expected data=[] not null")
	}
	if len(body.Data) != 0 {
		t.Errorf("expected 0 snapshots, got %d", len(body.Data))
	}
}

// compile-time guard
var _ = context.Background
