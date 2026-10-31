package repository_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	domainevent "github.com/gxiaopang/bookloop/backend/internal/event"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

// This external test package can exercise the real service and repositories in
// the same test binary as the other destructive repository tests, sequentially.
func TestTwoPartyHandoverWithMySQL8(t *testing.T) {
	dsn := os.Getenv("BOOKLOOP_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("BOOKLOOP_TEST_MYSQL_DSN is not set; skipping real MySQL integration test")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ParseTime, cfg.MultiStatements, cfg.Loc = true, true, time.UTC
	db, err := gorm.Open(gormmysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close MySQL: %v", err)
		}
	})
	dropTables := func() {
		for _, table := range []string{"notifications", "processed_messages", "outbox_events", "orders", "favorites", "book_images", "books", "users"} {
			if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
				t.Errorf("drop test table %s: %v", table, err)
			}
		}
	}
	dropTables()
	t.Cleanup(dropTables)
	apply := func(name string) error {
		contents, err := os.ReadFile(filepath.Join("../../migrations", name))
		if err != nil {
			return err
		}
		return db.Exec(string(contents)).Error
	}
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(file) == "000008_require_buyer_receipt.up.sql" {
			continue
		}
		if err := apply(filepath.Base(file)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	for _, user := range []model.User{
		{ID: 10, Username: "seller", Email: "seller@example.com", PasswordHash: "test-fixture", Status: model.UserStatusActive},
		{ID: 20, Username: "buyer", Email: "buyer@example.com", PasswordHash: "test-fixture", Status: model.UserStatusActive},
		{ID: 21, Username: "other", Email: "other@example.com", PasswordHash: "test-fixture", Status: model.UserStatusActive},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	newBook := func(status model.BookStatus) *model.Book {
		qr := "/uploads/test-contact.png"
		book := &model.Book{SellerID: 10, Title: "Handover", Price: "10.00", Condition: model.BookConditionGood,
			Status: status, WechatQRCodeURL: &qr}
		if err := db.Create(book).Error; err != nil {
			t.Fatal(err)
		}
		return book
	}
	book := newBook(model.BookStatusReserved)
	legacy := &model.Order{OrderNo: "01HF7YAT00ABCDEFGHJKMNPQRS", BookID: book.ID, BuyerID: 20, SellerID: 10,
		BookTitle: book.Title, BookPrice: book.Price, Status: model.OrderStatusPendingHandover,
		IdempotencyKey: "legacy-pending", RequestFingerprint: "legacy", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := db.Omit("HandedOverAt").Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	oldSold := newBook(model.BookStatusSold)
	completedAt := time.Now().UTC().Truncate(time.Millisecond)
	oldCompleted := &model.Order{OrderNo: "01HF7YAT00ABCDEFGHJKMNPQRT", BookID: oldSold.ID, BuyerID: 20, SellerID: 10,
		BookTitle: oldSold.Title, BookPrice: oldSold.Price, Status: model.OrderStatusCompleted, CompletedAt: &completedAt,
		IdempotencyKey: "legacy-completed", RequestFingerprint: "legacy", ExpiresAt: completedAt}
	if err := db.Omit("HandedOverAt").Create(oldCompleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := apply("000008_require_buyer_receipt.up.sql"); err != nil {
		t.Fatalf("upgrade populated database: %v", err)
	}
	orders := repository.NewOrderRepository(db)
	svc := service.NewOrder(orders, nil, 15*time.Minute, service.OrderIdempotencyOptions{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	names, err := orders.FindUsernames(ctx, []uint64{10, 20})
	if err != nil || len(names) != 2 || names[10] != "seller" || names[20] != "buyer" {
		t.Fatalf("database display names = %v/%v", names, err)
	}
	sellerName, err := repository.NewBookRepository(db).FindSellerUsername(ctx, 10)
	if err != nil || sellerName != "seller" {
		t.Fatalf("seller username = %s/%v", sellerName, err)
	}
	displayOrder, err := svc.Get(ctx, &model.User{ID: 20}, legacy.OrderNo)
	if err != nil || displayOrder.BuyerUsername != "buyer" || displayOrder.SellerUsername != "seller" {
		t.Fatalf("order display names = %+v/%v", displayOrder, err)
	}
	if existing, err := svc.Complete(ctx, 20, oldCompleted.OrderNo); err != nil || existing.HandedOverAt != nil ||
		existing.CompletedAt == nil || !existing.CompletedAt.Equal(completedAt) {
		t.Fatalf("legacy completion was changed by migration: %+v/%v", existing, err)
	}
	if _, err := svc.Complete(ctx, 20, legacy.OrderNo); !errors.Is(err, service.ErrOrderStateConflict) {
		t.Fatalf("buyer skipped seller handover: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_handover_outbox BEFORE INSERT ON outbox_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'forced outbox failure'`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deliver(ctx, 10, legacy.OrderNo); err == nil {
		t.Fatal("delivery succeeded despite event failure")
	}
	unchanged, err := orders.FindByOrderNo(ctx, legacy.OrderNo)
	if err != nil || unchanged.Status != model.OrderStatusPendingHandover || unchanged.HandedOverAt != nil {
		t.Fatalf("failed delivery did not roll back: %+v/%v", unchanged, err)
	}
	if err := db.Exec("DROP TRIGGER fail_handover_outbox").Error; err != nil {
		t.Fatal(err)
	}

	// Multiple real connections must converge on one handover and one event.
	concurrently := func(operation func() error) {
		start := make(chan struct{})
		results := make(chan error, 8)
		var workers sync.WaitGroup
		for i := 0; i < 8; i++ {
			workers.Add(1)
			go func() { defer workers.Done(); <-start; results <- operation() }()
		}
		close(start)
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("concurrent confirmation: %v", err)
			}
		}
	}
	concurrently(func() error { _, err := svc.Deliver(ctx, 10, legacy.OrderNo); return err })
	delivered, err := orders.FindByOrderNo(ctx, legacy.OrderNo)
	if err != nil || delivered.Status != model.OrderStatusPendingReceipt || delivered.HandedOverAt == nil ||
		delivered.ActiveBookID == nil || *delivered.ActiveBookID != book.ID {
		t.Fatalf("handover did not retain the active-book constraint: %+v/%v", delivered, err)
	}
	if err := apply("000008_require_buyer_receipt.down.sql"); err == nil {
		t.Fatal("rollback must reject pending receipt orders")
	}
	// Failed ALTER must preserve both the state and new timestamp column.
	if err := db.Model(delivered).Update("expires_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := svc.ExpireOverdue(ctx); err != nil || count != 0 {
		t.Fatalf("handed-over order expired: %d/%v", count, err)
	}
	if _, err := svc.Create(ctx, 21, book.ID, "another-buyer"); !errors.Is(err, service.ErrBookNotAvailable) {
		t.Fatalf("handed-over book could be repurchased: %v", err)
	}
	duplicate := *delivered
	duplicate.ID, duplicate.OrderNo, duplicate.BuyerID = 0, "01HF7YAT00ABCDEFGHJKMNPQRV", 21
	duplicate.IdempotencyKey = "duplicate-active"
	if err := orders.Create(ctx, &duplicate); !errors.Is(err, repository.ErrOrderActiveBookDuplicate) {
		t.Fatalf("pending_receipt failed to occupy unique active book: %v", err)
	}

	// Force event persistence to fail after the book/order updates. The entire
	// completion must roll back, including the book's reserved -> sold change.
	if err := db.Exec(`CREATE TRIGGER fail_handover_outbox BEFORE INSERT ON outbox_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'forced outbox failure'`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(ctx, 20, legacy.OrderNo); err == nil {
		t.Fatal("completion succeeded despite event failure")
	}
	var currentBook model.Book
	if err := db.Take(&currentBook, book.ID).Error; err != nil {
		t.Fatal(err)
	}
	current, err := orders.FindByOrderNo(ctx, legacy.OrderNo)
	if err != nil || current.Status != model.OrderStatusPendingReceipt || current.CompletedAt != nil || currentBook.Status != model.BookStatusReserved {
		t.Fatalf("failed completion was not atomic: order=%+v, book=%s, err=%v", current, currentBook.Status, err)
	}
	if err := db.Exec("DROP TRIGGER fail_handover_outbox").Error; err != nil {
		t.Fatal(err)
	}
	concurrently(func() error { _, err := svc.Complete(ctx, 20, legacy.OrderNo); return err })
	current, err = orders.FindByOrderNo(ctx, legacy.OrderNo)
	if err != nil || current.Status != model.OrderStatusCompleted || current.CompletedAt == nil ||
		current.HandedOverAt == nil || !current.HandedOverAt.Equal(*delivered.HandedOverAt) || current.ActiveBookID == nil {
		t.Fatalf("completion lost state or timestamps: %+v/%v", current, err)
	}
	if err := db.Take(&currentBook, book.ID).Error; err != nil || currentBook.Status != model.BookStatusSold {
		t.Fatalf("completed book status = %s/%v", currentBook.Status, err)
	}
	for _, eventType := range []string{domainevent.OrderDelivered, domainevent.OrderCompleted} {
		var count int64
		if err := db.Model(&model.OutboxEvent{}).Where("aggregate_id = ? AND event_type = ?", legacy.OrderNo, eventType).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s event count = %d/%v, want one", eventType, count, err)
		}
	}

	// Delivery and buyer cancellation serialize on the same book/order locks.
	raceBook := newBook(model.BookStatusAvailable)
	raceOrder, err := svc.Create(ctx, 20, raceBook.ID, "delivery-vs-cancel")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; _, err := svc.Deliver(ctx, 10, raceOrder.Order.OrderNo); results <- err }()
	go func() { <-start; _, err := svc.Cancel(ctx, 20, raceOrder.Order.OrderNo); results <- err }()
	close(start)
	succeeded, conflicted := 0, 0
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, service.ErrOrderStateConflict):
			conflicted++
		default:
			t.Fatalf("delivery/cancel race: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("delivery/cancel race = %d successes/%d conflicts", succeeded, conflicted)
	}
	raceCurrent, err := orders.FindByOrderNo(ctx, raceOrder.Order.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Take(raceBook, raceBook.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !((raceCurrent.Status == model.OrderStatusPendingReceipt && raceBook.Status == model.BookStatusReserved) ||
		(raceCurrent.Status == model.OrderStatusCancelled && raceBook.Status == model.BookStatusAvailable)) {
		t.Fatalf("delivery/cancel race left inconsistent state: %s/%s", raceCurrent.Status, raceBook.Status)
	}

	// Concurrent seller cancellations must release the unique active-book slot
	// once, so a subsequent buyer can reserve the book again.
	cancelBook := newBook(model.BookStatusAvailable)
	cancelOrder, err := svc.Create(ctx, 20, cancelBook.ID, "seller-cancel")
	if err != nil {
		t.Fatal(err)
	}
	concurrently(func() error { _, err := svc.Cancel(ctx, 10, cancelOrder.Order.OrderNo); return err })
	cancelled, err := orders.FindByOrderNo(ctx, cancelOrder.Order.OrderNo)
	if err != nil || cancelled.Status != model.OrderStatusCancelled || cancelled.ActiveBookID != nil ||
		cancelled.CancelReason == nil || *cancelled.CancelReason != "cancelled by seller" {
		t.Fatalf("seller cancellation = %+v/%v", cancelled, err)
	}
	if err := db.Take(cancelBook, cancelBook.ID).Error; err != nil || cancelBook.Status != model.BookStatusAvailable {
		t.Fatalf("seller cancellation book = %s/%v", cancelBook.Status, err)
	}
	var cancellationCount int64
	if err := db.Model(&model.OutboxEvent{}).Where("aggregate_id = ? AND event_type = ?", cancelled.OrderNo, domainevent.OrderCancelled).Count(&cancellationCount).Error; err != nil || cancellationCount != 1 {
		t.Fatalf("seller cancellation events = %d/%v", cancellationCount, err)
	}
	if _, err := svc.Create(ctx, 21, cancelBook.ID, "after-seller-cancel"); err != nil {
		t.Fatalf("repurchase after seller cancellation: %v", err)
	}
}
