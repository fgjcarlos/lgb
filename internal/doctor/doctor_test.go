// doctor_test.go — tests for the doctor registry and runner.
//
// Requirements: MVP-FND-8.1, MVP-FND-8.3, MVP-FND-8.6. Design: §10, §4.3, §20.4.
package doctor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// fakeCheck is a test double implementing the Check interface.
type fakeCheck struct {
	name   string
	result Result
}

func (f *fakeCheck) Name() string { return f.name }
func (f *fakeCheck) Run(_ context.Context) Result {
	f.result.Name = f.name
	return f.result
}

// panicCheck is a test double that panics during Run.
type panicCheck struct {
	name string
}

func (p *panicCheck) Name() string { return p.name }
func (p *panicCheck) Run(_ context.Context) Result {
	panic("deliberate panic from panicCheck")
}

// spyCheck counts how many times it was invoked (to verify parallel execution).
type spyCheck struct {
	name      string
	callCount atomic.Int64
}

func (s *spyCheck) Name() string { return s.name }
func (s *spyCheck) Run(_ context.Context) Result {
	s.callCount.Add(1)
	return Result{Name: s.name, Status: StatusPass, Message: "ok"}
}

// TestRun_ThreeChecksReturnThreeResults verifies that 3 registered checks →
// 3 results. (MVP-FND-8.1)
func TestRun_ThreeChecksReturnThreeResults(t *testing.T) {
	reg := &Registry{}
	for i := 0; i < 3; i++ {
		reg.Register(&fakeCheck{
			name:   fmt.Sprintf("check-%d", i),
			result: Result{Status: StatusPass, Message: "ok"},
		})
	}

	results := Run(context.Background(), reg)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

// TestRun_ParallelExecution verifies that all checks are actually called.
func TestRun_ParallelExecution(t *testing.T) {
	spies := []*spyCheck{
		{name: "spy-1"},
		{name: "spy-2"},
		{name: "spy-3"},
	}
	reg := &Registry{}
	for _, s := range spies {
		reg.Register(s)
	}

	Run(context.Background(), reg)

	for _, s := range spies {
		if s.callCount.Load() == 0 {
			t.Errorf("spy %q: Run was not called", s.name)
		}
	}
}

// TestRun_PanicRecoveredToFail verifies that a panicking check is recovered into
// a FAIL result without affecting other checks. (MVP-FND-8.6)
func TestRun_PanicRecoveredToFail(t *testing.T) {
	reg := &Registry{}
	reg.Register(&panicCheck{name: "panicking"})
	reg.Register(&fakeCheck{name: "healthy", result: Result{Status: StatusPass, Message: "ok"}})

	results := Run(context.Background(), reg)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	var panicResult, healthyResult Result
	for _, r := range results {
		switch r.Name {
		case "panicking":
			panicResult = r
		case "healthy":
			healthyResult = r
		}
	}

	if panicResult.Status != StatusFail {
		t.Errorf("expected panicking check to have StatusFail, got %v", panicResult.Status)
	}
	if healthyResult.Status != StatusPass {
		t.Errorf("expected healthy check to have StatusPass, got %v", healthyResult.Status)
	}
}

// TestExitCode_Table verifies the exit code rules per MVP-FND-8.3.
func TestExitCode_Table(t *testing.T) {
	cases := []struct {
		name     string
		results  []Result
		wantCode int
	}{
		{
			name:     "all_pass",
			results:  []Result{{Status: StatusPass}, {Status: StatusInfo}},
			wantCode: 0,
		},
		{
			name:     "warn_only",
			results:  []Result{{Status: StatusPass}, {Status: StatusWarn}},
			wantCode: 0,
		},
		{
			name:     "any_fail",
			results:  []Result{{Status: StatusPass}, {Status: StatusFail}},
			wantCode: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := ExitCodeFromResults(tc.results)
			if code != tc.wantCode {
				t.Errorf("expected exit code %d, got %d", tc.wantCode, code)
			}
		})
	}
}
