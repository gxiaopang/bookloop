package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

type MySQL struct {
	DB    *gorm.DB
	sqlDB *sql.DB
}

const mysqlConnectionAttemptTimeout = 3 * time.Second

func OpenMySQL(ctx context.Context, cfg config.DatabaseConfig) (*MySQL, error) {
	driverConfig := newMySQLDriverConfig(cfg)

	db, err := gorm.Open(gormmysql.New(gormmysql.Config{
		DSN:                       driverConfig.FormatDSN(),
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger: gormlogger.New(
			log.New(os.Stdout, "", log.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
				ParameterizedQueries:      true,
			},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("open MySQL connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying MySQL connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	if err := sqlDB.PingContext(ctx); err != nil {
		closeErr := sqlDB.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("ping MySQL: %w (also failed to close connection pool: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}

	return &MySQL{DB: db, sqlDB: sqlDB}, nil
}

func OpenMySQLWithRetry(
	ctx context.Context,
	cfg config.DatabaseConfig,
	retryInterval time.Duration,
) (*MySQL, error) {
	if retryInterval <= 0 {
		return nil, errors.New("MySQL retry interval must be greater than zero")
	}
	var lastErr error
	for {
		attemptContext, cancelAttempt := context.WithTimeout(ctx, mysqlConnectionAttemptTimeout)
		store, err := OpenMySQL(attemptContext, cfg)
		cancelAttempt()
		if err == nil {
			return store, nil
		}
		lastErr = err
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("connect to MySQL before startup deadline: %w", errors.Join(lastErr, ctx.Err()))
		case <-timer.C:
		}
	}
}

func MySQLDSN(cfg config.DatabaseConfig, multiStatements bool) string {
	driverConfig := newMySQLDriverConfig(cfg)
	driverConfig.MultiStatements = multiStatements
	return driverConfig.FormatDSN()
}

func newMySQLDriverConfig(cfg config.DatabaseConfig) *drivermysql.Config {
	driverConfig := drivermysql.NewConfig()
	driverConfig.User = cfg.User
	driverConfig.Passwd = cfg.Password
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	driverConfig.DBName = cfg.Name
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Params = map[string]string{
		"charset":   "utf8mb4",
		"time_zone": "'+00:00'",
	}
	return driverConfig
}

func (m *MySQL) Ping(ctx context.Context) error {
	return m.sqlDB.PingContext(ctx)
}

func (m *MySQL) Close() error {
	return m.sqlDB.Close()
}
