package sparkplug

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fgjcarlos/lgb/internal/cov"
	"github.com/fgjcarlos/lgb/internal/mqtt"
	"github.com/fgjcarlos/lgb/internal/retry"
	pb "github.com/fgjcarlos/lgb/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

// DeviceConfig maps a PLC to its Sparkplug metric definitions.
type DeviceConfig struct {
	DeviceID string
	Tags     []TagDef
}

// CommandHandler is invoked when a DCMD arrives with a tag write request.
type CommandHandler func(deviceID, tag string, value any)

// EdgeNodeConfig configures the Sparkplug B edge node.
type EdgeNodeConfig struct {
	GroupID      string
	NodeID       string
	Client       mqtt.Client
	Devices      []DeviceConfig
	Log          *slog.Logger
	OnCommand    CommandHandler
	RetryOptions *retry.Options // optional; if nil, defaults to 1s→30s exponential backoff
	// ChangeOnValue publishes DDATA only when a tag value changes beyond its
	// deadband (#60). The filter is reset on every birth so current values
	// are re-sent after NBIRTH/DBIRTH.
	ChangeOnValue bool
}

// EdgeNode orchestrates the Sparkplug B lifecycle.
type EdgeNode struct {
	groupID      string
	nodeID       string
	client       mqtt.Client
	devices      []DeviceConfig
	log          *slog.Logger
	onCommand    atomic.Pointer[CommandHandler] // read by the MQTT goroutine in handleDCMD
	started      atomic.Bool                    // true between Start and Stop; guards SetCommandHandler
	retryOptions retry.Options

	sm    StateMachine
	seq   SeqTracker
	bdSeq atomic.Uint64 // incremented on each birth cycle (R69-3)

	cov         *cov.Filter // nil when change-on-value is disabled
	updates     chan TagUpdate
	done        chan struct{}
	reconnectCh chan struct{} // signaled by onConnectionLost; reconnect goroutine listens
	wg          sync.WaitGroup
}

// NewEdgeNode creates an EdgeNode from the given configuration.
func NewEdgeNode(cfg EdgeNodeConfig) *EdgeNode {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	opts := retry.Options{
		Initial:     time.Second,
		Max:         30 * time.Second,
		MaxAttempts: 0,
	}
	if cfg.RetryOptions != nil {
		opts = *cfg.RetryOptions
	}
	var filter *cov.Filter
	if cfg.ChangeOnValue {
		filter = cov.NewFilter()
	}
	e := &EdgeNode{
		cov:          filter,
		groupID:      cfg.GroupID,
		nodeID:       cfg.NodeID,
		client:       cfg.Client,
		devices:      cfg.Devices,
		log:          log,
		retryOptions: opts,
		updates:      make(chan TagUpdate, 256),
	}
	if cfg.OnCommand != nil {
		h := cfg.OnCommand
		e.onCommand.Store(&h)
	}
	return e
}

// State returns the current state machine state.
func (e *EdgeNode) State() State {
	return e.sm.State()
}

// BdSeq returns the current birth-death sequence counter value (R69-3).
func (e *EdgeNode) BdSeq() uint64 {
	return e.bdSeq.Load()
}

// SetCommandHandler sets the handler for inbound DCMD metric writes.
// It MUST be called before Start (or after Stop): DCMDs can arrive as soon as
// Start subscribes, so a handler wired later would leave a window where writes
// bypass enforcement. Calling it while the node is started is a programming
// error and panics, so a refactor that reorders the wiring fails at the first
// run instead of silently disabling DCMD enforcement. (TWA-DCMD-3.2, #62)
func (e *EdgeNode) SetCommandHandler(h CommandHandler) {
	if e.started.Load() {
		panic("sparkplug: SetCommandHandler called after Start; wire the DCMD handler before Start")
	}
	if h == nil {
		e.onCommand.Store(nil)
		return
	}
	e.onCommand.Store(&h)
}

