package mqtt

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writePEMPair writes a self-signed certificate and its EC private key to dir
// and returns their paths. The certificate doubles as a CA bundle.
func writePEMPair(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "lgb-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func tlsConfigOf(c *PahoClient) *tls.Config {
	r := c.client.OptionsReader()
	return r.TLSConfig()
}

// TestNewClient_TLSConfigPerScheme pins that a TLS configuration is attached
// only for the TLS schemes paho dials with crypto/tls (issue #73).
func TestNewClient_TLSConfigPerScheme(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url     string
		wantTLS bool
	}{
		{"tcp://broker:1883", false},
		{"mqtt://broker:1883", false},
		{"ws://broker:9001", false},
		{"mqtts://broker:8883", true},
		{"ssl://broker:8883", true},
		{"tls://broker:8883", true},
		{"tcps://broker:8883", true},
		{"mqtt+ssl://broker:8883", true},
		{"wss://broker:443", true},
		{"MQTTS://broker:8883", true},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			c := NewClient(Options{BrokerURL: tc.url, ClientID: "test"})
			if c.initErr != nil {
				t.Fatalf("initErr = %v; want nil", c.initErr)
			}
			got := tlsConfigOf(c)
			if tc.wantTLS {
				if got == nil {
					t.Fatal("TLSConfig is nil; want a TLS config")
				}
				if got.MinVersion != tls.VersionTLS12 {
					t.Errorf("MinVersion = %x; want TLS 1.2", got.MinVersion)
				}
				if got.InsecureSkipVerify {
					t.Error("InsecureSkipVerify = true; want false by default")
				}
				if got.RootCAs != nil || len(got.Certificates) != 0 {
					t.Error("unexpected RootCAs/Certificates without CA/cert options")
				}
			} else if got != nil {
				t.Fatalf("TLSConfig = %+v; want nil for plaintext scheme", got)
			}
			if IsTLSScheme(tc.url) != tc.wantTLS {
				t.Errorf("IsTLSScheme(%q) = %v; want %v", tc.url, !tc.wantTLS, tc.wantTLS)
			}
		})
	}
}

func TestNewClient_TLSFilesLoaded(t *testing.T) {
	t.Parallel()
	certPath, keyPath := writePEMPair(t, t.TempDir())
	c := NewClient(Options{
		BrokerURL:             "mqtts://broker:8883",
		ClientID:              "test",
		TLSCACert:             certPath,
		TLSCertFile:           certPath,
		TLSKeyFile:            keyPath,
		TLSInsecureSkipVerify: true,
	})
	if c.initErr != nil {
		t.Fatalf("initErr = %v; want nil", c.initErr)
	}
	got := tlsConfigOf(c)
	if got == nil {
		t.Fatal("TLSConfig is nil")
	}
	if got.RootCAs == nil {
		t.Error("RootCAs is nil; want the CA bundle")
	}
	if len(got.Certificates) != 1 {
		t.Errorf("len(Certificates) = %d; want 1", len(got.Certificates))
	}
	if !got.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false; want true")
	}
}

// TestNewClient_InitErrorSurfacesOnConnect asserts that broken credential or
// TLS material fails Connect with ErrMQTTConnect before any dial.
func TestNewClient_InitErrorSurfacesOnConnect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath, keyPath := writePEMPair(t, dir)
	notPEM := filepath.Join(dir, "not.pem")
	if err := os.WriteFile(notPEM, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]Options{
		"missing CA":           {BrokerURL: "mqtts://b:8883", TLSCACert: missing},
		"CA without certs":     {BrokerURL: "mqtts://b:8883", TLSCACert: notPEM},
		"cert without key":     {BrokerURL: "mqtts://b:8883", TLSCertFile: certPath},
		"key without cert":     {BrokerURL: "mqtts://b:8883", TLSKeyFile: keyPath},
		"bad key pair":         {BrokerURL: "mqtts://b:8883", TLSCertFile: certPath, TLSKeyFile: notPEM},
		"missing passwordFile": {BrokerURL: "tcp://b:1883", PasswordFile: missing},
		"password and file":    {BrokerURL: "tcp://b:1883", Password: "x", PasswordFile: pwFile},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := NewClient(opts)
			err := c.Connect(context.Background())
			if !errors.Is(err, ErrMQTTConnect) {
				t.Fatalf("Connect error = %v; want ErrMQTTConnect", err)
			}
			if c.initErr == nil || !errors.Is(err, c.initErr) {
				t.Fatalf("Connect error = %v; want it to wrap initErr %v", err, c.initErr)
			}
		})
	}
}

