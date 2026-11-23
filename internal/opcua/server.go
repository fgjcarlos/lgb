package opcua

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua/id"
	opcserver "github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/writeguard"
)

// TagSource provides read access to current PLC tag values.
type TagSource interface {
	CurrentTag(plcName, tag string) (plc.TagValue, bool)
	CurrentSnapshot() map[string]map[string]plc.TagValue
}

// tagKey identifies a tag node in the address space.
type tagKey struct {
	plc string
	tag string
}

// tagNode describes a tag node exposed by the address space.
type tagNode struct {
	typ string // declared tag type
	id  *ua.NodeID
}

// addressSpace is an immutable snapshot of the PLC namespaces and tag nodes
// the server exposes. Reload builds a new one and swaps it atomically, so the
// value callbacks, the write path and the refresh loop read it without locks.
type addressSpace struct {
	// nsPLC maps a namespace id to the PLC that owns it, so the write handler
	// resolves a node id to its (plc, tag) pair without parsing namespace URIs.
	nsPLC map[uint16]string
	// tags is the authoritative set of exposed tag nodes and their types.
	tags map[tagKey]tagNode
}

// Server wraps a gopcua OPC UA server and populates the address space
// from the configured PLCs.
type Server struct {
	cfg  *config.Config
	tags TagSource
	log  *slog.Logger
	srv  *opcserver.Server

	// space is the current address space snapshot (nil before population).
	space atomic.Pointer[addressSpace]

	// reloadMu serializes address space rebuilds and guards the fields below.
	reloadMu sync.Mutex
	// plcs is the PLC list the address space is built from: the boot config,
	// then whatever the last Reload received.
	plcs []config.PLC
	// namespaces holds every per-PLC namespace ever created. gopcua cannot
	// delete namespaces or nodes, so a removed PLC keeps its namespace (and
	// its index, reused if the PLC comes back); only space decides what is
	// served.
	namespaces map[string]*opcserver.NodeNameSpace
	// nodeLevels records the AccessLevel of the node created for each tag, so
	// a reload only replaces a node when its AccessLevel changes.
	nodeLevels map[tagKey]ua.AccessLevelType

	// Write surface dependencies (see Options). They are nil-safe: without a
	// guard or a writer every write is denied.
	guard  *writeguard.Guard
	writer writeguard.TagWriter
	audit  writeguard.AuditSink

	// tagMeta exposes the per-tag metadata used to derive the node AccessLevel
	// attribute. When nil the server's own configuration is used.
	tagMeta writeguard.TagReadable

	mu      sync.Mutex
	running bool
}

// New creates an OPC UA Server. The server is not started until Start is called.
//
// opts carries the optional dependencies of the write surface (guard, PLC
// writer, audit sink). It is variadic so that the existing call sites
// (New(cfg, tags, logger)) keep compiling: without options the server is
// read-only and every write is denied.
func New(cfg *config.Config, tags TagSource, log *slog.Logger, opts ...Options) *Server {
	if log == nil {
		log = slog.Default()
	}
	var o Options
	for _, opt := range opts {
		o = mergeOptions(o, opt)
	}
	return &Server{
		cfg:        cfg,
		tags:       tags,
		log:        log,
		plcs:       cfg.PLCs,
		namespaces: make(map[string]*opcserver.NodeNameSpace),
		nodeLevels: make(map[tagKey]ua.AccessLevelType),
		guard:      o.Guard,
		writer:     o.Writer,
		audit:      o.Audit,
		tagMeta:    o.Tags,
	}
}

// mergeOptions merges two Options values: every non-nil field of next
// overrides the corresponding field of base.
func mergeOptions(base, next Options) Options {
	if next.Guard != nil {
		base.Guard = next.Guard
	}
	if next.Writer != nil {
		base.Writer = next.Writer
	}
	if next.Audit != nil {
		base.Audit = next.Audit
	}
	if next.Tags != nil {
		base.Tags = next.Tags
	}
	return base
}

