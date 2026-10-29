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
	gormlogger "gorm.io/gorm/logger"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
)

func TestBookMigrationAndRepositoryWithMySQL8(t *testing.T) {
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
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open test MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test MySQL pool: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test MySQL: %v", closeErr)
		}
	})

	usersDown := readMigration(t, "../../migrations/000001_create_users.down.sql")
	booksDown := readMigration(t, "../../migrations/000002_create_books.down.sql")
	if err := execIntegrationMigration(db, booksDown); err != nil {
		t.Fatalf("remove previous book schema: %v", err)
	}
	if err := db.Exec(usersDown).Error; err != nil {
		t.Fatalf("remove previous user schema: %v", err)
	}
	t.Cleanup(func() {
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
	legacySellerID := createIntegrationUser(t, db, "legacy_seller", "legacy@example.com", model.UserStatusActive)
	if err := db.Exec("INSERT INTO books (seller_id, title, price, `condition`, status) VALUES (?, 'Legacy listing', 10.00, 'good', 'available')", legacySellerID).Error; err != nil {
		t.Fatalf("create legacy listing: %v", err)
	}
	if err := execIntegrationMigration(db, readMigration(t, "../../migrations/000006_require_wechat_contact.up.sql")); err != nil {
		t.Fatalf("apply contact migration: %v", err)
	}
	var legacy model.Book
	if err := db.Where("seller_id = ?", legacySellerID).Take(&legacy).Error; err != nil || legacy.Status != model.BookStatusOffShelf || legacy.WechatQRCodeURL != nil {
		t.Fatalf("migrated legacy listing = %+v/%v, want off-shelf without contact", legacy, err)
	}

	assertBookSchema(t, db)
	activeSellerID := createIntegrationUser(t, db, "active_seller", "active@example.com", model.UserStatusActive)
	disabledSellerID := createIntegrationUser(t, db, "disabled_seller", "disabled@example.com", model.UserStatusDisabled)
	books := NewBookRepository(db)
	percentBook := createIntegrationBook(t, books, activeSellerID, "100% Go", "20.00", model.BookConditionGood, model.BookStatusAvailable)
	if err := books.UpdateWechatQRCode(context.Background(), percentBook.ID, "/uploads/books/wechat.png"); err != nil {
		t.Fatalf("save contact: %v", err)
	}
	contactBook, err := books.FindByID(context.Background(), percentBook.ID)
	if err != nil || contactBook.WechatQRCodeURL == nil || *contactBook.WechatQRCodeURL != "/uploads/books/wechat.png" {
		t.Fatalf("reload contact = %+v/%v", contactBook, err)
	}
	cheaperBook := createIntegrationBook(t, books, activeSellerID, "100X Go", "10.00", model.BookConditionFair, model.BookStatusAvailable)
	samePriceBook := createIntegrationBook(t, books, activeSellerID, "Algorithms", "10.00", model.BookConditionGood, model.BookStatusAvailable)
	createIntegrationBook(t, books, activeSellerID, "Hidden Book", "5.00", model.BookConditionGood, model.BookStatusOffShelf)
	disabledSellerBook := createIntegrationBook(t, books, disabledSellerID, "Disabled Seller Book", "1.00", model.BookConditionGood, model.BookStatusAvailable)

	secondImage := &model.BookImage{BookID: percentBook.ID, ImageURL: "/uploads/books/second.png", SortOrder: 2}
	if err := books.CreateImage(context.Background(), secondImage); err != nil {
		t.Fatalf("create second image: %v", err)
	}
	cover := &model.BookImage{BookID: percentBook.ID, ImageURL: "/uploads/books/cover.png", SortOrder: 1}
	if err := books.CreateImage(context.Background(), cover); err != nil {
		t.Fatalf("create cover image: %v", err)
	}
	if err := books.CreateImage(context.Background(), &model.BookImage{
		BookID: cheaperBook.ID, ImageURL: "/uploads/books/cheap.png", SortOrder: 1,
	}); err != nil {
		t.Fatalf("create cheaper book image: %v", err)
	}
	if err := books.CreateImage(context.Background(), &model.BookImage{
		BookID: samePriceBook.ID, ImageURL: "/uploads/books/algorithm.png", SortOrder: 1,
	}); err != nil {
		t.Fatalf("create same-price book image: %v", err)
	}

	publicBooks, total, err := books.ListPublic(context.Background(), dto.BookListQuery{
		Sort: "price_asc", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListPublic() error = %v", err)
	}
	if total != 3 || len(publicBooks) != 3 || publicBooks[0].ID != cheaperBook.ID ||
		publicBooks[1].ID != samePriceBook.ID || publicBooks[2].ID != percentBook.ID {
		t.Fatalf("ListPublic() total/items = %d/%+v, want active-seller available books with stable price order", total, publicBooks)
	}
	if publicBooks[2].CoverImageURL == nil || *publicBooks[2].CoverImageURL != cover.ImageURL {
		t.Fatalf("cover = %v, want %q", publicBooks[2].CoverImageURL, cover.ImageURL)
	}
	if _, _, err := books.FindPublicByID(context.Background(), disabledSellerBook.ID); !errors.Is(err, ErrBookNotFound) {
		t.Fatalf("FindPublicByID(disabled seller) error = %v, want ErrBookNotFound", err)
	}
	goodBooks, goodTotal, err := books.ListPublic(context.Background(), dto.BookListQuery{
		Condition: "good", Sort: "newest", Page: 1, PageSize: 20,
	})
	if err != nil || goodTotal != 2 || len(goodBooks) != 2 {
		t.Fatalf("condition filter total/items/error = %d/%+v/%v, want two good books", goodTotal, goodBooks, err)
	}
	secondPricePage, _, err := books.ListPublic(context.Background(), dto.BookListQuery{
		Sort: "price_asc", Page: 2, PageSize: 1,
	})
	if err != nil || len(secondPricePage) != 1 || secondPricePage[0].ID != samePriceBook.ID {
		t.Fatalf("stable second price page = %+v/%v, want same-price book by ID", secondPricePage, err)
	}

	literalPercent, total, err := books.ListPublic(context.Background(), dto.BookListQuery{
		Keyword: "100%", Sort: "newest", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListPublic(literal percent) error = %v", err)
	}
	if total != 1 || len(literalPercent) != 1 || literalPercent[0].ID != percentBook.ID {
		t.Fatalf("literal percent search = %d/%+v, want only percent title", total, literalPercent)
	}
	if err := db.Model(&model.User{}).Where("id = ?", disabledSellerID).
		Update("status", model.UserStatusActive).Error; err != nil {
		t.Fatalf("reactivate seller: %v", err)
	}
	restoredBooks, restoredTotal, err := books.ListPublic(context.Background(), dto.BookListQuery{
		Keyword: "Disabled Seller", Sort: "newest", Page: 1, PageSize: 20,
	})
	if err != nil || restoredTotal != 1 || len(restoredBooks) != 1 || restoredBooks[0].ID != disabledSellerBook.ID {
		t.Fatalf("restored seller books = %d/%+v/%v, want automatically visible book", restoredTotal, restoredBooks, err)
	}

	_, images, err := books.FindPublicByID(context.Background(), percentBook.ID)
	if err != nil {
		t.Fatalf("FindPublicByID() error = %v", err)
	}
	if len(images) != 2 || images[0].SortOrder != 1 || images[0].ID != cover.ID {
		t.Fatalf("detail images = %+v, want sort_order ascending", images)
	}
	if err := books.WithinTransaction(context.Background(), func(store BookStore) error {
		if _, err := store.FindByIDForUpdate(context.Background(), percentBook.ID); err != nil {
			return err
		}
		return store.ReplaceImageOrder(context.Background(), percentBook.ID, []uint64{secondImage.ID, cover.ID})
	}); err != nil {
		t.Fatalf("ReplaceImageOrder() error = %v", err)
	}
	reordered, err := books.ListImages(context.Background(), percentBook.ID)
	if err != nil || len(reordered) != 2 || reordered[0].ID != secondImage.ID || reordered[1].ID != cover.ID {
		t.Fatalf("reordered images/error = %+v/%v, want reversed image order", reordered, err)
	}

	duplicateSort := &model.BookImage{
		BookID: percentBook.ID, ImageURL: "/uploads/books/duplicate.png", SortOrder: 1,
	}
	if err := books.CreateImage(context.Background(), duplicateSort); err == nil {
		t.Fatal("CreateImage(duplicate sort order) error = nil, want unique-key error")
	}
	if err := db.Delete(&model.User{}, activeSellerID).Error; err == nil {
		t.Fatal("delete seller with books error = nil, want RESTRICT foreign-key error")
	}

	assertConcurrentImageLimitWithMySQL(t, books, activeSellerID)
}

func execIntegrationMigration(db *gorm.DB, migration string) error {
	for _, statement := range strings.Split(migration, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func assertBookSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for table, expectedColumns := range map[string]int64{"books": 10, "book_images": 5} {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
            WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count).Error; err != nil {
			t.Fatalf("count %s columns: %v", table, err)
		}
		if count != expectedColumns {
			t.Fatalf("%s column count = %d, want %d", table, count, expectedColumns)
		}
	}
	var tableName, createSQL string
	if err := db.Raw("SHOW CREATE TABLE books").Row().Scan(&tableName, &createSQL); err != nil {
		t.Fatalf("SHOW CREATE TABLE books: %v", err)
	}
	for _, fragment := range []string{
		"ENGINE=InnoDB", "CHARSET=utf8mb4", "fk_books_seller", "chk_books_price",
		"chk_books_condition", "chk_books_status", "ix_books_status_created_id",
		"ix_books_status_price_id", "ix_books_seller_status_created",
	} {
		if !strings.Contains(createSQL, fragment) {
			t.Fatalf("books schema does not contain %q:\n%s", fragment, createSQL)
		}
	}
	if err := db.Raw("SHOW CREATE TABLE book_images").Row().Scan(&tableName, &createSQL); err != nil {
		t.Fatalf("SHOW CREATE TABLE book_images: %v", err)
	}
	for _, fragment := range []string{"fk_book_images_book", "chk_book_images_sort_order", "ux_book_images_book_sort", "ON DELETE CASCADE"} {
		if !strings.Contains(createSQL, fragment) {
			t.Fatalf("book_images schema does not contain %q:\n%s", fragment, createSQL)
		}
	}
}

func createIntegrationUser(t *testing.T, db *gorm.DB, username, email string, status model.UserStatus) uint64 {
	t.Helper()
	user := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: "$2a$04$integration-test-placeholder-hash",
		Role:         model.UserRoleUser,
		Status:       status,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create integration user: %v", err)
	}
	return user.ID
}

func createIntegrationBook(
	t *testing.T,
	repository *BookRepository,
	sellerID uint64,
	title, price string,
	condition model.BookCondition,
	status model.BookStatus,
) *model.Book {
	t.Helper()
	book := &model.Book{
		SellerID: sellerID, Title: title, Price: price, Condition: condition, Status: status,
	}
	if err := repository.Create(context.Background(), book); err != nil {
		t.Fatalf("create integration book: %v", err)
	}
	return book
}

func assertConcurrentImageLimitWithMySQL(t *testing.T, books *BookRepository, sellerID uint64) {
	t.Helper()
	book := createIntegrationBook(t, books, sellerID, "Concurrent Images", "8.00", model.BookConditionGood, model.BookStatusOffShelf)
	for index := 1; index <= 5; index++ {
		if err := books.CreateImage(context.Background(), &model.BookImage{
			BookID: book.ID, ImageURL: fmt.Sprintf("/uploads/books/preexisting-%d.png", index), SortOrder: uint16(index),
		}); err != nil {
			t.Fatalf("create preexisting image: %v", err)
		}
	}

	errImageLimit := errors.New("test image limit reached")
	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for index := 1; index <= 2; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			results <- books.WithinTransaction(context.Background(), func(store BookStore) error {
				if _, err := store.FindByIDForUpdate(context.Background(), book.ID); err != nil {
					return err
				}
				images, err := store.ListImages(context.Background(), book.ID)
				if err != nil {
					return err
				}
				if len(images) >= 6 {
					return errImageLimit
				}
				time.Sleep(25 * time.Millisecond)
				return store.CreateImage(context.Background(), &model.BookImage{
					BookID: book.ID, ImageURL: fmt.Sprintf("/uploads/books/concurrent-%d.png", index), SortOrder: uint16(len(images) + 1),
				})
			})
		}(index)
	}
	close(start)
	waitGroup.Wait()
	close(results)
	successes := 0
	limits := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errImageLimit):
			limits++
		default:
			t.Fatalf("concurrent image transaction unexpected error = %v", err)
		}
	}
	images, err := books.ListImages(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("list concurrent images: %v", err)
	}
	if successes != 1 || limits != 1 || len(images) != 6 {
		t.Fatalf("concurrent image results success/limit/count = %d/%d/%d, want 1/1/6", successes, limits, len(images))
	}
}
