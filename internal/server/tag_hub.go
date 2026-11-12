package server

import (
	"sync"

	"github.com/fgjcarlos/lgb/internal/cov"
	"github.com/fgjcarlos/lgb/internal/plc"
)

type tagHub struct {
	mu      sync.RWMutex
	clients map[*tagSubscriber]struct{}
}

type tagSubscriber struct {
	mu     sync.RWMutex
	plc    string
	tag    string
	active bool
	ch     chan plc.TagUpdate
	cov    *cov.Filter // per-subscriber change-on-value state (#60); nil = off
}

// newTagSubscriber returns a subscriber with a buffer of size updates. With
// changeOnValue it gets its own COV filter, so its deadband state and dropped
// updates never affect other subscribers.
func newTagSubscriber(buffer int, changeOnValue bool) *tagSubscriber {
	sub := &tagSubscriber{ch: make(chan plc.TagUpdate, buffer)}
	if changeOnValue {
		sub.cov = cov.NewFilter()
	}
	return sub
}

func newTagHub() *tagHub {
	return &tagHub{clients: make(map[*tagSubscriber]struct{})}
}

func (h *tagHub) register(sub *tagSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[sub] = struct{}{}
}

func (h *tagHub) unregister(sub *tagSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, sub)
	close(sub.ch)
}

func (h *tagHub) publish(update plc.TagUpdate) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.clients {
		if !sub.matches(update) {
			continue
		}
		sample := cov.Sample{Key: update.PLCName + "/" + update.Tag, Value: update.Value, Quality: update.Quality, Deadband: update.Deadband}
		sub.cov.Offer(sample, func() bool {
			select {
			case sub.ch <- update:
				return true
			default:
				// Backpressure policy: drop newest update for slow clients rather than
				// blocking the PLC scan loop. A dropped update is not recorded as
				// delivered, so COV re-offers it on the next scan.
				return false
			}
		})
	}
}

func (s *tagSubscriber) setFilter(plcName, tagName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plc = plcName
	s.tag = tagName
	s.active = true
	// A new subscription starts from the current values again.
	s.cov.Reset()
}

func (s *tagSubscriber) unsubscribe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plc = ""
	s.tag = ""
	s.active = false
}

func (s *tagSubscriber) matches(update plc.TagUpdate) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return false
	}
	if s.plc != "" && s.plc != update.PLCName {
		return false
	}
	if s.tag != "" && s.tag != update.Tag {
		return false
	}
	return true
}
