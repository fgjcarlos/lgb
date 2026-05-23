package plc

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/danomagnum/gologix"
)

// timeoutErr is a fake net.Error that reports Timeout() == true.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestTranslateError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		err        error
		op         string
		tag        string
		wantSentinel error
	}{
		{
			name:         "CIPError on read op wraps ErrPLCRead",
			err:          &gologix.CIPError{Code: 0x08},
			op:           "read",
			tag:          "TestTag",
			wantSentinel: ErrPLCRead,
		},
		{
			name:         "CIPError on write op wraps ErrPLCWrite",
			err:          &gologix.CIPError{Code: 0x08},
			op:           "write",
			tag:          "TestTag",
			wantSentinel: ErrPLCWrite,
		},
		{
			name:         "net.OpError wraps ErrPLCConnect",
			err:          &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
			op:           "read",
			tag:          "TestTag",
			wantSentinel: ErrPLCConnect,
		},
		{
			name:         "io.EOF wraps ErrPLCConnect",
			err:          io.EOF,
			op:           "read",
			tag:          "TestTag",
			wantSentinel: ErrPLCConnect,
		},
		{
			name:         "timeout net.Error wraps ErrPLCTimeout",
			err:          timeoutErr{},
			op:           "read",
			tag:          "TestTag",
			wantSentinel: ErrPLCTimeout,
		},
		{
			name:         "nil returns nil",
			err:          nil,
			op:           "read",
			tag:          "TestTag",
			wantSentinel: nil,
		},
		{
			name:         "unknown error wraps ErrPLCRead (no panic)",
			err:          errors.New("some unexpected gologix error"),
			op:           "read",
			tag:          "TestTag",
			wantSentinel: ErrPLCRead,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := translateError(tc.err, tc.op, tc.tag)

			if tc.wantSentinel == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}

			if got == nil {
				t.Fatalf("expected non-nil error wrapping %v, got nil", tc.wantSentinel)
			}
			if !errors.Is(got, tc.wantSentinel) {
				t.Errorf("got %v, want error wrapping %v", got, tc.wantSentinel)
			}
		})
	}
}

// TestTranslateError_NotConnected verifies the "not connected" string sentinel
// maps to ErrPLCConnect.
func TestTranslateError_NotConnected(t *testing.T) {
	t.Parallel()
	err := errors.New("not connected and AutoConnect not enabled")
	got := translateError(err, "read", "Tag")
	if !errors.Is(got, ErrPLCConnect) {
		t.Errorf("expected ErrPLCConnect, got %v", got)
	}
}

// TestTranslateError_DeadlineExceeded verifies that a standard deadline-exceeded
// error that implements net.Error.Timeout() maps to ErrPLCTimeout.
func TestTranslateError_DeadlineExceeded(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen:", err)
	}
	defer ln.Close()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		t.Skip("cannot connect:", err)
	}
	defer conn.Close()
	// Set a past deadline to force a timeout error on the next read.
	_ = conn.SetDeadline(time.Now().Add(-time.Second))
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Skip("expected timeout error")
	}

	got := translateError(readErr, "read", "Tag")
	if !errors.Is(got, ErrPLCTimeout) {
		t.Errorf("expected ErrPLCTimeout for deadline exceeded, got %v", got)
	}
}
