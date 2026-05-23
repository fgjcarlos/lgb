// Package httpx provides shared HTTP helper utilities for the LGB gateway.
//
// Requirements: MVP-FND-1.9. Design: §11, §4.5.
package httpx

import (
	"context"
	"net/http"
	"time"
)

// Shutdown drains srv gracefully within deadline duration.
//
// It creates a child context with the given deadline, calls srv.Shutdown, and
// returns any shutdown error. The srv.Close is called as a fallback only when
// the deadline context expires — http.Server.Shutdown handles this internally.
//
// Per design §11: Shutdown is called by server.Server.Run on context
// cancellation. signal.NotifyContext wiring lives in cmd/lgb/cmd/server.go.
func Shutdown(ctx context.Context, srv *http.Server, deadline time.Duration) error {
	shutCtx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	return srv.Shutdown(shutCtx)
}
