// root_test.go — tests for the root command and PersistentPreRunE.
//
// Requirements: MVP-FND-1.1. Design: §6.2–6.3.
package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestRoot_HelpExits0 verifies that `lgb --help` exits 0 and lists subcommands.
// (MVP-FND-1.1 "Help flag exits successfully")
func TestRoot_HelpExits0(t *testing.T) {
	root, _, stdout, _ := newTestRoot(t)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	out := stdout.String()
	for _, sub := range []string{"version", "server", "doctor", "status", "config"} {
		if !strings.Contains(out, sub) {
			t.Errorf("expected --help output to contain %q, got: %q", sub, out)
		}
	}
}

// TestRoot_UnknownFlagExits verifies that `lgb --unknown-flag` returns an error.
// (MVP-FND-1.1 "Unknown flag returns error")
func TestRoot_UnknownFlagExits(t *testing.T) {
	root, _, _, _ := newTestRoot(t)
	root.SetArgs([]string{"--unknown-flag-xyz"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

// TestRoot_PersistentPreRunE_PopulatesConfig verifies that PersistentPreRunE
// populates d.Config before the version subcommand runs.
// (MVP-FND-1.1, design §6.3)
func TestRoot_PersistentPreRunE_PopulatesConfig(t *testing.T) {
	root, d, stdout, _ := newTestRoot(t)
	root.SetArgs([]string{"--config", "testdata/sample.yaml", "version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After Execute, d.Config should be non-nil (set by PersistentPreRunE).
	if d.Config == nil {
		t.Error("expected d.Config to be non-nil after PersistentPreRunE ran")
	}

	// Also verify the version command still wrote output.
	if stdout.String() == "" {
		t.Error("expected version output, got empty")
	}
}

// newTestRoot constructs a fresh root + Deps with captured stdout/stderr buffers.
// Shared by all CLI test files in this package.
func newTestRoot(t *testing.T) (*cobra.Command, *Deps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root, d := NewRoot()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root, d, stdout, stderr
}
