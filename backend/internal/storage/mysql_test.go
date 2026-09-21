package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

func TestMySQLDriverConfigPreservesDriverDefaults(t *testing.T) {
	driverConfig := newMySQLDriverConfig(config.DatabaseConfig{
		Host:     "127.0.0.1",
		Port:     3306,
		Name:     "bookloop",
		User:     "bookloop",
		Password: "password",
	})

	if !driverConfig.AllowNativePasswords {
		t.Fatal("AllowNativePasswords = false, want driver's compatibility default true")
	}
	if !driverConfig.CheckConnLiveness {
		t.Fatal("CheckConnLiveness = false, want driver's default true")
	}
	if !driverConfig.ParseTime {
		t.Fatal("ParseTime = false, want true")
	}
	if driverConfig.Loc.String() != "UTC" || driverConfig.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("timezone config = %s/%q, want UTC/+00:00", driverConfig.Loc, driverConfig.Params["time_zone"])
	}
	if dsn := MySQLDSN(config.DatabaseConfig{
		Host: "mysql", Port: 3306, Name: "bookloop", User: "bookloop", Password: "password",
	}, true); dsn == "" || !strings.Contains(dsn, "multiStatements=true") {
		t.Fatalf("MySQLDSN(multiStatements=true) = %q, want migration support", dsn)
	}
}

func TestOpenMySQLWithRetryRejectsInvalidInterval(t *testing.T) {
	if _, err := OpenMySQLWithRetry(context.Background(), config.DatabaseConfig{}, 0); err == nil {
		t.Fatal("OpenMySQLWithRetry(interval=0) error = nil, want validation error")
	}
}
