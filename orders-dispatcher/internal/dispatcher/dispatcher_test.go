package dispatcher

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

type fakeRepo struct {
	pending int
	err     error
}

func (f *fakeRepo) CountPending(_ context.Context) (int, error) {
	return f.pending, f.err
}

type fakeQueue struct {
	sent int
	err  error
}

func (f *fakeQueue) SendBatch(_ context.Context, n int) error {
	f.sent = n
	return f.err
}

func newTestDispatcher(repo *fakeRepo, queue *fakeQueue, batchSize, maxWorkers int) *Dispatcher {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(repo, queue, batchSize, maxWorkers, logger)
}

func TestRunNoPendingDoesNotEnqueue(t *testing.T) {
	repo := &fakeRepo{pending: 0}
	queue := &fakeQueue{}
	d := newTestDispatcher(repo, queue, 10, 20)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run devolvió error inesperado: %v", err)
	}
	if queue.sent != 0 {
		t.Fatalf("no esperaba trabajos encolados, obtuve %d", queue.sent)
	}
}

func TestRunEnqueuesOneWorkerForPartialBatch(t *testing.T) {
	repo := &fakeRepo{pending: 7}
	queue := &fakeQueue{}
	d := newTestDispatcher(repo, queue, 10, 20)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run devolvió error inesperado: %v", err)
	}
	if queue.sent != 1 {
		t.Fatalf("esperaba 1 trabajo encolado, obtuve %d", queue.sent)
	}
}

func TestRunEnqueuesExactWorkers(t *testing.T) {
	repo := &fakeRepo{pending: 25}
	queue := &fakeQueue{}
	d := newTestDispatcher(repo, queue, 10, 20)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run devolvió error inesperado: %v", err)
	}
	if queue.sent != 3 {
		t.Fatalf("esperaba 3 trabajos encolados, obtuve %d", queue.sent)
	}
}

func TestRunClampsToMaxWorkers(t *testing.T) {
	repo := &fakeRepo{pending: 1000}
	queue := &fakeQueue{}
	d := newTestDispatcher(repo, queue, 10, 20)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run devolvió error inesperado: %v", err)
	}
	if queue.sent != 20 {
		t.Fatalf("esperaba 20 trabajos encolados (tope), obtuve %d", queue.sent)
	}
}
