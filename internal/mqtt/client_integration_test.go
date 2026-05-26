//go:build integration

package mqtt_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/mqtt"
)

const testBroker = "tcp://localhost:1883"

func TestIntegration_ConnectPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	subClient := mqtt.NewClient(mqtt.Options{
		BrokerURL:    testBroker,
		ClientID:     "test-sub",
		CleanSession: true,
	})
	if err := subClient.Connect(ctx); err != nil {
		t.Skipf("mosquitto not reachable: %v", err)
	}
	defer subClient.Disconnect(250)

	pubClient := mqtt.NewClient(mqtt.Options{
		BrokerURL:    testBroker,
		ClientID:     "test-pub",
		WillTopic:    "spBv1.0/test/NDEATH/node",
		WillPayload:  []byte{0x01},
		WillQoS:      1,
		CleanSession: true,
	})
	if err := pubClient.Connect(ctx); err != nil {
		t.Fatalf("pub connect failed: %v", err)
	}
	defer pubClient.Disconnect(250)

	topic := "spBv1.0/test/NBIRTH/node"
	payload := []byte("test-nbirth")
	if err := pubClient.Publish(ctx, topic, 1, false, payload); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
}

func TestIntegration_SetOrderMatters_HighThroughput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := mqtt.NewClient(mqtt.Options{
		BrokerURL:    testBroker,
		ClientID:     "test-throughput",
		CleanSession: true,
	})
	if err := c.Connect(ctx); err != nil {
		t.Skipf("mosquitto not reachable: %v", err)
	}
	defer c.Disconnect(250)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = c.Publish(ctx, "spBv1.0/test/DDATA/node/plc", 1, false, []byte("data"))
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("high-throughput publish deadlocked — SetOrderMatters may be true")
	}
}
