package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// Issue #73: MQTT username, passwordFile and TLS validation.

// mqttConfig returns a valid config with a broker set and a readable file
// usable as password file, CA, cert or key (Validate only checks readability).
func mqttConfig(t *testing.T, brokerURL string) (*config.Config, string) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "material")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := validConfig(t)
	cfg.MQTT.BrokerURL = brokerURL
	cfg.MQTT.GroupID = "plant-a"
	cfg.MQTT.EdgeNodeID = "lgb-1"
	return cfg, f
}

func TestMQTTValidateSecurityViolations(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	cases := map[string]struct {
		mutate  func(m *config.MQTTSection, file string)
		wantKey string
	}{
		"password and passwordFile": {func(m *config.MQTTSection, f string) { m.Password = "x"; m.PasswordFile = f }, "mqtt.passwordFile"},
		"passwordFile missing":      {func(m *config.MQTTSection, _ string) { m.PasswordFile = missing }, "mqtt.passwordFile"},
		"passwordFile is dir":       {func(m *config.MQTTSection, f string) { m.PasswordFile = filepath.Dir(f) }, "mqtt.passwordFile"},
		"tlsCACert missing":         {func(m *config.MQTTSection, _ string) { m.TLSCACert = missing }, "mqtt.tlsCACert"},
		"tlsCertFile missing":       {func(m *config.MQTTSection, f string) { m.TLSCertFile = missing; m.TLSKeyFile = f }, "mqtt.tlsCertFile"},
		"tlsKeyFile missing":        {func(m *config.MQTTSection, f string) { m.TLSCertFile = f; m.TLSKeyFile = missing }, "mqtt.tlsKeyFile"},
		"cert without key":          {func(m *config.MQTTSection, f string) { m.TLSCertFile = f }, "mqtt.tlsKeyFile"},
		"key without cert":          {func(m *config.MQTTSection, f string) { m.TLSKeyFile = f }, "mqtt.tlsCertFile"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, f := mqttConfig(t, "mqtts://broker:8883")
			tc.mutate(&cfg.MQTT, f)
			err := cfg.Validate()
			if !errors.Is(err, errs.ErrConfigInvalid) {
				t.Fatalf("Validate() = %v; want ErrConfigInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("Validate() = %q; want it to mention %s", err.Error(), tc.wantKey)
			}
		})
	}
}

// TestMQTTValidateTLSOnPlaintextScheme rejects TLS settings that a plaintext
// broker URL would silently ignore.
func TestMQTTValidateTLSOnPlaintextScheme(t *testing.T) {
	for _, set := range []func(m *config.MQTTSection, f string){
		func(m *config.MQTTSection, f string) { m.TLSCACert = f },
		func(m *config.MQTTSection, f string) { m.TLSCertFile = f; m.TLSKeyFile = f },
		func(m *config.MQTTSection, _ string) { m.TLSInsecureSkipVerify = true },
	} {
		cfg, f := mqttConfig(t, "tcp://broker:1883")
		set(&cfg.MQTT, f)
		err := cfg.Validate()
		if !errors.Is(err, errs.ErrConfigInvalid) || !strings.Contains(err.Error(), "mqtt.brokerURL") {
			t.Errorf("Validate() = %v; want ErrConfigInvalid mentioning mqtt.brokerURL", err)
		}
	}
}

func TestMQTTValidateSecureConfigAccepted(t *testing.T) {
	cfg, f := mqttConfig(t, "mqtts://broker:8883")
	cfg.MQTT.Username = "lgb"
	cfg.MQTT.PasswordFile = f
	cfg.MQTT.TLSCACert = f
	cfg.MQTT.TLSCertFile = f
	cfg.MQTT.TLSKeyFile = f
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v; want nil", err)
	}

	plain, _ := mqttConfig(t, "tcp://broker:1883")
	plain.MQTT.Username = "lgb"
	plain.MQTT.Password = "x"
	if err := plain.Validate(); err != nil {
		t.Fatalf("plaintext with credentials: Validate() = %v; want nil (warning only)", err)
	}
}

func TestMQTTSecurityEnvOverlay(t *testing.T) {
	t.Setenv("LGB_MQTT_USERNAME", "env-user")
	t.Setenv("LGB_MQTT_TLSCACERT", "/etc/lgb/ca.pem")
	t.Setenv("LGB_MQTT_TLSINSECURESKIPVERIFY", "true")
	cfg, err := config.Load(testdataPath("sample.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTT.Username != "env-user" || cfg.MQTT.TLSCACert != "/etc/lgb/ca.pem" || !cfg.MQTT.TLSInsecureSkipVerify {
		t.Errorf("env overlay not applied: username=%q tlsCACert=%q insecure=%v",
			cfg.MQTT.Username, cfg.MQTT.TLSCACert, cfg.MQTT.TLSInsecureSkipVerify)
	}
}

// TestMQTTValidateDoesNotLeakURLPassword: paho uses URL userinfo as
// credentials, so violations must not echo the broker URL.
func TestMQTTValidateDoesNotLeakURLPassword(t *testing.T) {
	for _, u := range []string{"tcp://lgb:S3CRET-PW@broker:1883", "lgb:S3CRET-PW@broker:1883", "lgb:S3CRET-PW@broker:1883/x://y"} {
		cfg, f := mqttConfig(t, u)
		cfg.MQTT.TLSCACert = f
		err := cfg.Validate()
		if err == nil || strings.Contains(err.Error(), "S3CRET-PW") {
			t.Errorf("%s: Validate() = %v; want an error without the URL password", u, err)
		}
	}
}
