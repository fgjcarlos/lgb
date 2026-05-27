package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fgjcarlos/lgb/internal/backup"
)

func TestManager_BackupAllRunsEveryRepository(t *testing.T) {
	t.Parallel()

	fake := filepath.Join(t.TempDir(), "restic")
	logPath := filepath.Join(t.TempDir(), "calls.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + logPath + `"
printf '{"message_type":"summary","snapshot_id":"abc123"}\n'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
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
	t.Parallel()

	fake := filepath.Join(t.TempDir(), "restic")
	script := `#!/bin/sh
printf '[{"short_id":"snap1","time":"2026-05-26T10:00:00Z","hostname":"box","paths":["/data"]}]\n'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}
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
