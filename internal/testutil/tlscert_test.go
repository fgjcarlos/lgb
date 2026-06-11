// Package testutil — tlscert_test verifies that SelfSignedTLSConfig returns a
// *tls.Config that can be used as both server and client without disk access.
package testutil_test

import (
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/testutil"
)

// TestSelfSignedTLSConfig_ReturnsTLSConfig asserts that SelfSignedTLSConfig
// returns a non-nil *tls.Config with at least one certificate loaded.
func TestSelfSignedTLSConfig_ReturnsTLSConfig(t *testing.T) {
	cfg := testutil.SelfSignedTLSConfig(t)
	// SelfSignedTLSConfig calls t.Fatal on failure, so cfg is always non-nil here.
	if n := len(cfg.Certificates); n == 0 {
		t.Fatalf("SelfSignedTLSConfig returned TLSConfig with no certificates; want >= 1")
	}
}

// TestSelfSignedTLSConfig_ClientCanConnect asserts that the returned config
// allows a TLS dial to succeed (self-signed, InsecureSkipVerify on client).
// No disk I/O: listener + client live in-process.
func TestSelfSignedTLSConfig_ClientCanConnect(t *testing.T) {
	serverCfg := testutil.SelfSignedTLSConfig(t)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		// TLS handshake happens lazily; force it.
		if err := conn.(*tls.Conn).Handshake(); err != nil {
			done <- err
			return
		}
		conn.Close()
		done <- nil
	}()

	clientCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test only
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 3 * time.Second},
		"tcp",
		ln.Addr().String(),
		clientCfg,
	)
	if err != nil {
		t.Fatalf("TLS client dial: %v", err)
	}
	conn.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server accept/handshake error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TLS handshake timed out")
	}
}
