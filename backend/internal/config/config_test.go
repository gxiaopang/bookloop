package config

import (
	"strings"
	"testing"
	"time"
)

var requiredEnvironment = map[string]string{
	"APP_ENV":                    "development",
	"HTTP_ADDR":                  ":8080",
	"TRUSTED_PROXIES":            "127.0.0.1,10.0.0.0/8",
	"LOG_LEVEL":                  "debug",
	"DB_HOST":                    "127.0.0.1",
	"DB_PORT":                    "3306",
	"DB_NAME":                    "bookloop",
	"DB_USER":                    "bookloop",
	"DB_PASSWORD":                "local-password",
	"DB_MAX_OPEN_CONNS":          "25",
	"DB_MAX_IDLE_CONNS":          "10",
	"REDIS_ADDR":                 "127.0.0.1:6379",
	"REDIS_PASSWORD":             "",
	"REDIS_DB":                   "0",
	"RABBITMQ_HOST":              "127.0.0.1",
	"RABBITMQ_PORT":              "5672",
	"RABBITMQ_USER":              "bookloop",
	"RABBITMQ_PASSWORD":          "local-password",
	"RABBITMQ_VHOST":             "/",
	"JWT_SECRET":                 "development-secret",
	"JWT_ISSUER":                 "bookloop",
	"JWT_EXPIRES_IN":             "15m",
	"BCRYPT_COST":                "12",
	"UPLOAD_DIR":                 "./uploads",
	"UPLOAD_PUBLIC_PREFIX":       "/uploads/",
	"UPLOAD_MAX_BYTES":           "10485760",
	"ORDER_PAYMENT_TIMEOUT":      "15m",
	"IDEMPOTENCY_PROCESSING_TTL": "30s",
	"IDEMPOTENCY_RESULT_TTL":     "24h",
	"IDEMPOTENCY_ALLOW_DEGRADED": "true",
}

func TestLoad(t *testing.T) {
	setValidEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DB.Port != 3306 {
		t.Fatalf("DB.Port = %d, want 3306", cfg.DB.Port)
	}
	if len(cfg.HTTP.TrustedProxies) != 2 || cfg.HTTP.TrustedProxies[0] != "127.0.0.1" {
		t.Fatalf("HTTP.TrustedProxies = %+v, want parsed proxy list", cfg.HTTP.TrustedProxies)
	}
	if cfg.JWT.ExpiresIn != 15*time.Minute {
		t.Fatalf("JWT.ExpiresIn = %s, want 15m", cfg.JWT.ExpiresIn)
	}
	if cfg.RabbitMQ.Port != 5672 || cfg.RabbitMQ.VHost != "/" {
		t.Fatalf("RabbitMQ config = %+v, want port 5672 and root vhost", cfg.RabbitMQ)
	}
	if cfg.Upload.MaxBytes != 10*1024*1024 {
		t.Fatalf("Upload.MaxBytes = %d, want %d", cfg.Upload.MaxBytes, 10*1024*1024)
	}
	if cfg.Order.PaymentTimeout != 15*time.Minute {
		t.Fatalf("Order.PaymentTimeout = %s, want 15m", cfg.Order.PaymentTimeout)
	}
	if cfg.Idempotency.ProcessingTTL != 30*time.Second || cfg.Idempotency.ResultTTL != 24*time.Hour ||
		!cfg.Idempotency.AllowDegraded {
		t.Fatalf("Idempotency config = %+v, want 30s/24h/degraded", cfg.Idempotency)
	}
}

func TestLoadReportsAllMissingRequiredVariables(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("DB_HOST", "")
	t.Setenv("JWT_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "DB_HOST is required") {
		t.Fatalf("Load() error = %q, want DB_HOST error", err)
	}
	if !strings.Contains(err.Error(), "JWT_SECRET is required") {
		t.Fatalf("Load() error = %q, want JWT_SECRET error", err)
	}
}

func TestLoadRejectsInvalidConnectionPool(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "6")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DB_MAX_IDLE_CONNS must not exceed") {
		t.Fatalf("Load() error = %v, want connection pool validation error", err)
	}
}

func TestLoadRejectsInvalidTrustedProxy(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("TRUSTED_PROXIES", "not-an-ip")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES contains invalid") {
		t.Fatalf("Load() error = %v, want trusted proxy validation error", err)
	}
}

func TestLoadRequiresStrongProductionJWTSecret(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "too-short")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("Load() error = %v, want production JWT secret validation error", err)
	}
}

func TestLoadUsesDefaultBcryptCost(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("BCRYPT_COST", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BcryptCost != 12 {
		t.Fatalf("BcryptCost = %d, want 12", cfg.BcryptCost)
	}
}

func TestLoadRejectsInvalidBcryptCost(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("BCRYPT_COST", "32")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BCRYPT_COST must be between 4 and 31") {
		t.Fatalf("Load() error = %v, want bcrypt cost validation error", err)
	}
}

func TestLoadRejectsInvalidOrderPaymentTimeout(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("ORDER_PAYMENT_TIMEOUT", "0s")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ORDER_PAYMENT_TIMEOUT must be greater than zero") {
		t.Fatalf("Load() error = %v, want order timeout validation error", err)
	}
}

func TestLoadRejectsInvalidIdempotencyConfiguration(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("IDEMPOTENCY_PROCESSING_TTL", "2m")
	t.Setenv("IDEMPOTENCY_RESULT_TTL", "1m")
	t.Setenv("IDEMPOTENCY_ALLOW_DEGRADED", "not-a-bool")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "IDEMPOTENCY_RESULT_TTL must be greater") ||
		!strings.Contains(err.Error(), "IDEMPOTENCY_ALLOW_DEGRADED must be a boolean") {
		t.Fatalf("Load() error = %v, want idempotency configuration errors", err)
	}
}

func setValidEnvironment(t *testing.T) {
	t.Helper()
	for name, value := range requiredEnvironment {
		t.Setenv(name, value)
	}
}
