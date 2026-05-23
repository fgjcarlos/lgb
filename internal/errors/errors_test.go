// Package errors defines project-wide sentinel errors and multi-error helpers.
// Tests assert the requirements defined in MVP-FND-5.1 and MVP-FND-5.3.
package errors_test

import (
	"errors"
	"testing"

	errs "github.com/fgjcarlos/lgb/internal/errors"
)

// TestSentinelsAreDistinctNonNil asserts MVP-FND-5.1: all seven sentinels are
// distinct non-nil error values.
func TestSentinelsAreDistinctNonNil(t *testing.T) {
	t.Helper()

	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrConfigInvalid", errs.ErrConfigInvalid},
		{"ErrConfigMissing", errs.ErrConfigMissing},
		{"ErrConfigPermission", errs.ErrConfigPermission},
		{"ErrDataDirInvalid", errs.ErrDataDirInvalid},
		{"ErrDataDirPermission", errs.ErrDataDirPermission},
		{"ErrCheckFailed", errs.ErrCheckFailed},
		{"ErrMaxAttempts", errs.ErrMaxAttempts},
	}

	for _, s := range sentinels {
		s := s
		t.Run(s.name+"_not_nil", func(t *testing.T) {
			if s.err == nil {
				t.Errorf("%s is nil; want non-nil sentinel", s.name)
			}
		})
	}

	// Assert all are distinct — no two sentinels should be the same value.
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i >= j {
				continue
			}
			if errors.Is(a.err, b.err) {
				t.Errorf("%s and %s are not distinct (errors.Is returned true)", a.name, b.name)
			}
		}
	}
}

// TestErrorsIsWrapping asserts MVP-FND-5.1: errors.Is traversal works for wrapped sentinels.
func TestErrorsIsWrapping(t *testing.T) {
	t.Run("wrapped ErrConfigInvalid is detectable", func(t *testing.T) {
		wrapped := fmt_errorf_helper(errs.ErrConfigInvalid)
		if !errors.Is(wrapped, errs.ErrConfigInvalid) {
			t.Error("errors.Is(wrapped, ErrConfigInvalid) returned false; want true")
		}
		if errors.Is(wrapped, errs.ErrConfigMissing) {
			t.Error("errors.Is(wrapped, ErrConfigMissing) returned true; want false")
		}
	})

	t.Run("wrapped ErrMaxAttempts is detectable", func(t *testing.T) {
		wrapped := fmt_errorf_helper(errs.ErrMaxAttempts)
		if !errors.Is(wrapped, errs.ErrMaxAttempts) {
			t.Error("errors.Is(wrapped, ErrMaxAttempts) returned false; want true")
		}
	})
}

// fmt_errorf_helper wraps an error the way production code would.
func fmt_errorf_helper(sentinel error) error {
	return &wrappedErr{inner: sentinel}
}

type wrappedErr struct{ inner error }

func (e *wrappedErr) Error() string { return "wrapped: " + e.inner.Error() }
func (e *wrappedErr) Unwrap() error { return e.inner }

// TestJoinPreservesConstituents asserts MVP-FND-5.3: Join preserves each
// constituent error so that errors.Is works on the joined result.
func TestJoinPreservesConstituents(t *testing.T) {
	t.Run("joined error satisfies errors.Is for each constituent", func(t *testing.T) {
		joined := errs.Join(errs.ErrConfigInvalid, errs.ErrConfigMissing)
		if joined == nil {
			t.Fatal("Join returned nil; want non-nil error")
		}

		if !errors.Is(joined, errs.ErrConfigInvalid) {
			t.Error("errors.Is(joined, ErrConfigInvalid) returned false; want true")
		}
		if !errors.Is(joined, errs.ErrConfigMissing) {
			t.Error("errors.Is(joined, ErrConfigMissing) returned false; want true")
		}
	})

	t.Run("Join with single error returns non-nil", func(t *testing.T) {
		joined := errs.Join(errs.ErrDataDirInvalid)
		if joined == nil {
			t.Fatal("Join(single) returned nil; want non-nil")
		}
		if !errors.Is(joined, errs.ErrDataDirInvalid) {
			t.Error("errors.Is for single-element join failed")
		}
	})

	t.Run("Join with nil-only inputs returns nil", func(t *testing.T) {
		joined := errs.Join(nil, nil)
		if joined != nil {
			t.Errorf("Join(nil, nil) = %v; want nil", joined)
		}
	})
}
