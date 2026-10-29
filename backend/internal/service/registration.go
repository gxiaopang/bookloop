package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"time"
)

const (
	registrationCodeTTL     = 5 * time.Minute
	registrationCooldown    = time.Minute
	registrationRateWindow  = time.Hour
	registrationEmailLimit  = 10
	registrationIPLimit     = 30
	registrationMaxAttempts = 5
)

type registrationCodes interface {
	ReserveSend(context.Context, string, string, time.Duration, time.Duration, int, int) (time.Duration, error)
	Store(context.Context, string, string, time.Duration) error
	Consume(context.Context, string, string, int) (bool, error)
}

type registrationMailer interface {
	Configured() bool
	SendRegistrationCode(context.Context, string, string) error
}

type Registration struct {
	codes  registrationCodes
	mailer registrationMailer
	secret []byte
	logger *slog.Logger
}

func NewRegistration(codes registrationCodes, mailer registrationMailer, secret string, logger *slog.Logger) *Registration {
	return &Registration{codes: codes, mailer: mailer, secret: []byte(secret), logger: logger}
}

// EmailCodeRateLimitError preserves Redis's remaining cooldown for Retry-After.
type EmailCodeRateLimitError struct {
	RetryAfter time.Duration
}

func (e *EmailCodeRateLimitError) Error() string { return "registration email rate limit exceeded" }

func (s *Registration) send(ctx context.Context, email, clientIP string) error {
	if s == nil || s.codes == nil || s.mailer == nil || !s.mailer.Configured() || len(s.secret) == 0 {
		return ErrDependencyUnavailable
	}
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return ErrValidation
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	wait, err := s.codes.ReserveSend(ctx, registrationKey(email), registrationKey(ip.String()),
		registrationCooldown, registrationRateWindow, registrationEmailLimit, registrationIPLimit)
	if err != nil {
		s.logger.Error("registration email rate limiter unavailable", "error", err)
		return ErrDependencyUnavailable
	}
	if wait > 0 {
		return &EmailCodeRateLimitError{RetryAfter: wait}
	}
	number, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return fmt.Errorf("generate registration code: %w", err)
	}
	code := fmt.Sprintf("%06d", number.Int64())
	if err := s.mailer.SendRegistrationCode(ctx, email, code); err != nil {
		s.logger.Warn("registration email delivery failed", "error", err)
		return ErrDependencyUnavailable
	}
	// Only publish a new code after SMTP accepts the message. A failed send
	// leaves a previously delivered code valid and still consumes send quota.
	if err := s.codes.Store(ctx, registrationKey(email), s.digest(email, code), registrationCodeTTL); err != nil {
		s.logger.Error("registration code persistence failed", "error", err)
		return ErrDependencyUnavailable
	}
	return nil
}

func (s *Registration) consume(ctx context.Context, email, code string) error {
	if s == nil || s.codes == nil || len(s.secret) == 0 {
		return ErrDependencyUnavailable
	}
	accepted, err := s.codes.Consume(ctx, registrationKey(email), s.digest(email, code), registrationMaxAttempts)
	if err != nil {
		s.logger.Error("registration code verification unavailable", "error", err)
		return ErrDependencyUnavailable
	}
	if !accepted {
		return ErrEmailCodeInvalid
	}
	return nil
}

func (s *Registration) digest(email, code string) string {
	mac := hmac.New(sha256.New, s.secret)
	// Domain separation lets the existing JWT secret act as a code pepper
	// without producing JWT signatures or storing brute-forceable plain hashes.
	mac.Write([]byte("bookloop:registration:v1\x00" + email + "\x00" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func registrationKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validRegistrationCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
