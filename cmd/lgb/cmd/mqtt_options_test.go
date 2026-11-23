package cmd

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

// Issue #73: every MQTT security field reaches mqtt.Options, and credentials
// over a plaintext broker URL log a warning.
func TestMQTTOptions_MapsSecurityFields(t *testing.T) {
	m := config.MQTTSection{
		BrokerURL:             "mqtts://broker:8883",
		ClientID:              "lgb-1",
		Username:              "lgb",
		Password:              "mapped-value",
		PasswordFile:          "/run/secrets/mqtt",
		TLSCACert:             "/etc/lgb/ca.pem",
		TLSCertFile:           "/etc/lgb/client.pem",
		TLSKeyFile:            "/etc/lgb/client.key",
		TLSInsecureSkipVerify: true,
		QoS:                   1,
		CleanSession:          true,
	}
	var buf bytes.Buffer
	got := mqttOptions(m, 10*time.Second, slog.New(slog.NewTextHandler(&buf, nil)))

	if got.BrokerURL != m.BrokerURL || got.ClientID != m.ClientID || got.Username != m.Username || got.Password != m.Password ||
		got.PasswordFile != m.PasswordFile || got.TLSCACert != m.TLSCACert || got.TLSCertFile != m.TLSCertFile ||
		got.TLSKeyFile != m.TLSKeyFile || !got.TLSInsecureSkipVerify || got.QoS != 1 ||
		got.KeepAlive != 10*time.Second || !got.CleanSession {
		t.Errorf("mqttOptions did not map all fields: %+v", got)
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
	const sentinel = "sentinel-7f3a9c"
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
		"tcp + passwordFile":   {config.MQTTSection{BrokerURL: "tcp://b:1883", PasswordFile: "/f"}, true},
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