// TestConnect_TLSHandshakeAndCredentialsOnWire dials an in-process TLS
// listener: with the configured CA the handshake succeeds and the CONNECT
// packet carries the username and file-sourced password; without it the
// broker certificate is rejected.
func TestConnect_TLSHandshakeAndCredentialsOnWire(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath, keyPath := writePEMPair(t, dir)
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("wire-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	for name, withCA := range map[string]bool{"trusted CA": true, "system roots": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			type result struct {
				handshake error
				connect   []byte
			}
			res := make(chan result, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					res <- result{handshake: err}
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if err := conn.(*tls.Conn).Handshake(); err != nil {
					res <- result{handshake: err}
					return
				}
				buf := make([]byte, 512)
				n, _ := conn.Read(buf)
				res <- result{connect: buf[:n]}
			}()

			opts := Options{
				BrokerURL:    "mqtts://" + ln.Addr().String(),
				ClientID:     "lgb-tls-test",
				Username:     "lgb-user",
				PasswordFile: pwFile,
			}
			if withCA {
				opts.TLSCACert = certPath
			}
			c := NewClient(opts)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// The listener is not a broker, so Connect itself always fails.
			_ = c.Connect(ctx)

			r := <-res
			if !withCA {
				if r.handshake == nil {
					t.Fatal("handshake succeeded without the CA; want the client to reject the broker certificate")
				}
				return
			}
			if r.handshake != nil {
				t.Fatalf("handshake failed: %v", r.handshake)
			}
			for _, want := range []string{"MQTT", "lgb-user", "wire-secret"} {
				if !bytes.Contains(r.connect, []byte(want)) {
					t.Errorf("CONNECT packet %q does not contain %q", r.connect, want)
				}
			}
		})
	}
}

func TestNewClient_Credentials(t *testing.T) {
	t.Parallel()
	pwFile := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pwFile, []byte("from-file\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		opts             Options
		wantUser, wantPw string
	}{
		"inline":    {Options{BrokerURL: "tcp://b:1883", Username: "lgb", Password: "inline"}, "lgb", "inline"},
		"from file": {Options{BrokerURL: "tcp://b:1883", Username: "lgb", PasswordFile: pwFile}, "lgb", "from-file"},
		"none":      {Options{BrokerURL: "tcp://b:1883"}, "", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := NewClient(tc.opts)
			if c.initErr != nil {
				t.Fatalf("initErr = %v", c.initErr)
			}
			r := c.client.OptionsReader()
			if got := r.Username(); got != tc.wantUser {
				t.Errorf("Username = %q; want %q", got, tc.wantUser)
			}
			if got := r.Password(); got != tc.wantPw {
				t.Errorf("Password = %q; want %q", got, tc.wantPw)
			}
		})
	}
}

// TestNewClient_TLSOptionsOnPlaintextSchemeRejected: the server does not run
// config.Validate, so the client itself must refuse ignored TLS settings.
func TestNewClient_TLSOptionsOnPlaintextSchemeRejected(t *testing.T) {
	t.Parallel()
	for _, opts := range []Options{
		{BrokerURL: "tcp://b:1883", TLSCACert: "/x"},
		{BrokerURL: "mqtt://b:1883", TLSCertFile: "/c", TLSKeyFile: "/k"},
		{BrokerURL: "ws://b:9001", TLSInsecureSkipVerify: true},
	} {
		c := NewClient(opts)
		if err := c.Connect(context.Background()); !errors.Is(err, ErrMQTTConnect) || c.initErr == nil {
			t.Errorf("%+v: Connect = %v; want ErrMQTTConnect from initErr", opts, err)
		}
	}
}

func TestNewClient_EmptyPasswordFileRejected(t *testing.T) {
	t.Parallel()
	pwFile := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pwFile, []byte("\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Options{BrokerURL: "tcp://b:1883", Username: "u", PasswordFile: pwFile})
	if err := c.Connect(context.Background()); !errors.Is(err, ErrMQTTConnect) || c.initErr == nil {
		t.Fatalf("Connect = %v; want ErrMQTTConnect for an empty passwordFile", err)
	}
}

// TestConnect_ClientCertPresented: a broker requiring mutual TLS accepts the
// configured client certificate.
func TestConnect_ClientCertPresented(t *testing.T) {
	t.Parallel()
	certPath, keyPath := writePEMPair(t, t.TempDir())
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pemBytes)
	sc, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{sc}, ClientCAs: pool,
		ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	res := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			res <- err
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		res <- c.(*tls.Conn).Handshake()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = NewClient(Options{
		BrokerURL: "ssl://" + ln.Addr().String(), ClientID: "p",
		TLSCACert: certPath, TLSCertFile: certPath, TLSKeyFile: keyPath,
	}).Connect(ctx)
	if err := <-res; err != nil {
		t.Fatalf("mutual TLS handshake: %v", err)
	}
}