// Start connects to the MQTT broker, publishes NBIRTH + DBIRTH for all
// devices, subscribes to NCMD + DCMD (once), and transitions to ONLINE.
// ctx cancellation is forwarded to the reconnect loop — cancel it to stop
// reconnect attempts before calling Stop (useful in tests).
func (e *EdgeNode) Start(ctx context.Context) error {
	e.started.Store(true)
	if e.onCommand.Load() == nil {
		e.log.Warn("sparkplug: no DCMD command handler set; inbound DCMD writes will be dropped")
	}

	e.done = make(chan struct{})
	e.reconnectCh = make(chan struct{}, 1)

	e.client.SetOnConnect(e.onConnect)
	e.client.SetConnectionLost(e.onConnectionLost)

	e.sm.Transition(EventConnectAttempt)
	if err := e.client.Connect(ctx); err != nil {
		e.sm.Transition(EventConnectFail)
		e.started.Store(false)
		return fmt.Errorf("sparkplug: start: %w", err)
	}

	// Subscribe to commands exactly once per connection session (R68-1).
	if err := e.subscribeCommands(ctx); err != nil {
		e.log.Warn("sparkplug: initial subscribe error", slog.String("err", err.Error()))
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.publishLoop()
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.reconnectLoop(ctx)
	}()

	return nil
}

// Stop publishes DDEATHs → NDEATH → MQTT Disconnect in that order (R69-5).
// Waits for the publisher and reconnect goroutines to exit.
func (e *EdgeNode) Stop() error {
	ctx := context.Background()

	// DDEATHs first.
	for _, dev := range e.devices {
		seqVal := e.seq.Next()
		data, err := BuildDDEATH(dev.DeviceID, seqVal)
		if err != nil {
			e.log.Warn("sparkplug: DDEATH encode error", slog.String("device", dev.DeviceID), slog.String("err", err.Error()))
			continue
		}
		topic := deviceTopic(e.groupID, e.nodeID, dev.DeviceID, "DDEATH")
		if err := e.client.Publish(ctx, topic, 1, false, data); err != nil {
			e.log.Warn("sparkplug: DDEATH publish error", slog.String("device", dev.DeviceID), slog.String("err", err.Error()))
		}
	}

	// NDEATH with current bdSeq (R69-5 — NOT 0; LWT carries 0 per R69-4).
	ndeathData, err := BuildNDEATH(e.bdSeq.Load())
	if err != nil {
		e.log.Warn("sparkplug: NDEATH encode error", slog.String("err", err.Error()))
	} else {
		ndeathTopic := nodeTopic(e.groupID, e.nodeID, "NDEATH")
		if err := e.client.Publish(ctx, ndeathTopic, 1, false, ndeathData); err != nil {
			e.log.Warn("sparkplug: NDEATH publish error", slog.String("err", err.Error()))
		}
	}

	e.sm.Transition(EventDisconnect)

	// Signal goroutines to stop.
	if e.done != nil {
		close(e.done)
		e.wg.Wait()
	}

	e.client.Disconnect(250)
	e.started.Store(false)
	return nil
}

// HandleTagUpdate sends a tag update to the publisher goroutine.
// If the edge node is not ONLINE or the channel is full, the update is dropped.
// With ChangeOnValue enabled, updates that do not differ from the last value
// sent for the tag (beyond its deadband) are suppressed; a dropped update does
// not count as sent, so it is retried on the next scan.
func (e *EdgeNode) HandleTagUpdate(u TagUpdate) {
	if e.sm.State() != Online {
		return
	}
	sample := cov.Sample{Key: u.PLCName + "/" + u.Tag, Value: u.Value, Quality: u.Quality, Deadband: u.Deadband}
	e.cov.Offer(sample, func() bool {
		if e.sm.State() != Online {
			return false // went offline meanwhile: not sent
		}
		select {
		case e.updates <- u:
			return true
		default:
			e.log.Warn("sparkplug: tag update channel full, dropping", slog.String("tag", u.Tag))
			return false
		}
	})
}

// onConnect is called by the MQTT client on (re)connect. It publishes births.
// Subscriptions are NOT set up here — they are managed separately to ensure
// subscribe-once semantics (R68-1).
func (e *EdgeNode) onConnect() {
	e.publishBirths()
}

