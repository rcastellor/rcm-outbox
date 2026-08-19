package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rcastellor/rcm-outbox/orders-workers/internal/domain"
)

type fakeRepo struct {
	events      []domain.OutboxEvent
	published   []string
	retried     []string
	dead        []string
	resetCalled bool
}

func (f *fakeRepo) ClaimPending(_ context.Context, limit int) ([]domain.OutboxEvent, error) {
	if len(f.events) > limit {
		out := f.events[:limit]
		f.events = f.events[limit:]
		return out, nil
	}
	out := f.events
	f.events = nil
	return out, nil
}

func (f *fakeRepo) MarkPublished(_ context.Context, id string) error {
	f.published = append(f.published, id)
	return nil
}

func (f *fakeRepo) MarkRetry(_ context.Context, id string, _ time.Time, _ string) error {
	f.retried = append(f.retried, id)
	return nil
}

func (f *fakeRepo) MarkDead(_ context.Context, id, _ string) error {
	f.dead = append(f.dead, id)
	return nil
}

func (f *fakeRepo) ResetStuck(_ context.Context, _ time.Duration) error {
	f.resetCalled = true
	return nil
}

type fakePublisher struct {
	err error
}

func (f *fakePublisher) Publish(_ context.Context, _ domain.OutboxEvent) error {
	return f.err
}

func newTestWorker(repo *fakeRepo, pub *fakePublisher, maxAttempts int) *Worker {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(repo, pub, 10, maxAttempts, time.Minute, 8*time.Minute, logger)
}

func TestProcessPublishesAndMarksPublished(t *testing.T) {
	repo := &fakeRepo{events: []domain.OutboxEvent{{ID: "1", EventType: "CreatedOrder", Attempts: 1}}}
	pub := &fakePublisher{}
	w := newTestWorker(repo, pub, 5)

	if err := w.Process(context.Background()); err != nil {
		t.Fatalf("Process devolvió error inesperado: %v", err)
	}
	if !repo.resetCalled {
		t.Error("ResetStuck no fue llamado")
	}
	if len(repo.published) != 1 || repo.published[0] != "1" {
		t.Fatalf("esperaba publicado [1], obtuve %v", repo.published)
	}
	if len(repo.retried) != 0 || len(repo.dead) != 0 {
		t.Fatalf("no esperaba reintentos ni dead: retried=%v dead=%v", repo.retried, repo.dead)
	}
}

func TestProcessRetriesOnFailure(t *testing.T) {
	repo := &fakeRepo{events: []domain.OutboxEvent{{ID: "1", EventType: "CreatedOrder", Attempts: 2}}}
	pub := &fakePublisher{err: errors.New("sns caído")}
	w := newTestWorker(repo, pub, 5)

	if err := w.Process(context.Background()); err != nil {
		t.Fatalf("Process devolvió error inesperado: %v", err)
	}
	if len(repo.retried) != 1 || repo.retried[0] != "1" {
		t.Fatalf("esperaba reintento de [1], obtuve %v", repo.retried)
	}
	if len(repo.published) != 0 || len(repo.dead) != 0 {
		t.Fatalf("no esperaba publicados ni dead: published=%v dead=%v", repo.published, repo.dead)
	}
}

func TestProcessDeadLettersWhenMaxAttemptsReached(t *testing.T) {
	repo := &fakeRepo{events: []domain.OutboxEvent{{ID: "1", EventType: "CreatedOrder", Attempts: 5}}}
	pub := &fakePublisher{err: errors.New("fallo permanente")}
	w := newTestWorker(repo, pub, 5)

	if err := w.Process(context.Background()); err != nil {
		t.Fatalf("Process devolvió error inesperado: %v", err)
	}
	if len(repo.dead) != 1 || repo.dead[0] != "1" {
		t.Fatalf("esperaba dead [1], obtuve %v", repo.dead)
	}
	if len(repo.published) != 0 || len(repo.retried) != 0 {
		t.Fatalf("no esperaba publicados ni reintentos: published=%v retried=%v", repo.published, repo.retried)
	}
}
