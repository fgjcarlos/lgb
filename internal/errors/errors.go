// Package errors defines project-wide sentinel errors and multi-error helpers
// for the LGB gateway. All domain packages import this package for their
// sentinel values, ensuring a single canonical registry.
//
// Usage:
//
//	if errors.Is(err, errs.ErrConfigInvalid) { ... }
//
// Sentinels follow the pattern Err{Domain}{Condition} per MVP-FND-5.1.
// Multi-error aggregation uses Join (thin wrapper over stdlib errors.Join)
// per MVP-FND-5.3.
package errors

import "errors"

// Config-domain sentinels (MVP-FND-5.1).
var (
	// ErrConfigInvalid is returned when a config value fails schema or constraint validation.
	ErrConfigInvalid = errors.New("config invalid")

	// ErrConfigMissing is returned when the config file is not found.
	ErrConfigMissing = errors.New("config missing")

	// ErrConfigPermission is returned when the config file is not readable.
	ErrConfigPermission = errors.New("config permission denied")
)

// DataDir-domain sentinels (MVP-FND-5.1).
var (
	// ErrDataDirInvalid is returned when a path exists but is not a directory.
	ErrDataDirInvalid = errors.New("data dir invalid")

	// ErrDataDirPermission is returned when the data directory is not writable.
	ErrDataDirPermission = errors.New("data dir permission denied")
)

// Doctor-domain sentinels (MVP-FND-5.1).
var (
	// ErrCheckFailed is returned when a doctor check produces a FAIL result.
	ErrCheckFailed = errors.New("check failed")
)

// PLC-domain sentinels (PLC-DRV-1.*).
var (
	// ErrPLCConnect is returned when a TCP/CIP connection to the PLC cannot be established.
	ErrPLCConnect = errors.New("plc connect failed")

	// ErrPLCRead is returned when a tag read operation fails.
	ErrPLCRead = errors.New("plc read failed")

	// ErrPLCWrite is returned when a tag write operation fails.
	ErrPLCWrite = errors.New("plc write failed")

	// ErrPLCTimeout is returned when a PLC operation exceeds its configured deadline.
	ErrPLCTimeout = errors.New("plc operation timeout")
)

// Retry-domain sentinels (MVP-FND-5.1).
var (
	// ErrMaxAttempts is returned when retry.Do exhausts MaxAttempts.
	ErrMaxAttempts = errors.New("max attempts reached")
)

// Join combines multiple errors into a single error using stdlib errors.Join
// (Go 1.20+). The resulting error satisfies errors.Is for every non-nil
// constituent. Returns nil when all inputs are nil (MVP-FND-5.3).
func Join(errs ...error) error {
	return errors.Join(errs...)
}
