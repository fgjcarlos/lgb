package opcua

import (
	"context"
	"sync"
	"testing"

	"github.com/gopcua/opcua/ua"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// reloadTagSource serves a value for every tag the reload tests use.
func reloadTagSource() TagSource {
	return &mockTagSource{tags: map[string]map[string]plc.TagValue{
		"Silo-1": {
			"Feed.Rate": {Value: float32(11.5), Quality: "good"},
			"Level":     {Value: int32(42), Quality: "good"},
		},
		"Silo-2": {
			"Temp": {Value: float32(20.5), Quality: "good"},
		},
	}}
}

func reloadCfg(plcs ...config.PLC) *config.Config {
	return &config.Config{PLCs: plcs}
}

func plcWithTags(name string, tags ...string) config.PLC {
	p := config.PLC{Name: name}
	for _, tag := range tags {
		p.Tags = append(p.Tags, config.TagDef{Name: tag, Type: "REAL"})
	}
	return p
}

// readValue reads the Value attribute of plc/tag the way the OPC UA Read
// service does: through the gopcua namespace the node id's index routes to.
func readValue(t *testing.T, s *Server, plcName, tag string) *ua.DataValue {
	t.Helper()
	ns, ok := s.namespaces[plcName]
	if !ok {
		return &ua.DataValue{Status: ua.StatusBadNodeIDUnknown}
	}
	routed, err := s.srv.Namespace(int(ns.ID()))
	if err != nil {
		t.Fatalf("namespace %d: %v", ns.ID(), err)
	}
	return routed.Attribute(ua.NewStringNodeID(ns.ID(), tag), ua.AttributeIDValue)
}

func newReloadTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	s := New(cfg, reloadTagSource(), nil)
	s.srv = sharedOPCUAServer()
	s.populateAddressSpace()
	return s
}

// TestReload_NewPLCAndTagsAppear verifies that PLCs and tags added after
// startup are served by the address space once Reload runs. (#82)
func TestReload_NewPLCAndTagsAppear(t *testing.T) {
	t.Parallel()
	s := newReloadTestServer(t, reloadCfg(plcWithTags("Silo-1", "Feed.Rate")))

	if dv := readValue(t, s, "Silo-1", "Level"); dv.Status == ua.StatusOK {
		t.Fatalf("Level served before Reload: %+v", dv)
	}

	s.Reload(reloadCfg(plcWithTags("Silo-1", "Feed.Rate", "Level"), plcWithTags("Silo-2", "Temp")))

	if dv := readValue(t, s, "Silo-1", "Level"); dv.Status != ua.StatusOK || dv.Value.Value() != int32(42) {
		t.Errorf("Silo-1/Level after Reload = %+v, want Good 42", dv)
	}
	if dv := readValue(t, s, "Silo-2", "Temp"); dv.Status != ua.StatusOK || dv.Value.Value() != float32(20.5) {
		t.Errorf("Silo-2/Temp after Reload = %+v, want Good 20.5", dv)
	}
	if _, _, _, ok := s.resolveTag(ua.NewStringNodeID(s.namespaces["Silo-2"].ID(), "Temp")); !ok {
		t.Error("write path does not resolve the new Silo-2/Temp node")
	}
}

// TestReload_RemovedPLCAndTagsDisappear verifies that removed tags and PLCs
// stop being served and stop resolving for writes, while unaffected nodes keep
// the very same node instance (sessions browsing them are not disrupted). (#82)
func TestReload_RemovedPLCAndTagsDisappear(t *testing.T) {
	t.Parallel()
	s := newReloadTestServer(t, reloadCfg(plcWithTags("Silo-1", "Feed.Rate", "Level"), plcWithTags("Silo-2", "Temp")))
	ns1 := s.namespaces["Silo-1"]
	feedID := ua.NewStringNodeID(ns1.ID(), "Feed.Rate")
	before := ns1.Node(feedID)

	s.Reload(reloadCfg(plcWithTags("Silo-1", "Feed.Rate")))

	for _, c := range []struct{ plc, tag string }{{"Silo-1", "Level"}, {"Silo-2", "Temp"}} {
		if dv := readValue(t, s, c.plc, c.tag); dv.Status != ua.StatusBadNodeIDUnknown {
			t.Errorf("%s/%s after removal = %+v, want BadNodeIDUnknown", c.plc, c.tag, dv)
		}
		nsID := s.namespaces[c.plc].ID()
		if _, _, _, ok := s.resolveTag(ua.NewStringNodeID(nsID, c.tag)); ok {
			t.Errorf("write path still resolves removed %s/%s", c.plc, c.tag)
		}
	}
	if dv := readValue(t, s, "Silo-1", "Feed.Rate"); dv.Status != ua.StatusOK {
		t.Errorf("unaffected Silo-1/Feed.Rate = %+v, want Good", dv)
	}
	if ns1.Node(feedID) != before {
		t.Error("Reload replaced the node of an unaffected tag")
	}
}

