package server

import (
	"sync"
	"testing"
)

// TestTagHub_COV_ConcurrentRegisterPublish exercises per-subscriber COV
// filters against concurrent publishers, (re)subscribes and unregisters; it
// must be clean under -race and must not deadlock or send on a closed channel.
func TestTagHub_COV_ConcurrentRegisterPublish(t *testing.T) {
	h := newTagHub()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				h.publish(speed(float64(i % 7)))
			}
		}()
	}
	for i := 0; i < 300; i++ {
		sub := newTagSubscriber(4, i%2 == 0)
		sub.setFilter("", "")
		h.register(sub)
		go func() {
			for range sub.ch {
			}
		}()
		sub.setFilter("packaging", "")
		sub.unsubscribe()
		sub.setFilter("", "Speed")
		h.unregister(sub)
	}
	close(stop)
	wg.Wait()
}
