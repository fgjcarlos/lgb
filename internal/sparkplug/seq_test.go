package sparkplug_test

import (
	"sync"
	"testing"

	"github.com/fgjcarlos/lgb/internal/sparkplug"
)

func TestSeqTracker_NextStartsAtZero(t *testing.T) {
	t.Parallel()
	var s sparkplug.SeqTracker
	if got := s.Next(); got != 0 {
		t.Errorf("first Next() = %d; want 0", got)
	}
	if got := s.Next(); got != 1 {
		t.Errorf("second Next() = %d; want 1", got)
	}
}

func TestSeqTracker_WrapsAt256(t *testing.T) {
	t.Parallel()
	var s sparkplug.SeqTracker
	for i := 0; i < 255; i++ {
		s.Next()
	}
	if got := s.Next(); got != 255 {
		t.Errorf("Next() at 255 = %d; want 255", got)
	}
	if got := s.Next(); got != 0 {
		t.Errorf("Next() after 255 = %d; want 0 (wrap)", got)
	}
}

func TestSeqTracker_ResetClearsCounter(t *testing.T) {
	t.Parallel()
	var s sparkplug.SeqTracker
	for i := 0; i < 42; i++ {
		s.Next()
	}
	s.Reset()
	if got := s.Next(); got != 0 {
		t.Errorf("Next() after Reset() = %d; want 0", got)
	}
}

func TestSeqTracker_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	var s sparkplug.SeqTracker
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Next()
		}()
	}
	wg.Wait()
}
