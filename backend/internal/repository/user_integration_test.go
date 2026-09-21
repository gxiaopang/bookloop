package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

const mysqlTestDSNEnvironment = "BOOKLOOP_TEST_MYSQL_DSN"

func TestUserMigrationAndRepositoryWithMySQL8(t *testing.T) {
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

	downMigration := readMigration(t, "../../migrations/000001_create_users.down.sql")
	upMigration := readMigration(t, "../../migrations/000001_create_users.up.sql")
	if err := db.Exec(downMigration).Error; err != nil {
		t.Fatalf("prepare clean schema: %v", err)
	}
	t.Cleanup(func() {
		if cleanupErr := db.Exec(downMigration).Error; cleanupErr != nil {
			t.Errorf("remove users table: %v", cleanupErr)
		}
	})
	if err := db.Exec(upMigration).Error; err != nil {
		t.Fatalf("apply users migration: %v", err)
	}

	assertUserSchema(t, db)
	assertUserDefaults(t, db)
	repository := NewUserRepository(db)
	ctx := context.Background()
	user := &model.User{
		Username:     "reader_one",
		Email:        "reader.one@example.com",
		PasswordHash: "$2a$04$integration-test-placeholder-hash",
		Role:         model.UserRoleUser,
		Status:       model.UserStatusActive,
	}
	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if user.ID == 0 || user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
		t.Fatalf("Create() user = %+v, want database identity and timestamps", user)
	}

	findAssertions := []struct {
		name string
		find func() (*model.User, error)
	}{
		{name: "id", find: func() (*model.User, error) { return repository.FindByID(ctx, user.ID) }},
		{name: "username", find: func() (*model.User, error) { return repository.FindByUsername(ctx, user.Username) }},
		{name: "email", find: func() (*model.User, error) { return repository.FindByEmail(ctx, user.Email) }},
	}
	for _, assertion := range findAssertions {
		t.Run("find_by_"+assertion.name, func(t *testing.T) {
			found, err := assertion.find()
			if err != nil || found.ID != user.ID {
				t.Fatalf("find = %+v, %v, want user %d", found, err, user.ID)
			}
		})
	}

	duplicateUsername := &model.User{
		Username:     user.Username,
		Email:        "other@example.com",
		PasswordHash: user.PasswordHash,
		Role:         model.UserRoleUser,
		Status:       model.UserStatusActive,
	}
	if err := repository.Create(ctx, duplicateUsername); !errors.Is(err, ErrUsernameAlreadyExists) {
		t.Fatalf("Create(duplicate username) error = %v, want ErrUsernameAlreadyExists", err)
	}
	duplicateEmail := &model.User{
		Username:     "reader_two",
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         model.UserRoleUser,
		Status:       model.UserStatusActive,
	}
	if err := repository.Create(ctx, duplicateEmail); !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("Create(duplicate email) error = %v, want ErrEmailAlreadyExists", err)
	}

	assertConcurrentUsernameConflict(t, repository)
}

func assertUserDefaults(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Exec(
		"INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)",
		"default_reader",
		"default.reader@example.com",
		"$2a$04$integration-test-placeholder-hash",
	)
	if result.Error != nil {
		t.Fatalf("insert user with database defaults: %v", result.Error)
	}
	var user model.User
	if err := db.Where("username = ?", "default_reader").Take(&user).Error; err != nil {
		t.Fatalf("read user with database defaults: %v", err)
	}
	if user.Role != model.UserRoleUser || user.Status != model.UserStatusActive {
		t.Fatalf("database defaults role/status = %q/%q, want user/active", user.Role, user.Status)
	}
}

func assertUserSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	var (
		tableName string
		createSQL string
	)
	if err := db.Raw("SHOW CREATE TABLE users").Row().Scan(&tableName, &createSQL); err != nil {
		t.Fatalf("SHOW CREATE TABLE users: %v", err)
	}
	for _, fragment := range []string{
		"ENGINE=InnoDB",
		"CHARSET=utf8mb4",
		"`ux_users_username`",
		"`ux_users_email`",
		"CONSTRAINT `chk_users_role` CHECK",
		"CONSTRAINT `chk_users_status` CHECK",
	} {
		if !strings.Contains(createSQL, fragment) {
			t.Fatalf("users schema does not contain %q:\n%s", fragment, createSQL)
		}
	}
}

func assertConcurrentUsernameConflict(t *testing.T, repository *UserRepository) {
	t.Helper()
	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for _, email := range []string{"race.one@example.com", "race.two@example.com"} {
		waitGroup.Add(1)
		go func(email string) {
			defer waitGroup.Done()
			<-start
			results <- repository.Create(context.Background(), &model.User{
				Username:     "race_reader",
				Email:        email,
				PasswordHash: "$2a$04$integration-test-placeholder-hash",
				Role:         model.UserRoleUser,
				Status:       model.UserStatusActive,
			})
		}(email)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrUsernameAlreadyExists):
			conflicts++
		default:
			t.Fatalf("concurrent Create() unexpected error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent Create() successes/conflicts = %d/%d, want 1/1", successes, conflicts)
	}
}

func readMigration(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	return string(contents)
}
