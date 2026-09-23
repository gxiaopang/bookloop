package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
	"github.com/gxiaopang/bookloop/backend/internal/model"
)

type outboxRepository interface {
	ClaimBatch(context.Context, string, int, time.Duration) ([]model.OutboxEvent, error)
	MarkPublished(context.Context, uint64, string) error
	ReleaseWithError(context.Context, uint64, string, string, time.Time) error
}

type eventPublisher interface {
	Publish(context.Context, model.OutboxEvent) error
}

type RabbitEventPublisher struct {
	channel  *amqp.Channel
	confirms <-chan amqp.Confirmation
}

func (p *RabbitEventPublisher) Publish(ctx context.Context, event model.OutboxEvent) error {
	body, err := json.Marshal(domainevent.Envelope{
		ID: event.ID, AggregateType: event.AggregateType, AggregateID: event.AggregateID,
		Type: event.EventType, OccurredAt: event.OccurredAt, Payload: event.Payload,
	})
	if err != nil {
		return fmt.Errorf("marshal outbox envelope: %w", err)
	}
	return publishConfirmed(ctx, p.channel, p.confirms, eventsExchange, event.EventType, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		MessageId:    strconv.FormatUint(event.ID, 10),
		Type:         event.EventType,
		Timestamp:    event.OccurredAt,
		Body:         body,
	})
}

func (p *RabbitEventPublisher) Close() error { return p.channel.Close() }

type OutboxPublisher struct {
	repository   outboxRepository
	publisher    eventPublisher
	workerID     string
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
	logger       *slog.Logger
}

func NewOutboxPublisher(
	repository outboxRepository,
	publisher eventPublisher,
	workerID string,
	logger *slog.Logger,
) *OutboxPublisher {
	return &OutboxPublisher{
		repository: repository, publisher: publisher, workerID: workerID,
		batchSize: 100, pollInterval: 500 * time.Millisecond, lease: 30 * time.Second, logger: logger,
	}
}

func (p *OutboxPublisher) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	for {
		if err := p.publishBatch(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (p *OutboxPublisher) publishBatch(ctx context.Context) error {
	events, err := p.repository.ClaimBatch(ctx, p.workerID, p.batchSize, p.lease)
	if err != nil {
		return fmt.Errorf("claim outbox events: %w", err)
	}
	for _, event := range events {
		if err := p.publisher.Publish(ctx, event); err != nil {
			retryAt := time.Now().UTC().Add(outboxBackoff(event.Attempts + 1))
			if releaseErr := p.repository.ReleaseWithError(ctx, event.ID, p.workerID, err.Error(), retryAt); releaseErr != nil {
				p.logger.Error("failed to release outbox event after publish error",
					"event_id", event.ID, "publish_error", err, "release_error", releaseErr)
			}
			return fmt.Errorf("publish outbox event %d: %w", event.ID, err)
		}
		if err := p.repository.MarkPublished(ctx, event.ID, p.workerID); err != nil {
			return fmt.Errorf("mark outbox event %d published: %w", event.ID, err)
		}
		p.logger.Info("outbox event published", "event_id", event.ID, "event_type", event.EventType)
	}
	return nil
}

func outboxBackoff(attempt uint) time.Duration {
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<attempt) * time.Second
}
