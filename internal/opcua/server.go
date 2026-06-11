package opcua

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
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

	srv, err := newOPCUAServer(s.cfg, host, port)
	if err != nil {
		return err
	}
	s.srv = srv

	s.populateAddressSpace()

	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.log.Info("opcua server starting",
		slog.String("host", host),
		slog.Int("port", port))

	go s.refreshLoop(ctx)

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
				return dataValueForTV(tv)
			})
		}
	}
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
