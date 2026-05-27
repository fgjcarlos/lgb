//go:build integration

package plc_test

import (
	"context"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// ─── T-3.04: Manager integration tests ──────────────────────────────────────
//
// These tests require a real CIP server. startRealCIPSim (defined in
// gologix_integration_test.go) binds port 44818 — only one test binary may
// run these tests at a time.

// TestIntegration_ManagerStartStop verifies that Manager.Start connects to the
// plcsim and Manager.Stop returns within 2 seconds without goroutine leaks.
func TestIntegration_ManagerStartStop(t *testing.T) {
	addr := startRealCIPSim(t)

	cfg := &config.Config{
		PLCs: []config.PLC{
			{
				Name:          "sim",
				Address:       addr,
				Slot:          0,
				SocketTimeout: "5s",
				ScanRate:      "200ms",
			},
		},
	}

	mgr := plc.NewManager(cfg, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for the goroutine to connect.
	time.Sleep(500 * time.Millisecond)

	// Verify driver is accessible and connected.
	d, ok := mgr.Driver("sim")
	if !ok {
		t.Fatal("Driver(\"sim\") returned ok=false")
	}
	if !d.Connected() {
		t.Error("Driver not connected after 500ms")
	}

	// Stop must return within 2 seconds.
	done := make(chan error, 1)
	go func() { done <- mgr.Stop() }()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-timer.C:
		t.Fatal("Stop() did not return within 2s — goroutine leak suspected")
	}
}

// TestIntegration_ManagerReload verifies that Reload with a new PLC name
// removes the old driver and adds the new one.
//
// Note: gologix.Server.Serve() binds port 44818, so both "old" and "new"
// config entries point at the same simulated PLC for this test. The important
// assertions are about Manager state, not physical PLC separation.
func TestIntegration_ManagerReload(t *testing.T) {
	addr := startRealCIPSim(t)

	cfgA := &config.Config{
		PLCs: []config.PLC{
			{
				Name:          "plc-a",
				Address:       addr,
				Slot:          0,
				SocketTimeout: "5s",
				ScanRate:      "200ms",
			},
		},
	}

	mgr := plc.NewManager(cfgA, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Verify original driver is connected.
	origDriver, ok := mgr.Driver("plc-a")
	if !ok {
		t.Fatal("Driver(\"plc-a\") not found before Reload")
	}
	if !origDriver.Connected() {
		t.Error("original driver not connected before Reload")
	}

	// Reload: replace plc-a with plc-a-new (same physical sim for simplicity).
	cfgB := &config.Config{
		PLCs: []config.PLC{
			{
				Name:          "plc-a-new",
				Address:       addr,
				Slot:          0,
				SocketTimeout: "5s",
				ScanRate:      "200ms",
			},
		},
	}

	if err := mgr.Reload(ctx, cfgB); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Old driver should no longer be in the manager.
	_, stillThere := mgr.Driver("plc-a")
	if stillThere {
		t.Error("old plc-a driver still present after Reload")
	}

	// New driver should be registered and connected.
	newDriver, ok := mgr.Driver("plc-a-new")
	if !ok {
		t.Fatal("new plc-a-new driver not found after Reload")
	}
	if !newDriver.Connected() {
		t.Error("new driver not connected after Reload")
	}

	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop after Reload: %v", err)
	}
}

// TestIntegration_ManagerPLCRemoval verifies that when a PLC is removed via
// Reload, its goroutine stops while the remaining PLC continues.
func TestIntegration_ManagerPLCRemoval(t *testing.T) {
	addr := startRealCIPSim(t)

	cfg := &config.Config{
		PLCs: []config.PLC{
			{
				Name:          "plc-a",
				Address:       addr,
				Slot:          0,
				SocketTimeout: "5s",
				ScanRate:      "200ms",
			},
			{
				Name:          "plc-b",
				Address:       addr,
				Slot:          0,
				SocketTimeout: "5s",
				ScanRate:      "200ms",
			},
		},
	}

	mgr := plc.NewManager(cfg, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Both PLCs should be registered.
	_, okA := mgr.Driver("plc-a")
	_, okB := mgr.Driver("plc-b")
	if !okA || !okB {
		t.Fatal("both plc-a and plc-b should be registered after Start")
	}

	// Reload with only plc-a (remove plc-b).
	cfgNoB := &config.Config{
		PLCs: []config.PLC{
			{
				Name:          "plc-a",
				Address:       addr,
				Slot:          0,
				SocketTimeout: "5s",
				ScanRate:      "200ms",
			},
		},
	}

	if err := mgr.Reload(ctx, cfgNoB); err != nil {
		t.Fatalf("Reload (remove plc-b): %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// plc-b should be gone.
	_, okBAfter := mgr.Driver("plc-b")
	if okBAfter {
		t.Error("plc-b driver still present after Reload that removed it")
	}

	// plc-a should still be running.
	dA, okAAfter := mgr.Driver("plc-a")
	if !okAAfter {
		t.Fatal("plc-a driver missing after Reload")
	}
	if !dA.Connected() {
		t.Error("plc-a driver not connected after Reload")
	}

	// Stop cleanly — must return within 2 seconds.
	done := make(chan error, 1)
	go func() { done <- mgr.Stop() }()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-timer.C:
		t.Fatal("Stop() did not return within 2s after PLC removal")
	}
}
