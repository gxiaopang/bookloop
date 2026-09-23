package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
)

func TestRabbitDeliveryAttemptsReadsNotificationDeaths(t *testing.T) {
	headers := amqp.Table{"x-death": []interface{}{
		amqp.Table{"queue": "another.queue", "count": int64(99)},
		amqp.Table{"queue": notificationQueue, "count": int64(3)},
	}}
	if got := rabbitDeliveryAttempts(headers); got != 3 {
		t.Fatalf("rabbitDeliveryAttempts() = %d, want 3", got)
	}
}

func TestOutboxBackoffIsBounded(t *testing.T) {
	if got := outboxBackoff(1); got != 2*time.Second {
		t.Fatalf("outboxBackoff(1) = %s, want 2s", got)
	}
	if got := outboxBackoff(100); got != 64*time.Second {
		t.Fatalf("outboxBackoff(100) = %s, want 64s", got)
	}
}

func TestNotificationConsumerAcknowledgesSuccessAndRejectsTemporaryFailureForRetry(t *testing.T) {
	body, err := json.Marshal(domainevent.Envelope{ID: 1, Type: domainevent.OrderCreated})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	successAck := &fakeAcknowledger{}
	consumer := &RabbitNotificationConsumer{}
	if err := consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: successAck, DeliveryTag: 1, Body: body,
	}, &fakeNotificationProcessor{}, 5, discardLogger()); err != nil {
		t.Fatalf("handle(success) error = %v", err)
	}
	if !successAck.acked || successAck.nacked {
		t.Fatalf("success ack state = %+v, want ACK", successAck)
	}

	retryAck := &fakeAcknowledger{}
	processorErr := errors.New("mysql unavailable")
	if err := consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: retryAck, DeliveryTag: 2, Body: body,
	}, &fakeNotificationProcessor{err: processorErr}, 5, discardLogger()); err != nil {
		t.Fatalf("handle(retry) error = %v", err)
	}
	if retryAck.acked || !retryAck.nacked || retryAck.requeue {
		t.Fatalf("retry ack state = %+v, want NACK without immediate requeue", retryAck)
	}
}

type fakeNotificationProcessor struct{ err error }

func (p *fakeNotificationProcessor) Process(context.Context, domainevent.Envelope) (bool, error) {
	return p.err == nil, p.err
}

type fakeAcknowledger struct {
	acked   bool
	nacked  bool
	requeue bool
}

func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.acked = true
	return nil
}

func (a *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked = true
	a.requeue = requeue
	return nil
}

func (a *fakeAcknowledger) Reject(_ uint64, requeue bool) error {
	a.nacked = true
	a.requeue = requeue
	return nil
}
