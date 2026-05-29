// Package config_test validates the extracted ValidatePLC function.
// Requirements: PCS-STORE-1.7, PLC-CFG-1.1.
package config_test

import (
	"errors"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// TestValidatePLC is a table-driven test covering every ValidatePLC rule.
func TestValidatePLC(t *testing.T) {
	t.Parallel()

	validTag := config.TagDef{Name: "Motor.Speed", Type: "Float"}

	tests := []struct {
		name    string
		plc     config.PLC
		wantErr bool
		contain string // substring that must appear in the error message, if wantErr
	}{
		{
			name:    "all-valid returns nil",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", Slot: 0, SocketTimeout: "5s", ScanRate: "1s", Tags: []config.TagDef{validTag}},
			wantErr: false,
		},
		{
			name:    "empty address",
			plc:     config.PLC{Name: "p1", Address: ""},
			wantErr: true,
			contain: "address",
		},
		{
			name:    "invalid scanRate",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", ScanRate: "not-a-duration"},
			wantErr: true,
			contain: "scanRate",
		},
		{
			name:    "zero scanRate",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", ScanRate: "0s"},
			wantErr: true,
			contain: "scanRate",
		},
		{
			name:    "invalid socketTimeout",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", SocketTimeout: "bad"},
			wantErr: true,
			contain: "socketTimeout",
		},
		{
			name:    "negative socketTimeout",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", SocketTimeout: "-1s"},
			wantErr: true,
			contain: "socketTimeout",
		},
		{
			name:    "slot below zero",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", Slot: -1},
			wantErr: true,
			contain: "slot",
		},
		{
			name:    "slot above 15",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", Slot: 16},
			wantErr: true,
			contain: "slot",
		},
		{
			name:    "slot exactly 15 is valid",
			plc:     config.PLC{Name: "p1", Address: "10.0.0.1", Slot: 15},
			wantErr: false,
		},
		{
			name: "empty tag name",
			plc: config.PLC{Name: "p1", Address: "10.0.0.1", Tags: []config.TagDef{
				{Name: "", Type: "Float"},
			}},
			wantErr: true,
			contain: "name",
		},
		{
			name: "empty tag type",
			plc: config.PLC{Name: "p1", Address: "10.0.0.1", Tags: []config.TagDef{
				{Name: "Tag1", Type: ""},
			}},
			wantErr: true,
			contain: "type",
		},
		{
			name: "invalid tag type non-Sparkplug scalar",
			plc: config.PLC{Name: "p1", Address: "10.0.0.1", Tags: []config.TagDef{
				{Name: "Tag1", Type: "UDT"},
			}},
			wantErr: true,
			contain: "UDT",
		},
		{
			name: "writable:true has no validation error",
			plc: config.PLC{Name: "p1", Address: "10.0.0.1", Tags: []config.TagDef{
				{Name: "Tag1", Type: "Float", Writable: true},
			}},
			wantErr: false,
		},
		{
			name: "multiple violations aggregated via errors.Join",
			plc: config.PLC{Name: "p1", Address: "", Slot: 16, Tags: []config.TagDef{
				{Name: "", Type: "Bad"},
			}},
			wantErr: true,
			contain: "address", // at least one violation must mention address
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := config.ValidatePLC(tc.plc)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidatePLC() = nil; want error")
				}
				if !errors.Is(err, errs.ErrConfigInvalid) {
					t.Errorf("errors.Is(err, ErrConfigInvalid) = false; got %v", err)
				}
				if tc.contain != "" && !contains(err.Error(), tc.contain) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.contain)
				}
			} else {
				if err != nil {
					t.Fatalf("ValidatePLC() = %v; want nil", err)
				}
			}
		})
	}
}

// TestValidatePLC_MultipleViolationsJoined verifies that address+slot violations
// are both present in a single error value.
func TestValidatePLC_MultipleViolationsJoined(t *testing.T) {
	t.Parallel()
	plc := config.PLC{Name: "p1", Address: "", Slot: 20}
	err := config.ValidatePLC(plc)
	if err == nil {
		t.Fatal("ValidatePLC() = nil; want error")
	}
	msg := err.Error()
	if !contains(msg, "address") {
		t.Errorf("error missing 'address' violation; got %q", msg)
	}
	if !contains(msg, "slot") {
		t.Errorf("error missing 'slot' violation; got %q", msg)
	}
}

// contains is a simple substring check used in table tests.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && findStr(s, sub)
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
