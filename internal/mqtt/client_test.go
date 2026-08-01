package mqtt_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/mqtt"
)

var _ mqtt.Client = (*mqtt.PahoClient)(nil)

func TestNewClient_SetOrderMattersFalse(t *testing.T) {
	t.Parallel()
	opts := mqtt.Options{
		BrokerURL:  "tcp://localhost:1883",
		ClientID:   "test",
		WillTopic:  "spBv1.0/group/NDEATH/node",
		WillPayload: []byte{0x01},
		WillQoS:    1,
	}
	c := mqtt.NewClient(opts)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNewClient_WillConfigured(t *testing.T) {
	t.Parallel()
	opts := mqtt.Options{
		BrokerURL:   "tcp://localhost:1883",
		ClientID:    "test",
		WillTopic:   "spBv1.0/plant-a/NDEATH/lgb-1",
		WillPayload: []byte{0x0a, 0x0b},
		WillQoS:     1,
		WillRetain:  false,
	}
	c := mqtt.NewClient(opts)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestConnect_CancelledContext(t *testing.T) {
	t.Parallel()
	opts := mqtt.Options{
		BrokerURL:   "tcp://localhost:19999",
		ClientID:    "test-cancelled",
		WillTopic:   "spBv1.0/g/NDEATH/n",
		WillPayload: []byte{0x01},
		WillQoS:     1,
	}
	c := mqtt.NewClient(opts)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Connect(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, mqtt.ErrMQTTConnect) {
		t.Errorf("expected ErrMQTTConnect, got %v", err)
	}
}

func TestPublish_WhenNotConnected(t *testing.T) {
	t.Parallel()
	opts := mqtt.Options{
		BrokerURL:   "tcp://localhost:19999",
		ClientID:    "test-not-connected",
		WillTopic:   "spBv1.0/g/NDEATH/n",
		WillPayload: []byte{0x01},
		WillQoS:     1,
	}
	c := mqtt.NewClient(opts)

	err := c.Publish(context.Background(), "test/topic", 0, false, []byte("hello"))
	if err == nil {
		t.Fatal("expected error when not connected, got nil")
	}
	if !errors.Is(err, mqtt.ErrMQTTConnect) {
		t.Errorf("expected ErrMQTTConnect, got %v", err)
	}
}

// TestWaitTokenBoundedUnderCancel verifies that repeated context cancellations
// do not cause goroutine leaks. The token.Wait pattern used in Connect/Publish/Subscribe
// must bound goroutine lifetime (issue #90). This test does NOT use t.Parallel() because
// it measures runtime.NumGoroutine() globally, which would be affected by parallel tests.
func TestWaitTokenBoundedUnderCancel(t *testing.T) {
	// DO NOT use t.Parallel() — this test measures global goroutine count.
	if testing.Short() {
		t.Skip("skipping goroutine test in short mode")
	}

	opts := mqtt.Options{
		BrokerURL:   "tcp://localhost:19999", // Invalid address; will fail to connect
		ClientID:    "test-goroutine-bound",
		WillTopic:   "spBv1.0/g/NDEATH/n",
		WillPayload: []byte{0x01},
		WillQoS:     1,
	}
	c := mqtt.NewClient(opts)

	// Capture baseline goroutine count (excluding this test).
	// Note: not all goroutines are guaranteed to exit immediately, so we allow a margin.
	baseline := runtime.NumGoroutine()

	// Issue repeated operations with cancelled contexts (simulating shutdown).
	// Each operation starts a goroutine to wait on the token; it must not leak.
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		_ = c.Connect(ctx)  // Will fail quickly due to cancelled ctx
		_ = c.Connect(ctx)  // Repeat
	}

	// Give the broker timeout and goroutine cleanup some time (up to 35s per paho default ~30s).
	// We poll instead of sleeping to detect earlier if goroutines return.
	deadline := time.Now().Add(35 * time.Second)
	for {
		current := runtime.NumGoroutine()
		// Allow up to 2 extra goroutines beyond baseline (margin for cleanup).
		if current <= baseline+2 {
			// Good: goroutines have returned to baseline (or close to it).
			t.Logf("Goroutine count back to baseline+margin after %v (baseline=%d, current=%d)",
				time.Since(time.Now().Add(-35*time.Second)), baseline, current)
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("Goroutine count did not return to baseline after 35s: baseline=%d, current=%d",
				baseline, current)
		}

		time.Sleep(100 * time.Millisecond)
	}
}
