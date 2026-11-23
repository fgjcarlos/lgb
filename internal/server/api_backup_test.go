package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// ─── Retention (issue #83) ───────────────────────────────────────────────────

type backupStatusBody struct {
	Status  string  `json:"status"`
	LastRun *string `json:"last_run"`
	LastErr string  `json:"last_error"`
}

func getBackupStatus(t *testing.T, baseURL string) backupStatusBody {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/backup/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()
	var body backupStatusBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// waitBackupStatus polls until status is no longer "running".
func waitBackupStatus(t *testing.T, baseURL string) backupStatusBody {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if body := getBackupStatus(t, baseURL); body.Status != "running" {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("backup still running after 5s")
	return backupStatusBody{}
}

func TestHandleBackupTrigger_AppliesRetentionAndSurfacesForgetFailure(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	fake := writeFakeRestic(t, `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$3" = "forget" ]; then echo "repo locked" >&2; exit 1; fi
printf '{"message_type":"summary"}\n'
`)
	mgr := backup.NewManager(backup.NewRunner(fake), []backup.Repository{
		{URL: "repo", Password: "pw", Retention: backup.RetentionPolicy{KeepDaily: 7, Prune: true}},
	})
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{BkpMgr: mgr})
	defer stop()

	resp, err := http.Post(baseURL+"/api/backup/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger: %v", err)
	}
	resp.Body.Close()

	body := waitBackupStatus(t, baseURL)
	if body.Status != "failed" || !strings.Contains(body.LastErr, "forget repo repo") || !strings.Contains(body.LastErr, "repo locked") {
		t.Fatalf("status = %+v; want failed with forget error", body)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(data), "-r repo forget --keep-daily 7 --prune --json") {
		t.Fatalf("forget not invoked with policy args; calls:\n%s", data)
	}
}

func TestRecordScheduledBackup_UpdatesStatus(t *testing.T) {
	fake := writeFakeRestic(t, "#!/bin/sh\nprintf '{}\n'")
	mgr := backup.NewManager(backup.NewRunner(fake), nil)
	srv, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{BkpMgr: mgr})
	defer stop()

	srv.RecordScheduledBackup(errors.New("forget repo r: boom"))
	if body := getBackupStatus(t, baseURL); body.Status != "failed" || body.LastErr != "forget repo r: boom" || body.LastRun != nil {
		t.Fatalf("after failure status = %+v", body)
	}
	srv.RecordScheduledBackup(nil)
	if body := getBackupStatus(t, baseURL); body.Status != "idle" || body.LastErr != "" || body.LastRun == nil {
		t.Fatalf("after success status = %+v", body)
	}
}

func TestRecordScheduledBackup_KeepsManualRunning(t *testing.T) {
	fake := writeFakeRestic(t, "#!/bin/sh\nsleep 2\nprintf '{}\n'")
	mgr := backup.NewManager(backup.NewRunner(fake), []backup.Repository{{URL: "repo", Password: "pw"}})
	srv, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{BkpMgr: mgr})
	defer stop()

	resp, err := http.Post(baseURL+"/api/backup/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger: %v", err)
	}
	resp.Body.Close()
	srv.RecordScheduledBackup(nil)
	if body := getBackupStatus(t, baseURL); body.Status != "running" {
		t.Fatalf("scheduled result released the manual run: status = %q", body.Status)
	}
}
