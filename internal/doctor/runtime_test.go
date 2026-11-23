package doctor

import (
	"context"
	"strings"
	"testing"
)

func TestMQTTBrokerCheck(t *testing.T) {
	connected := false
	c := NewMQTTBrokerCheck("ssl://gw:s3cret@broker.local:8883", func() bool { return connected })
	if c.Name() != "mqtt-broker" {
		t.Fatalf("Name() = %q, want mqtt-broker", c.Name())
	}

	r := c.Run(context.Background())
	if r.Name != "mqtt-broker" || r.Status != StatusFail || r.Message != "not connected to MQTT broker ssl://gw:xxxxx@broker.local:8883" {
		t.Fatalf("disconnected: got %+v", r)
	}
	connected = true
	r = c.Run(context.Background())
	if r.Status != StatusPass || r.Message != "connected to MQTT broker ssl://gw:xxxxx@broker.local:8883" {
		t.Fatalf("connected: got %+v", r)
	}
	if strings.Contains(r.Message, "s3cret") {
		t.Fatalf("message leaks the broker password: %q", r.Message)
	}
}

func TestMQTTBrokerCheck_UnparseableURLIsNotEchoed(t *testing.T) {
	r := NewMQTTBrokerCheck("::bad url s3cret", func() bool { return true }).Run(context.Background())
	if r.Status != StatusPass || r.Message != "connected to MQTT broker" {
		t.Fatalf("got %+v", r)
	}
}

func TestOPCUAServerCheck(t *testing.T) {
	state, urls := "NotStarted", []string(nil)
	c := NewOPCUAServerCheck(func() (string, []string) { return state, urls })
	if c.Name() != "opcua-server" {
		t.Fatalf("Name() = %q, want opcua-server", c.Name())
	}

	r := c.Run(context.Background())
	if r.Name != "opcua-server" || r.Status != StatusFail || r.Message != "OPC UA server is not running (state: NotStarted)" {
		t.Fatalf("not started: got %+v", r)
	}
	state, urls = "Running", []string{"opc.tcp://127.0.0.1:4840"}
	r = c.Run(context.Background())
	want := "OPC UA server running at opc.tcp://127.0.0.1:4840 (session count is not exposed by the OPC UA stack)"
	if r.Status != StatusPass || r.Message != want {
		t.Fatalf("running: got %+v, want message %q", r, want)
	}
	state = "Shutdown"
	if r = c.Run(context.Background()); r.Status != StatusFail || r.Message != "OPC UA server is not running (state: Shutdown)" {
		t.Fatalf("shutdown: got %+v", r)
	}
}
