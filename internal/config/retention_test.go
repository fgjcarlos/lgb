package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
)

// TestValidateRetentionDays verifies #85: per-PLC and per-tag retentionDays
// accept 0 (inherit) up to config.MaxRetentionDays and reject anything else.
func TestValidateRetentionDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plcDays  int
		tagDays  int
		wantErrs []string
	}{
		{name: "zero inherits", plcDays: 0, tagDays: 0},
		{name: "positive values", plcDays: 30, tagDays: 7},
		{name: "upper bound", plcDays: config.MaxRetentionDays, tagDays: config.MaxRetentionDays},
		{name: "negative plc", plcDays: -1, wantErrs: []string{"retentionDays: must be between 0 and 36500, got -1"}},
		{name: "negative tag", tagDays: -5, wantErrs: []string{"tags[0].retentionDays: must be between 0 and 36500, got -5"}},
		{
			name: "both too large", plcDays: 36501, tagDays: 100000,
			wantErrs: []string{
				"retentionDays: must be between 0 and 36500, got 36501",
				"tags[0].retentionDays: must be between 0 and 36500, got 100000",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := config.PLC{Name: "p1", Address: "10.0.0.1", RetentionDays: tc.plcDays,
				Tags: []config.TagDef{{Name: "Speed", Type: "Float", RetentionDays: tc.tagDays}}}
			err := config.ValidatePLC(p)
			if len(tc.wantErrs) == 0 {
				if err != nil {
					t.Fatalf("ValidatePLC: unexpected error %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePLC: expected errors %q, got nil", tc.wantErrs)
			}
			if !errors.Is(err, config.ErrConfigInvalid) {
				t.Errorf("error does not wrap ErrConfigInvalid: %v", err)
			}
			for _, want := range tc.wantErrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err.Error(), want)
				}
			}
		})
	}
}

// TestConfigValidateRetentionDays verifies #85: the global default is bounded
// too, and per-PLC violations are reported with their plcs[i] path.
func TestConfigValidateRetentionDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr string
	}{
		{name: "global negative", mutate: func(c *config.Config) { c.Historian.RetentionDays = -1 },
			wantErr: "historian.retentionDays: must be a positive integer, got -1"},
		{name: "global too large", mutate: func(c *config.Config) { c.Historian.RetentionDays = 36501 },
			wantErr: "historian.retentionDays: must be at most 36500, got 36501"},
		{name: "global upper bound ok", mutate: func(c *config.Config) { c.Historian.RetentionDays = 36500 }},
		{name: "plc path prefixed", mutate: func(c *config.Config) {
			c.PLCs = []config.PLC{{Name: "p1", Address: "10.0.0.1",
				Tags: []config.TagDef{{Name: "Speed", Type: "Float", RetentionDays: -2}}}}
		}, wantErr: "plcs[0].tags[0].retentionDays: must be between 0 and 36500, got -2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig(t)
			tc.mutate(c)
			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate error = %v; want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestLoadRetentionDays verifies #85: retentionDays is read from YAML on PLCs
// and tags, and is 0 (inherit) when omitted.
func TestLoadRetentionDays(t *testing.T) {
	t.Parallel()

	y := filepath.Join(t.TempDir(), "lgb.yaml")
	content := `plcs:
  - name: p1
    address: 10.0.0.1
    retentionDays: 30
    tags:
      - name: Motor.Speed
        type: Float
        retentionDays: 7
      - name: Motor.Running
        type: Boolean
`
	if err := os.WriteFile(y, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(y)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cfg.PLCs[0]
	if p.RetentionDays != 30 || p.Tags[0].RetentionDays != 7 || p.Tags[1].RetentionDays != 0 {
		t.Errorf("retentionDays = plc %d, tags %d/%d; want 30, 7/0", p.RetentionDays, p.Tags[0].RetentionDays, p.Tags[1].RetentionDays)
	}
	if cfg.Historian.RetentionDays != 90 {
		t.Errorf("global retentionDays = %d; want default 90", cfg.Historian.RetentionDays)
	}
}
