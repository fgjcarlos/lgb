package mqtt

import (
	"context"
	"errors"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// stubToken is a paho.Token whose completion the test controls.
type stubToken struct {
	done chan struct{}
	err  error
}

var _ paho.Token = (*stubToken)(nil)

func newStubToken(err error) *stubToken {
	return &stubToken{done: make(chan struct{}), err: err}
}

func (s *stubToken) complete() { close(s.done) }

func (s *stubToken) Wait() bool {
	<-s.done
	return true
}

func (s *stubToken) WaitTimeout(d time.Duration) bool {
	select {
	case <-s.done:
		return true
	case <-time.After(d):
		return false
	}
}

func (s *stubToken) Done() <-chan struct{} { return s.done }

func (s *stubToken) Error() error { return s.err }

// TestWaitToken_ReturnsTokenError asserts that a failed token surfaces its
// error to the caller.
func TestWaitToken_ReturnsTokenError(t *testing.T) {
	t.Parallel()

	want := errors.New("broker rejected the packet")
	token := newStubToken(want)
	token.complete()

	if err := waitToken(context.Background(), token); !errors.Is(err, want) {
		t.Fatalf("waitToken returned %v; want %v", err, want)
	}
}

// TestWaitToken_ReturnsNilOnSuccess asserts that a completed token without an
// error is reported as success.
func TestWaitToken_ReturnsNilOnSuccess(t *testing.T) {
	t.Parallel()

	token := newStubToken(nil)
	token.complete()

	if err := waitToken(context.Background(), token); err != nil {
		t.Fatalf("waitToken returned %v; want nil", err)
	}
}

// TestWaitToken_ReturnsContextErrorWhenCancelled asserts that waitToken gives
// up when the caller's context is cancelled, instead of blocking until the
// broker answers. This is the bound the helper documents: the caller is never
// held past its own context, even when the token never completes.
func TestWaitToken_ReturnsContextErrorWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	token := newStubToken(nil) // never completes

	err := waitToken(ctx, token)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitToken returned %v; want context.Canceled", err)
	}
}

// TestWaitToken_ReturnsDeadlineExceeded asserts that an expired deadline also
// releases the caller.
func TestWaitToken_ReturnsDeadlineExceeded(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	token := newStubToken(nil) // never completes

	err := waitToken(ctx, token)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitToken returned %v; want context.DeadlineExceeded", err)
	}
}