// Start initializes the OPC UA server, populates the address space, and begins
// serving. It returns after the server is listening; call Stop to shut down.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("opcua: server already running")
	}
	s.mu.Unlock()

	host := s.cfg.OPCUA.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := s.cfg.OPCUA.Port
	if port <= 0 {
		port = 4840
	}

	srv, err := newOPCUAServer(s.cfg, host, port)
	if err != nil {
		return err
	}
	s.reloadMu.Lock()
	s.mu.Lock()
	s.srv = srv // under mu: Health reads it concurrently
	s.mu.Unlock()
	// Namespaces and nodes belong to the previous gopcua server, if any.
	s.namespaces = make(map[string]*opcserver.NodeNameSpace)
	s.nodeLevels = make(map[tagKey]ua.AccessLevelType)
	s.reloadMu.Unlock()

	// Register the gateway's Write handler BEFORE srv.Start(ctx): gopcua
	// registers its default Write service inside Server.Start (initHandlers)
	// and RegisterHandler does NOT override an already-registered handler.
	// Without this the default service would replace the node's value callback
	// and silently accept writes that never reach the PLC. (TWA-OPCUA-3.3)
	srv.RegisterHandler(id.WriteRequest_Encoding_DefaultBinary, s.handleWriteRequest)

	s.populateAddressSpace()

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.log.Info("opcua server starting",
		slog.String("host", host),
		slog.Int("port", port))

	go s.refreshLoop(ctx, srv)

	err = srv.Start(ctx)

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("opcua: start: %w", err)
	}
	return nil
}

func newOPCUAServer(cfg *config.Config, host string, port int) (*opcserver.Server, error) {
	opts, err := serverOptions(cfg, host, port)
	if err != nil {
		return nil, err
	}
	return opcserver.New(opts...), nil
}

func serverOptions(cfg *config.Config, host string, port int) ([]opcserver.Option, error) {
	opts := []opcserver.Option{
		opcserver.EndPoint(host, port),
		opcserver.EnableAuthMode(ua.UserTokenTypeAnonymous),
		opcserver.ServerName("LGB OPC UA Server"),
	}

	switch cfg.OPCUA.SecurityMode {
	case "None":
		opts = append(opts, opcserver.EnableSecurity("None", ua.MessageSecurityModeNone))
	case "Sign", "SignAndEncrypt":
		cert, key, err := loadCertificate(cfg.OPCUA.CertFile, cfg.OPCUA.KeyFile)
		if err != nil {
			return nil, err
		}

		mode := ua.MessageSecurityModeSign
		if cfg.OPCUA.SecurityMode == "SignAndEncrypt" {
			mode = ua.MessageSecurityModeSignAndEncrypt
		}

		opts = append(opts,
			opcserver.Certificate(cert),
			opcserver.PrivateKey(key),
			opcserver.EnableSecurity("Basic256Sha256", mode),
		)
	default:
		return nil, fmt.Errorf("opcua: securityMode must be one of None, Sign, or SignAndEncrypt, got %q", cfg.OPCUA.SecurityMode)
	}

	return opts, nil
}

func loadCertificate(certFile, keyFile string) ([]byte, *rsa.PrivateKey, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("opcua: load certificate: %w", err)
	}
	if len(cert.Certificate) == 0 {
		return nil, nil, fmt.Errorf("opcua: load certificate: no certificate data in %q", certFile)
	}

	key, ok := cert.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("opcua: private key must be RSA for Basic256Sha256")
	}

	return cert.Certificate[0], key, nil
}

// Health reports the OPC UA server state as gopcua tracks it ("Running",
// "Suspended", "Shutdown", ...; "NotStarted" before Start) and the endpoint
// URLs it listens on.
func (s *Server) Health() (state string, endpoints []string) {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return "NotStarted", nil
	}
	return strings.TrimPrefix(srv.Status().State.String(), "ServerState"), srv.URLs()
}

// Stop gracefully shuts down the OPC UA server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	if s.srv != nil {
		s.srv.Close()
	}
	s.log.Info("opcua server stopped")
	return nil
}

// dataValueForTV converts a TagValue to a ua.DataValue, returning a bad
// StatusCode when Quality is "bad". Used by populateAddressSpace and
// DataValueForTag (test helper).
func dataValueForTV(tv plc.TagValue) *ua.DataValue {
	if tv.Quality == "bad" {
		return &ua.DataValue{Status: ua.StatusBad}
	}
	return opcserver.DataValueFromValue(tv.Value)
}

// Reload replaces the PLCs and tags exposed by the address space with those of
// cfg (only cfg.PLCs is used). New PLCs and tags become readable and writable,
// removed ones answer BadNodeIDUnknown, and unaffected nodes are left as they
// are, so sessions using them are not disrupted. It is safe to call
// concurrently with Start, client requests and other Reload calls; before the
// OPC UA server exists it only records the PLCs for Start to build from. (#82)
func (s *Server) Reload(cfg *config.Config) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.plcs = cfg.PLCs
	s.mu.Lock()
	built := s.srv != nil
	s.mu.Unlock()
	if built {
		s.buildAddressSpace()
	}
}

// populateAddressSpace builds the address space from the current PLC list.
func (s *Server) populateAddressSpace() {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.buildAddressSpace()
}

