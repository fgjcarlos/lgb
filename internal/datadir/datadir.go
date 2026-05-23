// Package datadir provides cross-platform data directory resolution and
// bootstrap for the LGB gateway.
//
// Resolution order (highest priority first, per MVP-FND-7.1):
//  1. --data-dir CLI flag (cliOverride argument)
//  2. gateway.dataDir from *config.Config (already has env overlay applied)
//  3. Platform-conventional default (see default_unix.go, default_darwin.go, default_windows.go)
//
// Requirements: MVP-FND-7.1 through MVP-FND-7.5. Design: §9.1–9.4, §4.4.
package datadir

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fgjcarlos/lgb/internal/config"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// Re-export sentinels for ergonomic local use (design §8, decision #9).
var (
	ErrDataDirInvalid    = errs.ErrDataDirInvalid
	ErrDataDirPermission = errs.ErrDataDirPermission
)

// writeProbeFile is the name of the temporary file used to test writability.
const writeProbeFile = ".lgb-write-probe"

// Resolve returns the absolute path of the data directory, applying the
// priority ordering: cliOverride > cfg.Gateway.DataDir > DefaultPath().
//
// The caller is responsible for calling Ensure on the returned path before use.
// Per design §4.4.
func Resolve(cfg *config.Config, cliOverride string) (string, error) {
	var raw string
	switch {
	case cliOverride != "":
		raw = cliOverride
	case cfg != nil && cfg.Gateway.DataDir != "":
		raw = cfg.Gateway.DataDir
	default:
		raw = DefaultPath()
	}

	abs, err := filepath.Abs(expandPath(raw))
	if err != nil {
		return "", fmt.Errorf("datadir: resolving path %q: %w", raw, err)
	}
	return abs, nil
}

// Ensure guarantees that path is an existing, writable directory.
//
// Steps (per design §9.2):
//  1. Expand ~ and env vars, absolutise
//  2. os.Stat — branch on not-exist, not-dir, dir
//  3. Write probe: create and immediately remove a sentinel file
//
// Returns:
//   - (resolved, nil) on success
//   - ("", ErrDataDirInvalid)    if path exists but is not a directory
//   - ("", ErrDataDirPermission) if creation or write probe fails on perms
//   - ("", wrapped *os.PathError) otherwise
func Ensure(path string) (string, error) {
	expanded := expandPath(path)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("datadir: absolutising %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		// Create with 0700 on POSIX; default ACL on Windows.
		if mkErr := os.MkdirAll(abs, 0700); mkErr != nil {
			if os.IsPermission(mkErr) {
				return "", fmt.Errorf("datadir: creating %q: %w", abs, ErrDataDirPermission)
			}
			return "", fmt.Errorf("datadir: creating %q: %w", abs, mkErr)
		}
	} else if err != nil {
		return "", fmt.Errorf("datadir: stat %q: %w", abs, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("datadir: %q exists but is not a directory: %w", abs, ErrDataDirInvalid)
	}

	// Write probe: confirm we can actually write to the directory.
	probe := filepath.Join(abs, writeProbeFile)
	if writeErr := os.WriteFile(probe, nil, 0600); writeErr != nil {
		if os.IsPermission(writeErr) {
			return "", fmt.Errorf("datadir: %q is not writable: %w", abs, ErrDataDirPermission)
		}
		return "", fmt.Errorf("datadir: write probe in %q: %w", abs, writeErr)
	}
	_ = os.Remove(probe) // best-effort cleanup

	return abs, nil
}

// expandPath expands ~ to the user home directory and ${VAR} env references.
func expandPath(path string) string {
	if len(path) >= 1 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			path = home + path[1:]
		}
	}
	return os.ExpandEnv(path)
}
