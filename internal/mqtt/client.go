package mqtt

import (
	"context"
	"fmt"
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

// Client is the boundary interface for MQTT operations.
type Client interface {
	Connect(ctx context.Context) error
	Disconnect(quiesce uint)
	Publish(ctx context.Context, topic string, qos byte, retained bool, payload []byte) error
	IsConnected() bool
	SetOnConnect(fn func())
}

// PahoClient wraps paho.mqtt.golang. Exported for compile-time interface
// assertion in tests; construct via NewClient.
type PahoClient struct {
	client      paho.Client
	mu          sync.Mutex
	onConnectFn func()
}

// NewClient creates a PahoClient with the given options.
// SetOrderMatters(false) and AutoReconnect(false) are set unconditionally.
func NewClient(opts Options) *PahoClient {
	pahoOpts := paho.NewClientOptions()

	pahoOpts.AddBroker(opts.BrokerURL)
	pahoOpts.SetClientID(opts.ClientID)
	pahoOpts.SetOrderMatters(false)
	pahoOpts.SetAutoReconnect(false)

	if opts.Username != "" {
		pahoOpts.SetUsername(opts.Username)
	}
	if opts.Password != "" {
		pahoOpts.SetPassword(opts.Password)
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

	pc := &PahoClient{}

	pahoOpts.SetOnConnectHandler(func(_ paho.Client) {
		pc.mu.Lock()
		fn := pc.onConnectFn
		pc.mu.Unlock()
		if fn != nil {
			fn()
		}
	})

	pc.client = paho.NewClient(pahoOpts)
	return pc
}

// Connect establishes the MQTT connection. Respects context cancellation.
func (c *PahoClient) Connect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, ctx.Err())
	default:
	}

	token := c.client.Connect()
	done := make(chan struct{})
	go func() {
		token.Wait()
		close(done)
	}()

	select {
	case <-done:
		if token.Error() != nil {
			return fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, token.Error())
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqtt: connect: %w: %w", ErrMQTTConnect, ctx.Err())
	}
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
	done := make(chan struct{})
	go func() {
		token.Wait()
		close(done)
	}()

	select {
	case <-done:
		if token.Error() != nil {
			return fmt.Errorf("mqtt: publish %q: %w: %w", topic, ErrMQTTPublish, token.Error())
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqtt: publish %q: %w: %w", topic, ErrMQTTPublish, ctx.Err())
	}
}

// IsConnected returns true if the MQTT session is active.
func (c *PahoClient) IsConnected() bool {
	return c.client.IsConnected()
}

// SetOnConnect registers a callback invoked on every (re)connect.
func (c *PahoClient) SetOnConnect(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnectFn = fn
}
