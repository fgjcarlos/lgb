package historian_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/historian"
)

func TestStore_InsertBatchAndQueryRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "lgb.db"), historian.Options{RetentionDays: 90})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	}()

	ts := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	batch := []historian.Sample{
		{PLCName: "plc-a", Tag: "Temp", Timestamp: ts, Value: 21.5, Quality: "good"},
		{PLCName: "plc-a", Tag: "Temp", Timestamp: ts.Add(time.Second), Value: 22.0, Quality: "good"},
		{PLCName: "plc-a", Tag: "Pressure", Timestamp: ts, Value: int32(7), Quality: "good"},
	}
	if err := store.InsertBatch(ctx, batch); err != nil {
		t.Fatalf("InsertBatch returned error: %v", err)
	}

	got, err := store.Query(ctx, historian.Query{PLCName: "plc-a", Tag: "Temp", From: ts.Add(-time.Millisecond), To: ts.Add(2 * time.Second)})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Query returned %d samples; want 2", len(got))
	}
	if got[0].Value != "21.5" || got[1].Value != "22" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestStore_EnforceRetentionDeletesOldRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "lgb.db"), historian.Options{RetentionDays: 1})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	}()

	now := time.Now().UTC()
	if err := store.InsertBatch(ctx, []historian.Sample{
		{PLCName: "plc-a", Tag: "Temp", Timestamp: now.Add(-48 * time.Hour), Value: 1, Quality: "good"},
		{PLCName: "plc-a", Tag: "Temp", Timestamp: now, Value: 2, Quality: "good"},
	}); err != nil {
		t.Fatalf("InsertBatch returned error: %v", err)
	}

	if err := store.EnforceRetention(ctx, now); err != nil {
		t.Fatalf("EnforceRetention returned error: %v", err)
	}
	got, err := store.Query(ctx, historian.Query{PLCName: "plc-a", Tag: "Temp", From: now.Add(-72 * time.Hour), To: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(got) != 1 || got[0].Value != "2" {
		t.Fatalf("retention query = %#v; want only current sample", got)
	}
}

func TestStore_SingleWriterRejectsConcurrentBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	}()

	release := make(chan struct{})
	store.SetBeforeWriteHook(func() { <-release })
	done := make(chan error, 1)
	go func() {
		done <- store.InsertBatch(ctx, []historian.Sample{{PLCName: "plc-a", Tag: "Temp", Timestamp: time.Now(), Value: 1}})
	}()

	deadline := time.After(2 * time.Second)
	for !store.WriterBusy() {
		select {
		case <-deadline:
			t.Fatal("first writer did not become busy")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	err = store.InsertBatch(ctx, []historian.Sample{{PLCName: "plc-a", Tag: "Temp", Timestamp: time.Now(), Value: 2}})
	if !errors.Is(err, historian.ErrWriterBusy) {
		t.Fatalf("InsertBatch while busy error = %v; want ErrWriterBusy", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first InsertBatch returned error: %v", err)
	}
}

func TestStore_VacuumIntoCreatesSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store, err := historian.Open(ctx, filepath.Join(dir, "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	}()

	path, err := store.VacuumInto(ctx, filepath.Join(dir, "backup-tmp"))
	if err != nil {
		t.Fatalf("VacuumInto returned error: %v", err)
	}
	if filepath.Dir(path) != filepath.Join(dir, "backup-tmp") {
		t.Fatalf("snapshot path = %q; want under backup-tmp", path)
	}
}

func TestWriter_BuffersAndFlushesSamples(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := historian.Open(ctx, filepath.Join(t.TempDir(), "lgb.db"), historian.Options{})
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	}()

	writer := historian.NewWriter(store, historian.WriterOptions{BufferSize: 4, BatchSize: 2, FlushInterval: time.Hour})
	writer.Start(ctx)
	ts := time.Now().UTC()
	if err := writer.Enqueue(ctx, historian.Sample{PLCName: "plc-a", Tag: "Temp", Timestamp: ts, Value: 1}); err != nil {
		t.Fatalf("Enqueue 1 returned error: %v", err)
	}
	if err := writer.Enqueue(ctx, historian.Sample{PLCName: "plc-a", Tag: "Temp", Timestamp: ts.Add(time.Second), Value: 2}); err != nil {
		t.Fatalf("Enqueue 2 returned error: %v", err)
	}
	if err := writer.Stop(ctx); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	got, err := store.Query(ctx, historian.Query{PLCName: "plc-a", Tag: "Temp", From: ts.Add(-time.Second), To: ts.Add(2 * time.Second)})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Query returned %d samples; want 2", len(got))
	}
}
