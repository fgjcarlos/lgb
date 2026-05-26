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

func countLines(s string) int {
	count := 0
	for _, r := range s {
		if r == '\n' {
			count++
		}
	}
	return count
}
