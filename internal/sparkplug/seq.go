package sparkplug

import "sync/atomic"

// SeqTracker tracks the Sparkplug B message sequence number (0–255).
// It is safe for concurrent use.
type SeqTracker struct {
	counter atomic.Uint64
}

// Next returns the current sequence value and advances the counter.
// Values wrap from 255 back to 0.
func (s *SeqTracker) Next() uint64 {
	v := s.counter.Add(1) - 1
	return v % 256
}

// Reset sets the counter back to 0. Called before NBIRTH per Sparkplug B spec.
func (s *SeqTracker) Reset() {
	s.counter.Store(0)
}
