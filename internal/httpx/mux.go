// mux.go — shared HTTP mux constructor.
//
// NewMux returns a plain http.ServeMux using Go 1.22 pattern-based routing.
// All route registration is done by the caller (internal/server).
// Design §11, §16.
package httpx

import "net/http"

// NewMux returns a new *http.ServeMux for use by the server.
func NewMux() *http.ServeMux {
	return http.NewServeMux()
}
