// Package config — per-PLC validation extracted from Config.Validate.
// Requirements: PCS-STORE-1.7, PLC-CFG-1.1.
package config

import (
	"time"

	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// ValidatePLC validates a single PLC entry against schema constraints.
// It returns a joined error (via errors.Join) so ALL violations are reported
// at once, with each constituent wrapping ErrConfigInvalid so errors.Is works.
// Returns nil when the entry is valid.
//
// Rules enforced:
//   - address must not be empty (PLC-CFG-1.2)
//   - socketTimeout must be a valid positive duration when non-empty (PLC-CFG-1.3/1.4)
//   - scanRate must be a valid positive duration when non-empty (PLC-CFG-1.5)
//   - slot must be in [0, 15] (PLC-CFG-1.6)
//   - each tag: name and type must be non-empty; type must be a Sparkplug scalar (SPK-CFG-2.6/2.7)
//   - tag Writable is accepted without validation (PCS-CFG-5.1 — stored, not enforced)
func ValidatePLC(p PLC) error {
	var violations []error

	if p.Address == "" {
		violations = append(violations, errorf("address: must not be empty: %w", ErrConfigInvalid))
	}

	if p.SocketTimeout != "" {
		d, err := time.ParseDuration(p.SocketTimeout)
		if err != nil {
			violations = append(violations, errorf("socketTimeout: %q is not a valid Go duration: %w", p.SocketTimeout, ErrConfigInvalid))
		} else if d <= 0 {
			violations = append(violations, errorf("socketTimeout: must be positive, got %q: %w", p.SocketTimeout, ErrConfigInvalid))
		}
	}

	if p.ScanRate != "" {
		d, err := time.ParseDuration(p.ScanRate)
		if err != nil {
			violations = append(violations, errorf("scanRate: %q is not a valid Go duration: %w", p.ScanRate, ErrConfigInvalid))
		} else if d <= 0 {
			violations = append(violations, errorf("scanRate: must be positive, got %q: %w", p.ScanRate, ErrConfigInvalid))
		}
	}

	if p.Slot < 0 || p.Slot > 15 {
		violations = append(violations, errorf("slot: must be between 0 and 15, got %d: %w", p.Slot, ErrConfigInvalid))
	}

	for j, tag := range p.Tags {
		if tag.Name == "" {
			violations = append(violations, errorf("tags[%d].name: must not be empty: %w", j, ErrConfigInvalid))
		}
		if tag.Type == "" {
			violations = append(violations, errorf("tags[%d].type: must not be empty: %w", j, ErrConfigInvalid))
		} else if !validSparkplugType(tag.Type) {
			violations = append(violations, errorf("tags[%d].type: %q is not a supported Sparkplug B scalar type: %w", j, tag.Type, ErrConfigInvalid))
		}
		// tag.Writable is accepted without validation (PCS-CFG-5.1).
	}

	return errs.Join(violations...)
}

// prefixPLCViolations re-wraps each constituent of err (from ValidatePLC) with
// "plcs[i]." prepended, so Config.Validate produces messages like
// "plcs[0].address: must not be empty". Returns nil slice when err is nil.
func prefixPLCViolations(i int, err error) []error {
	if err == nil {
		return nil
	}
	// Unwrap the joined error into its constituent parts.
	type joinErr interface{ Unwrap() []error }
	if je, ok := err.(joinErr); ok {
		inner := je.Unwrap()
		out := make([]error, 0, len(inner))
		for _, e := range inner {
			out = append(out, errorf("plcs[%d].%w", i, e))
		}
		return out
	}
	// Single-error case (no Join was needed).
	return []error{errorf("plcs[%d].%w", i, err)}
}
