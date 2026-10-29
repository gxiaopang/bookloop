package storage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

func TestSMTPMailerSendsMIMECodeOverAuthenticatedTLS(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	pool := x509.NewCertPool()
	pool.AddCert(certificateServer.Certificate())
	certificateServer.Close()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	})
	messages := make(chan []byte, 1)
	completed := make(chan error, 1)
	go func() { completed <- serveTestSMTP(listener, messages) }()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	mailer := NewSMTPMailer(config.SMTPConfig{Host: host, Port: port, Username: "sender@example.com", Password: "smtp-test-password", From: "sender@example.com"})
	mailer.tlsConfig.RootCAs = pool
	if err := mailer.SendRegistrationCode(context.Background(), "reader@example.com", "012345"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP server did not finish")
	}
	message, err := mail.ReadMessage(bytes.NewReader(<-messages))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "012345") || !strings.Contains(string(body), "5 分钟") {
		t.Fatal("code or expiry missing from email")
	}
	if message.Header.Get("To") != "<reader@example.com>" || message.Header.Get("Content-Type") != "text/plain; charset=UTF-8" {
		t.Fatal("invalid MIME addressing or encoding")
	}
}

func serveTestSMTP(listener net.Listener, messages chan<- []byte) (returnErr error) {
	connection, err := listener.Accept()
	if err != nil {
		return err
	}
	defer func() {
		if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	protocol := textproto.NewReader(bufio.NewReader(connection))
	write := func(reply string) error { _, err := io.WriteString(connection, reply); return err }
	if err := write("220 localhost ESMTP\r\n"); err != nil {
		return err
	}
	authenticated := false
	for {
		line, err := protocol.ReadLine()
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(line, "EHLO "):
			err = write("250-localhost\r\n250-AUTH PLAIN\r\n250 8BITMIME\r\n")
		case strings.HasPrefix(line, "AUTH PLAIN "):
			raw, decodeErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
			if decodeErr != nil || string(raw) != "\x00sender@example.com\x00smtp-test-password" {
				return errors.New("unexpected SMTP credentials")
			}
			authenticated = true
			err = write("235 authenticated\r\n")
		case strings.HasPrefix(line, "MAIL FROM:"):
			if !authenticated {
				return errors.New("message sent without authentication")
			}
			err = write("250 sender accepted\r\n")
		case strings.HasPrefix(line, "RCPT TO:"):
			err = write("250 recipient accepted\r\n")
		case line == "DATA":
			if err := write("354 send data\r\n"); err != nil {
				return err
			}
			data, readErr := io.ReadAll(protocol.DotReader())
			if readErr != nil {
				return readErr
			}
			messages <- data
			err = write("250 queued\r\n")
		case line == "QUIT":
			return write("221 goodbye\r\n")
		default:
			return fmt.Errorf("unexpected SMTP command: %s", line)
		}
		if err != nil {
			return err
		}
	}
}

func TestRegistrationMessageRejectsHeaderInjectionAndNonDigitCodes(t *testing.T) {
	for _, test := range []struct{ address, code string }{
		{"reader@example.com\r\nBcc: victim@example.com", "123456"},
		{"reader@example.com", "12\r\n34"},
		{"reader@example.com", "12e456"},
	} {
		if _, err := registrationMessage("sender@example.com", test.address, test.code); err == nil {
			t.Fatal("invalid message input accepted")
		}
	}
}

func TestSMTPMailerCancellationStopsBlockedGreeting(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	certificate := server.TLS.Certificates[0]
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	server.Close()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	}()
	done := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		_, readErr := io.Copy(io.Discard, connection)
		done <- errors.Join(readErr, connection.Close())
	}()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	mailer := NewSMTPMailer(config.SMTPConfig{Host: host, Port: port, Username: "sender@example.com", Password: "test-password", From: "sender@example.com"})
	mailer.tlsConfig.RootCAs = pool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	if err := mailer.SendRegistrationCode(ctx, "reader@example.com", "123456"); err == nil {
		t.Fatal("blocked greeting succeeded")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("SMTP ignored context cancellation")
	}
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP connection leaked")
	}
}
