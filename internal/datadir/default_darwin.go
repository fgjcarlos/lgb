//go:build darwin

// default_darwin.go — platform default data directory for macOS.
//
// Default: ${HOME}/Library/Application Support/lgb
// Uses os.UserHomeDir() to avoid hardcoding HOME. Per MVP-FND-7.2 and design §9.1.
package datadir

import (
	"os"
	"path/filepath"
)

// DefaultPath returns the platform-conventional default data directory.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback if UserHomeDir fails (e.g. no $HOME set in CI).
		return filepath.Join("/tmp", "lgb")
	}
	return filepath.Join(home, "Library", "Application Support", "lgb")
}
