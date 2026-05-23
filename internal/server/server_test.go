// server_test.go — tests for the HTTP server stub.
//
// Requirements: MVP-FND-1.3, MVP-FND-1.8, MVP-FND-1.9. Design: §11, §4.3, §4.5.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/testutil"
)

// TestServer_HealthEndpoint verifies that Run(ctx) binds the configured address
// and /health returns 200. (MVP-FND-1.3)
func TestServer_HealthEndpoint(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0" // OS-assigned port

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start server in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Wait for server to bind.
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	// Check /health.
	resp, err := http.Get(fmt.Sprintf("http://%s/health", addr))
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Cancel context → graceful shutdown.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error on clean shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}

// TestServer_MetricsEndpoint verifies /metrics returns 200 with the correct
// Content-Type. (MVP-FND-1.8)
func TestServer_MetricsEndpoint(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	expected := "text/plain; version=0.0.4; charset=utf-8"
	if ct != expected {
		t.Errorf("expected Content-Type %q, got %q", expected, ct)
	}
}

// TestServer_GracefulShutdown verifies Run(ctx) returns nil on context cancel
// within 1 second. (MVP-FND-1.9)
func TestServer_GracefulShutdown(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.Server.ShutdownTimeout = "1s"

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Wait for bind.
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind")
	}

	start := time.Now()
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error on clean shutdown: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("shutdown took too long: %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}

// TestServer_ReadyzEndpoint verifies /readyz returns 200 after server binds.
// (MVP-FND-1.9)
func TestServer_ReadyzEndpoint(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind")
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/readyz", addr))
	if err != nil {
		t.Fatalf("GET /readyz failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		if status, ok := body["status"]; ok && status != "ok" {
			t.Errorf("unexpected readyz body: %q", body)
		}
	}
}

// mockPLCManager is a test double for the PLCManager interface used in server wiring.
// It records calls to Start and Stop so tests can verify lifecycle ordering.
// A sync.Mutex protects the bool fields because Start/Stop are called from the
// goroutine running Server.Run while the test goroutine reads them.
type mockPLCManager struct {
	mu          sync.Mutex
	startCalled bool
	stopCalled  bool
	startErr    error
	stopErr     error
}

func (m *mockPLCManager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.startCalled = true
	m.mu.Unlock()
	return m.startErr
}

func (m *mockPLCManager) Stop() error {
	m.mu.Lock()
	m.stopCalled = true
	m.mu.Unlock()
	return m.stopErr
}

func (m *mockPLCManager) StartWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *mockPLCManager) StopWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

// TestServer_WithPLCManager_StartStop verifies that Run(ctx) calls Start on the
// PLCManager before serving and Stop after ctx cancellation. (PLC-DRV-2.1)
func TestServer_WithPLCManager_StartStop(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	mgr := &mockPLCManager{}
	srv := New(cfg, logger, nil, mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Wait for server to bind — ensures Start was called before we check.
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	if !mgr.StartWasCalled() {
		t.Error("expected PLCManager.Start to be called before serving")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error on clean shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}

	if !mgr.StopWasCalled() {
		t.Error("expected PLCManager.Stop to be called after ctx cancellation")
	}
}

// TestServer_NilPLCManager_NoOp verifies that Run(ctx) works correctly when
// nil is passed for the PLCManager (backward-compatible path). (PLC-DRV-2.1)
func TestServer_NilPLCManager_NoOp(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, nil) // nil manager — must not panic

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error on clean shutdown with nil manager: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}
