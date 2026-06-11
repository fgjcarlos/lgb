// server_test.go — tests for the HTTP server stub.
//
// Requirements: MVP-FND-1.3, MVP-FND-1.8, MVP-FND-1.9. Design: §11, §4.3, §4.5.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// TestServer_HealthEndpoint verifies that Run(ctx) binds the configured address
// and /health returns 200. (MVP-FND-1.3)
func TestServer_HealthEndpoint(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0" // OS-assigned port

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, Opts{})

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
	srv := New(cfg, logger, nil, Opts{})

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
	srv := New(cfg, logger, nil, Opts{})

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
	srv := New(cfg, logger, nil, Opts{})

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

func (m *mockPLCManager) Reload(_ context.Context, _ *config.Config) error { return nil }
func (m *mockPLCManager) WriteTag(_ string, _ string, _ any) error         { return nil }

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
	srv := New(cfg, logger, nil, Opts{PLCMgr: mgr})

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
	srv := New(cfg, logger, nil, Opts{}) // nil manager — must not panic

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

// ─── SparkplugNode wiring tests ─────────────────────────────────────────────

type mockSparkplugNode struct {
	mu          sync.Mutex
	startCalled bool
	stopCalled  bool
}

func (m *mockSparkplugNode) Start(ctx context.Context) error {
	m.mu.Lock()
	m.startCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockSparkplugNode) Stop() error {
	m.mu.Lock()
	m.stopCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockSparkplugNode) StartWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *mockSparkplugNode) StopWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

func TestServer_WithSparkplugNode_StartStop(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	spNode := &mockSparkplugNode{}
	srv := New(cfg, logger, nil, Opts{SpNode: spNode})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	if !spNode.StartWasCalled() {
		t.Error("expected SparkplugNode.Start to be called before serving")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}

	if !spNode.StopWasCalled() {
		t.Error("expected SparkplugNode.Stop to be called after shutdown")
	}
}

// ─── HistorianWriter wiring tests ────────────────────────────────────────────

type mockHistorianWriter struct {
	mu          sync.Mutex
	startCalled bool
	stopCalled  bool
}

func (m *mockHistorianWriter) Start(ctx context.Context) {
	m.mu.Lock()
	m.startCalled = true
	m.mu.Unlock()
}

func (m *mockHistorianWriter) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.stopCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockHistorianWriter) StartWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *mockHistorianWriter) StopWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

func TestServer_WithHistorianWriter_StartStop(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	hw := &mockHistorianWriter{}
	srv := New(cfg, logger, nil, Opts{HistW: hw})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	if !hw.StartWasCalled() {
		t.Error("expected HistorianWriter.Start to be called before serving")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}

	if !hw.StopWasCalled() {
		t.Error("expected HistorianWriter.Stop to be called after shutdown")
	}
}

// ─── BackupScheduler wiring tests ────────────────────────────────────────────

type mockBackupScheduler struct {
	mu          sync.Mutex
	startCalled bool
	stopCalled  bool
}

func (m *mockBackupScheduler) Start(ctx context.Context) {
	m.mu.Lock()
	m.startCalled = true
	m.mu.Unlock()
}

func (m *mockBackupScheduler) Stop() error {
	m.mu.Lock()
	m.stopCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockBackupScheduler) StartWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *mockBackupScheduler) StopWasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

func TestServer_WithBackupScheduler_StartStop(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	bs := &mockBackupScheduler{}
	srv := New(cfg, logger, nil, Opts{BkpSch: bs})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	if !bs.StartWasCalled() {
		t.Error("expected BackupScheduler.Start to be called")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}

	if !bs.StopWasCalled() {
		t.Error("expected BackupScheduler.Stop to be called after shutdown")
	}
}

// ─── R75-1: buildHTTPServer timeout wiring ──────────────────────────────────

// TestBuildHTTPServerTimeouts asserts R75-1b: explicitly set timeout config
// strings are parsed and applied to the returned *http.Server.
func TestBuildHTTPServerTimeouts(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.Server.ReadHeaderTimeout = "10s"
	cfg.Server.ReadTimeout = "20s"
	cfg.Server.WriteTimeout = "40s"
	cfg.Server.IdleTimeout = "80s"

	srv := New(cfg, testutil.NewLogger(t), nil, Opts{})
	mux := http.NewServeMux()
	httpSrv := srv.buildHTTPServer(mux)

	if httpSrv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v; want 10s", httpSrv.ReadHeaderTimeout)
	}
	if httpSrv.ReadTimeout != 20*time.Second {
		t.Errorf("ReadTimeout = %v; want 20s", httpSrv.ReadTimeout)
	}
	if httpSrv.WriteTimeout != 40*time.Second {
		t.Errorf("WriteTimeout = %v; want 40s", httpSrv.WriteTimeout)
	}
	if httpSrv.IdleTimeout != 80*time.Second {
		t.Errorf("IdleTimeout = %v; want 80s", httpSrv.IdleTimeout)
	}
}

