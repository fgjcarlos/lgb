// doctor_test.go — tests for the doctor subcommand.
//
// Tests inject fake Check implementations via *Deps.DoctorRegistry to avoid
// real filesystem/network side-effects. Design §6.3.
// Requirements: MVP-FND-1.4, MVP-FND-8.3–8.5.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/doctor"
)

// fakePassCheck always returns PASS.
type fakePassCheck struct{ name string }

func (c *fakePassCheck) Name() string { return c.name }
func (c *fakePassCheck) Run(_ context.Context) doctor.Result {
	return doctor.Result{Name: c.name, Status: doctor.StatusPass, Message: "pass"}
}

// fakeFailCheck always returns FAIL.
type fakeFailCheck struct{ name string }

func (c *fakeFailCheck) Name() string { return c.name }
func (c *fakeFailCheck) Run(_ context.Context) doctor.Result {
	return doctor.Result{Name: c.name, Status: doctor.StatusFail, Message: "fail"}
}

// fakeWarnCheck always returns WARN.
type fakeWarnCheck struct{ name string }

func (c *fakeWarnCheck) Name() string { return c.name }
func (c *fakeWarnCheck) Run(_ context.Context) doctor.Result {
	return doctor.Result{Name: c.name, Status: doctor.StatusWarn, Message: "warn"}
}

// TestDoctorCmd_AllPassExits0 verifies that all-pass checks → exit 0 and
// stdout contains [PASS] entries. (MVP-FND-1.4, MVP-FND-8.3)
func TestDoctorCmd_AllPassExits0(t *testing.T) {
	reg := &doctor.Registry{}
	reg.Register(&fakePassCheck{name: "check-1"})
	reg.Register(&fakePassCheck{name: "check-2"})

	d := &Deps{DoctorRegistry: reg}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code, err := runDoctorTo(d, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "[PASS]") {
		t.Errorf("expected stdout to contain [PASS], got: %q", out)
	}
}

// TestDoctorCmd_FailCheckExits1 verifies that an injected FAIL → exit 1.
func TestDoctorCmd_FailCheckExits1(t *testing.T) {
	reg := &doctor.Registry{}
	reg.Register(&fakeFailCheck{name: "bad-check"})

	d := &Deps{DoctorRegistry: reg}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code, _ := runDoctorTo(d, stdout, stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// TestDoctorCmd_WarnOnlyExits0 verifies that warn-only checks → exit 0.
// (MVP-FND-8.3)
func TestDoctorCmd_WarnOnlyExits0(t *testing.T) {
	reg := &doctor.Registry{}
	reg.Register(&fakeWarnCheck{name: "warn-check"})

	d := &Deps{DoctorRegistry: reg}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code, _ := runDoctorTo(d, stdout, stderr)
	if code != 0 {
		t.Errorf("expected exit code 0 for warn-only, got %d", code)
	}
}

// TestDoctorCmd_JSONOutput verifies --json output is valid JSON with checks
// array and overall field. (MVP-FND-8.5)
func TestDoctorCmd_JSONOutput(t *testing.T) {
	reg := &doctor.Registry{}
	reg.Register(&fakePassCheck{name: "check-1"})
	reg.Register(&fakeWarnCheck{name: "check-2"})

	d := &Deps{JSON: true, DoctorRegistry: reg}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	_, err := runDoctorTo(d, stdout, stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out doctorOutput
	if jsonErr := json.Unmarshal(stdout.Bytes(), &out); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", jsonErr, stdout.String())
	}
	if len(out.Checks) == 0 {
		t.Error("expected non-empty checks array")
	}
	if out.Overall == "" {
		t.Error("expected non-empty overall field")
	}
}