// TestReload_ReAddedPLCReusesNamespace verifies that a PLC deleted and created
// again keeps its namespace index, so client node ids stay valid. (#82)
func TestReload_ReAddedPLCReusesNamespace(t *testing.T) {
	t.Parallel()
	s := newReloadTestServer(t, reloadCfg(plcWithTags("Silo-2", "Temp")))
	nsID := s.namespaces["Silo-2"].ID()

	s.Reload(reloadCfg())
	s.Reload(reloadCfg(plcWithTags("Silo-2", "Temp")))

	if got := s.namespaces["Silo-2"].ID(); got != nsID {
		t.Errorf("namespace index = %d, want %d", got, nsID)
	}
	if dv := readValue(t, s, "Silo-2", "Temp"); dv.Status != ua.StatusOK {
		t.Errorf("re-added Silo-2/Temp = %+v, want Good", dv)
	}
}

// TestReload_BeforeStartIsUsedByStart verifies that a Reload received before
// the OPC UA server is built is not lost: the address space is built from it.
func TestReload_BeforeStartIsUsedByStart(t *testing.T) {
	t.Parallel()
	s := New(reloadCfg(plcWithTags("Silo-1", "Feed.Rate")), reloadTagSource(), nil)
	s.Reload(reloadCfg(plcWithTags("Silo-2", "Temp")))
	s.srv = sharedOPCUAServer()
	s.populateAddressSpace()

	if _, ok := s.namespaces["Silo-1"]; ok {
		t.Error("stale boot PLC Silo-1 exposed after Reload")
	}
	if dv := readValue(t, s, "Silo-2", "Temp"); dv.Status != ua.StatusOK {
		t.Errorf("Silo-2/Temp = %+v, want Good", dv)
	}
}

// TestReload_ConcurrentWithReadsAndWrites is meant for -race: Reload runs
// while reads and write resolution hit the address space. (#82)
func TestReload_ConcurrentWithReadsAndWrites(t *testing.T) {
	t.Parallel()
	full := reloadCfg(plcWithTags("Silo-1", "Feed.Rate", "Level"), plcWithTags("Silo-2", "Temp"))
	small := reloadCfg(plcWithTags("Silo-1", "Feed.Rate"))
	s := newReloadTestServer(t, full)
	ns1 := s.namespaces["Silo-1"]
	routed, err := s.srv.Namespace(int(ns1.ID()))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				routed.Attribute(ua.NewStringNodeID(ns1.ID(), "Level"), ua.AttributeIDValue)
				s.resolveTag(ua.NewStringNodeID(ns1.ID(), "Level"))
			}
		}()
	}
	for j := 0; j < 50; j++ {
		if j%2 == 0 {
			s.Reload(small)
		} else {
			s.Reload(full)
		}
	}
	wg.Wait()
}

// TestReload_UpdatedTagReplacesNode verifies that a tag whose type or OPC UA
// writability changes is served with the new AccessLevel and resolves to the
// new type for writes. (#82)
func TestReload_UpdatedTagReplacesNode(t *testing.T) {
	t.Parallel()
	s := newReloadTestServer(t, reloadCfg(plcWithTags("Silo-1", "Level")))
	ns := s.namespaces["Silo-1"]
	id := ua.NewStringNodeID(ns.ID(), "Level")

	updated := plcWithTags("Silo-1", "Level")
	updated.Tags[0].Type = "DINT"
	updated.Tags[0].Writable = true
	updated.Tags[0].OPCUAEnabled = true
	s.Reload(reloadCfg(updated))

	routed, err := s.srv.Namespace(int(ns.ID()))
	if err != nil {
		t.Fatal(err)
	}
	want := uint8(ua.AccessLevelTypeCurrentRead | ua.AccessLevelTypeCurrentWrite)
	if dv := routed.Attribute(id, ua.AttributeIDAccessLevel); dv.Value.Value() != want {
		t.Errorf("AccessLevel = %v, want %d", dv.Value.Value(), want)
	}
	if _, _, typ, ok := s.resolveTag(id); !ok || typ != "DINT" {
		t.Errorf("resolveTag type = %q (ok=%v), want DINT", typ, ok)
	}
	if dv := readValue(t, s, "Silo-1", "Level"); dv.Status != ua.StatusOK {
		t.Errorf("Level after update = %+v, want Good", dv)
	}
}

// TestServer_RestartServesAddressSpace verifies that a second Start on the
// same Server rebuilds the namespaces on the new gopcua server instead of
// reusing those of the closed one.
func TestServer_RestartServesAddressSpace(t *testing.T) {
	t.Parallel()
	cfg := reloadCfg(plcWithTags("Silo-1", "Level"))
	cfg.OPCUA = config.OPCUASection{Enabled: true, Host: "127.0.0.1", Port: freeTCPPort(t), SecurityMode: "None"}
	s := New(cfg, reloadTagSource(), nil)

	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if err := s.Start(ctx); err != nil {
			cancel()
			t.Fatalf("Start #%d: %v", i+1, err)
		}
		if dv := readValue(t, s, "Silo-1", "Level"); dv.Status != ua.StatusOK {
			t.Errorf("Start #%d: Level = %+v, want Good", i+1, dv)
		}
		cancel()
		if err := s.Stop(); err != nil {
			t.Fatalf("Stop #%d: %v", i+1, err)
		}
	}
}
