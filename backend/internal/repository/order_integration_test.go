package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

func TestOrderMigrationAndConcurrencyWithMySQL8(t *testing.T) {
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
	sqlDB.SetMaxOpenConns(20)
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test MySQL: %v", closeErr)
		}
	})

	applyOrderTestSchema(t, db)
	assertOrderSchema(t, db)

	sellerID := createIntegrationUser(t, db, "order_seller", "order-seller@example.com", model.UserStatusActive)
	buyerIDs := make([]uint64, 8)
	for index := range buyerIDs {
		buyerIDs[index] = createIntegrationUser(t, db,
			fmt.Sprintf("order_buyer_%d", index), fmt.Sprintf("order-buyer-%d@example.com", index), model.UserStatusActive)
	}
	books := NewBookRepository(db)
	orders := NewOrderRepository(db)
	book := createIntegrationBook(t, books, sellerID, "Concurrent Purchase", "28.50", model.BookConditionGood, model.BookStatusAvailable)

	errBookUnavailable := errors.New("test book unavailable")
	start := make(chan struct{})
	results := make(chan error, len(buyerIDs))
	var waitGroup sync.WaitGroup
	for index, buyerID := range buyerIDs {
		waitGroup.Add(1)
		go func(index int, buyerID uint64) {
			defer waitGroup.Done()
			<-start
			results <- orders.WithinTransaction(context.Background(), func(store OrderStore) error {
				lockedBook, err := store.FindBookByIDForUpdate(context.Background(), book.ID)
				if err != nil {
					return err
				}
				if lockedBook.Status != model.BookStatusAvailable {
					return errBookUnavailable
				}
				seller, err := store.FindUserByIDForShare(context.Background(), lockedBook.SellerID)
				if err != nil {
					return err
				}
				if seller.Status != model.UserStatusActive {
					return errBookUnavailable
				}
				now, err := store.CurrentTime(context.Background())
				if err != nil {
					return err
				}
				if err := store.Create(context.Background(), &model.Order{
					OrderNo:            fmt.Sprintf("%026d", index+1),
					BookID:             lockedBook.ID,
					BuyerID:            buyerID,
					SellerID:           lockedBook.SellerID,
					BookTitle:          lockedBook.Title,
					BookPrice:          lockedBook.Price,
					Status:             model.OrderStatusPendingPayment,
					IdempotencyKey:     fmt.Sprintf("buyer-intent-%d", index),
					RequestFingerprint: fmt.Sprintf("%064d", index+1),
					ExpiresAt:          now.Add(15 * time.Minute),
				}); err != nil {
					return err
				}
				time.Sleep(20 * time.Millisecond)
				return store.UpdateBookStatus(context.Background(), lockedBook.ID, model.BookStatusAvailable, model.BookStatusReserved)
			})
		}(index, buyerID)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes, unavailable := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errBookUnavailable):
			unavailable++
		default:
			t.Fatalf("concurrent create unexpected error = %v", err)
		}
	}
	if successes != 1 || unavailable != len(buyerIDs)-1 {
		t.Fatalf("concurrent successes/unavailable = %d/%d, want 1/%d", successes, unavailable, len(buyerIDs)-1)
	}

	var persisted []model.Order
	if err := db.Find(&persisted).Error; err != nil {
		t.Fatalf("list persisted orders: %v", err)
	}
	if len(persisted) != 1 || persisted[0].Status != model.OrderStatusPendingPayment ||
		persisted[0].ActiveBookID == nil || *persisted[0].ActiveBookID != book.ID {
		t.Fatalf("persisted orders = %+v, want one active pending order", persisted)
	}
	var persistedBook model.Book
	if err := db.Take(&persistedBook, book.ID).Error; err != nil {
		t.Fatalf("load purchased book: %v", err)
	}
	if persistedBook.Status != model.BookStatusReserved {
		t.Fatalf("book status = %q, want reserved", persistedBook.Status)
	}
	assertIdempotencyRecheckUsesCurrentRead(t, orders, books, sellerID, buyerIDs[0])

	duplicateActive := &model.Order{
		OrderNo:            "99999999999999999999999999",
		BookID:             book.ID,
		BuyerID:            buyerIDs[1],
		SellerID:           sellerID,
		BookTitle:          book.Title,
		BookPrice:          book.Price,
		Status:             model.OrderStatusPaid,
		IdempotencyKey:     "another-intent",
		RequestFingerprint: strings.Repeat("a", 64),
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
	}
	if err := orders.Create(context.Background(), duplicateActive); !errors.Is(err, ErrOrderActiveBookDuplicate) {
		t.Fatalf("Create(second active chain) error = %v, want ErrOrderActiveBookDuplicate", err)
	}

	winner := &persisted[0]
	if err := orders.WithinTransaction(context.Background(), func(store OrderStore) error {
		lockedBook, err := store.FindBookByIDForUpdate(context.Background(), book.ID)
		if err != nil {
			return err
		}
		lockedOrder, err := store.FindByOrderNoForUpdate(context.Background(), winner.OrderNo)
		if err != nil {
			return err
		}
		now, err := store.CurrentTime(context.Background())
		if err != nil {
			return err
		}
		if err := store.UpdateOrderStatus(context.Background(), lockedOrder.ID,
			model.OrderStatusPendingPayment, model.OrderStatusPaid, map[string]any{"paid_at": now}); err != nil {
			return err
		}
		return store.UpdateBookStatus(context.Background(), lockedBook.ID, model.BookStatusReserved, model.BookStatusSold)
	}); err != nil {
		t.Fatalf("pay transaction: %v", err)
	}
	if err := db.Take(winner, winner.ID).Error; err != nil {
		t.Fatalf("reload paid order: %v", err)
	}
	if winner.Status != model.OrderStatusPaid || winner.ActiveBookID == nil || *winner.ActiveBookID != book.ID {
		t.Fatalf("paid order = %+v, want active_book_id retained", winner)
	}
	if err := orders.WithinTransaction(context.Background(), func(store OrderStore) error {
		if _, err := store.FindBookByIDForUpdate(context.Background(), book.ID); err != nil {
			return err
		}
		lockedOrder, err := store.FindByOrderNoForUpdate(context.Background(), winner.OrderNo)
		if err != nil {
			return err
		}
		now, err := store.CurrentTime(context.Background())
		if err != nil {
			return err
		}
		return store.UpdateOrderStatus(context.Background(), lockedOrder.ID,
			model.OrderStatusPaid, model.OrderStatusCompleted, map[string]any{"completed_at": now})
	}); err != nil {
		t.Fatalf("complete transaction: %v", err)
	}
	if err := db.Take(winner, winner.ID).Error; err != nil {
		t.Fatalf("reload completed order: %v", err)
	}
	if winner.Status != model.OrderStatusCompleted || winner.ActiveBookID == nil || *winner.ActiveBookID != book.ID {
		t.Fatalf("completed order = %+v, want active_book_id retained", winner)
	}

	releasableBook := createIntegrationBook(t, books, sellerID, "Cancellation", "15.00", model.BookConditionGood, model.BookStatusAvailable)
	first := createOrderForIntegration(t, orders, releasableBook, buyerIDs[2], "cancel-key", 100)
	if err := orders.WithinTransaction(context.Background(), func(store OrderStore) error {
		lockedBook, err := store.FindBookByIDForUpdate(context.Background(), releasableBook.ID)
		if err != nil {
			return err
		}
		lockedOrder, err := store.FindByOrderNoForUpdate(context.Background(), first.OrderNo)
		if err != nil {
			return err
		}
		now, err := store.CurrentTime(context.Background())
		if err != nil {
			return err
		}
		if err := store.UpdateOrderStatus(context.Background(), lockedOrder.ID,
			model.OrderStatusPendingPayment, model.OrderStatusCancelled,
			map[string]any{"cancelled_at": now, "cancel_reason": "test cancellation"}); err != nil {
			return err
		}
		return store.UpdateBookStatus(context.Background(), lockedBook.ID, model.BookStatusReserved, model.BookStatusAvailable)
	}); err != nil {
		t.Fatalf("cancel transaction: %v", err)
	}
	if err := db.Take(first, first.ID).Error; err != nil {
		t.Fatalf("reload cancelled order: %v", err)
	}
	if first.Status != model.OrderStatusCancelled || first.ActiveBookID != nil {
		t.Fatalf("cancelled order = %+v, want released active_book_id", first)
	}
	duplicateIdempotency := &model.Order{
		OrderNo:            "77777777777777777777777777",
		BookID:             releasableBook.ID,
		BuyerID:            first.BuyerID,
		SellerID:           sellerID,
		BookTitle:          releasableBook.Title,
		BookPrice:          releasableBook.Price,
		Status:             model.OrderStatusCancelled,
		IdempotencyKey:     first.IdempotencyKey,
		RequestFingerprint: strings.Repeat("c", 64),
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
	}
	if err := orders.Create(context.Background(), duplicateIdempotency); !errors.Is(err, ErrOrderIdempotencyDuplicate) {
		t.Fatalf("Create(duplicate buyer/key) error = %v, want ErrOrderIdempotencyDuplicate", err)
	}
	second := createOrderForIntegration(t, orders, releasableBook, buyerIDs[3], "new-key", 101)
	if second.Status != model.OrderStatusPendingPayment {
		t.Fatalf("replacement order status = %q, want pending_payment", second.Status)
	}
	if err := orders.WithinTransaction(context.Background(), func(store OrderStore) error {
		lockedBook, err := store.FindBookByIDForUpdate(context.Background(), releasableBook.ID)
		if err != nil {
			return err
		}
		lockedOrder, err := store.FindByOrderNoForUpdate(context.Background(), second.OrderNo)
		if err != nil {
			return err
		}
		now, err := store.CurrentTime(context.Background())
		if err != nil {
			return err
		}
		if err := store.UpdateOrderStatus(context.Background(), lockedOrder.ID,
			model.OrderStatusPendingPayment, model.OrderStatusExpired,
			map[string]any{"cancelled_at": now, "cancel_reason": "payment timeout"}); err != nil {
			return err
		}
		return store.UpdateBookStatus(context.Background(), lockedBook.ID, model.BookStatusReserved, model.BookStatusAvailable)
	}); err != nil {
		t.Fatalf("expire transaction: %v", err)
	}
	if err := db.Take(second, second.ID).Error; err != nil {
		t.Fatalf("reload expired order: %v", err)
	}
	if second.Status != model.OrderStatusExpired || second.ActiveBookID != nil {
		t.Fatalf("expired order = %+v, want released active_book_id", second)
	}
	third := createOrderForIntegration(t, orders, releasableBook, buyerIDs[4], "after-expiry-key", 102)
	if third.Status != model.OrderStatusPendingPayment {
		t.Fatalf("post-expiry replacement status = %q, want pending_payment", third.Status)
	}

	invalidSelfOrder := &model.Order{
		OrderNo:            "88888888888888888888888888",
		BookID:             releasableBook.ID,
		BuyerID:            sellerID,
		SellerID:           sellerID,
		BookTitle:          releasableBook.Title,
		BookPrice:          releasableBook.Price,
		Status:             model.OrderStatusCancelled,
		IdempotencyKey:     "self-order",
		RequestFingerprint: strings.Repeat("b", 64),
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(invalidSelfOrder).Error; err == nil {
		t.Fatal("database accepted buyer_id = seller_id, want CHECK violation")
	}
}

