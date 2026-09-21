package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadinessRequiresMySQL(t *testing.T) {
	readiness := NewReadiness(
		func(context.Context) error { return errors.New("mysql unavailable") },
		func(context.Context) error { return nil },
		time.Second,
		false,
	)

	result := readiness.Check(context.Background())
	if result.Ready() {
		t.Fatal("Ready() = true when MySQL is unavailable")
	}
}

func TestReadinessAllowsRedisDegradation(t *testing.T) {
	readiness := NewReadiness(
		func(context.Context) error { return nil },
		func(context.Context) error { return errors.New("redis unavailable") },
		time.Second,
		false,
	)

	result := readiness.Check(context.Background())
	if !result.Ready() {
		t.Fatal("Ready() = false when only Redis is unavailable")
	}
	if !result.Degraded() {
		t.Fatal("Degraded() = false when Redis is unavailable")
	}
}

func TestReadinessRequiresRedisWhenDegradationIsDisabled(t *testing.T) {
	readiness := NewReadiness(
		func(context.Context) error { return nil },
		func(context.Context) error { return errors.New("redis unavailable") },
		time.Second,
		true,
	)

	result := readiness.Check(context.Background())
	if result.Ready() {
		t.Fatal("Ready() = true when required Redis is unavailable")
	}
	if result.Degraded() {
		t.Fatal("Degraded() = true when Redis is required")
	}
}