// publishBirths publishes NBIRTH + all DBIRTHs and transitions SM to Online.
// Called on initial connect and on reconnect. Also called on NCMD rebirth.
// bdSeq is incremented each time (R69-2, R69-3).
func (e *EdgeNode) publishBirths() {
	ctx := context.Background()
	bdSeqVal := e.bdSeq.Add(1)

	var allTags []TagDef
	for _, dev := range e.devices {
		allTags = append(allTags, dev.Tags...)
	}
	nbirthData, err := BuildNBIRTH(&e.seq, allTags, bdSeqVal)
	if err != nil {
		e.log.Error("sparkplug: NBIRTH encode error", slog.String("err", err.Error()))
		e.sm.Transition(EventConnectFail)
		return
	}
	topic := nodeTopic(e.groupID, e.nodeID, "NBIRTH")
	if err := e.client.Publish(ctx, topic, 1, false, nbirthData); err != nil {
		e.log.Error("sparkplug: NBIRTH publish error", slog.String("err", err.Error()))
		e.sm.Transition(EventConnectFail)
		return
	}

	for _, dev := range e.devices {
		dbirthSeq := e.seq.Next()
		tagValues := make(map[string]any)
		dbirthData, err := BuildDBIRTH(dev.DeviceID, tagValues, dbirthSeq)
		if err != nil {
			e.log.Error("sparkplug: DBIRTH encode error", slog.String("device", dev.DeviceID), slog.String("err", err.Error()))
			continue
		}
		dbirthTopic := deviceTopic(e.groupID, e.nodeID, dev.DeviceID, "DBIRTH")
		if err := e.client.Publish(ctx, dbirthTopic, 1, false, dbirthData); err != nil {
			e.log.Error("sparkplug: DBIRTH publish error", slog.String("device", dev.DeviceID), slog.String("err", err.Error()))
		}
	}

	e.sm.Transition(EventConnectSuccess)
	// Births carry no values, so every tag must be re-sent as DDATA (#60).
	// Reset after going Online: an update recorded as sent while the node was
	// still offline (and then dropped by publishLoop) is forgotten here.
	e.cov.Reset()
}

// subscribeCommands subscribes to NCMD and DCMD topics.
// For reconnect paths: call Unsubscribe first to avoid handler accumulation (R68-1).
func (e *EdgeNode) subscribeCommands(ctx context.Context) error {
	ncmdTopic := nodeTopic(e.groupID, e.nodeID, "NCMD")
	if err := e.client.Subscribe(ctx, ncmdTopic, 1, e.handleNCMD); err != nil {
		e.log.Warn("sparkplug: NCMD subscribe error", slog.String("err", err.Error()))
		return err
	}

	dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", e.groupID, e.nodeID)
	if err := e.client.Subscribe(ctx, dcmdTopic, 1, e.handleDCMD); err != nil {
		e.log.Warn("sparkplug: DCMD subscribe error", slog.String("err", err.Error()))
		return err
	}
	return nil
}

// resubscribeCommands unsubscribes then resubscribes — used on reconnect to
// avoid handler accumulation when CleanSession=true drops prior subscriptions (R68-1).
func (e *EdgeNode) resubscribeCommands(ctx context.Context) {
	ncmdTopic := nodeTopic(e.groupID, e.nodeID, "NCMD")
	dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", e.groupID, e.nodeID)

	// Unsubscribe-first is safe regardless of CleanSession setting.
	_ = e.client.Unsubscribe(ctx, ncmdTopic)
	_ = e.client.Unsubscribe(ctx, dcmdTopic)

	if err := e.subscribeCommands(ctx); err != nil {
		e.log.Warn("sparkplug: resubscribe error after reconnect", slog.String("err", err.Error()))
	}
}

// onConnectionLost is registered with the MQTT client (R69-1).
// It transitions the SM to Offline and signals the reconnect goroutine.
func (e *EdgeNode) onConnectionLost(err error) {
	e.log.Warn("sparkplug: connection lost", slog.String("err", err.Error()))
	e.sm.Transition(EventConnectionLost)

	// Signal reconnect goroutine (non-blocking; buffered channel capacity=1).
	select {
	case e.reconnectCh <- struct{}{}:
	default:
	}
}

