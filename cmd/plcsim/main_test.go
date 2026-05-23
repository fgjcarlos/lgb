//go:build integration

// Package main_test provides integration tests for the plcsim binary.
//
// These tests require real TCP listeners and the in-process gologix server.
// Run with: go test -tags=integration ./cmd/plcsim/...
//
// Requirements: MVP-FND-9.2. Design: §12, §20.5.
package main

import (
	"net"
	"testing"

	"github.com/fgjcarlos/lgb/internal/testutil"
)

// TestStartPLCSim_TCPAccept verifies that StartPLCSim starts an in-process
// gologix server and that a TCP connection to the returned address succeeds.
func TestStartPLCSim_TCPAccept(t *testing.T) {
	addr, stop := testutil.StartPLCSim(t)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("net.Dial(%q): %v", addr, err)
	}
	conn.Close()
}

// TestStartPLCSim_TagsSeeded verifies that the three required tags
// (SimBool, SimInt, SimFloat) are pre-seeded in the provider returned by
// NewPLCSimProvider, which is the canonical constructor used by StartPLCSim.
func TestStartPLCSim_TagsSeeded(t *testing.T) {
	p := testutil.NewPLCSimProvider()

	cases := []struct {
		tag  string
		want any
	}{
		{"simbool", true},
		{"simint", int16(42)},
		{"simfloat", float32(3.14)},
	}

	for _, tc := range cases {
		val, err := p.TagRead(tc.tag, 1)
		if err != nil {
			t.Errorf("TagRead(%q): %v", tc.tag, err)
			continue
		}
		if val != tc.want {
			t.Errorf("TagRead(%q) = %v (%T), want %v (%T)", tc.tag, val, val, tc.want, tc.want)
		}
	}
}
