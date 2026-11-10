package historian_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/historian"
)

// newTestStore opens a historian store backed by a temporary SQLite file.
func newTestStore(t *testing.T) *historian.Store {
	t.Helper()
	store, err := historian.Open(context.Background(), filepath.Join(t.TempDir(), "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	})
	return store
}

// enqueueSamples buffers n samples that are below BatchSize, so they are only
// persisted by an explicit flush.
func enqueueSamples(t *testing.T, w *historian.Writer, n int) time.Time {
	t.Helper()
	ts := time.Now().UTC()
	for i := 0; i < n; i++ {
		if err := w.Enqueue(context.Background(), historian.Sample{
			PLCName:   "plc-a",
			Tag:       "Temp",
			Timestamp: ts.Add(time.Duration(i) * time.Second),
			Value:     float64(i),
		}); err != nil {
			t.Fatalf("Enqueue %d returned error: %v", i, err)
		}
	}
	return ts
}

// countSamples returns the number of persisted samples for plc-a/Temp.
func countSamples(t *testing.T, store *historian.Store, ts time.Time) int {
	t.Helper()
	got, err := store.Query(context.Background(), historian.Query{
		PLCName: "plc-a",
		Tag:     "Temp",
		From:    ts.Add(-time.Second),
		To:      ts.Add(20 * time.Second),
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	return len(got)
}

// TestWriter_StopFlushesBufferedSamplesAfterContextCancelled covers issue #90:
// the server context is already cancelled when the writer is stopped, so the
// final flush must not inherit that cancellation. Before the fix the insert
// used the cancelled writer context and the whole batch was dropped.
func TestWriter_StopFlushesBufferedSamplesAfterContextCancelled(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	// The writer runs with the server context, which is cancelled at shutdown.
	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	writer := historian.NewWriter(store, historian.WriterOptions{
		BufferSize:    100,
		BatchSize:     50,
		FlushInterval: time.Hour,
	})
	writer.Start(serverCtx)

	ts := enqueueSamples(t, writer, 20)

	// Shutdown: the server context is cancelled before the writer is stopped.
	cancelServer()

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if got := countSamples(t, store, ts); got != 20 {
		t.Fatalf("Query returned %d samples; want 20 (final batch lost)", got)
	}
}

// TestWriter_StopFlushesBufferedSamplesWithLiveContext verifies the ordinary
// shutdown path: buffered samples below BatchSize are persisted by Stop.
func TestWriter_StopFlushesBufferedSamplesWithLiveContext(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)

	ctx := context.Background()
	writer := historian.NewWriter(store, historian.WriterOptions{
		BufferSize:    100,
		BatchSize:     50,
		FlushInterval: time.Hour,
	})
	writer.Start(ctx)

	ts := enqueueSamples(t, writer, 20)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if got := countSamples(t, store, ts); got != 20 {
		t.Fatalf("Query returned %d samples; want 20", got)
	}
}
