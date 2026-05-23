//go:build integration

// Package config watcher integration tests.
// Requirements: MVP-FND-2.5. Design: §5.2.
// Run with: go test -tags=integration ./internal/config/...
package config_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

// TestWatcherFileChangeTriggersCallback asserts MVP-FND-2.5: a file change
// triggers onChange within 1 s.
func TestWatcherFileChangeTriggersCallback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lgb.yaml")

	initial := []byte("gateway:\n  id: \"initial\"\n  logLevel: \"info\"\n  logFormat: \"text\"\nserver:\n  httpAddr: \":8080\"\nauth:\n  sessionTTL: \"8h\"\nhistorian:\n  retentionDays: 90\n")
	if err := os.WriteFile(cfgPath, initial, 0600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var callCount int32
	var receivedID atomic.Value

	errCh := make(chan error, 1)
	go func() {
		errCh <- config.Watch(ctx, cfgPath, func(cfg *config.Config) {
			atomic.AddInt32(&callCount, 1)
			receivedID.Store(cfg.Gateway.ID)
		})
	}()

	// Wait briefly for watcher to start.
	time.Sleep(100 * time.Millisecond)

	// Modify the file.
	updated := []byte("gateway:\n  id: \"updated\"\n  logLevel: \"info\"\n  logFormat: \"text\"\nserver:\n  httpAddr: \":8080\"\nauth:\n  sessionTTL: \"8h\"\nhistorian:\n  retentionDays: 90\n")
	if err := os.WriteFile(cfgPath, updated, 0600); err != nil {
		t.Fatalf("writing updated config: %v", err)
	}

	// Wait up to 1 s for callback.
	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("onChange not called within 1s; callCount=%d", atomic.LoadInt32(&callCount))
		default:
		}
		if atomic.LoadInt32(&callCount) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if id, ok := receivedID.Load().(string); !ok || id != "updated" {
		t.Errorf("received id = %v; want %q", receivedID.Load(), "updated")
	}

	cancel()
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "context canceled" {
			// context.Canceled is expected.
			t.Logf("Watch returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return after context cancel")
	}
}

// TestWatcherDebounceCoalescesFiveWrites asserts MVP-FND-2.5: five writes
// within 100 ms trigger exactly one callback.
func TestWatcherDebounceCoalescesFiveWrites(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lgb.yaml")

	base := "gateway:\n  id: \"v%d\"\n  logLevel: \"info\"\n  logFormat: \"text\"\nserver:\n  httpAddr: \":8080\"\nauth:\n  sessionTTL: \"8h\"\nhistorian:\n  retentionDays: 90\n"

	if err := os.WriteFile(cfgPath, []byte("gateway:\n  id: \"init\"\n  logLevel: \"info\"\n  logFormat: \"text\"\nserver:\n  httpAddr: \":8080\"\nauth:\n  sessionTTL: \"8h\"\nhistorian:\n  retentionDays: 90\n"), 0600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var callCount int32
	errCh := make(chan error, 1)
	go func() {
		errCh <- config.Watch(ctx, cfgPath, func(_ *config.Config) {
			atomic.AddInt32(&callCount, 1)
		})
	}()

	// Wait for watcher to start.
	time.Sleep(100 * time.Millisecond)

	// Write 5 times in under 100 ms.
	for i := 1; i <= 5; i++ {
		content := []byte(base)
		_ = i
		_ = base
		content = []byte("gateway:\n  id: \"v" + string(rune('0'+i)) + "\"\n  logLevel: \"info\"\n  logFormat: \"text\"\nserver:\n  httpAddr: \":8080\"\nauth:\n  sessionTTL: \"8h\"\nhistorian:\n  retentionDays: 90\n")
		if err := os.WriteFile(cfgPath, content, 0600); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce window to pass plus a safety margin.
	time.Sleep(500 * time.Millisecond)

	got := atomic.LoadInt32(&callCount)
	// We expect exactly 1 callback for the debounced writes.
	if got != 1 {
		t.Errorf("callCount = %d; want 1 (debounced)", got)
	}

	cancel()
}

// TestWatcherContextCancelStopsWatcher asserts MVP-FND-2.5: cancelling
// the context stops the watcher and returns ctx.Err().
func TestWatcherContextCancelStopsWatcher(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lgb.yaml")

	if err := os.WriteFile(cfgPath, []byte("gateway:\n  id: \"stop\"\n  logLevel: \"info\"\n  logFormat: \"text\"\nserver:\n  httpAddr: \":8080\"\nauth:\n  sessionTTL: \"8h\"\nhistorian:\n  retentionDays: 90\n"), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- config.Watch(ctx, cfgPath, func(_ *config.Config) {})
	}()

	// Let the watcher start.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Watch returned nil; want ctx.Err()")
		}
		t.Logf("Watch returned: %v (expected)", err)
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return after context cancel within 2s")
	}
}
