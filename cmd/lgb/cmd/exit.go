// Package cmd contains the Cobra command tree for the LGB gateway binary.
//
// Exit code mapping is defined in exit.go and used by all subcommands.
// Requirements: MVP-FND-5.5. Design: §6.4.
package cmd

import (
	"errors"

	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// ExitCode maps a returned error to a sysexits.h-aligned exit code.
// Exported for use by cmd/lgb/main.go.
//
// Design §6.4 exit code table:
//
//	0  — success
//	1  — user/validation error (ErrConfigInvalid, ErrDataDirInvalid, ErrCheckFailed, generic)
//	2  — internal error (unexpected, should not normally occur)
//	64 — usage error (bad flags; Cobra surfaces these itself)
//	77 — permission denied (ErrConfigPermission, ErrDataDirPermission)
//
// Subcommands MUST return an error and let the root dispatcher call this
// function. Direct os.Exit(N) calls inside subcommands are FORBIDDEN.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, errs.ErrConfigPermission),
		errors.Is(err, errs.ErrDataDirPermission):
		return 77
	case errors.Is(err, errs.ErrConfigInvalid),
		errors.Is(err, errs.ErrConfigMissing),
		errors.Is(err, errs.ErrDataDirInvalid),
		errors.Is(err, errs.ErrCheckFailed):
		return 1
	default:
		return 1
	}
}
