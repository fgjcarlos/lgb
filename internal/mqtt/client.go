package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	errs "github.com/fgjcarlos/lgb/internal/errors"
)

var (
	ErrMQTTConnect   = errs.ErrMQTTConnect
	ErrMQTTPublish   = errs.ErrMQTTPublish
	ErrMQTTSubscribe = errs.ErrMQTTSubscribe
)

// MessageHandler is called when a message arrives on a subscribed topic.
type MessageHandler func(topic string, payload []byte)

// Client is the boundary interface for MQTT operations.
type Client interface {
	Connect(ctx context.Context) error
	Disconnect(quiesce uint)
	Publish(ctx context.Context, topic string, qos byte, retained bool, payload []byte) error
	Subscribe(ctx context.Context, topic string, qos byte, handler MessageHandler) error
	Unsubscribe(ctx context.Context, topic string) error
	IsConnected() bool
	SetOnConnect(fn func())
	SetConnectionLost(fn func(error))
}

// PahoClient wraps paho.mqtt.golang. Exported for compile-time interface
// assertion in tests; construct via NewClient.
type PahoClient struct {
	client paho.Client
	// initErr records invalid credential or TLS material found by NewClient;
	// Connect returns it instead of dialing.
	initErr      error
	mu           sync.Mutex
	onConnectFn  func()
	onConnLostFn func(error)
}

// NewClient creates a PahoClient with the given options.
// SetOrderMatters(false) and AutoReconnect(false) are set unconditionally.
func NewClient(opts Options) *PahoClient {
	pahoOpts := paho.NewClientOptions()

	pahoOpts.AddBroker(opts.BrokerURL)
	pahoOpts.SetClientID(opts.ClientID)
	pahoOpts.SetOrderMatters(false)
	pahoOpts.SetAutoReconnect(false)

	pc := &PahoClient{}

	if opts.Username != "" {
		pahoOpts.SetUsername(opts.Username)
	}
	password, err := resolvePassword(opts)
	if err != nil {
		pc.initErr = err
	}
	if password != "" {
		pahoOpts.SetPassword(password)
	}

	if IsTLSScheme(opts.BrokerURL) {
		tlsCfg, err := buildTLSConfig(opts)
		if err != nil {
			pc.initErr = errors.Join(pc.initErr, err)
		}
		pahoOpts.SetTLSConfig(tlsCfg)
	} else if opts.TLSCACert != "" || opts.TLSCertFile != "" || opts.TLSKeyFile != "" || opts.TLSInsecureSkipVerify {
		// Refuse rather than silently drop TLS settings the operator asked for.
		pc.initErr = errors.Join(pc.initErr, errors.New("mqtt: TLS settings require a TLS broker URL scheme (mqtts://, ssl://, ...)"))
	}

	keepAlive := opts.KeepAlive
	if keepAlive <= 0 {
		keepAlive = 30 * time.Second
	}
	pahoOpts.SetKeepAlive(keepAlive)
	pahoOpts.SetCleanSession(opts.CleanSession)

	if opts.WillTopic != "" && len(opts.WillPayload) > 0 {
		pahoOpts.SetWill(opts.WillTopic, string(opts.WillPayload), opts.WillQoS, opts.WillRetain)
	}

	pahoOpts.SetOnConnectHandler(func(_ paho.Client) {
		pc.mu.Lock()
		fn := pc.onConnectFn
		pc.mu.Unlock()
		if fn != nil {
			fn()
		}
	})

	pahoOpts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		pc.mu.Lock()
		fn := pc.onConnLostFn
		pc.mu.Unlock()
		if fn != nil {
			fn(err)
		}
	})

	pc.client = paho.NewClient(pahoOpts)
	return pc
}

// IsTLSScheme reports whether paho dials brokerURL over TLS.
func IsTLSScheme(brokerURL string) bool {
	u, err := url.Parse(brokerURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "ssl", "tls", "mqtts", "mqtt+ssl", "tcps", "wss":
		return true
	}
	return false
}

func resolvePassword(opts Options) (string, error) {
	if opts.PasswordFile == "" {
		return opts.Password, nil
	}
	if opts.Password != "" {
		return "", errors.New("mqtt: password and passwordFile are mutually exclusive")
	}
	b, err := os.ReadFile(opts.PasswordFile)
	if err != nil {
		return "", fmt.Errorf("mqtt: reading passwordFile: %w", err)
	}
	password := strings.TrimRight(string(b), "\r\n")
	if password == "" {
		return "", errors.New("mqtt: passwordFile is empty")
	}
	return password, nil
}

