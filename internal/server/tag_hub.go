package server

import (
	"sync"

	"github.com/fgjcarlos/lgb/internal/plc"
)

type tagHub struct {
	mu      sync.RWMutex
	clients map[*tagSubscriber]struct{}
}

type tagSubscriber struct {
	plc string
	tag string
	ch  chan plc.TagUpdate
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
		select {
		case sub.ch <- update:
		default:
			// Backpressure policy: drop newest update for slow clients rather than
			// blocking the PLC scan loop.
		}
	}
}

func (s *tagSubscriber) matches(update plc.TagUpdate) bool {
	if s.plc != "" && s.plc != update.PLCName {
		return false
	}
	if s.tag != "" && s.tag != update.Tag {
		return false
	}
	return true
}
