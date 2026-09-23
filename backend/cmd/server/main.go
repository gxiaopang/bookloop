package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/handler"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/router"
	"github.com/gxiaopang/bookloop/backend/internal/service"
	"github.com/gxiaopang/bookloop/backend/internal/storage"
)

const (
	startupTimeout   = 30 * time.Second
	startupRetry     = time.Second
	readinessTimeout = 2 * time.Second
	shutdownTimeout  = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() (returnErr error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := config.NewLogger(os.Stdout, cfg.Log.Level)

	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	mysqlStore, err := storage.OpenMySQLWithRetry(startupContext, cfg.DB, startupRetry)
	cancelStartup()
	if err != nil {
		return err
	}

	redisStore := storage.NewRedis(cfg.Redis)
	resourcesClosed := false
	defer func() {
		if resourcesClosed {
			return
		}
		returnErr = errors.Join(returnErr, mysqlStore.Close(), redisStore.Close())
	}()

	redisContext, cancelRedis := context.WithTimeout(context.Background(), startupTimeout)
	redisErr := redisStore.Ping(redisContext)
	cancelRedis()
	if redisErr != nil {
		if cfg.Idempotency.AllowDegraded {
			logger.Warn("Redis startup check failed; continuing in degraded mode", "error", redisErr)
		} else {
			logger.Error("Redis startup check failed; readiness will remain unavailable", "error", redisErr)
		}
	} else {
		logger.Info("Redis connection established")
	}
	logger.Info("MySQL connection established")

	readiness := service.NewReadiness(
		mysqlStore.Ping,
		redisStore.Ping,
		readinessTimeout,
		!cfg.Idempotency.AllowDegraded,
	)
	healthHandler := handler.NewHealth(readiness, logger)
	userRepository := repository.NewUserRepository(mysqlStore.DB)
	jwtManager := service.NewJWTManager(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.ExpiresIn)
	authService, err := service.NewAuth(userRepository, jwtManager, cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("initialize authentication service: %w", err)
	}
	authHandler := handler.NewAuth(authService, logger)
	authentication := middleware.NewAuthentication(authService, logger)
	imageStorage, err := storage.NewLocalImages(cfg.Upload)
	if err != nil {
		return fmt.Errorf("initialize image storage: %w", err)
	}
	bookRepository := repository.NewBookRepository(mysqlStore.DB)
	bookService := service.NewBook(bookRepository, imageStorage, logger)
	bookHandler := handler.NewBook(bookService, logger, cfg.Upload.MaxBytes)
	favoriteRepository := repository.NewFavoriteRepository(mysqlStore.DB)
	favoriteService := service.NewFavorite(favoriteRepository, bookRepository)
	favoriteHandler := handler.NewFavorite(favoriteService, logger)
	orderRepository := repository.NewOrderRepository(mysqlStore.DB)
	orderIdempotencyRepository := repository.NewOrderIdempotencyRepository(redisStore.Client)
	orderService := service.NewOrder(
		orderRepository,
		orderIdempotencyRepository,
		cfg.Order.PaymentTimeout,
		service.OrderIdempotencyOptions{
			ProcessingTTL: cfg.Idempotency.ProcessingTTL,
			ResultTTL:     cfg.Idempotency.ResultTTL,
			AllowDegraded: cfg.Idempotency.AllowDegraded,
		},
		logger,
	)
	orderHandler := handler.NewOrder(orderService, logger)
	notificationRepository := repository.NewNotificationRepository(mysqlStore.DB)
	notificationService := service.NewNotification(notificationRepository)
	notificationHandler := handler.NewNotification(notificationService, logger)
	outboxRepository := repository.NewOutboxRepository(mysqlStore.DB)
	messagingStatusService := service.NewMessagingStatus(outboxRepository)
	messagingStatusHandler := handler.NewMessagingStatus(messagingStatusService, logger)
	engine := router.New(logger, healthHandler, authHandler, bookHandler, favoriteHandler, orderHandler,
		notificationHandler, messagingStatusHandler, authentication)
	if err := engine.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
		return fmt.Errorf("configure trusted proxies: %w", err)
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	workerContext, cancelWorkers := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go runOrderExpirationWorker(workerContext, orderService, logger, workerDone)

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "address", cfg.HTTP.Address, "app_env", cfg.AppEnv)
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErrors <- err
	}()

	var serveErr error
	select {
	case <-signalContext.Done():
		logger.Info("shutdown signal received")
	case serveErr = <-serverErrors:
		if serveErr != nil {
			logger.Error("HTTP server failed", "error", serveErr)
		}
	}
	cancelWorkers()
	<-workerDone

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	shutdownErr := httpServer.Shutdown(shutdownContext)
	cancelShutdown()
	if shutdownErr != nil {
		logger.Error("HTTP server shutdown failed", "error", shutdownErr)
	} else {
		logger.Info("HTTP server stopped")
	}

	mysqlCloseErr := mysqlStore.Close()
	if mysqlCloseErr != nil {
		logger.Error("MySQL connection close failed", "error", mysqlCloseErr)
	} else {
		logger.Info("MySQL connection closed")
	}
	redisCloseErr := redisStore.Close()
	if redisCloseErr != nil {
		logger.Error("Redis connection close failed", "error", redisCloseErr)
	} else {
		logger.Info("Redis connection closed")
	}
	resourcesClosed = true

	return errors.Join(serveErr, shutdownErr, mysqlCloseErr, redisCloseErr)
}

func runOrderExpirationWorker(
	ctx context.Context,
	orders *service.Order,
	logger *slog.Logger,
	done chan<- struct{},
) {
	defer close(done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired, err := orders.ExpireOverdue(ctx)
			if err != nil {
				if ctx.Err() == nil {
					logger.Error("order expiration scan failed", "error", err)
				}
				continue
			}
			if expired > 0 {
				logger.Info("expired pending orders", "count", expired)
			}
		}
	}
}
