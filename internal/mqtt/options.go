package mqtt

import "time"

// Options configures the MQTT client.
type Options struct {
	BrokerURL string
	ClientID  string
	Username  string
	Password  string
	// PasswordFile is read at NewClient time; trailing CR/LF is trimmed.
	// Mutually exclusive with Password.
	PasswordFile string
	// TLS settings apply only to TLS broker schemes (see IsTLSScheme).
	// TLSCACert is a PEM bundle replacing the system roots; TLSCertFile and
	// TLSKeyFile form an optional client certificate and must be set together.
	TLSCACert             string
	TLSCertFile           string
	TLSKeyFile            string
	TLSInsecureSkipVerify bool
	QoS                   byte
	KeepAlive             time.Duration
	CleanSession          bool
	WillTopic             string
	WillPayload           []byte
	WillQoS               byte
	WillRetain            bool
}
