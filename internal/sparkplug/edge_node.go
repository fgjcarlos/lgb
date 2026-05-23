package sparkplug

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/fgjcarlos/lgb/internal/mqtt"
)

// DeviceConfig maps a PLC to its Sparkplug metric definitions.
type DeviceConfig struct {
	DeviceID string
	Tags     []TagDef
}

// EdgeNodeConfig configures the Sparkplug B edge node.
type EdgeNodeConfig struct {
	GroupID string
	NodeID  string
	Client  mqtt.Client
	Devices []DeviceConfig
	Log     *slog.Logger
}

// EdgeNode orchestrates the Sparkplug B lifecycle.
type EdgeNode struct {
	groupID string
	nodeID  string
	client  mqtt.Client
	devices []DeviceConfig
	log     *slog.Logger

	sm    StateMachine
	seq   SeqTracker
	bdSeq atomic.Uint64

	updates chan TagUpdate
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewEdgeNode creates an EdgeNode from the given configuration.
func NewEdgeNode(cfg EdgeNodeConfig) *EdgeNode {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &EdgeNode{
		groupID: cfg.GroupID,
		nodeID:  cfg.NodeID,
		client:  cfg.Client,
		devices: cfg.Devices,
		log:     log,
		updates: make(chan TagUpdate, 256),
	}
}

// State returns the current state machine state.
func (e *EdgeNode) State() State {
	return e.sm.State()
}

// Start connects to the MQTT broker, publishes NBIRTH + DBIRTH for all
// devices, and transitions to ONLINE.
func (e *EdgeNode) Start(ctx context.Context) error {
	e.client.SetOnConnect(e.onConnect)

	e.sm.Transition(EventConnectAttempt)
	if err := e.client.Connect(ctx); err != nil {
		e.sm.Transition(EventConnectFail)
		return fmt.Errorf("sparkplug: start: %w", err)
	}

	e.done = make(chan struct{})
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.publishLoop()
	}()

	return nil
}

// Stop publishes DDEATH for each device, disconnects MQTT, and transitions
// to OFFLINE. Waits for the publisher goroutine to exit.
func (e *EdgeNode) Stop() error {
	ctx := context.Background()

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

	e.sm.Transition(EventDisconnect)
	if e.done != nil {
		close(e.done)
		e.wg.Wait()
	}
	e.client.Disconnect(250)
	return nil
}

// HandleTagUpdate sends a tag update to the publisher goroutine.
// If the edge node is not ONLINE or the channel is full, the update is dropped.
func (e *EdgeNode) HandleTagUpdate(u TagUpdate) {
	if e.sm.State() != Online {
		return
	}
	select {
	case e.updates <- u:
	default:
		e.log.Warn("sparkplug: tag update channel full, dropping", slog.String("tag", u.Tag))
	}
}

func (e *EdgeNode) onConnect() {
	ctx := context.Background()

	// BuildNBIRTH calls seq.Reset() internally, ensuring seq=0.
	var allTags []TagDef
	for _, dev := range e.devices {
		allTags = append(allTags, dev.Tags...)
	}
	nbirthData, err := BuildNBIRTH(&e.seq, allTags)
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
