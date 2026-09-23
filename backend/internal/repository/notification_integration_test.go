package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

func TestNotificationOutboxMigrationAndRepositoriesWithMySQL8(t *testing.T) {
	dsn := os.Getenv(mysqlTestDSNEnvironment)
	if dsn == "" {
		t.Skip(mysqlTestDSNEnvironment + " is not set; skipping real MySQL integration test")
	}
	driverConfig, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	db, err := gorm.Open(gormmysql.Open(driverConfig.FormatDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test MySQL pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	down := readMigration(t, "../../migrations/000005_create_notifications_and_outbox.down.sql")
	if err := execIntegrationMigration(db, down); err != nil {
		t.Fatalf("remove messaging schema: %v", err)
	}
	applyOrderTestSchema(t, db)
	t.Cleanup(func() {
		if cleanupErr := execIntegrationMigration(db, down); cleanupErr != nil {
			t.Errorf("remove messaging schema: %v", cleanupErr)
		}
	})
	if err := execIntegrationMigration(db, readMigration(t, "../../migrations/000005_create_notifications_and_outbox.up.sql")); err != nil {
		t.Fatalf("apply messaging migration: %v", err)
	}
	assertMessagingSchema(t, db)

	sellerID := createIntegrationUser(t, db, "notify_seller", "notify-seller@example.com", model.UserStatusActive)
	buyerID := createIntegrationUser(t, db, "notify_buyer", "notify-buyer@example.com", model.UserStatusActive)
	book := createIntegrationBook(t, NewBookRepository(db), sellerID, "Messaging", "18.00", model.BookConditionGood, model.BookStatusAvailable)
	order := createOrderForIntegration(t, NewOrderRepository(db), book, buyerID, "notification-intent", 500)

	orders := NewOrderRepository(db)
	event := &model.OutboxEvent{
		AggregateType: "order", AggregateID: order.OrderNo, EventType: "order.created.v1",
		Payload:    []byte(`{"order_no":"` + order.OrderNo + `"}`),
		OccurredAt: time.Now().UTC(), AvailableAt: time.Now().UTC(),
	}
	if err := orders.CreateOutboxEvent(context.Background(), event); err != nil {
		t.Fatalf("CreateOutboxEvent() error = %v", err)
	}
	outbox := NewOutboxRepository(db)
	claimed, err := outbox.ClaimBatch(context.Background(), "worker-one", 10, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].ID != event.ID {
		t.Fatalf("ClaimBatch() = %+v/%v, want event %d", claimed, err, event.ID)
	}
	secondClaim, err := outbox.ClaimBatch(context.Background(), "worker-two", 10, time.Minute)
	if err != nil || len(secondClaim) != 0 {
		t.Fatalf("concurrent ClaimBatch() = %+v/%v, want leased event hidden", secondClaim, err)
	}
	if err := outbox.MarkPublished(context.Background(), event.ID, "worker-one"); err != nil {
		t.Fatalf("MarkPublished() error = %v", err)
	}

	notifications := NewNotificationRepository(db)
	orderNo := order.OrderNo
	created, err := notifications.ProcessEvent(context.Background(), "notification.v1", event.ID, []model.Notification{
		{UserID: buyerID, EventID: event.ID, Type: event.EventType, Title: "created", Content: "buyer", OrderNo: &orderNo},
		{UserID: sellerID, EventID: event.ID, Type: event.EventType, Title: "created", Content: "seller", OrderNo: &orderNo},
	})
	if err != nil || !created {
		t.Fatalf("ProcessEvent() = %v/%v, want created", created, err)
	}
	created, err = notifications.ProcessEvent(context.Background(), "notification.v1", event.ID, nil)
	if err != nil || created {
		t.Fatalf("duplicate ProcessEvent() = %v/%v, want idempotent no-op", created, err)
	}
	items, total, unread, err := notifications.ListByUser(context.Background(), buyerID, false, 1, 20)
	if err != nil || len(items) != 1 || total != 1 || unread != 1 {
		t.Fatalf("ListByUser() = %+v/%d/%d/%v, want one unread", items, total, unread, err)
	}
	if err := notifications.MarkRead(context.Background(), sellerID, items[0].ID); !errors.Is(err, ErrNotificationNotFound) {
		t.Fatalf("MarkRead(other user) error = %v, want ErrNotificationNotFound", err)
	}
	if err := notifications.MarkRead(context.Background(), buyerID, items[0].ID); err != nil {
		t.Fatalf("MarkRead(owner) error = %v", err)
	}

	rollbackErr := errors.New("force rollback")
	if err := orders.WithinTransaction(context.Background(), func(store OrderStore) error {
		if err := store.CreateOutboxEvent(context.Background(), &model.OutboxEvent{
			AggregateType: "order", AggregateID: "rollback", EventType: "order.created.v1",
			Payload: []byte(`{}`), OccurredAt: time.Now().UTC(), AvailableAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback transaction error = %v, want marker", err)
	}
	var rollbackCount int64
	if err := db.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", "rollback").Count(&rollbackCount).Error; err != nil || rollbackCount != 0 {
		t.Fatalf("rolled back outbox count/error = %d/%v, want zero", rollbackCount, err)
	}
}

func assertMessagingSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for table, fragments := range map[string][]string{
		"outbox_events":      {"ix_outbox_publishable", "ix_outbox_locked", "payload"},
		"processed_messages": {"PRIMARY KEY (`consumer_name`,`event_id`)", "fk_processed_messages_event"},
		"notifications":      {"ux_notifications_user_event", "ix_notifications_user_read", "fk_notifications_user"},
	} {
		var tableName, createSQL string
		if err := db.Raw("SHOW CREATE TABLE "+table).Row().Scan(&tableName, &createSQL); err != nil {
			t.Fatalf("SHOW CREATE TABLE %s: %v", table, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(createSQL, fragment) {
				t.Fatalf("%s schema does not contain %q:\n%s", table, fragment, createSQL)
			}
		}
	}
}
