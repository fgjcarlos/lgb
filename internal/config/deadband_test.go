package config_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

func TestValidatePLCDeadband(t *testing.T) {
	t.Parallel()

	plcWith := func(tag config.TagDef) config.PLC {
		return config.PLC{Name: "p1", Address: "10.0.0.1", Tags: []config.TagDef{tag}}
	}
	tests := []struct {
		name    string
		tag     config.TagDef
		wantErr string // "" = valid
	}{
		{"no deadband is valid", config.TagDef{Name: "T", Type: "Float"}, ""},
		{"absolute on numeric is valid", config.TagDef{Name: "T", Type: "Int32", Deadband: config.Deadband{Absolute: 2}}, ""},
		{"percent on numeric is valid", config.TagDef{Name: "T", Type: "Double", Deadband: config.Deadband{Percent: 100}}, ""},
		{"both on numeric is valid", config.TagDef{Name: "T", Type: "Float", Deadband: config.Deadband{Absolute: 0.5, Percent: 1}}, ""},
		{"negative absolute", config.TagDef{Name: "T", Type: "Float", Deadband: config.Deadband{Absolute: -1}},
			"tags[0].deadband.absolute: must be a finite number >= 0, got -1"},
		{"NaN absolute", config.TagDef{Name: "T", Type: "Float", Deadband: config.Deadband{Absolute: math.NaN()}},
			"tags[0].deadband.absolute: must be a finite number >= 0, got NaN"},
		{"infinite absolute", config.TagDef{Name: "T", Type: "Float", Deadband: config.Deadband{Absolute: math.Inf(1)}},
			"tags[0].deadband.absolute: must be a finite number >= 0, got +Inf"},
		{"negative percent", config.TagDef{Name: "T", Type: "Float", Deadband: config.Deadband{Percent: -0.5}},
			"tags[0].deadband.percent: must be between 0 and 100, got -0.5"},
		{"percent above 100", config.TagDef{Name: "T", Type: "Float", Deadband: config.Deadband{Percent: 101}},
			"tags[0].deadband.percent: must be between 0 and 100, got 101"},
		{"deadband on Boolean", config.TagDef{Name: "T", Type: "Boolean", Deadband: config.Deadband{Absolute: 1}},
			`tags[0].deadband: only numeric tag types support a deadband, got "Boolean"`},
		{"deadband on String", config.TagDef{Name: "T", Type: "String", Deadband: config.Deadband{Percent: 1}},
			`tags[0].deadband: only numeric tag types support a deadband, got "String"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := config.ValidatePLC(plcWith(tc.tag))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePLC() = %v; want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePLC() = nil; want %q", tc.wantErr)
			}
			if !errors.Is(err, errs.ErrConfigInvalid) {
				t.Errorf("errors.Is(err, ErrConfigInvalid) = false; got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadDeadbandAndChangeOnValue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	y := filepath.Join(dir, "lgb.yaml")
	content := `plcs:
  - name: p1
    address: 10.0.0.1
    tags:
      - name: Motor.Speed
        type: Float
        deadband:
          absolute: 0.5
          percent: 2
      - name: Motor.Running
        type: Boolean
`
	if err := os.WriteFile(y, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(y)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.PLCs[0].Tags[0].Deadband, (config.Deadband{Absolute: 0.5, Percent: 2}); got != want {
		t.Errorf("deadband = %+v, want %+v", got, want)
	}
	if got := cfg.PLCs[0].Tags[1].Deadband; got != (config.Deadband{}) {
		t.Errorf("unset deadband = %+v, want zero", got)
	}
	want := config.ChangeOnValueSection{Sparkplug: true, Realtime: true, Historian: false}
	if cfg.ChangeOnValue != want {
		t.Errorf("changeOnValue defaults = %+v, want %+v", cfg.ChangeOnValue, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadChangeOnValueOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	y := filepath.Join(dir, "lgb.yaml")
	content := `changeOnValue:
  sparkplug: false
  realtime: false
  historian: true
`
	if err := os.WriteFile(y, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(y)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.ChangeOnValueSection{Sparkplug: false, Realtime: false, Historian: true}
	if cfg.ChangeOnValue != want {
		t.Errorf("changeOnValue = %+v, want %+v", cfg.ChangeOnValue, want)
	}
}
