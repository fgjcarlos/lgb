package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/backup"
)

func TestRunner_InitBackupCheckUseJSONAndPasswordEnv(t *testing.T) {
	t.Parallel()

	fake := filepath.Join(t.TempDir(), "restic")
	logPath := filepath.Join(t.TempDir(), "calls.log")
	script := `#!/bin/sh
printf '%s|%s\n' "$*" "$RESTIC_PASSWORD" >> "` + logPath + `"
case "$1" in
  snapshots) printf '[{"short_id":"abc123","time":"2026-05-26T10:00:00Z"}]\n' ;;
  *) printf '{"message_type":"summary","snapshot_id":"abc123"}\n' ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake restic: %v", err)
	}

	runner := backup.NewRunner(fake)
	repo := backup.Repository{URL: filepath.Join(t.TempDir(), "repo"), Password: "[REDACTED]"}
	if err := runner.Init(context.Background(), repo); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if _, err := runner.Backup(context.Background(), repo, []string{"/tmp/lgb.db"}); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}
	if _, err := runner.Check(context.Background(), repo); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("recorded %d calls; want 3: %q", len(lines), data)
	}
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			t.Fatalf("bad log line: %q", line)
		}
		if !strings.HasSuffix(parts[0], "--json") {
			t.Fatalf("args missing trailing --json: %q", parts[0])
		}
		if parts[1] != "[REDACTED]" {
			t.Fatalf("RESTIC_PASSWORD not forwarded: %q", parts[1])
		}
	}
}

func TestRunner_Snapshots(t *testing.T) {
	t.Parallel()

	t.Run("two snapshots JSON returns two Snapshot values", func(t *testing.T) {
		t.Parallel()

		fake := filepath.Join(t.TempDir(), "restic")
		script := `#!/bin/sh
printf '[{"short_id":"abc123","time":"2026-05-26T10:00:00Z","hostname":"host1","paths":["/data"]},{"short_id":"def456","time":"2026-05-27T12:00:00Z","hostname":"host2","paths":["/data","/etc"]}]\n'
`
		if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake restic: %v", err)
		}

		runner := backup.NewRunner(fake)
		repo := backup.Repository{URL: filepath.Join(t.TempDir(), "repo"), Password: "secret"}
		snaps, err := runner.Snapshots(context.Background(), repo)
		if err != nil {
			t.Fatalf("Snapshots returned error: %v", err)
		}
		if len(snaps) != 2 {
			t.Fatalf("expected 2 snapshots, got %d", len(snaps))
		}
		if snaps[0].ID != "abc123" {
			t.Errorf("snap[0].ID = %q; want %q", snaps[0].ID, "abc123")
		}
		if snaps[0].Hostname != "host1" {
			t.Errorf("snap[0].Hostname = %q; want %q", snaps[0].Hostname, "host1")
		}
		if len(snaps[0].Paths) != 1 || snaps[0].Paths[0] != "/data" {
			t.Errorf("snap[0].Paths = %v; want [\"/data\"]", snaps[0].Paths)
		}
		if snaps[1].ID != "def456" {
			t.Errorf("snap[1].ID = %q; want %q", snaps[1].ID, "def456")
		}
		if len(snaps[1].Paths) != 2 {
			t.Errorf("snap[1].Paths has %d entries; want 2", len(snaps[1].Paths))
		}
	})

	t.Run("empty JSON array returns empty slice not nil", func(t *testing.T) {
		t.Parallel()

		fake := filepath.Join(t.TempDir(), "restic")
		script := "#!/bin/sh\nprintf '[]\n'\n"
		if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake restic: %v", err)
		}

		runner := backup.NewRunner(fake)
		repo := backup.Repository{URL: filepath.Join(t.TempDir(), "repo"), Password: "secret"}
		snaps, err := runner.Snapshots(context.Background(), repo)
		if err != nil {
			t.Fatalf("Snapshots returned error: %v", err)
		}
		if snaps == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(snaps) != 0 {
			t.Errorf("expected 0 snapshots, got %d", len(snaps))
		}
	})
}

func TestRunner_RestoreRequiresCleanDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "restore")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("mkdir restore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "existing"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	runner := backup.NewRunner("restic")
	_, err := runner.Restore(context.Background(), backup.Repository{URL: dir, Password: "[REDACTED]"}, "latest", dest)
	if err == nil {
		t.Fatal("Restore returned nil error for non-empty destination")
	}
}
