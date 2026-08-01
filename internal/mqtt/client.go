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
	client         paho.Client
	mu             sync.Mutex
	onConnectFn    func()
	onConnLostFn   func(error)
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

// waitToken waits for token.Done or ctx cancellation, whichever comes first.
// The goroutine lifetime is bounded by the broker's keepalive/timeout
// (paho default ~30 seconds). This helper centralizes the pattern and documents
// the expected goroutine leak bound (issue #90).
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
