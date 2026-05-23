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

// TestPLCSentinelsAreDistinctNonNil asserts PLC-ERR-1.1: all four PLC
// sentinels are distinct, non-nil, and not equal to any existing sentinel.
func TestPLCSentinelsAreDistinctNonNil(t *testing.T) {
	plcSentinels := []struct {
		name string
		err  error
	}{
		{"ErrPLCConnect", errs.ErrPLCConnect},
		{"ErrPLCRead", errs.ErrPLCRead},
		{"ErrPLCWrite", errs.ErrPLCWrite},
		{"ErrPLCTimeout", errs.ErrPLCTimeout},
	}

	// Each PLC sentinel must be non-nil.
	for _, s := range plcSentinels {
		s := s
		t.Run(s.name+"_not_nil", func(t *testing.T) {
			if s.err == nil {
				t.Errorf("%s is nil; want non-nil sentinel", s.name)
			}
		})
	}

	// All PLC sentinels must be distinct from each other.
	for i, a := range plcSentinels {
		for j, b := range plcSentinels {
			if i >= j {
				continue
			}
			if errors.Is(a.err, b.err) {
				t.Errorf("%s and %s are not distinct (errors.Is returned true)", a.name, b.name)
			}
		}
	}

	// No PLC sentinel may equal any existing (non-PLC) sentinel.
	existingSentinels := []struct {
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
	for _, plcS := range plcSentinels {
		for _, existS := range existingSentinels {
			if errors.Is(plcS.err, existS.err) {
				t.Errorf("%s equals existing sentinel %s; they must be distinct", plcS.name, existS.name)
			}
		}
	}
}

// TestPLCSentinelsWrapping asserts PLC-ERR-1.1: errors.Is traversal works
// for wrapped PLC sentinels, and wrapping one does not match another.
func TestPLCSentinelsWrapping(t *testing.T) {
	pairs := []struct {
		name    string
		target  error
		other   error
	}{
		{"ErrPLCConnect", errs.ErrPLCConnect, errs.ErrPLCRead},
		{"ErrPLCRead", errs.ErrPLCRead, errs.ErrPLCWrite},
		{"ErrPLCWrite", errs.ErrPLCWrite, errs.ErrPLCTimeout},
		{"ErrPLCTimeout", errs.ErrPLCTimeout, errs.ErrPLCConnect},
	}

	for _, p := range pairs {
		p := p
		t.Run(p.name+"_wrapped_is_detectable", func(t *testing.T) {
			wrapped := fmt_errorf_helper(p.target)
			if !errors.Is(wrapped, p.target) {
				t.Errorf("errors.Is(wrapped, %s) = false; want true", p.name)
			}
			if errors.Is(wrapped, p.other) {
				t.Errorf("errors.Is(wrapped %s, %s) = true; want false (sentinels must differ)", p.name, "other")
			}
		})
	}
}

// TestMQTTSparkplugSentinelsAreDistinctNonNil asserts SPK-ERR-4.1: all four
// MQTT/Sparkplug sentinels are distinct, non-nil, and not equal to any
// existing sentinel.
func TestMQTTSparkplugSentinelsAreDistinctNonNil(t *testing.T) {
	newSentinels := []struct {
		name string
		err  error
	}{
		{"ErrMQTTConnect", errs.ErrMQTTConnect},
		{"ErrMQTTPublish", errs.ErrMQTTPublish},
		{"ErrMQTTSubscribe", errs.ErrMQTTSubscribe},
		{"ErrSparkplugEncode", errs.ErrSparkplugEncode},
	}

	for _, s := range newSentinels {
		s := s
		t.Run(s.name+"_not_nil", func(t *testing.T) {
			if s.err == nil {
				t.Errorf("%s is nil; want non-nil sentinel", s.name)
			}
		})
	}

	for i, a := range newSentinels {
		for j, b := range newSentinels {
			if i >= j {
				continue
			}
			if errors.Is(a.err, b.err) {
				t.Errorf("%s and %s are not distinct (errors.Is returned true)", a.name, b.name)
			}
		}
	}

	existingSentinels := []struct {
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
		{"ErrPLCConnect", errs.ErrPLCConnect},
		{"ErrPLCRead", errs.ErrPLCRead},
		{"ErrPLCWrite", errs.ErrPLCWrite},
		{"ErrPLCTimeout", errs.ErrPLCTimeout},
	}
	for _, newS := range newSentinels {
		for _, existS := range existingSentinels {
			if errors.Is(newS.err, existS.err) {
				t.Errorf("%s equals existing sentinel %s; they must be distinct", newS.name, existS.name)
			}
		}
	}
}

// TestMQTTSparkplugSentinelsWrapping asserts SPK-ERR-4.1: errors.Is traversal
// works for wrapped MQTT/Sparkplug sentinels.
func TestMQTTSparkplugSentinelsWrapping(t *testing.T) {
	pairs := []struct {
		name   string
		target error
		other  error
	}{
		{"ErrMQTTConnect", errs.ErrMQTTConnect, errs.ErrMQTTPublish},
		{"ErrMQTTPublish", errs.ErrMQTTPublish, errs.ErrMQTTSubscribe},
		{"ErrMQTTSubscribe", errs.ErrMQTTSubscribe, errs.ErrSparkplugEncode},
		{"ErrSparkplugEncode", errs.ErrSparkplugEncode, errs.ErrMQTTConnect},
	}

	for _, p := range pairs {
		p := p
		t.Run(p.name+"_wrapped_is_detectable", func(t *testing.T) {
			wrapped := fmt_errorf_helper(p.target)
			if !errors.Is(wrapped, p.target) {
				t.Errorf("errors.Is(wrapped, %s) = false; want true", p.name)
			}
			if errors.Is(wrapped, p.other) {
				t.Errorf("errors.Is(wrapped %s, other) = true; want false", p.name)
			}
		})
	}
}

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
