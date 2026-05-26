package plc

import (
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

func TestManager_AddTagCallbackReceivesStoredUpdates(t *testing.T) {
	cfg := &config.Config{PLCs: []config.PLC{{Name: "packaging"}}}
	mgr := NewManager(cfg, nil, nil, nil)

	var got TagUpdate
	mgr.AddTagCallback(func(update TagUpdate) {
		got = update
	})

	want := TagUpdate{PLCName: "packaging", Tag: "Speed", Value: int32(120), Timestamp: time.Date(2026, 5, 26, 12, 2, 0, 0, time.UTC)}
	mgr.emitTagUpdate(want)

	if got != want {
		t.Fatalf("callback got %#v, want %#v", got, want)
	}
	stored, ok := mgr.CurrentTag("packaging", "Speed")
	if !ok || stored.Value != want.Value || stored.Quality != "good" || !stored.Timestamp.Equal(want.Timestamp) {
		t.Fatalf("stored tag = %#v, ok=%v", stored, ok)
	}
}
