// Package doctor provides the diagnostic check registry and runner for the LGB gateway.
//
// Each check is a small struct implementing the Check interface. Checks are run
// concurrently via errgroup; panics are recovered into FAIL results.
//
// Requirements: MVP-FND-8.1–8.6. Design: §10, §4.3.
package doctor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

// CheckStatus represents the severity of a diagnostic check result.
type CheckStatus int

const (
	// StatusInfo is informational — not a problem, just context.
	StatusInfo CheckStatus = iota
	// StatusPass means the check passed with no issues.
	StatusPass
	// StatusWarn means the check found something potentially problematic.
	// WARN does not change the exit code from 0 (MVP-FND-8.3).
	StatusWarn
	// StatusFail means the check found a definite problem. Exit code 1.
	StatusFail
)

// String returns the lowercase string representation used in JSON output.
func (s CheckStatus) String() string {
	switch s {
	case StatusInfo:
		return "info"
	case StatusPass:
		return "pass"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	default:
		return "unknown"
	}
}

// Result holds the outcome of a single diagnostic check.
type Result struct {
	Name    string
	Status  CheckStatus
	Message string
	Took    time.Duration
}

// Check is the interface that every diagnostic check must implement.
// Implementations MUST NOT panic — but the runner recovers panics as a safety net.
type Check interface {
	// Name returns the check's unique identifier (kebab-case, e.g. "data-dir-writable").
	Name() string
	// Run executes the check and returns a Result. Context should be respected for
	// long-running checks.
	Run(ctx context.Context) Result
}

// Registry holds a set of registered checks.
type Registry struct {
	mu     sync.Mutex
	checks []Check
}

// Register adds a check to the registry.
func (r *Registry) Register(c Check) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks = append(r.checks, c)
}

// Checks returns a snapshot of all registered checks.
func (r *Registry) Checks() []Check {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Check, len(r.checks))
	copy(out, r.checks)
	return out
}

// Run executes all registered checks concurrently using goroutines and a WaitGroup.
// Individual panics are recovered into StatusFail results so one panicking check
// cannot prevent others from running or crash the process.
//
// Results are returned in registration order.
func Run(ctx context.Context, r *Registry) []Result {
	checks := r.Checks()
	results := make([]Result, len(checks))

	var wg sync.WaitGroup
	wg.Add(len(checks))
	for i, c := range checks {
		i, c := i, c // capture
		go func() {
			defer wg.Done()
			results[i] = runSafe(ctx, c)
		}()
	}
	wg.Wait()
	return results
}

// runSafe runs a single check and recovers any panic into a FAIL result.
func runSafe(ctx context.Context, c Check) (r Result) {
	defer func() {
		if p := recover(); p != nil {
			r = Result{
				Name:    c.Name(),
				Status:  StatusFail,
				Message: fmt.Sprintf("panic: %v", p),
			}
		}
	}()
	start := time.Now()
	r = c.Run(ctx)
	r.Took = time.Since(start)
	return r
}

// ExitCodeFromResults returns the exit code determined by the worst result.
//
// Per spec MVP-FND-8.3:
//   - 0 — all Info/Pass, or any Warn (no Fail)
//   - 1 — any Fail
func ExitCodeFromResults(results []Result) int {
	for _, r := range results {
		if r.Status == StatusFail {
			return 1
		}
	}
	return 0
}

// Default returns a *Registry pre-populated with the 5 Phase-0 checks plus
// one plcReachableCheck per configured PLC (PLC-DOC-1.5).
// cfg is used to resolve the server.httpAddr and gateway.dataDir.
func Default(cfg *config.Config) *Registry {
	r := &Registry{}
	r.Register(&dataDirCheck{cfg: cfg})
	r.Register(&resticCheck{})
	r.Register(&goRuntimeCheck{})
	r.Register(&portCheck{cfg: cfg})
	r.Register(&configLoadedCheck{})

	// Register one TCP-reachability probe per configured PLC.
	// If no PLCs are configured the count stays at 5 (PLC-DOC-1.5).
	for _, plc := range cfg.PLCs {
		r.Register(&plcReachableCheck{plc: plc})
	}

	return r
}
