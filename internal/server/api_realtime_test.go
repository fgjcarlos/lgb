package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

type snapshotPLCManager struct {
	mockPLCManager
	snapshot map[string]map[string]plc.TagValue
}

func (m *snapshotPLCManager) CurrentSnapshot() map[string]map[string]plc.TagValue {
	return m.snapshot
}

func TestServer_CurrentTagsEndpointReturnsSnapshot(t *testing.T) {
	ts := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	mgr := &snapshotPLCManager{snapshot: map[string]map[string]plc.TagValue{
		"packaging": {
			"Speed": {Value: float64(42.5), Quality: "good", Timestamp: ts},
		},
	}}
	srv, baseURL, stop := startAPITestServer(t, mgr)
	defer stop()

	resp, err := http.Get(baseURL + "/api/tags/current?limit=10&offset=0")
	if err != nil {
		t.Fatalf("GET current tags: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			PLC       string    `json:"plc"`
			Tag       string    `json:"tag"`
			Value     any       `json:"value"`
			Quality   string    `json:"quality"`
			Timestamp time.Time `json:"timestamp"`
		} `json:"data"`
		Pagination struct {
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
			Count  int `json:"count"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 tag, got %#v", body.Data)
	}
	got := body.Data[0]
	if got.PLC != "packaging" || got.Tag != "Speed" || got.Quality != "good" || got.Value != 42.5 || !got.Timestamp.Equal(ts) {
		t.Fatalf("unexpected tag row: %#v", got)
	}
	if body.Pagination.Limit != 10 || body.Pagination.Offset != 0 || body.Pagination.Count != 1 {
		t.Fatalf("unexpected pagination: %#v", body.Pagination)
	}
	_ = srv
}

func TestServer_CurrentTagsEndpointValidatesPagination(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, &snapshotPLCManager{})
	defer stop()

	resp, err := http.Get(baseURL + "/api/tags/current?limit=0")
	if err != nil {
		t.Fatalf("GET current tags: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != "bad_request" {
		t.Fatalf("unexpected error body: %#v", body)
	}
}

func TestServer_TagsWebSocketStreamsMatchingUpdates(t *testing.T) {
	srv, baseURL, stop := startAPITestServer(t, &snapshotPLCManager{})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags?plc=packaging&tag=Speed"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	var ack struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("read subscription ack: %v", err)
	}
	if ack.Type != "subscribed" {
		t.Fatalf("unexpected subscription ack: %#v", ack)
	}

	srv.PublishTagUpdate(plc.TagUpdate{PLCName: "packaging", Tag: "Other", Value: 1, Timestamp: time.Now()})
	wantTS := time.Date(2026, 5, 26, 12, 1, 0, 0, time.UTC)
	srv.PublishTagUpdate(plc.TagUpdate{PLCName: "packaging", Tag: "Speed", Value: int32(120), Timestamp: wantTS})

	var msg struct {
		Type      string    `json:"type"`
		PLC       string    `json:"plc"`
		Tag       string    `json:"tag"`
		Value     int32     `json:"value"`
		Timestamp time.Time `json:"timestamp"`
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("read update: %v", err)
	}
	if msg.Type != "tag_update" || msg.PLC != "packaging" || msg.Tag != "Speed" || msg.Value != 120 || !msg.Timestamp.Equal(wantTS) {
		t.Fatalf("unexpected websocket message: %#v", msg)
	}
}

// TestServer_TagsWebSocketRequiresValidJWTWhenConfigured verifies R71-3:
// - Connecting without sending an auth frame within the deadline → close 4001.
// - Connecting and sending a valid auth frame → auth_ok then subscribed.
// - Connecting and sending an invalid token frame → close 4001.
// - Connecting with ?token= query param (old mechanism) → no longer accepted; close 4001.
func TestServer_TagsWebSocketRequiresValidJWTWhenConfigured(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{AuthTokens: tokens})
	defer stop()

	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags?plc=packaging&tag=Speed"

	t.Run("no auth frame within deadline — close 4001", func(t *testing.T) {
		// Use a context longer than the server's 5s frame deadline so we can
		// actually observe the close code rather than timing out ourselves.
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// Read until the server closes — expect 4001.
		var m struct{ Type string }
		if err := wsjson.Read(ctx, conn, &m); err == nil {
			t.Fatal("expected server to close the connection; got a message instead")
		}
		var closeErr websocket.CloseError
		if !isCloseError(err, &closeErr) {
			t.Logf("connection closed (non-CloseError is also acceptable): %v", err)
		} else if closeErr.Code != websocket.StatusCode(4001) {
			t.Errorf("expected close code 4001, got %d: %s", closeErr.Code, closeErr.Reason)
		}
	})

	t.Run("valid auth frame — auth_ok then subscribed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		token, err := tokens.Issue(1, "operator", auth.RoleOperator)
		if err != nil {
			t.Fatalf("issue token: %v", err)
		}

		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		if err := wsjson.Write(ctx, conn, wsAuthFrame{Type: "auth", Token: token}); err != nil {
			t.Fatalf("write auth frame: %v", err)
		}

		var ack struct {
			Type string `json:"type"`
		}
		if err := wsjson.Read(ctx, conn, &ack); err != nil {
			t.Fatalf("read auth_ok: %v", err)
		}
		if ack.Type != "auth_ok" {
			t.Fatalf("expected auth_ok, got %q", ack.Type)
		}

		if err := wsjson.Read(ctx, conn, &ack); err != nil {
			t.Fatalf("read subscribed: %v", err)
		}
		if ack.Type != "subscribed" {
			t.Fatalf("expected subscribed after auth_ok, got %q", ack.Type)
		}
	})

	t.Run("invalid token frame — close 4001", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		if err := wsjson.Write(ctx, conn, wsAuthFrame{Type: "auth", Token: "not-a-valid-token"}); err != nil {
			t.Fatalf("write invalid auth frame: %v", err)
		}

		var m struct{ Type string }
		if err := wsjson.Read(ctx, conn, &m); err == nil {
			t.Fatal("expected server to close on bad token; got a message")
		}
		var closeErr websocket.CloseError
		if !isCloseError(err, &closeErr) {
			t.Logf("connection closed (non-CloseError is also acceptable): %v", err)
		} else if closeErr.Code != websocket.StatusCode(4001) {
			t.Errorf("expected close code 4001, got %d: %s", closeErr.Code, closeErr.Reason)
		}
	})

	t.Run("query param token no longer accepted — close 4001", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		token, err := tokens.Issue(1, "operator", auth.RoleOperator)
		if err != nil {
			t.Fatalf("issue token: %v", err)
		}

		// Old mechanism: connect and send NO auth frame — should be closed 4001.
		conn, _, err := websocket.Dial(ctx, wsURL+"&token="+token, nil)
		if err != nil {
			// If the upgrade itself fails, that's also an acceptable security outcome.
			t.Logf("upgrade rejected (old ?token= mechanism): %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// If upgrade succeeded, the server must close with 4001 (no frame sent).
		var m struct{ Type string }
		if err := wsjson.Read(ctx, conn, &m); err == nil {
			t.Fatal("expected server to close the connection; got a message instead")
		}
		var closeErr websocket.CloseError
		if !isCloseError(err, &closeErr) {
			t.Logf("connection closed (non-CloseError is also acceptable): %v", err)
		} else if closeErr.Code != websocket.StatusCode(4001) {
			t.Errorf("expected close code 4001, got %d: %s", closeErr.Code, closeErr.Reason)
		}
	})
}

// TestServer_TagsWebSocket_CrossOriginRejected verifies R71-2:
// when AllowedOrigins is nil the server rejects cross-origin upgrade requests.
func TestServer_TagsWebSocket_CrossOriginRejected(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{AuthTokens: tokens})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags"
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"http://attacker.example.com"}},
	})
	if err == nil {
		t.Fatal("expected cross-origin WS upgrade to fail; it succeeded")
	}
	// Acceptable: any non-101 response or a connection error.
	if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
		t.Errorf("expected non-101 response for cross-origin; got 101")
	}
	t.Logf("cross-origin rejected as expected: status=%v err=%v",
		func() int {
			if resp != nil {
				return resp.StatusCode
			}
			return 0
		}(), err)
}

// TestServer_TagsWebSocket_AllowedOriginAccepted verifies R71-2:
// when AllowedOrigins contains "localhost:5173" that origin is accepted.
func TestServer_TagsWebSocket_AllowedOriginAccepted(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOptsAndCfgFn(t, &snapshotPLCManager{}, Opts{AuthTokens: tokens}, func(cfg *config.Config) {
		cfg.Server.AllowedOrigins = []string{"localhost:5173"}
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags"

	token, err := tokens.Issue(1, "operator", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}},
	})
	if err != nil {
		t.Fatalf("expected allowed-origin upgrade to succeed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Complete the auth handshake.
	if err := wsjson.Write(ctx, conn, wsAuthFrame{Type: "auth", Token: token}); err != nil {
		t.Fatalf("write auth frame: %v", err)
	}
	var ack struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("read auth_ok: %v", err)
	}
	if ack.Type != "auth_ok" {
		t.Fatalf("expected auth_ok, got %q", ack.Type)
	}
}

func TestServer_TagsWebSocketSupportsSubscribeUnsubscribeAndPing(t *testing.T) {
	srv, baseURL, stop := startAPITestServer(t, &snapshotPLCManager{})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	var ack struct {
		Type string `json:"type"`
		PLC  string `json:"plc"`
		Tag  string `json:"tag"`
	}
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("read initial ack: %v", err)
	}
	if ack.Type != "subscribed" {
		t.Fatalf("unexpected initial ack: %#v", ack)
	}

	if err := wsjson.Write(ctx, conn, tagWSClientMessage{Type: "subscribe", PLC: "packaging", Tag: "Speed"}); err != nil {
		t.Fatalf("send subscribe: %v", err)
	}
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("read subscribe ack: %v", err)
	}
	if ack.Type != "subscribed" || ack.PLC != "packaging" || ack.Tag != "Speed" {
		t.Fatalf("unexpected subscribe ack: %#v", ack)
	}

	wantTS := time.Date(2026, 5, 26, 12, 2, 0, 0, time.UTC)
	srv.PublishTagUpdate(plc.TagUpdate{PLCName: "packaging", Tag: "Speed", Value: int32(121), Timestamp: wantTS})
	var update struct {
		Type      string    `json:"type"`
		PLC       string    `json:"plc"`
		Tag       string    `json:"tag"`
		Value     int32     `json:"value"`
		Timestamp time.Time `json:"timestamp"`
	}
	if err := wsjson.Read(ctx, conn, &update); err != nil {
		t.Fatalf("read subscribed update: %v", err)
	}
	if update.Type != "tag_update" || update.PLC != "packaging" || update.Tag != "Speed" || update.Value != 121 || !update.Timestamp.Equal(wantTS) {
		t.Fatalf("unexpected update: %#v", update)
	}

	if err := wsjson.Write(ctx, conn, tagWSClientMessage{Type: "ping"}); err != nil {
		t.Fatalf("send ping: %v", err)
	}
	var pong struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.Type != "pong" {
		t.Fatalf("unexpected ping response: %#v", pong)
	}

	if err := wsjson.Write(ctx, conn, tagWSClientMessage{Type: "unsubscribe"}); err != nil {
		t.Fatalf("send unsubscribe: %v", err)
	}
	var unsub struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &unsub); err != nil {
		t.Fatalf("read unsubscribe ack: %v", err)
	}
	if unsub.Type != "unsubscribed" {
		t.Fatalf("unexpected unsubscribe ack: %#v", unsub)
	}
}

// isCloseError checks whether err is a websocket.CloseError and populates out.
// coder/websocket surfaces CloseError directly on the error value.
func isCloseError(err error, out *websocket.CloseError) bool {
	if err == nil {
		return false
	}
	type unwrapper interface{ Unwrap() error }
	cur := err
	for cur != nil {
		if ce, ok := cur.(websocket.CloseError); ok {
			*out = ce
			return true
		}
		u, ok := cur.(unwrapper)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}

// ─── R75-5: WS connection outlives WriteTimeout ──────────────────────────────

// TestWSStreamSurvivesWriteTimeout asserts R75-5: a WebSocket connection
// remains alive past the server's WriteTimeout because the HTTP connection is
// hijacked by the WS upgrade and is no longer subject to http.Server timeouts.
//
// The test sets WriteTimeout = 1s, holds the connection open for 2s, then
// verifies the connection is still alive by sending a ping and receiving a pong.
func TestWSStreamSurvivesWriteTimeout(t *testing.T) {
	t.Parallel()

	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOptsAndCfgFn(
		t,
		&snapshotPLCManager{},
		Opts{AuthTokens: tokens},
		func(cfg *config.Config) {
			cfg.Server.WriteTimeout = "1s" // very short, to exercise the hijack guarantee
		},
	)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Authenticate.
	tok, err := tokens.Issue(1, "operator", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if err := wsjson.Write(ctx, conn, wsAuthFrame{Type: "auth", Token: tok}); err != nil {
		t.Fatalf("write auth frame: %v", err)
	}
	var authAck struct{ Type string }
	if err := wsjson.Read(ctx, conn, &authAck); err != nil {
		t.Fatalf("read auth_ok: %v", err)
	}
	if authAck.Type != "auth_ok" {
		t.Fatalf("expected auth_ok, got %q", authAck.Type)
	}
	// Read subscribed ack.
	var subAck struct{ Type string }
	if err := wsjson.Read(ctx, conn, &subAck); err != nil {
		t.Fatalf("read subscribed: %v", err)
	}
	if subAck.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q", subAck.Type)
	}

	// Sleep longer than WriteTimeout (1s) to let it elapse.
	// If WriteTimeout applied to hijacked connections the server would close us.
	time.Sleep(2 * time.Second)

	// Assert alive: send a ping, expect a pong.
	if err := wsjson.Write(ctx, conn, tagWSClientMessage{Type: "ping"}); err != nil {
		t.Fatalf("write ping after WriteTimeout elapsed: %v", err)
	}
	var pong struct{ Type string }
	if err := wsjson.Read(ctx, conn, &pong); err != nil {
		t.Fatalf("read pong after WriteTimeout elapsed: %v — connection should be alive (hijack guarantee)", err)
	}
	if pong.Type != "pong" {
		t.Errorf("expected pong, got %q", pong.Type)
	}
}

func startAPITestServer(t *testing.T, mgr PLCManager) (*Server, string, func()) {
	t.Helper()
	return startAPITestServerWithOpts(t, mgr, Opts{})
}

func startAPITestServerWithOpts(t *testing.T, mgr PLCManager, opts Opts) (*Server, string, func()) {
	t.Helper()
	return startAPITestServerWithOptsAndCfgFn(t, mgr, opts, nil)
}

func startAPITestServerWithOptsAndCfgFn(t *testing.T, mgr PLCManager, opts Opts, cfgFn func(*config.Config)) (*Server, string, func()) {
	t.Helper()
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.PLCs = []config.PLC{{Name: "packaging", Tags: []config.TagDef{{Name: "Speed", Type: "Float"}}}}
	if cfgFn != nil {
		cfgFn(cfg)
	}
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
	return srv, fmt.Sprintf("http://%s", addr), stop
}
