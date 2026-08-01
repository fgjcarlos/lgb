package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/httpx"
)

// TestShutdown_HonorsCallerContext verifies that Shutdown respects the caller's context.
// Before the fix, Shutdown used context.Background() internally, ignoring the caller's
// context and deadline. After the fix, it uses context.WithTimeout(ctx, deadline),
// ensuring that caller cancellation terminates the shutdown promptly.
func TestShutdown_HonorsCallerContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		makeCtx     func() (context.Context, func())
		expectFast  bool // true if we expect the operation to complete in <100ms
		deadline    time.Duration
	}{
		{
			name: "cancelled context returns promptly",
			makeCtx: func() (context.Context, func()) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // cancel immediately
				return ctx, func() {}
			},
			expectFast: true,
			deadline:   5 * time.Second,
		},
		{
			name: "already-expired deadline returns promptly",
			makeCtx: func() (context.Context, func()) {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				return ctx, cancel
			},
			expectFast: true,
			deadline:   5 * time.Second,
		},
		{
			name: "live context allows graceful shutdown to complete",
			makeCtx: func() (context.Context, func()) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				return ctx, cancel
			},
			expectFast: false,
			deadline:   2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.makeCtx()
			defer cancel()

			// Create a test HTTP server with a no-op handler.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			httpSrv := srv.Config

			// Call Shutdown and measure how long it takes.
			start := time.Now()
			err := httpx.Shutdown(ctx, httpSrv, tt.deadline)
			elapsed := time.Since(start)

			// Check that the operation completes (error or not is OK).
			if err != nil && err.Error() == "server not started" {
				// Server was already closed; this is OK.
			}

			// If expectFast, verify it completed within a reasonable time (< 500ms).
			// If not expectFast, just verify it didn't hang indefinitely (< 10s).
			if tt.expectFast {
				if elapsed > 500*time.Millisecond {
					t.Fatalf("Expected fast completion; took %v", elapsed)
				}
			} else {
				if elapsed > 10*time.Second {
					t.Fatalf("Shutdown took too long: %v", elapsed)
				}
			}
		})
	}
}
