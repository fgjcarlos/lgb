package historian

import (
	"context"
	"time"
)

type WriterOptions struct {
	BufferSize    int
	BatchSize     int
	FlushInterval time.Duration
}

type Writer struct {
	store *Store
	opts  WriterOptions
	ch    chan Sample
	done  chan struct{}
	errCh chan error
}

func NewWriter(store *Store, opts WriterOptions) *Writer {
	if opts.BufferSize <= 0 {
		opts.BufferSize = 1024
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = time.Second
	}
	return &Writer{store: store, opts: opts, ch: make(chan Sample, opts.BufferSize), done: make(chan struct{}), errCh: make(chan error, 1)}
}

func (w *Writer) Start(ctx context.Context) {
	go w.run(ctx)
}

func (w *Writer) Enqueue(ctx context.Context, sample Sample) error {
	select {
	case w.ch <- sample:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) Stop(ctx context.Context) error {
	close(w.ch)
	select {
	case <-w.done:
		select {
		case err := <-w.errCh:
			return err
		default:
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) run(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.opts.FlushInterval)
	defer ticker.Stop()
	batch := make([]Sample, 0, w.opts.BatchSize)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		if err := w.store.InsertBatch(ctx, batch); err != nil {
			select {
			case w.errCh <- err:
			default:
			}
			return false
		}
		batch = batch[:0]
		return true
	}
	for {
		select {
		case <-ctx.Done():
			_ = flush()
			return
		case sample, ok := <-w.ch:
			if !ok {
				_ = flush()
				return
			}
			batch = append(batch, sample)
			if len(batch) >= w.opts.BatchSize && !flush() {
				return
			}
		case <-ticker.C:
			if !flush() {
				return
			}
		}
	}
}
