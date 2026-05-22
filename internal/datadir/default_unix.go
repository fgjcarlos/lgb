//go:build !darwin && !windows

// default_unix.go — platform default data directory for Linux and other
// non-macOS, non-Windows systems.
//
// Default: /var/lib/lgb (FHS-compliant for daemon data).
// Per MVP-FND-7.2 and design §9.1.
package datadir

// DefaultPath returns the platform-conventional default data directory.
func DefaultPath() string {
	return "/var/lib/lgb"
}
