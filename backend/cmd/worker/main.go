package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/messaging"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/service"
	"github.com/gxiaopang/bookloop/backend/internal/storage"
)

const (
	startupTimeout            = 30 * time.Second
	startupRetry              = time.Second
	notificationRetryDelay    = 30 * time.Second
	notificationMaxAttempts   = 5
	messagingMonitoringPeriod = time.Minute
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := config.NewLogger(os.Stdout, cfg.Log.Level)
	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	mysqlStore, err := storage.OpenMySQLWithRetry(startupContext, cfg.DB, startupRetry)
	if err != nil {
		cancelStartup()
		return err
	}
	rabbit, err := messaging.DialRabbitMQWithRetry(startupContext, cfg.RabbitMQ, startupRetry)
	cancelStartup()
	if err != nil {
		_ = mysqlStore.Close()
		return err
	}
	defer func() {
		if closeErr := errors.Join(rabbit.Close(), mysqlStore.Close()); closeErr != nil {
			logger.Error("worker resource close failed", "error", closeErr)
		}
	}()

	rabbitPublisher, err := rabbit.OpenPublisher()
	if err != nil {
		return err
	}
	defer rabbitPublisher.Close()
	rabbitConsumer, err := rabbit.OpenNotificationConsumer(notificationRetryDelay)
	if err != nil {
		return err
	}
	defer rabbitConsumer.Close()
	queueMonitor, err := rabbit.OpenQueueMonitor()
	if err != nil {
		return err
	}
	defer queueMonitor.Close()

	outboxRepository := repository.NewOutboxRepository(mysqlStore.DB)
	notificationRepository := repository.NewNotificationRepository(mysqlStore.DB)
	notificationService := service.NewNotification(notificationRepository)
	hostname, _ := os.Hostname()
	workerID := hostname + ":" + strconv.Itoa(os.Getpid())
	publisher := messaging.NewOutboxPublisher(outboxRepository, rabbitPublisher, workerID, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- publisher.Run(ctx) }()
	go func() {
		errorsChannel <- rabbitConsumer.Run(ctx, notificationService, notificationMaxAttempts, logger)
	}()
	go monitor(ctx, outboxRepository, queueMonitor, logger)
	logger.Info("messaging worker started", "worker_id", workerID)

	select {
	case <-ctx.Done():
		logger.Info("messaging worker shutdown requested")
		return waitForWorkers(errorsChannel, 2, 10*time.Second)
	case runErr := <-errorsChannel:
		stop()
		shutdownErr := waitForWorkers(errorsChannel, 1, 10*time.Second)
		if runErr == nil {
			return shutdownErr
		}
		return errors.Join(fmt.Errorf("messaging worker failed: %w", runErr), shutdownErr)
	}
}

func waitForWorkers(errorsChannel <-chan error, count int, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var result error
	for range count {
		select {
		case err := <-errorsChannel:
			result = errors.Join(result, err)
		case <-timer.C:
			return errors.Join(result, errors.New("timed out waiting for messaging workers to stop"))
		}
	}
	return result
}

type outboxStats interface {
	Stats(context.Context) (repository.OutboxStats, error)
}

type queueDepths interface {
	QueueDepths() (int, int, error)
}

func monitor(ctx context.Context, outbox outboxStats, queues queueDepths, logger *slog.Logger) {
	ticker := time.NewTicker(messagingMonitoringPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, statsErr := outbox.Stats(ctx)
			ready, dead, queueErr := queues.QueueDepths()
			if statsErr != nil || queueErr != nil {
				logger.Error("messaging monitoring failed", "outbox_error", statsErr, "queue_error", queueErr)
				continue
			}
			logger.Info("messaging status", "pending_outbox", stats.Pending, "retrying_outbox", stats.Retrying,
				"notification_queue", ready, "notification_dead_letters", dead,
				"oldest_pending_at", stats.OldestPending)
		}
	}
}
