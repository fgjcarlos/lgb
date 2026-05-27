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

func TestServer_TagsWebSocketRequiresValidJWTWhenConfigured(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{}, Opts{AuthTokens: tokens})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + baseURL[len("http"):] + "/api/ws/tags?plc=packaging&tag=Speed"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected websocket dial without token to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		got := 0
		if resp != nil {
			got = resp.StatusCode
		}
		t.Fatalf("expected 401 response, got %d (err=%v)", got, err)
	}

	token, err := tokens.Issue(1, "operator", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	conn, _, err := websocket.Dial(ctx, wsURL+"&token="+token, nil)
	if err != nil {
		t.Fatalf("dial websocket with token: %v", err)
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

func startAPITestServer(t *testing.T, mgr PLCManager) (*Server, string, func()) {
	t.Helper()
	return startAPITestServerWithOpts(t, mgr, Opts{})
}

func startAPITestServerWithOpts(t *testing.T, mgr PLCManager, opts Opts) (*Server, string, func()) {
	t.Helper()
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	cfg.PLCs = []config.PLC{{Name: "packaging", Tags: []config.TagDef{{Name: "Speed", Type: "Float"}}}}
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