// reconnectLoop waits for connection-loss signals and attempts reconnect with
// exponential backoff (mirroring internal/plc/manager.go reconnect pattern).
// On success it resubscribes commands and republishes births (R69-2).
// outerCtx is the context passed to Start; it controls retry cancellation.
func (e *EdgeNode) reconnectLoop(outerCtx context.Context) {
	for {
		select {
		case <-e.done:
			return
		case <-outerCtx.Done():
			return
		case <-e.reconnectCh:
		}

		// Merge outerCtx and done so either can abort the retry.
		ctx, cancel := context.WithCancel(outerCtx)

		// Watch done in parallel so Stop() also aborts the retry.
		go func() {
			select {
			case <-e.done:
				cancel()
			case <-ctx.Done():
			}
		}()

		err := retry.Do(ctx, e.retryOptions, func(ctx context.Context) error {
			return e.client.Connect(ctx)
		})

		cancel()

		if err != nil {
			// Context cancelled (Stop or outer ctx) — exit.
			return
		}

		// Reconnect succeeded: resubscribe then republish births.
		e.resubscribeCommands(context.Background())
		e.publishBirths()

		// Drain extra reconnect signals that may have fired during retry.
		select {
		case <-e.reconnectCh:
		default:
		}
	}
}

func (e *EdgeNode) handleNCMD(_ string, payload []byte) {
	var p pb.Payload
	if err := proto.Unmarshal(payload, &p); err != nil {
		e.log.Warn("sparkplug: NCMD decode error", slog.String("err", err.Error()))
		return
	}
	for _, m := range p.Metrics {
		if m.Name != nil && *m.Name == "Node Control/Rebirth" {
			if v, ok := m.Value.(*pb.Payload_Metric_BooleanValue); ok && v.BooleanValue {
				e.log.Info("sparkplug: rebirth requested via NCMD")
				// Rebirth: republish births only — do NOT resubscribe (R68-1).
				e.publishBirths()
				return
			}
		}
	}
}

func (e *EdgeNode) handleDCMD(topic string, payload []byte) {
	hp := e.onCommand.Load()
	if hp == nil {
		return
	}
	onCommand := *hp

	// Extract device ID from topic: spBv1.0/{group}/DCMD/{node}/{device}
	parts := strings.Split(topic, "/")
	if len(parts) < 5 {
		return
	}
	deviceID := parts[4]

	var p pb.Payload
	if err := proto.Unmarshal(payload, &p); err != nil {
		e.log.Warn("sparkplug: DCMD decode error", slog.String("device", deviceID), slog.String("err", err.Error()))
		return
	}

	for _, m := range p.Metrics {
		if m.Name == nil {
			continue
		}
		val := DecodeMetricValue(m)
		if val == nil {
			continue
		}
		onCommand(deviceID, *m.Name, val)
	}
}

func (e *EdgeNode) publishLoop() {
	for {
		select {
		case <-e.done:
			return
		case u := <-e.updates:
			if e.sm.State() != Online {
				continue
			}
			seqVal := e.seq.Next()
			data, err := BuildDDATA([]TagUpdate{u}, seqVal)
			if err != nil {
				e.log.Warn("sparkplug: DDATA encode error", slog.String("tag", u.Tag), slog.String("err", err.Error()))
				continue
			}
			topic := deviceTopic(e.groupID, e.nodeID, u.PLCName, "DDATA")
			if err := e.client.Publish(context.Background(), topic, 0, false, data); err != nil {
				e.log.Warn("sparkplug: DDATA publish error", slog.String("tag", u.Tag), slog.String("err", err.Error()))
			}
		}
	}
}

func nodeTopic(group, node, verb string) string {
	return fmt.Sprintf("spBv1.0/%s/%s/%s", group, verb, node)
}

func deviceTopic(group, node, device, verb string) string {
	return fmt.Sprintf("spBv1.0/%s/%s/%s/%s", group, verb, node, device)
}