// TestBuildHTTPServerTimeoutDefaults asserts R75-1a: when all timeout strings
// are empty, defaults of 5s/30s/60s/120s are applied.
func TestBuildHTTPServerTimeoutDefaults(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	// Leave all timeout strings at zero value (empty string).
	cfg.Server.ReadHeaderTimeout = ""
	cfg.Server.ReadTimeout = ""
	cfg.Server.WriteTimeout = ""
	cfg.Server.IdleTimeout = ""

	srv := New(cfg, testutil.NewLogger(t), nil, Opts{})
	mux := http.NewServeMux()
	httpSrv := srv.buildHTTPServer(mux)

	if httpSrv.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("default ReadHeaderTimeout = %v; want 5s", httpSrv.ReadHeaderTimeout)
	}
	if httpSrv.ReadTimeout != 30*time.Second {
		t.Errorf("default ReadTimeout = %v; want 30s", httpSrv.ReadTimeout)
	}
	if httpSrv.WriteTimeout != 60*time.Second {
		t.Errorf("default WriteTimeout = %v; want 60s", httpSrv.WriteTimeout)
	}
	if httpSrv.IdleTimeout != 120*time.Second {
		t.Errorf("default IdleTimeout = %v; want 120s", httpSrv.IdleTimeout)
	}
}

// ─── R72: TLS server wiring ─────────────────────────────────────────────────

// TestBuildHTTPServerTLS asserts R72: when TLSEnabled=true and Opts.TLSConfig is
// set, the server binds a TLS listener and an HTTPS client can connect.
func TestBuildHTTPServerTLS(t *testing.T) {
	t.Parallel()

	tlsCfg := testutil.SelfSignedTLSConfig(t)

	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.Server.TLSEnabled = true
	cfg.Server.TLSCertFile = "/unused-in-test-seam" // seam: Opts.TLSConfig takes precedence
	cfg.Server.TLSKeyFile = "/unused-in-test-seam"

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, Opts{TLSConfig: tlsCfg})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("TLS server did not bind within timeout")
	}

	// HTTPS client with InsecureSkipVerify because the cert is self-signed.
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test only
		},
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get(fmt.Sprintf("https://%s/health", addr))
	if err != nil {
		t.Fatalf("HTTPS GET /health failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error on TLS shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("TLS server did not shut down within 3s")
	}
}

// TestBuildHTTPServerPlaintext asserts R72: when TLSEnabled=false the server
// continues to serve plaintext HTTP (existing behaviour is preserved).
func TestBuildHTTPServerPlaintext(t *testing.T) {
	t.Parallel()

	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.Server.TLSEnabled = false

	srv := New(cfg, testutil.NewLogger(t), nil, Opts{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind")
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/health", addr))
	if err != nil {
		t.Fatalf("HTTP GET /health failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned non-nil error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}

// TestPlaintextStartupWarnLogged asserts that Run() emits a WARN-level log record
// containing "TLS" when TLSEnabled=false (plaintext path). This covers the R72
// spec requirement that operators are notified when TLS is off (W2).
func TestPlaintextStartupWarnLogged(t *testing.T) {
	t.Parallel()

	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.Server.TLSEnabled = false // plaintext path

	// Capture log output into a buffer via a text handler.
	var buf strings.Builder
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	srv := New(cfg, logger, nil, Opts{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Wait for the server to bind — the WARN is emitted before bind completes.
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server did not bind within timeout")
	}

	// The WARN is emitted synchronously in Run() before the serve goroutine starts,
	// so once Addr() returns the buffer already contains the log record.
	logged := buf.String()
	if !strings.Contains(logged, "WARN") || !strings.Contains(logged, "TLS") {
		t.Errorf("expected a WARN log record mentioning 'TLS' on plaintext startup, got:\n%s", logged)
	}

	cancel()
	<-errCh
}

// TestRunFailsFastOnMissingTLSCert asserts that Run() returns a descriptive error
// immediately when TLSEnabled=true but TLSCertFile or TLSKeyFile is empty, even
// when the caller bypasses Validate() and Opts.TLSConfig is nil (production path).
// This is the fail-fast guard required by the transport-hardening spec (W1).
func TestRunFailsFastOnMissingTLSCert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		certFile string
		keyFile  string
	}{
		{"empty cert and key", "", ""},
		{"empty cert only", "", "/some/key.pem"},
		{"empty key only", "/some/cert.pem", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.MinimalConfig(t)
			cfg.Server.HTTPAddr = "127.0.0.1:0"
			cfg.Server.TLSEnabled = true
			cfg.Server.TLSCertFile = tt.certFile
			cfg.Server.TLSKeyFile = tt.keyFile
			// No TLSConfig seam — exercises the production fail-fast guard.

			srv := New(cfg, testutil.NewLogger(t), nil, Opts{})
			ctx := context.Background()

			err := srv.Run(ctx)
			if err == nil {
				t.Fatal("expected Run to return an error for missing TLS files, got nil")
			}
			// The error must be descriptive — not a cryptic stdlib error.
			errMsg := err.Error()
			if !strings.Contains(errMsg, "TLS") && !strings.Contains(errMsg, "not configured") {
				t.Errorf("error message %q does not mention 'TLS' or 'not configured'", errMsg)
			}
		})
	}
}

func TestServer_NilSparkplugNode_NoOp(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"

	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, Opts{})

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
			t.Errorf("Run returned non-nil error with nil sparkplug node: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}
