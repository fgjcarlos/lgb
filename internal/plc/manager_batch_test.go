package plc_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// batchCountingDriver records every ReadMulti call and fails on ReadTag so the
// test proves the scan loop reads through one batched call per tick.
type batchCountingDriver struct {
	mu        sync.Mutex
	batches   [][]string
	tagCalls  int
	connected bool
}

func (d *batchCountingDriver) Connect(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = true
	return nil
}
func (d *batchCountingDriver) Close() error               { return nil }
func (d *batchCountingDriver) WriteTag(string, any) error { return nil }
func (d *batchCountingDriver) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}
func (d *batchCountingDriver) ReadTag(string, any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tagCalls++
	return nil
}
func (d *batchCountingDriver) ReadMulti(tags []string, dests []any) ([]error, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.batches = append(d.batches, append([]string(nil), tags...))
	return make([]error, len(tags)), nil
}

// TestManager_Scan_ReadsAllTagsInOneBatchPerTick verifies each scan tick hands
// every readable tag to a single ReadMulti call (unknown types are excluded)
// instead of issuing one ReadTag per tag.
func TestManager_Scan_ReadsAllTagsInOneBatchPerTick(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{PLCs: []config.PLC{{
		Name: "plc-a", Address: "127.0.0.1:44818", ScanRate: "20ms",
		Tags: []config.TagDef{
			{Name: "A", Type: "Float"},
			{Name: "Weird", Type: "NoSuchType"},
			{Name: "B", Type: "Int32"},
			{Name: "C", Type: "Boolean"},
		},
	}}}
	d := &batchCountingDriver{}
	mgr := plc.NewManager(cfg, nil, func(config.PLC) plc.Driver { return d }, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = mgr.Start(ctx)
	time.Sleep(110 * time.Millisecond)
	_ = mgr.Stop()

	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.batches) < 2 {
		t.Fatalf("ReadMulti calls = %d, want at least 2 ticks", len(d.batches))
	}
	for i, b := range d.batches {
		if !reflect.DeepEqual(b, []string{"A", "B", "C"}) {
			t.Errorf("batch %d = %v, want [A B C]", i, b)
		}
	}
	if d.tagCalls != 0 {
		t.Errorf("ReadTag calls = %d, want 0", d.tagCalls)
	}
}
