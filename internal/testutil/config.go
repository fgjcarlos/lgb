// Package testutil provides helpers for tests across the LGB codebase.
// It MUST only be imported from _test.go files or test packages.
//
// This package intentionally imports internal/config — that is safe because
// testutil is never compiled into the production binary.
//
// Requirements: MVP-FND-2.6 (test ergonomics). Design: §3 (testutil package).
package testutil

import (
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
)

// MinimalConfig returns a *config.Config with all required fields set to safe
// test values. The data directory is set to t.TempDir() so filesystem tests
// are isolated and automatically cleaned up.
//
// Use this helper in tests that need a valid Config but don't care about
// specific field values:
//
//	cfg := testutil.MinimalConfig(t)
//	// use cfg...
func MinimalConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Gateway: config.GatewaySection{
			ID:        "lgb-test",
			LogLevel:  "info",
			LogFormat: "text",
			DataDir:   t.TempDir(),
		},
		Server: config.ServerSection{
			HTTPAddr:        ":0", // OS-assigned port — safe for parallel tests
			TLSEnabled:      false,
			ShutdownTimeout: "1s",
		},
		Auth: config.AuthSection{
			JwtSecret:  "fixture-jwt-value",
			SessionTTL: "8h",
		},
		MQTT: config.MQTTSection{
			BrokerURL:    "tcp://localhost:1883",
			ClientID:     "lgb-test",
			GroupID:      "test-group",
			EdgeNodeID:   "lgb-test",
			QoS:          1,
			KeepAlive:    "30s",
			CleanSession: true,
		},
		Historian: config.HistorianSection{
			RetentionDays: 90,
		},
		PLCSim: config.PLCSimSection{
			Addr: "127.0.0.1:44818", // default for tests; override per-test as needed
		},
	}
}
