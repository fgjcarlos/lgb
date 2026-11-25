package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

func loadYAML(t *testing.T, content string) *config.Config {
	t.Helper()
	y := filepath.Join(t.TempDir(), "lgb.yaml")
	if err := os.WriteFile(y, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(y)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// TestLoadChangeOnValueHeartbeat verifies #122: per-consumer heartbeats load
// from YAML and are disabled (empty) by default.
func TestLoadChangeOnValueHeartbeat(t *testing.T) {
	t.Parallel()

	cfg := loadYAML(t, "changeOnValue:\n  sparkplugHeartbeat: 60s\n  historianHeartbeat: 15m\n")
	want := config.ChangeOnValueSection{
		Sparkplug: true, Realtime: true, Historian: false,
		SparkplugHeartbeat: "60s", HistorianHeartbeat: "15m",
	}
	if cfg.ChangeOnValue != want {
		t.Errorf("changeOnValue = %+v, want %+v", cfg.ChangeOnValue, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := config.HeartbeatInterval(cfg.ChangeOnValue.SparkplugHeartbeat); got != time.Minute {
		t.Errorf("sparkplug heartbeat = %v, want 1m", got)
	}
	if got := config.HeartbeatInterval(cfg.ChangeOnValue.RealtimeHeartbeat); got != 0 {
		t.Errorf("unset realtime heartbeat = %v, want 0 (disabled)", got)
	}
}

// TestValidateChangeOnValueHeartbeat verifies #122: a heartbeat must be a
// positive Go duration.
func TestValidateChangeOnValueHeartbeat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		field, value, wantErr string
	}{
		{"sparkplugHeartbeat", "soon", `changeOnValue.sparkplugHeartbeat: "soon" is not a valid Go duration`},
		{"realtimeHeartbeat", "0s", `changeOnValue.realtimeHeartbeat: must be positive, got "0s"`},
		{"historianHeartbeat", "-1m", `changeOnValue.historianHeartbeat: must be positive, got "-1m"`},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			cfg := loadYAML(t, "changeOnValue:\n  "+tc.field+": "+tc.value+"\n")
			err := cfg.Validate()
			if !errors.Is(err, config.ErrConfigInvalid) {
				t.Fatalf("Validate = %v, want ErrConfigInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
