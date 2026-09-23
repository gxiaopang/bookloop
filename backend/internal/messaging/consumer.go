package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

type notificationProcessor interface {
	Process(context.Context, domainevent.Envelope) (bool, error)
}

type RabbitNotificationConsumer struct {
	channel    *amqp.Channel
	confirms   <-chan amqp.Confirmation
	deliveries <-chan amqp.Delivery
}

func (c *RabbitNotificationConsumer) Run(
	ctx context.Context,
	processor notificationProcessor,
	maxAttempts int,
	logger *slog.Logger,
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-c.deliveries:
			if !ok {
				return errors.New("RabbitMQ notification delivery channel closed")
			}
			if err := c.handle(ctx, delivery, processor, maxAttempts, logger); err != nil {
				return err
			}
		}
	}
}

func (c *RabbitNotificationConsumer) handle(
	ctx context.Context,
	delivery amqp.Delivery,
	processor notificationProcessor,
	maxAttempts int,
	logger *slog.Logger,
) error {
	var envelope domainevent.Envelope
	if err := json.Unmarshal(delivery.Body, &envelope); err != nil {
		logger.Warn("invalid notification event moved to dead-letter queue", "error", err)
		return c.moveToDead(ctx, delivery, "invalid envelope")
	}
	processed, err := processor.Process(ctx, envelope)
	if err == nil {
		if ackErr := delivery.Ack(false); ackErr != nil {
			return fmt.Errorf("ack notification event: %w", ackErr)
		}
		logger.Info("notification event consumed", "event_id", envelope.ID,
			"event_type", envelope.Type, "created", processed)
		return nil
	}
	if errors.Is(err, service.ErrValidation) {
		logger.Warn("unsupported notification event moved to dead-letter queue",
			"event_id", envelope.ID, "event_type", envelope.Type, "error", err)
		return c.moveToDead(ctx, delivery, err.Error())
	}
	attempt := rabbitDeliveryAttempts(delivery.Headers) + 1
	if attempt >= int64(maxAttempts) {
		logger.Error("notification event exhausted retries", "event_id", envelope.ID,
			"event_type", envelope.Type, "attempt", attempt, "error", err)
		return c.moveToDead(ctx, delivery, err.Error())
	}
	logger.Warn("notification event scheduled for retry", "event_id", envelope.ID,
		"event_type", envelope.Type, "attempt", attempt, "error", err)
	if nackErr := delivery.Nack(false, false); nackErr != nil {
		return fmt.Errorf("reject notification event for retry: %w", nackErr)
	}
	return nil
}

func (c *RabbitNotificationConsumer) moveToDead(
	ctx context.Context,
	delivery amqp.Delivery,
	reason string,
) error {
	headers := delivery.Headers
	if headers == nil {
		headers = amqp.Table{}
	}
	headers["x-bookloop-dead-reason"] = reason
	err := publishConfirmed(ctx, c.channel, c.confirms, deadExchange, notificationRoute, amqp.Publishing{
		Headers: headers, DeliveryMode: amqp.Persistent, ContentType: delivery.ContentType,
		MessageId: delivery.MessageId, Type: delivery.Type, Timestamp: time.Now().UTC(), Body: delivery.Body,
	})
	if err != nil {
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			return errors.Join(err, nackErr)
		}
		return nil
	}
	return delivery.Ack(false)
}

func rabbitDeliveryAttempts(headers amqp.Table) int64 {
	deaths, ok := headers["x-death"].([]interface{})
	if !ok {
		return 0
	}
	var attempts int64
	for _, value := range deaths {
		death, ok := value.(amqp.Table)
		if !ok || death["queue"] != notificationQueue {
			continue
		}
		if count, ok := death["count"].(int64); ok && count > attempts {
			attempts = count
		}
	}
	return attempts
}

func (c *RabbitNotificationConsumer) Close() error { return c.channel.Close() }
