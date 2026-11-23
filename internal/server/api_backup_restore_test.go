package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/backup"
)

type restoreEnv struct {
	tokens   *auth.TokenService
	baseURL  string
	auditDir string
	logPath  string
}

// newRestoreEnv starts an authenticated, audited server whose fake restic logs
// every invocation to logPath and runs script after logging.
func newRestoreEnv(t *testing.T, script string) *restoreEnv {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	fake := writeFakeRestic(t, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+logPath+"\"\n"+script)
	mgr := backup.NewManager(backup.NewRunner(fake), []backup.Repository{{URL: "repo-a", Password: "pw"}})
	auditDir := t.TempDir()
	auditLog, err := auth.OpenAuditLogger(auditDir)
	if err != nil {
		t.Fatalf("open audit logger: %v", err)
	}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens, AuditLog: auditLog, BkpMgr: mgr})
	t.Cleanup(func() {
		stop()
		_ = auditLog.Close()
	})
	return &restoreEnv{tokens: tokens, baseURL: baseURL, auditDir: auditDir, logPath: logPath}
}

func (e *restoreEnv) admin(t *testing.T) string { return adminToken(t, e.tokens, 1, "alice") }

func (e *restoreEnv) calls(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

type restoreStatusBody struct {
	Status      string  `json:"status"`
	SnapshotID  string  `json:"snapshot_id"`
	Destination string  `json:"destination"`
	FinishedAt  *string `json:"finished_at"`
	Error       string  `json:"error"`
}

// waitRestoreDone polls GET /api/backup/status until the restore leaves "running".
func (e *restoreEnv) waitRestoreDone(t *testing.T) restoreStatusBody {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := doRequest(t, http.MethodGet, e.baseURL+"/api/backup/status", "", e.admin(t))
		var body struct {
			Restore restoreStatusBody `json:"restore"`
		}
		err := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if body.Restore.Status != "running" {
			return body.Restore
		}
		if time.Now().After(deadline) {
			t.Fatal("restore still running after 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func restoreBody(snap, dest string) string {
	b, _ := json.Marshal(map[string]any{"repo": 0, "snapshot_id": snap, "destination": dest})
	return string(b)
}

func TestHandleBackupRestore_RequiresAdmin(t *testing.T) {
	env := newRestoreEnv(t, "")
	body := restoreBody("abcdef12", filepath.Join(t.TempDir(), "r"))
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"viewer", viewerToken(t, env.tokens, 2, "vic"), http.StatusForbidden},
		{"operator", func() string {
			tok, err := env.tokens.Issue(3, "olga", auth.RoleOperator)
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}(), http.StatusForbidden},
	} {
		resp := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore", body, tc.token)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status = %d; want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
	if got := env.calls(t); len(got) != 0 {
		t.Errorf("restic invoked for rejected callers: %q", got)
	}
	if ev := readAuditEventsDCMD(t, env.auditDir); len(ev) != 0 {
		t.Errorf("rejected callers audited: %+v", ev)
	}
}

func TestHandleBackupRestore_AcceptedRunsAuditsAndReportsSuccess(t *testing.T) {
	env := newRestoreEnv(t, "printf '{\"message_type\":\"summary\"}\\n'\n")
	dest := filepath.Join(t.TempDir(), "restore")

	resp := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore", restoreBody("abcdef12", dest), env.admin(t))
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || strings.TrimSpace(string(raw)) != `{"status":"started"}` {
		t.Fatalf("restore = %d %s; want 202 {\"status\":\"started\"}", resp.StatusCode, raw)
	}

	st := env.waitRestoreDone(t)
	if st.Status != "succeeded" || st.SnapshotID != "abcdef12" || st.Destination != dest || st.Error != "" || st.FinishedAt == nil {
		t.Fatalf("restore status = %+v; want succeeded for abcdef12 -> %s", st, dest)
	}
	want := "-r repo-a restore abcdef12 --target " + dest + " --json"
	if got := env.calls(t); len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %q; want [%q]", got, want)
	}

	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) != 1 {
		t.Fatalf("audit events = %+v; want 1", events)
	}
	ev := events[0]
	wantDetail := "repo=0 snapshot=abcdef12 destination=" + dest
	if ev.Action != "backup.restore" || ev.Username != "alice" || ev.Detail != wantDetail || !strings.HasPrefix(ev.IP, "127.0.0.1:") {
		t.Errorf("audit = %+v; want backup.restore by alice with %q", ev, wantDetail)
	}
	if strings.Contains(ev.Detail, "repo-a") || strings.Contains(ev.Detail, "pw") {
		t.Errorf("audit detail leaks repo URL or password: %q", ev.Detail)
	}
}

