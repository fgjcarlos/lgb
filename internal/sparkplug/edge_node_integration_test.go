//go:build integration

package sparkplug_test

import (
	"context"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/sparkplug"
)

const integrationBroker = "tcp://localhost:1883"

func TestIntegration_FullLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mqttClient := mqtt.NewClient(mqtt.Options{
		BrokerURL:    integrationBroker,
		ClientID:     "test-lifecycle",
		WillTopic:    "spBv1.0/test/NDEATH/lgb-test",
		WillPayload:  []byte{0x01},
		WillQoS:      1,
		CleanSession: true,
	})

	en := sparkplug.NewEdgeNode(sparkplug.EdgeNodeConfig{
		GroupID: "test",
		NodeID:  "lgb-test",
		Client:  mqttClient,
		Devices: []sparkplug.DeviceConfig{
			{DeviceID: "plc-a", Tags: []sparkplug.TagDef{
				{Name: "Motor.Speed", SparkplugType: "Float"},
			}},
		},
	})

	if err := en.Start(ctx); err != nil {
		t.Skipf("mosquitto not reachable: %v", err)
	}

	if en.State() != sparkplug.Online {
		t.Errorf("state after Start = %v; want Online", en.State())
	}

	en.HandleTagUpdate(sparkplug.TagUpdate{
		PLCName:   "plc-a",
		Tag:       "Motor.Speed",
		Value:     float32(1200.5),
		Timestamp: time.Now(),
	})

	time.Sleep(200 * time.Millisecond)

	if err := en.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if en.State() != sparkplug.Offline {
		t.Errorf("state after Stop = %v; want Offline", en.State())
	}
}
