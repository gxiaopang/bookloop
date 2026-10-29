package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/storage"
)

// This opt-in test sends one real email. Normal test/CI runs never set the
// recipient variable and therefore cannot accidentally send external mail.
func TestRegistrationWithLiveSMTP(t *testing.T) {
	to := strings.ToLower(strings.TrimSpace(os.Getenv("BOOKLOOP_TEST_SMTP_TO")))
	if to == "" {
		t.Skip("BOOKLOOP_TEST_SMTP_TO is not set; real email test disabled")
	}
	if !validEmail(to) {
		t.Fatal("invalid explicit test recipient")
	}
	address := os.Getenv("BOOKLOOP_TEST_REDIS_ADDR")
	if address == "" {
		t.Fatal("a disposable BOOKLOOP_TEST_REDIS_ADDR is required")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	mailer := &capturingLiveMailer{SMTPMailer: storage.NewSMTPMailer(config.SMTPConfig{
		Host: os.Getenv("SMTP_HOST"), Port: 465, Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"), From: os.Getenv("SMTP_FROM"),
	})}
	if !mailer.Configured() {
		t.Fatal("complete SMTP configuration required")
	}
	registration := NewRegistration(repository.NewRegistrationCodeRepository(client), mailer, "live-test-registration-secret", testRegistrationLogger())
	users := newFakeUserRepository()
	auth, err := NewAuth(users, NewJWTManager("live-test-token-secret", "bookloop-test", time.Minute), bcrypt.MinCost, registration)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := auth.RequestRegistrationCode(ctx, dto.EmailCodeRequest{Email: to}, "127.0.0.1"); err != nil {
		t.Fatalf("send real code: %v", err)
	}
	user, err := auth.Register(ctx, dto.RegisterRequest{Username: "email_test_reader", Email: to, Password: "test-only-password", EmailCode: mailer.code})
	if err != nil || user == nil || user.Email != to {
		t.Fatalf("register with delivered code: %v", err)
	}
	if err := registration.consume(ctx, to, mailer.code); err != ErrEmailCodeInvalid {
		t.Fatalf("delivered code replay was not rejected: %v", err)
	}
	// Neither the real code nor SMTP credentials are printed by this test.
}

type capturingLiveMailer struct {
	*storage.SMTPMailer
	code string
}

func (m *capturingLiveMailer) SendRegistrationCode(ctx context.Context, to, code string) error {
	if err := m.SMTPMailer.SendRegistrationCode(ctx, to, code); err != nil {
		return err
	}
	m.code = code
	return nil
}
