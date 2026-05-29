package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// startConfigTestServer mounts a real server with a caller-supplied config
// and PLC manager, so the GET /api/config/mappings handler can read
// s.cfg.PLCs directly.
func startConfigTestServer(t *testing.T, cfg *config.Config, mgr PLCManager, opts Opts) (string, func()) {
	t.Helper()
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	logger := testutil.NewLogger(t)
	opts.PLCMgr = mgr
	srv := New(cfg, logger, nil, opts)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	addr := srv.Addr()
	if addr == "" {
		cancel()
		t.Fatal("server did not bind")
	}
	stop := func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("server shutdown: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("server did not stop")
		}
	}
	return fmt.Sprintf("http://%s", addr), stop
}

func configWithPLCs(plcs []config.PLC) *config.Config {
	c := &config.Config{}
	c.Server.HTTPAddr = "127.0.0.1:0"
	c.Gateway.LogLevel = "info"
	c.PLCs = plcs
	return c
}

// TestHandleConfigMappings_ReturnsConfiguredPLCs verifies that the endpoint
// returns one row per configured PLC with its tag definitions.
func TestHandleConfigMappings_ReturnsConfiguredPLCs(t *testing.T) {
	cfg := configWithPLCs([]config.PLC{
		{
			Name:     "packaging",
			Address:  "192.168.1.50:44818",
			ScanRate: "200ms",
			Tags: []config.TagDef{
				{Name: "Speed", Type: "Float"},
				{Name: "Running", Type: "Bool"},
			},
		},
		{
			Name:     "mixing",
			Address:  "192.168.1.51:44818",
			ScanRate: "500ms",
			Tags:     []config.TagDef{{Name: "Level", Type: "Int"}},
		},
	})

	baseURL, stop := startConfigTestServer(t, cfg, &snapshotPLCManager{}, Opts{})
	defer stop()

	resp, err := http.Get(baseURL + "/api/config/mappings")
	if err != nil {
		t.Fatalf("GET /api/config/mappings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			PLC      string `json:"plc"`
			Address  string `json:"address"`
			ScanRate string `json:"scan_rate"`
			Tags     []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"tags"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("expected 2 PLCs, got %d", len(body.Data))
	}
	if body.Data[0].PLC != "packaging" || body.Data[0].Address != "192.168.1.50:44818" {
		t.Errorf("unexpected first PLC row: %+v", body.Data[0])
	}
	if len(body.Data[0].Tags) != 2 {
		t.Fatalf("expected packaging to have 2 tags, got %d", len(body.Data[0].Tags))
	}
	if body.Data[0].Tags[1].Name != "Running" || body.Data[0].Tags[1].Type != "Bool" {
		t.Errorf("unexpected second tag: %+v", body.Data[0].Tags[1])
	}
	if body.Data[1].PLC != "mixing" || body.Data[1].ScanRate != "500ms" {
		t.Errorf("unexpected second PLC row: %+v", body.Data[1])
	}
}

// TestHandleConfigMappings_EmptyConfigReturnsEmptyArray verifies that the
// response envelope keeps `data` as `[]`, never `null`.
func TestHandleConfigMappings_EmptyConfigReturnsEmptyArray(t *testing.T) {
	cfg := configWithPLCs(nil)
	baseURL, stop := startConfigTestServer(t, cfg, &snapshotPLCManager{}, Opts{})
	defer stop()

	resp, err := http.Get(baseURL + "/api/config/mappings")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), `"data":[]`) {
		t.Errorf("expected data to be an empty JSON array, got: %s", string(raw))
	}
}

// TestHandleConfigMappings_StoreCreate_ReflectsNewPLC verifies that after a
// store Create, GET /api/config/mappings returns the new PLC without restart.
// This covers the read-path redirect (PCS-API-2.6): the handler queries the
// store directly instead of the frozen s.cfg pointer.
func TestHandleConfigMappings_StoreCreate_ReflectsNewPLC(t *testing.T) {
	ctx := context.Background()
	store, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open plcstore: %v", err)
	}
	defer store.Close()

	cfg := configWithPLCs(nil) // start with no PLCs in cfg
	baseURL, stop := startConfigTestServer(t, cfg, &snapshotPLCManager{}, Opts{PLCStore: store})
	defer stop()

	// Insert a PLC directly into the store (simulating a prior POST /api/plcs).
	if err := store.Create(ctx, config.PLC{Name: "factory", Address: "10.0.1.1:44818", ScanRate: "1s", SocketTimeout: "5s"}); err != nil {
		t.Fatalf("store create: %v", err)
	}

	resp, err := http.Get(baseURL + "/api/config/mappings")
	if err != nil {
		t.Fatalf("GET /api/config/mappings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			PLC string `json:"plc"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 PLC from store, got %d", len(body.Data))
	}
	if body.Data[0].PLC != "factory" {
		t.Errorf("expected PLC 'factory', got %q", body.Data[0].PLC)
	}
}

// TestHandleConfigMappings_StoreDelete_PLCRemoved verifies that after a store
// Delete, GET /api/config/mappings no longer contains the deleted PLC.
// (PCS-API-2.6)
func TestHandleConfigMappings_StoreDelete_PLCRemoved(t *testing.T) {
	ctx := context.Background()
	store, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open plcstore: %v", err)
	}
	defer store.Close()

	// Pre-populate store.
	if err := store.Create(ctx, config.PLC{Name: "factory", Address: "10.0.1.1:44818", ScanRate: "1s", SocketTimeout: "5s"}); err != nil {
		t.Fatalf("store create: %v", err)
	}

	cfg := configWithPLCs(nil)
	baseURL, stop := startConfigTestServer(t, cfg, &snapshotPLCManager{}, Opts{PLCStore: store})
	defer stop()

	// Delete from store (simulating DELETE /api/plcs/factory).
	if err := store.Delete(ctx, "factory"); err != nil {
		t.Fatalf("store delete: %v", err)
	}

	resp, err := http.Get(baseURL + "/api/config/mappings")
	if err != nil {
		t.Fatalf("GET /api/config/mappings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			PLC string `json:"plc"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 0 {
		t.Errorf("expected 0 PLCs after store delete, got %d", len(body.Data))
	}
}

// TestHandleConfigMappings_AuthGated verifies that the endpoint is
// auth-gated when a TokenService is configured.
func TestHandleConfigMappings_AuthGated(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	cfg := configWithPLCs([]config.PLC{{Name: "p1", Address: "1.2.3.4:44818"}})

	baseURL, stop := startConfigTestServer(t, cfg, &snapshotPLCManager{},
		Opts{AuthTokens: tokens})
	defer stop()

	resp, err := http.Get(baseURL + "/api/config/mappings")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	tok, err := tokens.Issue(1, "viewer", auth.RoleViewer)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/config/mappings", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authed GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with viewer token, got %d", resp2.StatusCode)
	}
}
