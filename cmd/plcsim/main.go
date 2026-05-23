// cmd/plcsim — deterministic in-process CIP PLC simulator.
//
// Starts a gologix server pre-seeded with three named tags so CI smoke tests
// can verify gateway-to-PLC connectivity without real hardware:
//
//	SimBool  = true   (bool)
//	SimInt   = 42     (int16)
//	SimFloat = 3.14   (float32)
//
// Listens on TCP :44818 (the standard EtherNet/IP port). Handles SIGTERM by
// closing the listeners and exiting 0.
//
// The tag fixture is the same data set used by internal/testutil.StartPLCSim
// so Docker and in-process tests share a single canonical source.
//
// Requirements: MVP-FND-9.2. Design: §12, §20.5. Pure-Go (ADR-0009).
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/danomagnum/gologix"
)

func main() {
	logger := slog.Default()

	// Build the canonical tag provider (same fixture as testutil.NewPLCSimProvider).
	provider := &gologix.MapTagProvider{
		Data: make(map[string]any),
	}
	// MapTagProvider.TagRead lowercases all names; store lowercase.
	provider.Data["simbool"] = true
	provider.Data["simint"] = int16(42)
	provider.Data["simfloat"] = float32(3.14)

	// Wire the provider into the router at the default CIP path (backplane slot 0).
	router := gologix.NewRouter()
	path, err := gologix.ParsePath("1,0")
	if err != nil {
		logger.Error("plcsim: parse path", "error", err)
		os.Exit(1)
	}
	router.Handle(path.Bytes(), provider)

	srv := gologix.NewServer(router)

	// Start serving in a background goroutine; Serve() blocks until the
	// listeners are closed.
	errCh := make(chan error, 1)
	go func() {
		logger.Info("plcsim listening", "addr", ":44818")
		errCh <- srv.Serve()
	}()

	// Wait for SIGTERM or SIGINT, then close the server gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("plcsim shutting down", "signal", sig.String())
		// Close listeners to unblock Serve().
		if srv.TCPListener != nil {
			srv.TCPListener.Close()
		}
		if srv.UDPListener != nil {
			srv.UDPListener.Close()
		}
	case err := <-errCh:
		if err != nil {
			logger.Error("plcsim serve error", "error", err)
			os.Exit(1)
		}
	}
}