func TestHandleBackupRestore_FailureSurfacesInStatus(t *testing.T) {
	env := newRestoreEnv(t, "echo 'snapshot not found' >&2\nexit 1\n")
	resp := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore",
		restoreBody("abcdef12", filepath.Join(t.TempDir(), "r")), env.admin(t))
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restore status = %d; want 202", resp.StatusCode)
	}
	st := env.waitRestoreDone(t)
	if st.Status != "failed" || !strings.Contains(st.Error, "snapshot not found") || st.FinishedAt == nil {
		t.Fatalf("restore status = %+v; want failed with restic stderr", st)
	}
	if strings.Contains(st.Error, "repo-a") {
		t.Errorf("status error leaks repo URL: %q", st.Error)
	}
}

func TestHandleBackupRestore_RejectsInvalidInput(t *testing.T) {
	env := newRestoreEnv(t, "")
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := filepath.Join(t.TempDir(), "r")
	for _, tc := range []struct{ name, body, msg string }{
		{"malformed json", "{", "invalid JSON body"},
		{"unknown field", `{"snapshot_id":"abcdef12","destination":"` + ok + `","force":true}`, "invalid JSON body"},
		{"unknown repo", `{"repo":5,"snapshot_id":"abcdef12","destination":"` + ok + `"}`, "unknown backup repository 0-based index 5"},
		{"bad snapshot", restoreBody("latest", ok), "snapshot_id must be 8-64 lowercase hex characters"},
		{"relative destination", restoreBody("abcdef12", "data/restore"), "destination must be an absolute path"},
		{"non-empty destination", restoreBody("abcdef12", full), "restore destination " + full + " must be empty"},
	} {
		resp := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore", tc.body, env.admin(t))
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || body.Error.Code != "bad_request" || body.Error.Message != tc.msg {
			t.Errorf("%s: got %d %+v; want 400 bad_request %q", tc.name, resp.StatusCode, body.Error, tc.msg)
		}
	}
	if got := env.calls(t); len(got) != 0 {
		t.Errorf("restic invoked for invalid input: %q", got)
	}
	if ev := readAuditEventsDCMD(t, env.auditDir); len(ev) != 0 {
		t.Errorf("invalid input audited: %+v", ev)
	}
	if _, err := os.Stat(ok); !os.IsNotExist(err) {
		t.Errorf("rejected request created %s", ok)
	}
}

func TestHandleBackupRestore_ConcurrentRestore409(t *testing.T) {
	env := newRestoreEnv(t, "sleep 1\n")
	first := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore",
		restoreBody("abcdef12", filepath.Join(t.TempDir(), "a")), env.admin(t))
	first.Body.Close()
	second := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore",
		restoreBody("abcdef12", filepath.Join(t.TempDir(), "b")), env.admin(t))
	second.Body.Close()
	if first.StatusCode != http.StatusAccepted || second.StatusCode != http.StatusConflict {
		t.Fatalf("statuses = %d, %d; want 202, 409", first.StatusCode, second.StatusCode)
	}
	env.waitRestoreDone(t)
	if ev := readAuditEventsDCMD(t, env.auditDir); len(ev) != 1 {
		t.Errorf("audit events = %d; want 1 (rejected restore not audited)", len(ev))
	}
}

func TestHandleBackupStatus_RestoreIdleByDefault(t *testing.T) {
	env := newRestoreEnv(t, "")
	resp := doRequest(t, http.MethodGet, env.baseURL+"/api/backup/status", "", env.admin(t))
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), `"restore":{"status":"idle"}`) {
		t.Fatalf("status body = %s; want restore idle", raw)
	}
}

func TestHandleBackupRestore_StatusErrorDoesNotLeakRepoURL(t *testing.T) {
	env := newRestoreEnv(t, "echo \"Fatal: unable to open repository at $2\" >&2\nexit 1\n")
	resp := doRequest(t, http.MethodPost, env.baseURL+"/api/backup/restore",
		restoreBody("abcdef12", filepath.Join(t.TempDir(), "r")), env.admin(t))
	resp.Body.Close()
	st := env.waitRestoreDone(t)
	if st.Status != "failed" || strings.Contains(st.Error, "repo-a") || !strings.Contains(st.Error, "unable to open repository at <repository 0>") {
		t.Fatalf("status = %+v; want failed with redacted repo URL", st)
	}
}