func buildTLSConfig(opts Options) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: opts.TLSInsecureSkipVerify, // operator opt-in; the server logs a warning
	}
	if opts.TLSCACert != "" {
		pem, err := os.ReadFile(opts.TLSCACert)
		if err != nil {
			return cfg, fmt.Errorf("mqtt: reading tlsCACert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return cfg, fmt.Errorf("mqtt: tlsCACert %q contains no PEM certificates", opts.TLSCACert)
		}
		cfg.RootCAs = pool
	}
	if (opts.TLSCertFile == "") != (opts.TLSKeyFile == "") {
		return cfg, errors.New("mqtt: tlsCertFile and tlsKeyFile must be set together")
	}
	if opts.TLSCertFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.TLSCertFile, opts.TLSKeyFile)
		if err != nil {
			return cfg, fmt.Errorf("mqtt: loading client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// waitToken waits for the token to complete or for ctx to be cancelled,
// whichever comes first, and returns the token error or the context error.
//
// The caller is never held past its own context. The goroutine that waits on
// the token keeps running until the broker answers or the connection times out
// (paho default ~30 seconds), so its lifetime is bounded by the broker rather
// than by the caller's context (issue #90).
func waitToken(ctx context.Context, token paho.Token) error {
	done := make(chan struct{})
	go func() {
		token.Wait()
		close(done)
	}()

	select {
	case <-done:
		return token.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Connect establishes the MQTT connection. Respects context cancellation.
func (c *PahoClient) Connect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, ctx.Err())
	default:
	}
	if c.initErr != nil {
		return fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, c.initErr)
	}

	token := c.client.Connect()
	if err := waitToken(ctx, token); err != nil {
		return fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, err)
	}
	return nil
}

// Disconnect gracefully disconnects with the given quiesce period in milliseconds.
func (c *PahoClient) Disconnect(quiesce uint) {
	c.client.Disconnect(quiesce)
}

// Publish sends a message. Returns ErrMQTTConnect if not connected.
func (c *PahoClient) Publish(ctx context.Context, topic string, qos byte, retained bool, payload []byte) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("mqtt: publish: not connected: %w", ErrMQTTConnect)
	}

	token := c.client.Publish(topic, qos, retained, payload)
	if err := waitToken(ctx, token); err != nil {
		return fmt.Errorf("mqtt: publish %q: %w: %w", topic, ErrMQTTPublish, err)
	}
	return nil
}

// IsConnected returns true if the MQTT session is active.
func (c *PahoClient) IsConnected() bool {
	return c.client.IsConnected()
}

// Subscribe registers a handler for the given topic filter.
func (c *PahoClient) Subscribe(ctx context.Context, topic string, qos byte, handler MessageHandler) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("mqtt: subscribe: not connected: %w", ErrMQTTSubscribe)
	}

	callback := func(_ paho.Client, msg paho.Message) {
		handler(msg.Topic(), msg.Payload())
	}

	token := c.client.Subscribe(topic, qos, callback)
	if err := waitToken(ctx, token); err != nil {
		return fmt.Errorf("mqtt: subscribe %q: %w: %w", topic, ErrMQTTSubscribe, err)
	}
	return nil
}

// SetOnConnect registers a callback invoked on every (re)connect.
func (c *PahoClient) SetOnConnect(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnectFn = fn
}

// SetConnectionLost registers a callback invoked when the broker connection
// is lost unexpectedly. Mirrors the SetOnConnect mutex pattern.
func (c *PahoClient) SetConnectionLost(fn func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnLostFn = fn
}

// Unsubscribe removes the subscription for the given topic filter.
func (c *PahoClient) Unsubscribe(ctx context.Context, topic string) error {
	token := c.client.Unsubscribe(topic)
	done := make(chan struct{})
	go func() {
		token.Wait()
		close(done)
	}()

	select {
	case <-done:
		if token.Error() != nil {
			return fmt.Errorf("mqtt: unsubscribe %q: %w: %w", topic, ErrMQTTSubscribe, token.Error())
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqtt: unsubscribe %q: %w: %w", topic, ErrMQTTSubscribe, ctx.Err())
	}
}
