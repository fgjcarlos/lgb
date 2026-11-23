// runtime.go — checks that read live server components (MQTT client, OPC UA
// server). They are built by the server command after those components exist,
// so they appear in GET /api/doctor but not in the offline `lgb doctor` CLI.
package doctor

import (
	"context"
	"net/url"
	"strings"
)

// mqttBrokerCheck reports whether the running MQTT client is connected.
type mqttBrokerCheck struct {
	broker    string // redacted broker URL; empty when it cannot be shown safely
	connected func() bool
}

// NewMQTTBrokerCheck returns the "mqtt-broker" check: PASS while connected()
// reports true, FAIL otherwise. brokerURL is shown with its password redacted.
func NewMQTTBrokerCheck(brokerURL string, connected func() bool) Check {
	broker := ""
	if u, err := url.Parse(brokerURL); err == nil && u.Host != "" {
		broker = u.Redacted()
	}
	return &mqttBrokerCheck{broker: broker, connected: connected}
}

func (c *mqttBrokerCheck) Name() string { return "mqtt-broker" }

func (c *mqttBrokerCheck) Run(ctx context.Context) Result {
	target := "MQTT broker"
	if c.broker != "" {
		target += " " + c.broker
	}
	if c.connected() {
		return Result{Name: c.Name(), Status: StatusPass, Message: "connected to " + target}
	}
	return Result{Name: c.Name(), Status: StatusFail, Message: "not connected to " + target}
}

// opcuaServerCheck reports the OPC UA server state.
type opcuaServerCheck struct {
	health func() (state string, endpoints []string)
}

// NewOPCUAServerCheck returns the "opcua-server" check: PASS while health()
// reports the "Running" state, FAIL otherwise. The OPC UA stack (gopcua) does
// not expose its session count, so the check reports state and endpoints only.
func NewOPCUAServerCheck(health func() (state string, endpoints []string)) Check {
	return &opcuaServerCheck{health: health}
}

func (c *opcuaServerCheck) Name() string { return "opcua-server" }

func (c *opcuaServerCheck) Run(ctx context.Context) Result {
	state, endpoints := c.health()
	if state != "Running" {
		return Result{Name: c.Name(), Status: StatusFail, Message: "OPC UA server is not running (state: " + state + ")"}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "OPC UA server running at " + strings.Join(endpoints, ", ") + " (session count is not exposed by the OPC UA stack)",
	}
}
