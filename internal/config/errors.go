package config

import "fmt"

// errorf is a package-internal helper that formats an error message.
// It is equivalent to fmt.Errorf but lives here to avoid importing fmt in config.go.
func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
