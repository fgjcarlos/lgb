// Package version_test tests the build-metadata package.
// Requirement: MVP-FND-1.7, design §3, decision #25.
package version_test

import (
	"testing"

	"github.com/fgjcarlos/lgb/internal/version"
)

// TestDefaultFallbacks asserts MVP-FND-1.7: when no ldflags are injected
// (the common case in tests), the package variables fall back to safe defaults.
func TestDefaultFallbacks(t *testing.T) {
	t.Run("Version defaults to dev", func(t *testing.T) {
		if version.Version != "dev" {
			t.Errorf("Version = %q; want %q", version.Version, "dev")
		}
	})

	t.Run("Commit defaults to none", func(t *testing.T) {
		if version.Commit != "none" {
			t.Errorf("Commit = %q; want %q", version.Commit, "none")
		}
	})

	t.Run("Date defaults to unknown", func(t *testing.T) {
		if version.Date != "unknown" {
			t.Errorf("Date = %q; want %q", version.Date, "unknown")
		}
	})
}

// TestInfoReturnsPopulatedStruct asserts that Info() returns a struct
// whose fields match the package-level variables.
func TestInfoReturnsPopulatedStruct(t *testing.T) {
	info := version.Info()

	if info.Version == "" {
		t.Error("Info().Version is empty; want non-empty string")
	}
	if info.Commit == "" {
		t.Error("Info().Commit is empty; want non-empty string")
	}
	if info.Date == "" {
		t.Error("Info().Date is empty; want non-empty string")
	}

	// Values must mirror the package variables.
	if info.Version != version.Version {
		t.Errorf("Info().Version = %q; want %q (== version.Version)", info.Version, version.Version)
	}
	if info.Commit != version.Commit {
		t.Errorf("Info().Commit = %q; want %q (== version.Commit)", info.Commit, version.Commit)
	}
	if info.Date != version.Date {
		t.Errorf("Info().Date = %q; want %q (== version.Date)", info.Date, version.Date)
	}
}
