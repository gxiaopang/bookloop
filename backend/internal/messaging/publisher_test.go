package messaging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

func TestOutboxPublisherMarksConfirmedEvents(t *testing.T) {
	repository := &fakeOutboxRepository{events: []model.OutboxEvent{{ID: 7, EventType: "order.created.v1"}}}
	publisher := NewOutboxPublisher(repository, &fakeEventPublisher{}, "worker", discardLogger())
	if err := publisher.publishBatch(context.Background()); err != nil {
		t.Fatalf("publishBatch() error = %v", err)
	}
	if repository.published != 7 || repository.released != 0 {
		t.Fatalf("published/released = %d/%d, want 7/0", repository.published, repository.released)
	}
}

func TestOutboxPublisherReleasesFailedEventsForRetry(t *testing.T) {
	repository := &fakeOutboxRepository{events: []model.OutboxEvent{{ID: 8, Attempts: 2}}}
	publishErr := errors.New("broker unavailable")
	publisher := NewOutboxPublisher(repository, &fakeEventPublisher{err: publishErr}, "worker", discardLogger())
	if err := publisher.publishBatch(context.Background()); !errors.Is(err, publishErr) {
		t.Fatalf("publishBatch() error = %v, want broker error", err)
	}
	if repository.published != 0 || repository.released != 8 || repository.retryAt.IsZero() {
		t.Fatalf("published/released/retry = %d/%d/%s, want 0/8/non-zero",
			repository.published, repository.released, repository.retryAt)
	}
}

type fakeEventPublisher struct{ err error }

func (p *fakeEventPublisher) Publish(context.Context, model.OutboxEvent) error { return p.err }

type fakeOutboxRepository struct {
	events    []model.OutboxEvent
	published uint64
	released  uint64
	retryAt   time.Time
}

func (r *fakeOutboxRepository) ClaimBatch(context.Context, string, int, time.Duration) ([]model.OutboxEvent, error) {
	return r.events, nil
}

func (r *fakeOutboxRepository) MarkPublished(_ context.Context, eventID uint64, _ string) error {
	r.published = eventID
	return nil
}

func (r *fakeOutboxRepository) ReleaseWithError(
	_ context.Context, eventID uint64, _, _ string, retryAt time.Time,
) error {
	r.released = eventID
	r.retryAt = retryAt
	return nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
