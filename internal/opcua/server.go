package opcua

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	opcserver "github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// TagSource provides read access to current PLC tag values.
type TagSource interface {
	CurrentTag(plcName, tag string) (plc.TagValue, bool)
	CurrentSnapshot() map[string]map[string]plc.TagValue
}

// Server wraps a gopcua OPC UA server and populates the address space
// from the configured PLCs.
type Server struct {
	cfg  *config.Config
	tags TagSource
	log  *slog.Logger
	srv  *opcserver.Server

	// namespaces tracks per-PLC NodeNameSpace instances for value updates.
	namespaces map[string]*opcserver.NodeNameSpace

	mu      sync.Mutex
	running bool
}

// New creates an OPC UA Server. The server is not started until Start is called.
func New(cfg *config.Config, tags TagSource, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		cfg:        cfg,
		tags:       tags,
		log:        log,
		namespaces: make(map[string]*opcserver.NodeNameSpace),
	}
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

	opts := []opcserver.Option{
		opcserver.EndPoint(host, port),
		opcserver.EnableSecurity("None", ua.MessageSecurityModeNone),
		opcserver.EnableAuthMode(ua.UserTokenTypeAnonymous),
		opcserver.ServerName("LGB OPC UA Server"),
	}

	if s.cfg.OPCUA.SecurityMode == "Sign" || s.cfg.OPCUA.SecurityMode == "SignAndEncrypt" {
		mode := ua.MessageSecurityModeSign
		if s.cfg.OPCUA.SecurityMode == "SignAndEncrypt" {
			mode = ua.MessageSecurityModeSignAndEncrypt
		}
		opts = append(opts, opcserver.EnableSecurity("Basic256Sha256", mode))
	}

	srv := opcserver.New(opts...)
	s.srv = srv

	s.populateAddressSpace()

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.log.Info("opcua server starting",
		slog.String("host", host),
		slog.Int("port", port))

	go s.refreshLoop(ctx)

	err := srv.Start(ctx)

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("opcua: start: %w", err)
	}
	return nil
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

func (s *Server) populateAddressSpace() {
	for _, plcCfg := range s.cfg.PLCs {
		ns := opcserver.NewNodeNameSpace(s.srv, fmt.Sprintf("urn:lgb:plc:%s", plcCfg.Name))
		s.namespaces[plcCfg.Name] = ns

		for _, tag := range plcCfg.Tags {
			plcName := plcCfg.Name
			tagName := tag.Name
			ns.AddNewVariableStringNode(tagName, func() *ua.DataValue {
				tv, ok := s.tags.CurrentTag(plcName, tagName)
				if !ok {
					return opcserver.DataValueFromValue(nil)
				}
				return opcserver.DataValueFromValue(tv.Value)
			})
		}
	}
}

func (s *Server) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for plcName, ns := range s.namespaces {
				for _, plcCfg := range s.cfg.PLCs {
					if plcCfg.Name != plcName {
						continue
					}
					for _, tag := range plcCfg.Tags {
						nodeID := ua.NewStringNodeID(ns.ID(), tag.Name)
						ns.ChangeNotification(nodeID)
					}
				}
			}
		}
	}
}
