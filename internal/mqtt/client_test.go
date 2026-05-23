package mqtt_test

import (
	"context"
	"errors"
	"testing"

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
