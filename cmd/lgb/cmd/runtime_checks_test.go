package cmd

import (
	"context"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
)

type fakeConnectedNode struct{}

func (fakeConnectedNode) Start(context.Context) error { return nil }
func (fakeConnectedNode) Stop() error                 { return nil }
func (fakeConnectedNode) Connected() bool             { return true }

type fakeHealthOPCUA struct{}

func (fakeHealthOPCUA) Start(context.Context) error { return nil }
func (fakeHealthOPCUA) Stop() error                 { return nil }
func (fakeHealthOPCUA) Health() (string, []string)  { return "Running", nil }

type plainNode struct{}

func (plainNode) Start(context.Context) error { return nil }
func (plainNode) Stop() error                 { return nil }

func TestRuntimeChecks(t *testing.T) {
	cfg := &config.Config{MQTT: config.MQTTSection{BrokerURL: "tcp://broker:1883"}}

	got := runtimeChecks(cfg, fakeConnectedNode{}, fakeHealthOPCUA{})
	if len(got) != 2 || got[0].Name() != "mqtt-broker" || got[1].Name() != "opcua-server" {
		t.Fatalf("runtimeChecks = %v, want mqtt-broker, opcua-server", got)
	}
	if r := got[0].Run(context.Background()); r.Message != "connected to MQTT broker tcp://broker:1883" {
		t.Fatalf("mqtt check message = %q", r.Message)
	}

	if got := runtimeChecks(cfg, nil, nil); len(got) != 0 {
		t.Fatalf("runtimeChecks(nil, nil) = %v, want none", got)
	}
	if got := runtimeChecks(cfg, plainNode{}, plainNode{}); len(got) != 0 {
		t.Fatalf("runtimeChecks without capabilities = %v, want none", got)
	}
}
