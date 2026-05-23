package mqtt

import "time"

// Options configures the MQTT client.
type Options struct {
	BrokerURL    string
	ClientID     string
	Username     string
	Password     string
	QoS          byte
	KeepAlive    time.Duration
	CleanSession bool
	WillTopic    string
	WillPayload  []byte
	WillQoS      byte
	WillRetain   bool
}
