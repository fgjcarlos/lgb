// Package mqtt provides a thin wrapper around paho.mqtt.golang v1.5.1
// for MQTT broker connectivity in the LGB gateway.
//
// The wrapper isolates paho types at the package boundary — no paho types
// are exported. SetOrderMatters(false) is set unconditionally to prevent
// the known paho v1 deadlock. Auto-reconnect is disabled; the project
// owns the reconnect lifecycle via the OnConnect callback.
package mqtt
