package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv      string
	HTTP        HTTPConfig
	Log         LogConfig
	DB          DatabaseConfig
	Redis       RedisConfig
	RabbitMQ    RabbitMQConfig
	JWT         JWTConfig
	BcryptCost  int
	Upload      UploadConfig
	Order       OrderConfig
	Idempotency IdempotencyConfig
}

type HTTPConfig struct {
	Address        string
	TrustedProxies []string
}

type LogConfig struct {
	Level string
}

type DatabaseConfig struct {
	Host         string
	Port         int
	Name         string
	User         string
	Password     string
	MaxOpenConns int
	MaxIdleConns int
}

type RedisConfig struct {
	Address  string
	Password string
	DB       int
}

type RabbitMQConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	VHost    string
}

type JWTConfig struct {
	Secret    string
	Issuer    string
	ExpiresIn time.Duration
}

type UploadConfig struct {
	Directory    string
	PublicPrefix string
	MaxBytes     int64
}

type OrderConfig struct {
	PaymentTimeout time.Duration
}

type IdempotencyConfig struct {
	ProcessingTTL time.Duration
	ResultTTL     time.Duration
	AllowDegraded bool
}

func Load() (Config, error) {
	var cfg Config
	var validationErrors []error

	cfg.AppEnv = required("APP_ENV", &validationErrors)
	cfg.HTTP.Address = required("HTTP_ADDR", &validationErrors)
	cfg.HTTP.TrustedProxies = requiredList("TRUSTED_PROXIES", &validationErrors)
	cfg.Log.Level = strings.ToLower(required("LOG_LEVEL", &validationErrors))

	cfg.DB.Host = required("DB_HOST", &validationErrors)
	cfg.DB.Port = requiredInt("DB_PORT", &validationErrors)
	cfg.DB.Name = required("DB_NAME", &validationErrors)
	cfg.DB.User = required("DB_USER", &validationErrors)
	cfg.DB.Password = required("DB_PASSWORD", &validationErrors)
	cfg.DB.MaxOpenConns = requiredInt("DB_MAX_OPEN_CONNS", &validationErrors)
	cfg.DB.MaxIdleConns = requiredInt("DB_MAX_IDLE_CONNS", &validationErrors)

	cfg.Redis.Address = required("REDIS_ADDR", &validationErrors)
	cfg.Redis.Password = optional("REDIS_PASSWORD")
	cfg.Redis.DB = requiredInt("REDIS_DB", &validationErrors)
	cfg.RabbitMQ.Host = required("RABBITMQ_HOST", &validationErrors)
	cfg.RabbitMQ.Port = requiredInt("RABBITMQ_PORT", &validationErrors)
	cfg.RabbitMQ.User = required("RABBITMQ_USER", &validationErrors)
	cfg.RabbitMQ.Password = required("RABBITMQ_PASSWORD", &validationErrors)
	cfg.RabbitMQ.VHost = required("RABBITMQ_VHOST", &validationErrors)

	cfg.JWT.Secret = required("JWT_SECRET", &validationErrors)
	cfg.JWT.Issuer = required("JWT_ISSUER", &validationErrors)
	cfg.JWT.ExpiresIn = requiredDuration("JWT_EXPIRES_IN", &validationErrors)
	cfg.BcryptCost = optionalInt("BCRYPT_COST", 12, &validationErrors)

	cfg.Upload.Directory = required("UPLOAD_DIR", &validationErrors)
	cfg.Upload.PublicPrefix = required("UPLOAD_PUBLIC_PREFIX", &validationErrors)
	cfg.Upload.MaxBytes = requiredInt64("UPLOAD_MAX_BYTES", &validationErrors)
	cfg.Order.PaymentTimeout = requiredDuration("ORDER_PAYMENT_TIMEOUT", &validationErrors)
	cfg.Idempotency.ProcessingTTL = requiredDuration("IDEMPOTENCY_PROCESSING_TTL", &validationErrors)
	cfg.Idempotency.ResultTTL = requiredDuration("IDEMPOTENCY_RESULT_TTL", &validationErrors)
	cfg.Idempotency.AllowDegraded = optionalBool("IDEMPOTENCY_ALLOW_DEGRADED", true, &validationErrors)

	validate(cfg, &validationErrors)
	if len(validationErrors) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(validationErrors...))
	}

	return cfg, nil
}

func required(name string, validationErrors *[]error) string {
	value, ok := os.LookupEnv(name)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s is required", name))
	}
	return value
}

func optional(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func requiredList(name string, validationErrors *[]error) []string {
	value := required(name, validationErrors)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must contain at least one value", name))
	}
	return result
}

func requiredInt(name string, validationErrors *[]error) int {
	value := required(name, validationErrors)
	if value == "" {
		return 0
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be an integer", name))
		return 0
	}
	return parsed
}

func optionalInt(name string, defaultValue int, validationErrors *[]error) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be an integer", name))
		return 0
	}
	return parsed
}

