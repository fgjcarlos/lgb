// Package testutil provides helpers for tests across the LGB codebase.
// It MUST only be imported from _test.go files or test packages.

package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

// SelfSignedTLSConfig generates an in-memory self-signed TLS certificate and
// returns a *tls.Config configured to use it as a server certificate. No files
// are written to disk.
//
// The certificate is valid for localhost and 127.0.0.1 with a 1-hour lifetime.
// Use InsecureSkipVerify on the client side in tests because the cert is
// self-signed and not in the system trust store.
//
// This helper exists so TLS server tests can use a real *tls.Config without
// requiring disk-resident cert/key files — important for CI isolation (R72 test seam).
func SelfSignedTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("SelfSignedTLSConfig: generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("SelfSignedTLSConfig: generate serial: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "lgb-test"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("SelfSignedTLSConfig: create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("SelfSignedTLSConfig: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("SelfSignedTLSConfig: X509KeyPair: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}
}