func assertIdempotencyRecheckUsesCurrentRead(
	t *testing.T,
	orders *OrderRepository,
	books *BookRepository,
	sellerID, buyerID uint64,
) {
	t.Helper()
	book := createIntegrationBook(t, books, sellerID, "Idempotent Concurrent Request", "16.00", model.BookConditionGood, model.BookStatusAvailable)
	const (
		orderNo = "66666666666666666666666666"
		key     = "concurrent-same-key"
	)
	firstLocked := make(chan struct{})
	secondRead := make(chan struct{})
	releaseFirst := make(chan struct{})
	results := make(chan error, 2)

	go func() {
		results <- orders.WithinTransaction(context.Background(), func(store OrderStore) error {
			lockedBook, err := store.FindBookByIDForUpdate(context.Background(), book.ID)
			if err != nil {
				return err
			}
			now, err := store.CurrentTime(context.Background())
			if err != nil {
				return err
			}
			if err := store.Create(context.Background(), &model.Order{
				OrderNo:            orderNo,
				BookID:             book.ID,
				BuyerID:            buyerID,
				SellerID:           sellerID,
				BookTitle:          book.Title,
				BookPrice:          book.Price,
				Status:             model.OrderStatusPendingPayment,
				IdempotencyKey:     key,
				RequestFingerprint: strings.Repeat("d", 64),
				ExpiresAt:          now.Add(15 * time.Minute),
			}); err != nil {
				return err
			}
			if err := store.UpdateBookStatus(context.Background(), book.ID, model.BookStatusAvailable, model.BookStatusReserved); err != nil {
				return err
			}
			close(firstLocked)
			<-releaseFirst
			_ = lockedBook
			return nil
		})
	}()

	<-firstLocked
	go func() {
		results <- orders.WithinTransaction(context.Background(), func(store OrderStore) error {
			if _, err := store.FindByBuyerAndIdempotencyKey(context.Background(), buyerID, key); !errors.Is(err, ErrOrderNotFound) {
				return fmt.Errorf("initial consistent read error = %v, want ErrOrderNotFound", err)
			}
			close(secondRead)
			if _, err := store.FindBookByIDForUpdate(context.Background(), book.ID); err != nil {
				return err
			}
			existing, err := store.FindByBuyerAndIdempotencyKeyForUpdate(context.Background(), buyerID, key)
			if err != nil {
				return err
			}
			if existing.OrderNo != orderNo {
				return fmt.Errorf("rechecked order_no = %q, want %q", existing.OrderNo, orderNo)
			}
			return nil
		})
	}()

	<-secondRead
	close(releaseFirst)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent idempotency transaction: %v", err)
		}
	}
}

func applyOrderTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	usersDown := readMigration(t, "../../migrations/000001_create_users.down.sql")
	booksDown := readMigration(t, "../../migrations/000002_create_books.down.sql")
	favoritesDown := readMigration(t, "../../migrations/000003_create_favorites.down.sql")
	ordersDown := readMigration(t, "../../migrations/000004_create_orders.down.sql")

	for _, migration := range []string{ordersDown, favoritesDown} {
		if err := db.Exec(migration).Error; err != nil {
			t.Fatalf("remove dependent schema: %v", err)
		}
	}
	if err := execIntegrationMigration(db, booksDown); err != nil {
		t.Fatalf("remove book schema: %v", err)
	}
	if err := db.Exec(usersDown).Error; err != nil {
		t.Fatalf("remove user schema: %v", err)
	}
	t.Cleanup(func() {
		if cleanupErr := db.Exec(ordersDown).Error; cleanupErr != nil {
			t.Errorf("remove order schema: %v", cleanupErr)
		}
		if cleanupErr := db.Exec(favoritesDown).Error; cleanupErr != nil {
			t.Errorf("remove favorite schema: %v", cleanupErr)
		}
		if cleanupErr := execIntegrationMigration(db, booksDown); cleanupErr != nil {
			t.Errorf("remove book schema: %v", cleanupErr)
		}
		if cleanupErr := db.Exec(usersDown).Error; cleanupErr != nil {
			t.Errorf("remove user schema: %v", cleanupErr)
		}
	})

	if err := db.Exec(readMigration(t, "../../migrations/000001_create_users.up.sql")).Error; err != nil {
		t.Fatalf("apply users migration: %v", err)
	}
	if err := execIntegrationMigration(db, readMigration(t, "../../migrations/000002_create_books.up.sql")); err != nil {
		t.Fatalf("apply books migration: %v", err)
	}
	if err := db.Exec(readMigration(t, "../../migrations/000003_create_favorites.up.sql")).Error; err != nil {
		t.Fatalf("apply favorites migration: %v", err)
	}
	if err := db.Exec(readMigration(t, "../../migrations/000004_create_orders.up.sql")).Error; err != nil {
		t.Fatalf("apply orders migration: %v", err)
	}
}

func assertOrderSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	var columnCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'orders'`).Scan(&columnCount).Error; err != nil {
		t.Fatalf("count orders columns: %v", err)
	}
	if columnCount != 18 {
		t.Fatalf("orders column count = %d, want 18", columnCount)
	}
	var tableName, createSQL string
	if err := db.Raw("SHOW CREATE TABLE orders").Row().Scan(&tableName, &createSQL); err != nil {
		t.Fatalf("SHOW CREATE TABLE orders: %v", err)
	}
	for _, fragment := range []string{
		"active_book_id", "GENERATED ALWAYS AS", "ux_orders_order_no", "ux_orders_buyer_idempotency",
		"ux_orders_active_book", "chk_orders_distinct_participants", "chk_orders_status",
		"ix_orders_status_expires", "fk_orders_book", "fk_orders_buyer", "fk_orders_seller",
	} {
		if !strings.Contains(createSQL, fragment) {
			t.Fatalf("orders schema does not contain %q:\n%s", fragment, createSQL)
		}
	}
}

func createOrderForIntegration(
	t *testing.T,
	orders *OrderRepository,
	book *model.Book,
	buyerID uint64,
	key string,
	sequence int,
) *model.Order {
	t.Helper()
	var created *model.Order
	err := orders.WithinTransaction(context.Background(), func(store OrderStore) error {
		lockedBook, err := store.FindBookByIDForUpdate(context.Background(), book.ID)
		if err != nil {
			return err
		}
		if lockedBook.Status != model.BookStatusAvailable {
			return fmt.Errorf("book status is %s", lockedBook.Status)
		}
		now, err := store.CurrentTime(context.Background())
		if err != nil {
			return err
		}
		created = &model.Order{
			OrderNo:            fmt.Sprintf("%026d", sequence),
			BookID:             lockedBook.ID,
			BuyerID:            buyerID,
			SellerID:           lockedBook.SellerID,
			BookTitle:          lockedBook.Title,
			BookPrice:          lockedBook.Price,
			Status:             model.OrderStatusPendingPayment,
			IdempotencyKey:     key,
			RequestFingerprint: fmt.Sprintf("%064d", sequence),
			ExpiresAt:          now.Add(15 * time.Minute),
		}
		if err := store.Create(context.Background(), created); err != nil {
			return err
		}
		return store.UpdateBookStatus(context.Background(), book.ID, model.BookStatusAvailable, model.BookStatusReserved)
	})
	if err != nil {
		t.Fatalf("create integration order: %v", err)
	}
	return created
}
