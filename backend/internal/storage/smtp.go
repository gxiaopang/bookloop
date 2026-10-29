package storage

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

// SMTPMailer uses implicit TLS, including Alibaba Cloud DirectMail's port 465.
// It never falls back to plaintext or disables certificate validation.
type SMTPMailer struct {
	cfg       config.SMTPConfig
	tlsConfig *tls.Config
}

func NewSMTPMailer(cfg config.SMTPConfig) *SMTPMailer {
	return &SMTPMailer{cfg: cfg, tlsConfig: &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}}
}

func (m *SMTPMailer) Configured() bool {
	return m.cfg.Host != "" && m.cfg.Username != "" && m.cfg.Password != "" && m.cfg.From != ""
}

func (m *SMTPMailer) SendRegistrationCode(ctx context.Context, recipient, code string) (returnErr error) {
	if !m.Configured() {
		return errors.New("SMTP is not configured")
	}
	message, err := registrationMessage(m.cfg.From, recipient, code)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 8 * time.Second},
		Config:    m.tlsConfig,
	}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port)))
	if err != nil {
		return fmt.Errorf("connect SMTP TLS: %w", err)
	}
	defer func() {
		if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			returnErr = errors.Join(returnErr, fmt.Errorf("close SMTP connection: %w", err))
		}
	}()
	// A deadline bounds both the initial greeting and every SMTP command.
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return err
		}
	}
	cancelledClose := make(chan error, 1)
	stopCancel := context.AfterFunc(ctx, func() { cancelledClose <- connection.Close() })
	defer func() {
		if !stopCancel() {
			if err := <-cancelledClose; err != nil && !errors.Is(err, net.ErrClosed) {
				returnErr = errors.Join(returnErr, fmt.Errorf("cancel SMTP connection: %w", err))
			}
		}
	}()
	client, err := smtp.NewClient(connection, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("read SMTP greeting: %w", err)
	}
	if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
		return fmt.Errorf("authenticate SMTP: %w", err)
	}
	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("begin SMTP message: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		return errors.Join(fmt.Errorf("write SMTP message: %w", err), writer.Close())
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("submit SMTP message: %w", err)
	}
	return client.Quit()
}

func registrationMessage(from, to, code string) ([]byte, error) {
	for _, address := range []string{from, to} {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address {
			return nil, errors.New("invalid mail address")
		}
	}
	if len(code) != 6 {
		return nil, errors.New("invalid email code")
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return nil, errors.New("invalid email code")
		}
	}
	var body bytes.Buffer
	encoder := quotedprintable.NewWriter(&body)
	if _, err := fmt.Fprintf(encoder, "你的 BookLoop 注册验证码是：%s\r\n\r\n验证码 5 分钟内有效，仅用于注册。请勿转发给他人。\r\n如果不是你本人操作，请忽略此邮件。\r\n", code); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	sender := (&mail.Address{Name: "BookLoop", Address: from}).String()
	message := fmt.Sprintf("Date: %s\r\nFrom: %s\r\nTo: <%s>\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s",
		time.Now().UTC().Format(time.RFC1123Z), sender, to, mime.QEncoding.Encode("UTF-8", "BookLoop 注册验证码"), body.String())
	return []byte(message), nil
}
