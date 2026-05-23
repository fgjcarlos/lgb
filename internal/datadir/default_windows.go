//go:build windows

// default_windows.go — platform default data directory for Windows.
//
// Default: %PROGRAMDATA%\lgb (standard location for per-machine app data).
// Uses os.Getenv("PROGRAMDATA") with fallback to C:\ProgramData\lgb.
// Per MVP-FND-7.2 and design §9.1.
package datadir

import (
	"os"
	"path/filepath"
)

// DefaultPath returns the platform-conventional default data directory.
func DefaultPath() string {
	pd := os.Getenv("PROGRAMDATA")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "lgb")
}
