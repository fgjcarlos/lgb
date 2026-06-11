package opcua

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/gopcua/opcua/ua"
)

type mockTagSource struct {
	tags map[string]map[string]plc.TagValue
}

func (m *mockTagSource) CurrentTag(plcName, tag string) (plc.TagValue, bool) {
	if m.tags == nil {
		return plc.TagValue{}, false
	}
	if tags, ok := m.tags[plcName]; ok {
		v, ok := tags[tag]
		return v, ok
	}
	return plc.TagValue{}, false
}

func (m *mockTagSource) CurrentSnapshot() map[string]map[string]plc.TagValue {
	return m.tags
}

func TestNew_ReturnsNonNil(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Port: 0},
	}
	srv := New(cfg, &mockTagSource{}, nil)
	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestServer_StartStop(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{
			Enabled:      true,
			Host:         "127.0.0.1",
			Port:         freeTCPPort(t),
			SecurityMode: "None",
		},
		PLCs: []config.PLC{
			{
				Name: "sim",
				Tags: []config.TagDef{
					{Name: "Motor.Speed", Type: "Float"},
					{Name: "Valve.Open", Type: "Boolean"},
				},
			},
		},
	}

	tags := &mockTagSource{
		tags: map[string]map[string]plc.TagValue{
			"sim": {
				"Motor.Speed": {Value: float32(1200.5), Timestamp: time.Now(), Quality: "good"},
				"Valve.Open":  {Value: true, Timestamp: time.Now(), Quality: "good"},
			},
		},
	}

	srv := New(cfg, tags, nil)

	ctx, cancel := context.WithCancel(context.Background())

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if srv.srv != nil {
		defer srv.srv.Close()
	}

	cancel()

	// Stop should be idempotent and clean.
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func TestServer_StopBeforeStart(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0, SecurityMode: "None"},
	}

	srv := New(cfg, &mockTagSource{}, nil)

	// Stop on a never-started server should be a no-op.
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop on unstarted server returned error: %v", err)
	}
}

// TestOpcUA_BadQuality_ReturnsBadStatusCode verifies R70-3: the address-space
// node reader returns a DataValue with a bad StatusCode when the stored tag has
// Quality=="bad".  We call the exported DataValueForTag test-helper to
// exercise the branch without starting the full server.
func TestOpcUA_BadQuality_ReturnsBadStatusCode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0, SecurityMode: "None"},
		PLCs: []config.PLC{
			{
				Name: "sim",
				Tags: []config.TagDef{{Name: "Pressure", Type: "Float"}},
			},
		},
	}

	tags := &mockTagSource{
		tags: map[string]map[string]plc.TagValue{
			"sim": {
				"Pressure": {Value: float32(42), Quality: "bad", Timestamp: time.Now()},
			},
		},
	}

	srv := New(cfg, tags, nil)

	dv := srv.DataValueForTag("sim", "Pressure")
	if dv == nil {
		t.Fatal("DataValueForTag returned nil for bad-quality tag")
		return
	}
	// StatusCode 0 == StatusOK — bad quality must produce a non-zero (bad) status.
	if dv.Status == 0 {
		t.Errorf("DataValueForTag returned StatusCode=0 (StatusOK) for bad-quality tag; want non-zero bad StatusCode")
	}
}

// TestOpcUA_GoodQuality_ReturnsGoodStatusCode verifies the inverse: good
// quality returns a DataValue with StatusCode==0 (StatusOK / Good).
func TestOpcUA_GoodQuality_ReturnsGoodStatusCode(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		OPCUA: config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: 0, SecurityMode: "None"},
		PLCs: []config.PLC{
			{
				Name: "sim",
				Tags: []config.TagDef{{Name: "Pressure", Type: "Float"}},
			},
		},
	}

	tags := &mockTagSource{
		tags: map[string]map[string]plc.TagValue{
			"sim": {
				"Pressure": {Value: float32(42), Quality: "good", Timestamp: time.Now()},
			},
		},
	}

	srv := New(cfg, tags, nil)

	dv := srv.DataValueForTag("sim", "Pressure")
	if dv == nil {
		t.Fatal("DataValueForTag returned nil for good-quality tag")
		return
	}
	if dv.Status != 0 {
		t.Errorf("DataValueForTag returned StatusCode=%v for good-quality tag; want 0 (StatusOK)", dv.Status)
	}
}

func TestServer_EndpointSecurityMatrix(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeOPCUACertFiles(t)
	tests := []struct {
		name       string
		mode       string
		certFile   string
		keyFile    string
		wantMode   ua.MessageSecurityMode
		wantPolicy string
		rejectNone bool
	}{
		{
			name:       "explicit None exposes only None endpoint",
			mode:       "None",
			wantMode:   ua.MessageSecurityModeNone,
			wantPolicy: ua.SecurityPolicyURINone,
		},
		{
			name:       "Sign exposes Basic256Sha256 without None endpoint",
			mode:       "Sign",
			certFile:   certFile,
			keyFile:    keyFile,
			wantMode:   ua.MessageSecurityModeSign,
			wantPolicy: ua.SecurityPolicyURIBasic256Sha256,
			rejectNone: true,
		},
		{
			name:       "SignAndEncrypt exposes Basic256Sha256 without None endpoint",
			mode:       "SignAndEncrypt",
			certFile:   certFile,
			keyFile:    keyFile,
			wantMode:   ua.MessageSecurityModeSignAndEncrypt,
			wantPolicy: ua.SecurityPolicyURIBasic256Sha256,
			rejectNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				OPCUA: config.OPCUASection{
					Enabled:      true,
					Host:         "127.0.0.1",
					Port:         freeTCPPort(t),
					SecurityMode: tt.mode,
					CertFile:     tt.certFile,
					KeyFile:      tt.keyFile,
				},
			}

			srv := New(cfg, &mockTagSource{}, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := srv.Start(ctx); err != nil {
				t.Fatalf("Start() returned error: %v", err)
			}
			defer srv.srv.Close()

			endpoints := srv.srv.Endpoints()
			if len(endpoints) != 1 {
				t.Fatalf("got %d endpoints, want 1", len(endpoints))
			}

			ep := endpoints[0]
			if ep.SecurityMode != tt.wantMode {
				t.Errorf("SecurityMode = %v, want %v", ep.SecurityMode, tt.wantMode)
			}
			if ep.SecurityPolicyURI != tt.wantPolicy {
				t.Errorf("SecurityPolicyURI = %q, want %q", ep.SecurityPolicyURI, tt.wantPolicy)
			}
			if tt.rejectNone && ep.SecurityPolicyURI == ua.SecurityPolicyURINone {
				t.Fatal("secure mode exposed SecurityPolicy#None endpoint")
			}
			if tt.rejectNone && len(ep.ServerCertificate) == 0 {
				t.Fatal("secure mode endpoint has no server certificate")
			}
		})
	}
}

func TestServer_SecureModeMissingCertificateFailsFast(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		OPCUA: config.OPCUASection{
			Enabled:      true,
			Host:         "127.0.0.1",
			Port:         0,
			SecurityMode: "Sign",
			CertFile:     "missing.crt",
			KeyFile:      "missing.key",
		},
	}

	srv := New(cfg, &mockTagSource{}, nil)
	if err := srv.Start(context.Background()); err == nil {
		t.Fatal("Start() returned nil for secure mode with missing cert/key; want error")
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func writeOPCUACertFiles(t *testing.T) (string, string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "lgb-opcua-test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	dir := t.TempDir()
	certFile := dir + "/opcua.crt"
	keyFile := dir + "/opcua.key"
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}