// buildAddressSpace creates the namespaces and nodes s.plcs needs and
// publishes the new snapshot. Existing nodes are reused. Callers hold reloadMu.
func (s *Server) buildAddressSpace() {
	next := &addressSpace{nsPLC: make(map[uint16]string), tags: make(map[tagKey]tagNode)}
	for _, plcCfg := range s.plcs {
		ns, ok := s.namespaces[plcCfg.Name]
		if !ok {
			ns = opcserver.NewNodeNameSpace(s.srv, fmt.Sprintf("urn:lgb:plc:%s", plcCfg.Name))
			s.namespaces[plcCfg.Name] = ns
		}
		next.nsPLC[ns.ID()] = plcCfg.Name

		for _, tag := range plcCfg.Tags {
			key := tagKey{plc: plcCfg.Name, tag: tag.Name}
			nodeID := ua.NewStringNodeID(ns.ID(), tag.Name)
			next.tags[key] = tagNode{typ: tag.Type, id: nodeID}

			// The AccessLevel attribute is a client hint that mirrors the
			// server policy: CurrentRead|CurrentWrite only when the tag is
			// OPC UA writable, CurrentRead alone otherwise. The guard stays
			// the enforcement point. (TWA-OPCUA-3.3)
			level := ua.AccessLevelTypeCurrentRead
			if s.tagOPCUAWritable(key.plc, key.tag, tag) {
				level |= ua.AccessLevelTypeCurrentWrite
			}
			if prev, ok := s.nodeLevels[key]; ok && prev == level {
				continue
			}
			// A changed node is replaced, never mutated: node attributes are a
			// plain map that concurrent client requests read without locks.
			node := opcserver.NewVariableNode(nodeID, tag.Name, func() *ua.DataValue {
				return s.tagValue(key)
			})
			_ = node.SetAttribute(ua.AttributeIDAccessLevel, opcserver.DataValueFromValue(uint8(level)))
			ns.AddNode(node)
			s.nodeLevels[key] = level
		}
	}
	s.space.Store(next)
}

// tagValue serves the Value attribute of a tag node: BadNodeIDUnknown once the
// tag is no longer part of the address space, its live value otherwise.
//
// shortcut: a removed node's other attributes (BrowseName, AccessLevel...) stay
// readable because gopcua v0.8 cannot delete nodes; drop the node instead if
// gopcua gains node removal.
func (s *Server) tagValue(key tagKey) *ua.DataValue {
	if sp := s.space.Load(); sp == nil {
		return unknownNode()
	} else if _, ok := sp.tags[key]; !ok {
		return unknownNode()
	}
	tv, ok := s.tags.CurrentTag(key.plc, key.tag)
	if !ok {
		return opcserver.DataValueFromValue(nil)
	}
	return dataValueForTV(tv)
}

func unknownNode() *ua.DataValue {
	return &ua.DataValue{
		EncodingMask:    ua.DataValueServerTimestamp | ua.DataValueStatusCode,
		ServerTimestamp: time.Now(),
		Status:          ua.StatusBadNodeIDUnknown,
	}
}

// tagOPCUAWritable reports whether the tag may be written over OPC UA.
//
// The answer comes from the same metadata the write guard reads (the PLC
// config store) so the client-visible AccessLevel matches what the gateway
// actually accepts. When no metadata reader is wired, the server's own
// configuration is used.
//
// (TWA-OPCUA-3.3 AccessLevel requirement)
func (s *Server) tagOPCUAWritable(plcName, tagName string, tag config.TagDef) bool {
	if s.tagMeta != nil {
		meta, ok := s.tagMeta.TagMeta(context.Background(), plcName, tagName)
		if !ok {
			return false
		}
		return meta.Writable && meta.OPCUAEnabled
	}
	return tag.Writable && tag.OPCUAEnabled
}

// DataValueForTag returns the ua.DataValue that the OPC UA address space would
// serve for the named tag. Exported for testing without starting the server.
func (s *Server) DataValueForTag(plcName, tag string) *ua.DataValue {
	tv, ok := s.tags.CurrentTag(plcName, tag)
	if !ok {
		return opcserver.DataValueFromValue(nil)
	}
	return dataValueForTV(tv)
}

// refreshLoop notifies monitored items of every exposed tag periodically. Tags
// removed by a Reload get one last notification so subscribers see
// BadNodeIDUnknown instead of keeping a stale value.
func (s *Server) refreshLoop(ctx context.Context, srv *opcserver.Server) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var prev *addressSpace
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sp := s.space.Load()
			if sp == nil {
				continue
			}
			if prev != nil && prev != sp {
				for key, t := range prev.tags {
					if _, ok := sp.tags[key]; !ok {
						srv.ChangeNotification(t.id)
					}
				}
			}
			prev = sp
			for _, t := range sp.tags {
				srv.ChangeNotification(t.id)
			}
		}
	}
}
