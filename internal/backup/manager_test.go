package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/backup"
)

func TestManager_BackupAllRunsEveryRepository(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + logPath + `"
printf '{"message_type":"summary","snapshot_id":"abc123"}\n'
`
	fake := writeFakeRestic(t, script)
	mgr := backup.NewManager(backup.NewRunner(fake), []backup.Repository{
		{URL: "local-a", Password: "[REDACTED]"},
		{URL: "local-b", Password: "[REDACTED]"},
	})
	if err := mgr.BackupAll(context.Background(), []string{"/tmp/lgb.db"}); err != nil {
		t.Fatalf("BackupAll returned error: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if got := string(data); got == "" || countLines(got) != 2 {
		t.Fatalf("calls = %q; want 2 restic invocations", got)
	}
}

func TestManager_SnapshotsDelegatesToFirstRepo(t *testing.T) {
	script := `#!/bin/sh
printf '[{"short_id":"snap1","time":"2026-05-26T10:00:00Z","hostname":"box","paths":["/data"]}]\n'
`
	fake := writeFakeRestic(t, script)
	mgr := backup.NewManager(backup.NewRunner(fake), []backup.Repository{
		{URL: "local-a", Password: "pw"},
	})
	snaps, err := mgr.Snapshots(context.Background())
	if err != nil {
		t.Fatalf("Snapshots returned error: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].ID != "snap1" {
		t.Errorf("snap[0].ID = %q; want %q", snaps[0].ID, "snap1")
	}
}

func TestManager_SnapshotsEmptyWhenNoRepos(t *testing.T) {
	t.Parallel()

	mgr := backup.NewManager(backup.NewRunner("restic"), []backup.Repository{})
	snaps, err := mgr.Snapshots(context.Background())
	if err != nil {
		t.Fatalf("Snapshots returned error: %v", err)
	}
	if snaps == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(snaps) != 0 {
		t.Errorf("expected 0 snapshots, got %d", len(snaps))
	}
}

func countLines(s string) int {
	count := 0
	for _, r := range s {
		if r == '\n' {
			count++
		}
	}
	return count
}

func TestManager_BackupAllForgetsPerRepoPolicy(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	mgr := backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, "")), []backup.Repository{
		{URL: "with-policy", Retention: backup.RetentionPolicy{KeepDaily: 7, Prune: true}},
		{URL: "no-policy"},
	})
	if err := mgr.BackupAll(context.Background(), []string{"/data"}); err != nil {
		t.Fatalf("BackupAll: %v", err)
	}
	want := []string{
		"-r with-policy backup /data --json",
		"-r with-policy forget --keep-daily 7 --prune --json",
		"-r no-policy backup /data --json",
	}
	if got := readCalls(t, logPath); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q; want %q", got, want)
	}
}

func TestManager_BackupAllForgetFailureDoesNotStopOtherRepos(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	policy := backup.RetentionPolicy{KeepDaily: 7}
	mgr := backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, "forget")), []backup.Repository{
		{URL: "a", Retention: policy},
		{URL: "b", Retention: policy},
	})
	err := mgr.BackupAll(context.Background(), []string{"/data"})
	if err == nil || !strings.Contains(err.Error(), "forget repo a") || !strings.Contains(err.Error(), "forget repo b") {
		t.Fatalf("err = %v; want forget failures for a and b", err)
	}
	if got := readCalls(t, logPath); len(got) != 4 {
		t.Fatalf("calls = %q; want backup+forget for both repos", got)
	}
}

func TestManager_BackupFailureSkipsForget(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	mgr := backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, "backup")), []backup.Repository{
		{URL: "a", Retention: backup.RetentionPolicy{KeepDaily: 7, Prune: true}},
	})
	if err := mgr.BackupAll(context.Background(), []string{"/data"}); err == nil || !strings.Contains(err.Error(), "backup repo a") {
		t.Fatalf("err = %v; want backup failure", err)
	}
	if got := readCalls(t, logPath); len(got) != 1 {
		t.Fatalf("calls = %q; forget must not run after a failed backup", got)
	}
}

func TestScheduler_OnResultReceivesScheduledRunError(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "calls.log")
	mgr := backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, "forget")), []backup.Repository{
		{URL: "a", Retention: backup.RetentionPolicy{KeepDaily: 7}},
	})
	sched := backup.NewScheduler(mgr, []string{"/data"}, 10*time.Millisecond)
	results := make(chan error, 8)
	sched.OnResult = func(err error) { results <- err }
	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)
	defer func() { cancel(); _ = sched.Stop() }()

	select {
	case err := <-results:
		if err == nil || !strings.Contains(err.Error(), "forget repo a") {
			t.Fatalf("OnResult err = %v; want forget failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnResult never called")
	}
}

func TestManager_BackupAllRetentionErrorOnlyWhenBackupsSucceeded(t *testing.T) {
	policy := backup.RetentionPolicy{KeepDaily: 7}

	logPath := filepath.Join(t.TempDir(), "calls.log")
	mgr := backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, "forget")), []backup.Repository{
		{URL: "a", Retention: policy},
	})
	err := mgr.BackupAll(context.Background(), []string{"/data"})
	var re *backup.RetentionError
	if !errors.As(err, &re) {
		t.Fatalf("forget-only failure: err = %v; want *backup.RetentionError", err)
	}

	logPath = filepath.Join(t.TempDir(), "calls.log")
	mgr = backup.NewManager(backup.NewRunner(writeLoggingRestic(t, logPath, "backup")), []backup.Repository{
		{URL: "a", Retention: policy},
	})
	err = mgr.BackupAll(context.Background(), []string{"/data"})
	if err == nil || errors.As(err, &re) {
		t.Fatalf("backup failure: err = %v; want a non-RetentionError", err)
	}
}

func TestScheduler_CancelledRunIsNotReported(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	// exec replaces sh so the context kill reaches sleep directly.
	script := "#!/bin/sh\ntouch " + started + "\nexec sleep 10\n"
	fake := writeFakeRestic(t, script)
	sched := backup.NewScheduler(backup.NewManager(backup.NewRunner(fake), []backup.Repository{{URL: "a"}}),
		[]string{"/data"}, 10*time.Millisecond)
	var calls atomic.Int32
	sched.OnResult = func(error) { calls.Add(1) }
	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("scheduled run never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	_ = sched.Stop()
	if n := calls.Load(); n != 0 {
		t.Fatalf("OnResult called %d times for a run cancelled at shutdown", n)
	}
}
