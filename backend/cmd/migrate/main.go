package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/storage"
)

const (
	databaseWaitTimeout = 60 * time.Second
	migrationLockName   = "bookloop_schema_migrations"
	migrationLockWait   = 30
)

var migrationFilename = regexp.MustCompile(`^(\d{6})_[a-z0-9_]+\.up\.sql$`)

type migration struct {
	version  uint64
	name     string
	path     string
	checksum string
}

type appliedMigration struct {
	name     string
	checksum string
}

func main() {
	directory := flag.String("dir", "/app/migrations", "directory containing .up.sql migrations")
	flag.Parse()

	logger := log.New(os.Stdout, "migration ", log.LstdFlags|log.LUTC)
	if err := run(*directory, logger); err != nil {
		logger.Printf("failed: %v", err)
		os.Exit(1)
	}
}

func run(directory string, logger *log.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	migrations, err := discoverMigrations(directory)
	if err != nil {
		return err
	}

	database, err := sql.Open("mysql", storage.MySQLDSN(cfg.DB, true))
	if err != nil {
		return fmt.Errorf("open MySQL migration connection: %w", err)
	}
	defer func() {
		if closeErr := database.Close(); closeErr != nil {
			logger.Printf("close database connection: %v", closeErr)
		}
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	waitContext, cancelWait := context.WithTimeout(signalContext, databaseWaitTimeout)
	err = waitForDatabase(waitContext, database, time.Second)
	cancelWait()
	if err != nil {
		return err
	}

	locked, err := acquireMigrationLock(signalContext, database)
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("timed out waiting for migration lock")
	}
	defer releaseMigrationLock(database, logger)

	if _, err := database.ExecContext(signalContext, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT UNSIGNED NOT NULL,
    name VARCHAR(255) NOT NULL,
    checksum CHAR(64) NOT NULL,
    applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := loadAppliedMigrations(signalContext, database)
	if err != nil {
		return err
	}
	for _, item := range migrations {
		if existing, ok := applied[item.version]; ok {
			if existing.name != item.name || existing.checksum != item.checksum {
				return fmt.Errorf("migration %06d changed after it was applied", item.version)
			}
			logger.Printf("already applied: %s", item.name)
			continue
		}
		contents, err := os.ReadFile(item.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", item.name, err)
		}
		if _, err := database.ExecContext(signalContext, string(contents)); err != nil {
			return fmt.Errorf("apply migration %s: %w", item.name, err)
		}
		if _, err := database.ExecContext(signalContext,
			"INSERT INTO schema_migrations (version, name, checksum) VALUES (?, ?, ?)",
			item.version, item.name, item.checksum,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", item.name, err)
		}
		logger.Printf("applied: %s", item.name)
	}
	logger.Printf("complete: %d migration files verified", len(migrations))
	return nil
}

func discoverMigrations(directory string) ([]migration, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}
	items := make([]migration, 0, len(entries))
	seen := make(map[uint64]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilename.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		version, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil || version == 0 {
			return nil, fmt.Errorf("invalid migration version in %s", entry.Name())
		}
		if previous, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %06d in %s and %s", version, previous, entry.Name())
		}
		path := filepath.Join(directory, entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		hash := sha256.Sum256(contents)
		items = append(items, migration{
			version:  version,
			name:     entry.Name(),
			path:     path,
			checksum: hex.EncodeToString(hash[:]),
		})
		seen[version] = entry.Name()
	}
	if len(items) == 0 {
		return nil, errors.New("no up migrations found")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	return items, nil
}

func waitForDatabase(ctx context.Context, database *sql.DB, interval time.Duration) error {
	var lastErr error
	for {
		if err := database.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("connect to MySQL before migration deadline: %w", errors.Join(lastErr, ctx.Err()))
		case <-timer.C:
		}
	}
}

func acquireMigrationLock(ctx context.Context, database *sql.DB) (bool, error) {
	var result sql.NullInt64
	if err := database.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", migrationLockName, migrationLockWait).Scan(&result); err != nil {
		return false, fmt.Errorf("acquire migration lock: %w", err)
	}
	return result.Valid && result.Int64 == 1, nil
}

func releaseMigrationLock(database *sql.DB, logger *log.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", migrationLockName); err != nil {
		logger.Printf("release migration lock: %v", err)
	}
}

func loadAppliedMigrations(ctx context.Context, database *sql.DB) (map[uint64]appliedMigration, error) {
	rows, err := database.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()
	result := make(map[uint64]appliedMigration)
	for rows.Next() {
		var version uint64
		var item appliedMigration
		if err := rows.Scan(&version, &item.name, &item.checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		result[version] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return result, nil
}
