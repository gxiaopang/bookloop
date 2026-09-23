package messaging

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

const (
	eventsExchange       = "bookloop.events"
	retryExchange        = "bookloop.retry"
	deadExchange         = "bookloop.dead"
	notificationQueue    = "bookloop.notification"
	notificationRetry    = "bookloop.notification.retry"
	notificationDead     = "bookloop.notification.dead"
	notificationRoute    = "notification"
	notificationRetryKey = "notification.retry"
)

type RabbitMQ struct {
	connection *amqp.Connection
}

func DialRabbitMQWithRetry(
	ctx context.Context,
	cfg config.RabbitMQConfig,
	retryInterval time.Duration,
) (*RabbitMQ, error) {
	if retryInterval <= 0 {
		return nil, errors.New("RabbitMQ retry interval must be greater than zero")
	}
	address := rabbitURL(cfg)
	var lastErr error
	for {
		connection, err := amqp.DialConfig(address, amqp.Config{
			Heartbeat:  10 * time.Second,
			Properties: amqp.Table{"connection_name": "bookloop-worker"},
		})
		if err == nil {
			return &RabbitMQ{connection: connection}, nil
		}
		lastErr = err
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("connect to RabbitMQ before startup deadline: %w", errors.Join(lastErr, ctx.Err()))
		case <-timer.C:
		}
	}
}

func rabbitURL(cfg config.RabbitMQConfig) string {
	return (&url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
		Path:   cfg.VHost,
	}).String()
}

func (r *RabbitMQ) OpenPublisher() (*RabbitEventPublisher, error) {
	channel, err := r.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open RabbitMQ publisher channel: %w", err)
	}
	if err := channel.ExchangeDeclare(eventsExchange, "topic", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("declare events exchange: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	return &RabbitEventPublisher{
		channel:  channel,
		confirms: channel.NotifyPublish(make(chan amqp.Confirmation, 1)),
	}, nil
}

func (r *RabbitMQ) OpenNotificationConsumer(
	retryDelay time.Duration,
) (*RabbitNotificationConsumer, error) {
	if retryDelay <= 0 {
		return nil, errors.New("notification retry delay must be greater than zero")
	}
	channel, err := r.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open RabbitMQ consumer channel: %w", err)
	}
	closeWithError := func(operation string, err error) (*RabbitNotificationConsumer, error) {
		_ = channel.Close()
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	for _, exchange := range []struct{ name, kind string }{
		{eventsExchange, "topic"}, {retryExchange, "direct"}, {deadExchange, "direct"},
	} {
		if err := channel.ExchangeDeclare(exchange.name, exchange.kind, true, false, false, false, nil); err != nil {
			return closeWithError("declare "+exchange.name, err)
		}
	}
	mainQueue, err := channel.QueueDeclare(notificationQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    retryExchange,
		"x-dead-letter-routing-key": notificationRoute,
	})
	if err != nil {
		return closeWithError("declare notification queue", err)
	}
	if err := channel.QueueBind(mainQueue.Name, "order.*.v1", eventsExchange, false, nil); err != nil {
		return closeWithError("bind order events", err)
	}
	if err := channel.QueueBind(mainQueue.Name, notificationRetryKey, eventsExchange, false, nil); err != nil {
		return closeWithError("bind notification retries", err)
	}
	retryQueue, err := channel.QueueDeclare(notificationRetry, true, false, false, false, amqp.Table{
		"x-message-ttl":             retryDelay.Milliseconds(),
		"x-dead-letter-exchange":    eventsExchange,
		"x-dead-letter-routing-key": notificationRetryKey,
	})
	if err != nil {
		return closeWithError("declare notification retry queue", err)
	}
	if err := channel.QueueBind(retryQueue.Name, notificationRoute, retryExchange, false, nil); err != nil {
		return closeWithError("bind notification retry queue", err)
	}
	deadQueue, err := channel.QueueDeclare(notificationDead, true, false, false, false, nil)
	if err != nil {
		return closeWithError("declare notification dead-letter queue", err)
	}
	if err := channel.QueueBind(deadQueue.Name, notificationRoute, deadExchange, false, nil); err != nil {
		return closeWithError("bind notification dead-letter queue", err)
	}
	if err := channel.Qos(10, 0, false); err != nil {
		return closeWithError("configure notification prefetch", err)
	}
	if err := channel.Confirm(false); err != nil {
		return closeWithError("enable retry publisher confirms", err)
	}
	deliveries, err := channel.Consume(mainQueue.Name, "", false, false, false, false, nil)
	if err != nil {
		return closeWithError("start notification consumer", err)
	}
	return &RabbitNotificationConsumer{
		channel:    channel,
		confirms:   channel.NotifyPublish(make(chan amqp.Confirmation, 1)),
		deliveries: deliveries,
	}, nil
}

type RabbitQueueMonitor struct{ channel *amqp.Channel }

func (r *RabbitMQ) OpenQueueMonitor() (*RabbitQueueMonitor, error) {
	channel, err := r.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open RabbitMQ monitoring channel: %w", err)
	}
	return &RabbitQueueMonitor{channel: channel}, nil
}

func (m *RabbitQueueMonitor) QueueDepths() (int, int, error) {
	main, err := m.channel.QueueInspect(notificationQueue)
	if err != nil {
		return 0, 0, err
	}
	dead, err := m.channel.QueueInspect(notificationDead)
	if err != nil {
		return 0, 0, err
	}
	return main.Messages, dead.Messages, nil
}

func (m *RabbitQueueMonitor) Close() error { return m.channel.Close() }

func (r *RabbitMQ) Close() error {
	if r == nil || r.connection == nil {
		return nil
	}
	return r.connection.Close()
}

func publishConfirmed(
	ctx context.Context,
	channel *amqp.Channel,
	confirms <-chan amqp.Confirmation,
	exchange, routingKey string,
	message amqp.Publishing,
) error {
	if err := channel.PublishWithContext(ctx, exchange, routingKey, false, false, message); err != nil {
		return err
	}
	select {
	case confirmation, ok := <-confirms:
		if !ok {
			return amqp.ErrClosed
		}
		if !confirmation.Ack {
			return errors.New("RabbitMQ negatively acknowledged publication")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
