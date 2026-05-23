// Package sparkplug implements the Sparkplug B Edge Node state machine,
// payload builders, metric encoding, and sequence number tracking.
//
// Phase 1 supports scalar tag types only (bool, int8–int64, uint8–uint64,
// float32, float64, string). UDT support is deferred to Phase 2.
//
// All exported types are safe for concurrent use from multiple goroutines.
package sparkplug
