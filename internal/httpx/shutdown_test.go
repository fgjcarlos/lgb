package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/httpx"
)

// busyServer is an HTTP server with one in-flight request whose handler blocks
// until release is called. A server without in-flight work shuts down
// immediately, whatever the caller's context says, so the caller's context can
// only be observed while a request is still running.
type busyServer struct {
	*httptest.Server
	release func()
}

// newBusyServer starts a server and waits until its handler is running.
func newBusyServer(t *testing.T) *busyServer {
	t.Helper()

	started := make(chan struct{})
	releaseCh := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(releaseCh) }) }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-releaseCh
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() {
		release()
		srv.Close()
	})

	go func() {
		resp, err := http.Get(srv.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the handler")
	}

	return &busyServer{Server: srv, release: release}
}

// TestShutdown_HonorsCallerContext verifies that Shutdown respects the caller's
// context. Before the fix it derived its deadline from context.Background(),
// so a cancelled caller context was ignored and the shutdown only finished
// when its own deadline expired.
func TestShutdown_HonorsCallerContext(t *testing.T) {
	t.Parallel()

	t.Run("cancelled context releases the caller", func(t *testing.T) {
		srv := newBusyServer(t)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		err := httpx.Shutdown(ctx, srv.Config, 5*time.Second)
		elapsed := time.Since(start)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Shutdown returned %v; want context.Canceled", err)
		}
		if elapsed > 500*time.Millisecond {
			t.Fatalf("Shutdown took %v; want it to return as soon as the context is cancelled", elapsed)
		}
	})

	t.Run("expired deadline releases the caller", func(t *testing.T) {
		srv := newBusyServer(t)

		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		start := time.Now()
		err := httpx.Shutdown(ctx, srv.Config, 5*time.Second)
		elapsed := time.Since(start)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown returned %v; want context.DeadlineExceeded", err)
		}
		if elapsed > 500*time.Millisecond {
			t.Fatalf("Shutdown took %v; want it to return as soon as the deadline expires", elapsed)
		}
	})

	t.Run("live context waits for the in-flight request", func(t *testing.T) {
		srv := newBusyServer(t)
		srv.release() // let the handler answer

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := httpx.Shutdown(ctx, srv.Config, 5*time.Second); err != nil {
			t.Fatalf("Shutdown returned %v; want nil", err)
		}
	})
}
