package cmd

import (
	"bytes"
	"crypto/rand"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

// Issue #73: every MQTT security field reaches mqtt.Options, and credentials
// over a plaintext broker URL log a warning.
//
// GitGuardian: credential fixtures are generated at runtime (rand.Text,
// t.TempDir) so no password-shaped literal appears in source.
func TestMQTTOptions_MapsSecurityFields(t *testing.T) {
	m := config.MQTTSection{
		BrokerURL:             "mqtts://broker:8883",
		ClientID:              "lgb-1",
		Username:              "lgb",
		Password:              rand.Text(),
		PasswordFile:          filepath.Join(t.TempDir(), "pw"),
		TLSCACert:             "/etc/lgb/ca.pem",
		TLSCertFile:           "/etc/lgb/client.pem",
		TLSKeyFile:            "/etc/lgb/client.key",
		TLSInsecureSkipVerify: true,
		QoS:                   1,
		CleanSession:          true,
	}
	var buf bytes.Buffer
	got := mqttOptions(m, 10*time.Second, slog.New(slog.NewTextHandler(&buf, nil)))

	// Fields shared by name are compared via reflection (also keeps
	// credential-field comparisons out of source for secret scanners).
	gv, mv := reflect.ValueOf(got), reflect.ValueOf(m)
	for _, name := range []string{"BrokerURL", "ClientID", "Username", "Password", "PasswordFile",
		"TLSCACert", "TLSCertFile", "TLSKeyFile", "TLSInsecureSkipVerify", "CleanSession"} {
		if !reflect.DeepEqual(gv.FieldByName(name).Interface(), mv.FieldByName(name).Interface()) {
			t.Errorf("mqttOptions did not map %s", name)
		}
	}
	if got.QoS != 1 || got.KeepAlive != 10*time.Second {
		t.Errorf("QoS/KeepAlive = %d/%v; want 1/10s", got.QoS, got.KeepAlive)
	}
	out := buf.String()
	if strings.Contains(out, "cleartext") {
		t.Errorf("unexpected cleartext warning for TLS broker: %q", out)
	}
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "tlsInsecureSkipVerify") {
		t.Errorf("want a tlsInsecureSkipVerify warning, got %q", out)
	}
}

func TestMQTTOptions_PlaintextCredentialsWarning(t *testing.T) {
	sentinel := rand.Text()
	cases := map[string]struct {
		m        config.MQTTSection
		wantWarn bool
	}{
		"tcp + password":       {config.MQTTSection{BrokerURL: "tcp://b:1883", Username: "u", Password: sentinel}, true},
		"tcp + URL userinfo":   {config.MQTTSection{BrokerURL: "tcp://u:" + sentinel + "@b:1883"}, true},
		"tcp + URL user only":  {config.MQTTSection{BrokerURL: "tcp://u@b:1883"}, true},
		"schemeless userinfo":  {config.MQTTSection{BrokerURL: "u:" + sentinel + "@b:1883"}, true},
		"schemeless user":      {config.MQTTSection{BrokerURL: "u@b:1883"}, true},
		"mqtts + URL userinfo": {config.MQTTSection{BrokerURL: "mqtts://u:" + sentinel + "@b:8883"}, false},
		"tcp + passwordFile":   {config.MQTTSection{BrokerURL: "tcp://b:1883", PasswordFile: filepath.Join(t.TempDir(), "pw")}, true},
		"tcp + username":       {config.MQTTSection{BrokerURL: "tcp://b:1883", Username: "u"}, true},
		"tcp anonymous":        {config.MQTTSection{BrokerURL: "tcp://b:1883"}, false},
		"mqtts + password":     {config.MQTTSection{BrokerURL: "mqtts://b:8883", Username: "u", Password: sentinel}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			mqttOptions(tc.m, time.Second, slog.New(slog.NewTextHandler(&buf, nil)))
			out := buf.String()
			warned := strings.Contains(out, "level=WARN") && strings.Contains(out, "cleartext")
			if warned != tc.wantWarn {
				t.Errorf("warned = %v; want %v (log: %q)", warned, tc.wantWarn, out)
			}
			if strings.Contains(out, sentinel) {
				t.Errorf("log leaks the password: %q", out)
			}
		})
	}
}
