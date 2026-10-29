package repository

import (
	"context"
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

func TestFavoriteMigrationAndRepositoryWithMySQL8(t *testing.T) {
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
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test MySQL: %v", closeErr)
		}
	})

	usersDown := readMigration(t, "../../migrations/000001_create_users.down.sql")
	booksDown := readMigration(t, "../../migrations/000002_create_books.down.sql")
	favoritesDown := readMigration(t, "../../migrations/000003_create_favorites.down.sql")
	if err := db.Exec(favoritesDown).Error; err != nil {
		t.Fatalf("remove previous favorite schema: %v", err)
	}
	if err := execIntegrationMigration(db, booksDown); err != nil {
		t.Fatalf("remove previous book schema: %v", err)
	}
	if err := db.Exec(usersDown).Error; err != nil {
		t.Fatalf("remove previous user schema: %v", err)
	}
	t.Cleanup(func() {
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
	if err := execIntegrationMigration(db, readMigration(t, "../../migrations/000006_require_wechat_contact.up.sql")); err != nil {
		t.Fatalf("apply contact migration: %v", err)
	}
	if err := db.Exec(readMigration(t, "../../migrations/000003_create_favorites.up.sql")).Error; err != nil {
		t.Fatalf("apply favorites migration: %v", err)
	}

	assertFavoriteSchema(t, db)
	sellerID := createIntegrationUser(t, db, "favorite_seller", "favorite-seller@example.com", model.UserStatusActive)
	buyerID := createIntegrationUser(t, db, "favorite_buyer", "favorite-buyer@example.com", model.UserStatusActive)
	otherBuyerID := createIntegrationUser(t, db, "favorite_other", "favorite-other@example.com", model.UserStatusActive)
	books := NewBookRepository(db)
	favorites := NewFavoriteRepository(db)

	statuses := []model.BookStatus{
		model.BookStatusAvailable,
		model.BookStatusOffShelf,
		model.BookStatusBlocked,
		model.BookStatusSold,
	}
	createdBooks := make([]*model.Book, 0, len(statuses))
	for _, status := range statuses {
		book := createIntegrationBook(t, books, sellerID, "Favorite "+string(status), "18.00", model.BookConditionGood, status)
		createdBooks = append(createdBooks, book)
		if err := favorites.Add(context.Background(), &model.Favorite{UserID: buyerID, BookID: book.ID}); err != nil {
			t.Fatalf("add %s favorite: %v", status, err)
		}
	}
	if err := favorites.Add(context.Background(), &model.Favorite{UserID: buyerID, BookID: createdBooks[0].ID}); err != nil {
		t.Fatalf("add duplicate favorite: %v", err)
	}
	if err := favorites.Add(context.Background(), &model.Favorite{UserID: otherBuyerID, BookID: createdBooks[0].ID}); err != nil {
		t.Fatalf("add other user's favorite: %v", err)
	}

	var buyerFavoriteCount int64
	if err := db.Model(&model.Favorite{}).Where("user_id = ?", buyerID).Count(&buyerFavoriteCount).Error; err != nil {
		t.Fatalf("count buyer favorites: %v", err)
	}
	if buyerFavoriteCount != 4 {
		t.Fatalf("buyer favorite count = %d, want 4 after duplicate add", buyerFavoriteCount)
	}

	records, total, err := favorites.ListByUser(context.Background(), buyerID, 1, 20)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if total != 4 || len(records) != 4 {
		t.Fatalf("ListByUser() total/items = %d/%d, want 4/4", total, len(records))
	}
	availability := make(map[model.BookStatus]bool, len(records))
	for _, record := range records {
		availability[record.Status] = record.IsAvailable
	}
	if !availability[model.BookStatusAvailable] {
		t.Fatal("available favorite is_available = false, want true")
	}
	for _, status := range []model.BookStatus{model.BookStatusOffShelf, model.BookStatusBlocked, model.BookStatusSold} {
		if availability[status] {
			t.Fatalf("%s favorite is_available = true, want false", status)
		}
	}

	for range 2 {
		if err := favorites.Remove(context.Background(), buyerID, createdBooks[0].ID); err != nil {
			t.Fatalf("idempotent Remove() error = %v", err)
		}
	}
	if err := db.Model(&model.Favorite{}).Where("user_id = ?", otherBuyerID).Count(&buyerFavoriteCount).Error; err != nil {
		t.Fatalf("count other user's favorites: %v", err)
	}
	if buyerFavoriteCount != 1 {
		t.Fatalf("other user's favorite count = %d, want 1 after buyer removal", buyerFavoriteCount)
	}
}

func assertFavoriteSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	var columnCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'favorites'`).Scan(&columnCount).Error; err != nil {
		t.Fatalf("count favorites columns: %v", err)
	}
	if columnCount != 3 {
		t.Fatalf("favorites column count = %d, want 3", columnCount)
	}
	var tableName, createSQL string
	if err := db.Raw("SHOW CREATE TABLE favorites").Row().Scan(&tableName, &createSQL); err != nil {
		t.Fatalf("SHOW CREATE TABLE favorites: %v", err)
	}
	for _, fragment := range []string{
		"PRIMARY KEY (`user_id`,`book_id`)", "fk_favorites_user", "fk_favorites_book",
		"ix_favorites_book", "ON DELETE CASCADE",
	} {
		if !strings.Contains(createSQL, fragment) {
			t.Fatalf("favorites schema does not contain %q:\n%s", fragment, createSQL)
		}
	}
}
