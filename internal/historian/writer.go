package historian

import (
	"context"
	"time"
)

// defaultFlushTimeout bounds the final flush performed while the writer shuts
// down. The final flush runs with a context detached from the caller's, so it
// needs a deadline of its own.
const defaultFlushTimeout = 5 * time.Second

type WriterOptions struct {
	BufferSize    int
	BatchSize     int
	FlushInterval time.Duration
	// FlushTimeout bounds the final flush performed when the writer shuts down.
	// Defaults to defaultFlushTimeout.
	FlushTimeout time.Duration
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
	if opts.FlushTimeout <= 0 {
		opts.FlushTimeout = defaultFlushTimeout
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
	flush := func(fctx context.Context) bool {
		if len(batch) == 0 {
			return true
		}
		if err := w.store.InsertBatch(fctx, batch); err != nil {
			select {
			case w.errCh <- err:
			default:
			}
			return false
		}
		batch = batch[:0]
		return true
	}

	// drain moves the samples still buffered in the channel into the batch.
	// It stops at the first empty read, so it never blocks.
	drain := func() {
		for {
			select {
			case sample, ok := <-w.ch:
				if !ok {
					return
				}
				batch = append(batch, sample)
			default:
				return
			}
		}
	}

	// finalFlush persists the last batch while the writer shuts down. The
	// writer's context is already cancelled at that point, so the insert runs
	// with a detached context bounded by FlushTimeout: the final batch must
	// not be lost (issue #90).
	finalFlush := func() {
		drain()
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.opts.FlushTimeout)
		defer cancel()
		_ = flush(fctx)
	}

	for {
		select {
		case <-ctx.Done():
			finalFlush()
			return
		case sample, ok := <-w.ch:
			if !ok {
				finalFlush()
				return
			}
			batch = append(batch, sample)
			if len(batch) >= w.opts.BatchSize && !flush(ctx) {
				return
			}
		case <-ticker.C:
			if !flush(ctx) {
				return
			}
		}
	}
}