func optionalBool(name string, defaultValue bool, validationErrors *[]error) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be a boolean", name))
		return false
	}
	return parsed
}

func requiredInt64(name string, validationErrors *[]error) int64 {
	value := required(name, validationErrors)
	if value == "" {
		return 0
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be an integer", name))
		return 0
	}
	return parsed
}

func requiredDuration(name string, validationErrors *[]error) time.Duration {
	value := required(name, validationErrors)
	if value == "" {
		return 0
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be a valid duration", name))
		return 0
	}
	return parsed
}

func validate(cfg Config, validationErrors *[]error) {
	if cfg.AppEnv != "development" && cfg.AppEnv != "test" && cfg.AppEnv != "production" {
		*validationErrors = append(*validationErrors, errors.New("APP_ENV must be one of development, test, production"))
	}

	if _, _, err := net.SplitHostPort(cfg.HTTP.Address); err != nil {
		*validationErrors = append(*validationErrors, errors.New("HTTP_ADDR must include a valid host and port, for example :8080"))
	}
	for _, proxy := range cfg.HTTP.TrustedProxies {
		if net.ParseIP(proxy) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(proxy); err != nil {
			*validationErrors = append(*validationErrors, fmt.Errorf("TRUSTED_PROXIES contains invalid IP or CIDR %q", proxy))
		}
	}

	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		*validationErrors = append(*validationErrors, errors.New("LOG_LEVEL must be one of debug, info, warn, error"))
	}

	if cfg.DB.Port < 1 || cfg.DB.Port > 65535 {
		*validationErrors = append(*validationErrors, errors.New("DB_PORT must be between 1 and 65535"))
	}
	if cfg.DB.MaxOpenConns < 1 {
		*validationErrors = append(*validationErrors, errors.New("DB_MAX_OPEN_CONNS must be greater than zero"))
	}
	if cfg.DB.MaxIdleConns < 0 {
		*validationErrors = append(*validationErrors, errors.New("DB_MAX_IDLE_CONNS must not be negative"))
	}
	if cfg.DB.MaxIdleConns > cfg.DB.MaxOpenConns {
		*validationErrors = append(*validationErrors, errors.New("DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS"))
	}

	if _, _, err := net.SplitHostPort(cfg.Redis.Address); err != nil {
		*validationErrors = append(*validationErrors, errors.New("REDIS_ADDR must include a valid host and port, for example 127.0.0.1:6379"))
	}
	if cfg.Redis.DB < 0 {
		*validationErrors = append(*validationErrors, errors.New("REDIS_DB must not be negative"))
	}
	if cfg.RabbitMQ.Port < 1 || cfg.RabbitMQ.Port > 65535 {
		*validationErrors = append(*validationErrors, errors.New("RABBITMQ_PORT must be between 1 and 65535"))
	}
	if !strings.HasPrefix(cfg.RabbitMQ.VHost, "/") {
		*validationErrors = append(*validationErrors, errors.New("RABBITMQ_VHOST must start with /"))
	}

	if cfg.JWT.ExpiresIn <= 0 {
		*validationErrors = append(*validationErrors, errors.New("JWT_EXPIRES_IN must be greater than zero"))
	}
	if cfg.AppEnv == "production" && len(cfg.JWT.Secret) < 32 {
		*validationErrors = append(*validationErrors, errors.New("JWT_SECRET must be at least 32 bytes in production"))
	}
	if cfg.BcryptCost < 4 || cfg.BcryptCost > 31 {
		*validationErrors = append(*validationErrors, errors.New("BCRYPT_COST must be between 4 and 31"))
	}

	if !strings.HasPrefix(cfg.Upload.PublicPrefix, "/") || !strings.HasSuffix(cfg.Upload.PublicPrefix, "/") {
		*validationErrors = append(*validationErrors, errors.New("UPLOAD_PUBLIC_PREFIX must start and end with /"))
	}
	if cfg.Upload.MaxBytes <= 0 {
		*validationErrors = append(*validationErrors, errors.New("UPLOAD_MAX_BYTES must be greater than zero"))
	}
	if cfg.Order.PaymentTimeout <= 0 {
		*validationErrors = append(*validationErrors, errors.New("ORDER_PAYMENT_TIMEOUT must be greater than zero"))
	}
	if cfg.Idempotency.ProcessingTTL <= 0 {
		*validationErrors = append(*validationErrors, errors.New("IDEMPOTENCY_PROCESSING_TTL must be greater than zero"))
	}
	if cfg.Idempotency.ResultTTL <= 0 {
		*validationErrors = append(*validationErrors, errors.New("IDEMPOTENCY_RESULT_TTL must be greater than zero"))
	}
	if cfg.Idempotency.ResultTTL <= cfg.Idempotency.ProcessingTTL {
		*validationErrors = append(*validationErrors, errors.New("IDEMPOTENCY_RESULT_TTL must be greater than IDEMPOTENCY_PROCESSING_TTL"))
	}
}
