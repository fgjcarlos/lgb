// Package plc provides a thin adapter layer over the gologix CIP client for
// communicating with Allen-Bradley / Rockwell Automation PLCs via EtherNet/IP.
//
// # Architecture
//
// One [Driver] instance wraps a single *gologix.Client (one per configured PLC).
// The [Manager] owns the lifecycle of all drivers and is started/stopped by the
// server subsystem. Reconnection is delegated to [internal/retry.Do] with
// exponential backoff to prevent CIP session slot exhaustion.
//
// # Phase 1 Limitations
//
// Phase 1 supports scalar tag types and 1-D arrays only. UDT (User Defined Type)
// tag support is deferred to Phase 2.
//
// Boolean arrays ([]) must have a length that is a multiple of 32 — this is a
// constraint of the gologix CIP encoding (booleans are packed into 32-bit words).
//
// # Timeout Behaviour
//
// gologix Read and Write operations do not accept a context.Context. The only
// per-operation deadline available is the SocketTimeout field on the underlying
// *gologix.Client. Configure the timeout via [config.PLC].SocketTimeout before
// constructing a driver.
//
// # Error Handling
//
// All gologix errors are translated at the adapter boundary into project-level
// sentinel errors defined in [internal/errors]: [ErrPLCConnect], [ErrPLCRead],
// [ErrPLCWrite], and [ErrPLCTimeout]. Use errors.Is for sentinel checking.
package plc
