package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
)

type fakeRegistrationCodes struct {
	values                                 map[string]string
	wait                                   time.Duration
	reserveErr, storeErr, consumeErr       error
	reserveCalls, storeCalls, consumeCalls int
	accept                                 bool
}

func (r *fakeRegistrationCodes) ReserveSend(context.Context, string, string, time.Duration, time.Duration, int, int) (time.Duration, error) {
	r.reserveCalls++
	return r.wait, r.reserveErr
}
func (r *fakeRegistrationCodes) Store(_ context.Context, key, digest string, _ time.Duration) error {
	r.storeCalls++
	if r.storeErr != nil {
		return r.storeErr
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = digest
	return nil
}
func (r *fakeRegistrationCodes) Consume(_ context.Context, key, digest string, _ int) (bool, error) {
	r.consumeCalls++
	if r.consumeErr != nil {
		return false, r.consumeErr
	}
	if r.accept {
		return true, nil
	}
	if r.values[key] != digest {
		return false, nil
	}
	delete(r.values, key)
	return true, nil
}

type fakeRegistrationMailer struct {
	email, code string
	err         error
	disabled    bool
	calls       int
}

func (m *fakeRegistrationMailer) Configured() bool { return !m.disabled }
func (m *fakeRegistrationMailer) SendRegistrationCode(_ context.Context, email, code string) error {
	m.calls++
	m.email, m.code = email, code
	return m.err
}

func testRegistration(codes *fakeRegistrationCodes, mailer *fakeRegistrationMailer) *Registration {
	return NewRegistration(codes, mailer, "registration-test-secret", testRegistrationLogger())
}

func testRegistrationLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRegistrationCodeIsSixDigitsBoundToEmailAndConsumedOnce(t *testing.T) {
	codes, mailer := &fakeRegistrationCodes{}, &fakeRegistrationMailer{}
	registration := testRegistration(codes, mailer)
	if err := registration.send(context.Background(), "reader@example.com", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if !validRegistrationCode(mailer.code) || mailer.email != "reader@example.com" {
		t.Fatal("expected a six digit code delivered to the requested address")
	}
	if len(codes.values[registrationKey(mailer.email)]) != 64 || codes.values[registrationKey(mailer.email)] == mailer.code {
		t.Fatal("expected a keyed digest, not a plaintext code")
	}
	if err := registration.consume(context.Background(), "other@example.com", mailer.code); !errors.Is(err, ErrEmailCodeInvalid) {
		t.Fatalf("code for another email accepted: %v", err)
	}
	if err := registration.consume(context.Background(), mailer.email, mailer.code); err != nil {
		t.Fatal(err)
	}
	if err := registration.consume(context.Background(), mailer.email, mailer.code); !errors.Is(err, ErrEmailCodeInvalid) {
		t.Fatalf("replayed code accepted: %v", err)
	}
}

func TestRegistrationSendFailuresNeverPublishANewCode(t *testing.T) {
	for _, name := range []string{"unconfigured", "rate-limited", "redis-unavailable", "smtp-failure", "store-failure"} {
		t.Run(name, func(t *testing.T) {
			codes, mailer := &fakeRegistrationCodes{}, &fakeRegistrationMailer{}
			switch name {
			case "unconfigured":
				mailer.disabled = true
			case "rate-limited":
				codes.wait = 42 * time.Second
			case "redis-unavailable":
				codes.reserveErr = errors.New("Redis unavailable")
			case "smtp-failure":
				mailer.err = errors.New("SMTP unavailable")
			case "store-failure":
				codes.storeErr = errors.New("Redis write unavailable")
			}
			err := testRegistration(codes, mailer).send(context.Background(), "reader@example.com", "127.0.0.1")
			if name == "rate-limited" {
				var limited *EmailCodeRateLimitError
				if !errors.As(err, &limited) || limited.RetryAfter != 42*time.Second {
					t.Fatalf("expected accurate rate limit: %v", err)
				}
			} else if !errors.Is(err, ErrDependencyUnavailable) {
				t.Fatalf("expected dependency failure: %v", err)
			}
			if len(codes.values) != 0 {
				t.Fatal("failed send published a code")
			}
			if name == "unconfigured" && codes.reserveCalls != 0 {
				t.Fatal("missing SMTP configuration consumed send quota")
			}
			if (name == "rate-limited" || name == "redis-unavailable" || name == "unconfigured") && mailer.calls != 0 {
				t.Fatal("mail sent before dependency/rate checks")
			}
		})
	}
}

func TestRegistrationMailFailurePreservesPreviouslyDeliveredCode(t *testing.T) {
	codes, mailer := &fakeRegistrationCodes{}, &fakeRegistrationMailer{}
	registration := testRegistration(codes, mailer)
	if err := registration.send(context.Background(), "reader@example.com", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	previous := mailer.code
	mailer.err = errors.New("temporary SMTP failure")
	if err := registration.send(context.Background(), "reader@example.com", "127.0.0.1"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatal(err)
	}
	if err := registration.consume(context.Background(), "reader@example.com", previous); err != nil {
		t.Fatalf("previous code was invalidated by failed send: %v", err)
	}
}

func TestRegisterRequiresValidEmailCodeAndFailsClosedOnRedisFailure(t *testing.T) {
	for _, name := range []string{"missing", "malformed", "wrong", "redis-unavailable"} {
		t.Run(name, func(t *testing.T) {
			users := newFakeUserRepository()
			auth := newTestAuth(t, users)
			codes := &fakeRegistrationCodes{}
			auth.registration = testRegistration(codes, &fakeRegistrationMailer{})
			request := dto.RegisterRequest{Username: "reader", Email: "reader@example.com", Password: "password-one", EmailCode: "123456"}
			want := ErrEmailCodeInvalid
			switch name {
			case "missing":
				request.EmailCode = ""
				want = ErrValidation
			case "malformed":
				request.EmailCode = "12e456"
				want = ErrValidation
			case "redis-unavailable":
				codes.consumeErr = errors.New("Redis offline")
				want = ErrDependencyUnavailable
			}
			if _, err := auth.Register(context.Background(), request); !errors.Is(err, want) {
				t.Fatalf("Register error=%v, want %v", err, want)
			}
			if len(users.byID) != 0 {
				t.Fatal("created account without email verification")
			}
		})
	}
}

func TestRequestRegistrationCodeNormalizesEmailAndRejectsExistingAccounts(t *testing.T) {
	users := newFakeUserRepository()
	auth := newTestAuth(t, users)
	mailer := &fakeRegistrationMailer{}
	auth.registration = testRegistration(&fakeRegistrationCodes{}, mailer)
	if err := auth.RequestRegistrationCode(context.Background(), dto.EmailCodeRequest{Email: " Reader@Example.COM "}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if mailer.email != "reader@example.com" {
		t.Fatalf("email=%q", mailer.email)
	}
	users.addUser(t, "reader", "reader@example.com", "password-one", "active")
	if err := auth.RequestRegistrationCode(context.Background(), dto.EmailCodeRequest{Email: "READER@EXAMPLE.COM"}, "127.0.0.1"); !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatal(err)
	}
	if mailer.calls != 1 {
		t.Fatal("sent mail to an already registered account")
	}
}
