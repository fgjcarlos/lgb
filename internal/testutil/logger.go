// Package testutil provides helpers for tests across the LGB codebase.
// It MUST only be imported from _test.go files or test packages.
//
// Requirements: MVP-FND-2.6 (test ergonomics). Design: §3 (testutil package).
package testutil

import (
	"log/slog"
	"os"
	"testing"
)

// NewLogger returns a *slog.Logger suitable for tests. It writes to os.Stderr
// with text format at DEBUG level so test log output is visible on failure.
func NewLogger(t *testing.T) *slog.Logger {
	t.Helper()
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h)
}
