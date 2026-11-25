package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/backup"
)

func newRestoreManager(t *testing.T, failCmd string) (*backup.Manager, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	mgr := backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, failCmd)), []backup.Repository{
		{URL: "repo-a", Password: "pw"},
		{URL: "repo-b", Password: "pw"},
	})
	return mgr, logPath
}

func TestManager_RestoreInvokesRunnerWithRepoSnapshotAndTarget(t *testing.T) {
	mgr, logPath := newRestoreManager(t, "")
	dest := filepath.Join(t.TempDir(), "restore")

	if err := mgr.Restore(context.Background(), 1, "abcdef12", dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	want := "-r repo-b restore abcdef12 --target " + dest + " --json"
	if got := readCalls(t, logPath); len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %q; want [%q]", got, want)
	}
}

func TestManager_RestoreSurfacesResticFailure(t *testing.T) {
	mgr, _ := newRestoreManager(t, "restore")
	err := mgr.Restore(context.Background(), 0, "abcdef12", filepath.Join(t.TempDir(), "restore"))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Restore err = %v; want restic stderr surfaced", err)
	}
}

func TestManager_CheckRestoreRejectsInvalidRequests(t *testing.T) {
	base := t.TempDir()
	nonEmpty := filepath.Join(base, "full")
	if err := os.MkdirAll(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := filepath.Join(base, "new")

	cases := []struct {
		name, snap, dest, wantErr string
		repo                      int
	}{
		{"negative repo", "abcdef12", ok, "unknown backup repository 0-based index -1", -1},
		{"repo out of range", "abcdef12", ok, "unknown backup repository 0-based index 2", 2},
		{"snapshot not hex", "latest", ok, "snapshot_id must be 8-64 lowercase hex characters", 0},
		{"snapshot uppercase", "ABCDEF12", ok, "snapshot_id must be 8-64 lowercase hex characters", 0},
		{"snapshot too short", "abc", ok, "snapshot_id must be 8-64 lowercase hex characters", 0},
		{"relative destination", "abcdef12", "restore", "destination must be an absolute path", 0},
		{"empty destination", "abcdef12", "", "destination must be an absolute path", 0},
		{"non-empty destination", "abcdef12", nonEmpty, "restore destination " + nonEmpty + " must be empty", 0},
		{"destination is a file", "abcdef12", file, "restore destination " + file + " is not a directory", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, logPath := newRestoreManager(t, "")
			err := mgr.CheckRestore(tc.repo, tc.snap, tc.dest)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("CheckRestore err = %v; want %q", err, tc.wantErr)
			}
			if err := mgr.Restore(context.Background(), tc.repo, tc.snap, tc.dest); err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Restore err = %v; want %q", err, tc.wantErr)
			}
			if got := readCalls(t, logPath); len(got) != 0 {
				t.Fatalf("restic was invoked: %q", got)
			}
			if _, err := os.Stat(ok); !os.IsNotExist(err) {
				t.Fatalf("validation created %s", ok)
			}
		})
	}
}

func TestManager_CheckRestoreAcceptsMissingOrEmptyDestination(t *testing.T) {
	mgr, _ := newRestoreManager(t, "")
	empty := t.TempDir()
	for _, dest := range []string{empty, filepath.Join(empty, "missing")} {
		if err := mgr.CheckRestore(0, "0123456789abcdef", dest); err != nil {
			t.Errorf("CheckRestore(%q) = %v; want nil", dest, err)
		}
	}
}
