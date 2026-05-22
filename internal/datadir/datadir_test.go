// Package datadir_test tests cross-platform data directory resolution and bootstrap.
// Requirements: MVP-FND-7.1 through MVP-FND-7.4. Design: §9.1–9.4, §4.4.
package datadir_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/datadir"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// TestDefaultPathLinux asserts MVP-FND-7.2: on Linux, DefaultPath returns /var/lib/lgb.
// Triangulation: on darwin, the path will be different.
func TestDefaultPathLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping linux default path test on %s", runtime.GOOS)
	}
	got := datadir.DefaultPath()
	if got != "/var/lib/lgb" {
		t.Errorf("DefaultPath() on linux = %q; want %q", got, "/var/lib/lgb")
	}
}

// TestDefaultPathDarwin asserts MVP-FND-7.2: on darwin, DefaultPath uses HOME.
func TestDefaultPathDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("skipping darwin default path test on %s", runtime.GOOS)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	got := datadir.DefaultPath()
	want := filepath.Join(home, "Library", "Application Support", "lgb")
	if got != want {
		t.Errorf("DefaultPath() on darwin = %q; want %q", got, want)
	}
}

// TestEnsureCreatesMissingDir asserts MVP-FND-7.3: Ensure creates a missing
// directory with 0700 permissions on POSIX.
func TestEnsureCreatesMissingDir(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "new-data-dir")

	resolved, err := datadir.Ensure(target)
	if err != nil {
		t.Fatalf("Ensure(%q) returned error: %v", target, err)
	}

	info, statErr := os.Stat(resolved)
	if statErr != nil {
		t.Fatalf("os.Stat(%q) failed: %v", resolved, statErr)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", resolved)
	}

	// POSIX permission check — skip on Windows.
	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode != 0700 {
			t.Errorf("directory mode = %o; want 0700", mode)
		}
	}
}

// TestEnsureOnExistingWritableDirReturnsNil asserts MVP-FND-7.4: Ensure on
// an existing writable directory returns nil.
func TestEnsureOnExistingWritableDirReturnsNil(t *testing.T) {
	existing := t.TempDir()

	_, err := datadir.Ensure(existing)
	if err != nil {
		t.Errorf("Ensure(%q) returned error on existing dir: %v", existing, err)
	}
}

// TestEnsureOnRegularFileReturnsErrDataDirInvalid asserts MVP-FND-7.4:
// Ensure on a path that is a file returns ErrDataDirInvalid.
func TestEnsureOnRegularFileReturnsErrDataDirInvalid(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatalf("creating file: %v", err)
	}

	_, err := datadir.Ensure(filePath)
	if err == nil {
		t.Fatal("Ensure on regular file returned nil; want error")
	}
	if !errors.Is(err, errs.ErrDataDirInvalid) {
		t.Errorf("errors.Is(err, ErrDataDirInvalid) = false; got %v", err)
	}
}

// TestResolveWithCLIOverrideWins asserts MVP-FND-7.1: cliOverride takes
// priority over the cfg.Gateway.DataDir value.
func TestResolveWithCLIOverrideWins(t *testing.T) {
	cfg := &config.Config{
		Gateway: config.GatewaySection{
			DataDir: "/var/lib/lgb-config",
		},
	}

	override := t.TempDir()
	got, err := datadir.Resolve(cfg, override)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	// The override should win; Resolve returns the absolute path.
	wantAbs, _ := filepath.Abs(override)
	if got != wantAbs {
		t.Errorf("Resolve with cliOverride = %q; want %q", got, wantAbs)
	}
}

// TestResolveWithEmptyOverrideUsesCfg asserts MVP-FND-7.1: when cliOverride
// is empty, cfg.Gateway.DataDir is used.
func TestResolveWithEmptyOverrideUsesCfg(t *testing.T) {
	cfgDir := t.TempDir()
	cfg := &config.Config{
		Gateway: config.GatewaySection{
			DataDir: cfgDir,
		},
	}

	got, err := datadir.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantAbs, _ := filepath.Abs(cfgDir)
	if got != wantAbs {
		t.Errorf("Resolve with empty override = %q; want %q", got, wantAbs)
	}
}

// TestResolveWithBothEmptyUsesDefault asserts MVP-FND-7.1: when both
// cliOverride and cfg.Gateway.DataDir are empty, platform default is used.
func TestResolveWithBothEmptyUsesDefault(t *testing.T) {
	cfg := &config.Config{}

	got, err := datadir.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got == "" {
		t.Error("Resolve returned empty string; want platform default")
	}
	// Should be an absolute path.
	if !filepath.IsAbs(got) {
		t.Errorf("Resolve returned relative path %q; want absolute", got)
	}
}
