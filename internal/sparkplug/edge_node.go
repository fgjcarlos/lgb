package sparkplug

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/fgjcarlos/lgb/internal/mqtt"
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
	GroupID   string
	NodeID    string
	Client    mqtt.Client
	Devices   []DeviceConfig
	Log       *slog.Logger
	OnCommand CommandHandler
}

// EdgeNode orchestrates the Sparkplug B lifecycle.
type EdgeNode struct {
	groupID   string
	nodeID    string
	client    mqtt.Client
	devices   []DeviceConfig
	log       *slog.Logger
	onCommand CommandHandler

	sm  StateMachine
	seq SeqTracker

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
		groupID:   cfg.GroupID,
		nodeID:    cfg.NodeID,
		client:    cfg.Client,
		devices:   cfg.Devices,
		log:       log,
		onCommand: cfg.OnCommand,
		updates:   make(chan TagUpdate, 256),
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

	// Subscribe to NCMD (node commands) and DCMD (device commands).
	ncmdTopic := nodeTopic(e.groupID, e.nodeID, "NCMD")
	if err := e.client.Subscribe(ctx, ncmdTopic, 1, e.handleNCMD); err != nil {
		e.log.Warn("sparkplug: NCMD subscribe error", slog.String("err", err.Error()))
	}

	dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", e.groupID, e.nodeID)
	if err := e.client.Subscribe(ctx, dcmdTopic, 1, e.handleDCMD); err != nil {
		e.log.Warn("sparkplug: DCMD subscribe error", slog.String("err", err.Error()))
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
				e.onConnect()
				return
			}
		}
	}
}

func (e *EdgeNode) handleDCMD(topic string, payload []byte) {
	if e.onCommand == nil {
		return
	}

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
		e.onCommand(deviceID, *m.Name, val)
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
