package service

import (
	"context"
	"time"
)

type ReadinessResult struct {
	MySQLError    error
	RedisError    error
	RedisRequired bool
}

func (r ReadinessResult) Ready() bool {
	return r.MySQLError == nil && (!r.RedisRequired || r.RedisError == nil)
}

func (r ReadinessResult) Degraded() bool {
	return r.MySQLError == nil && !r.RedisRequired && r.RedisError != nil
}

type Readiness struct {
	mysqlPing     func(context.Context) error
	redisPing     func(context.Context) error
	timeout       time.Duration
	redisRequired bool
}

func NewReadiness(
	mysqlPing func(context.Context) error,
	redisPing func(context.Context) error,
	timeout time.Duration,
	redisRequired bool,
) *Readiness {
	return &Readiness{
		mysqlPing:     mysqlPing,
		redisPing:     redisPing,
		timeout:       timeout,
		redisRequired: redisRequired,
	}
}

func (r *Readiness) Check(ctx context.Context) ReadinessResult {
	return ReadinessResult{
		MySQLError:    pingWithTimeout(ctx, r.timeout, r.mysqlPing),
		RedisError:    pingWithTimeout(ctx, r.timeout, r.redisPing),
		RedisRequired: r.redisRequired,
	}
}

func pingWithTimeout(ctx context.Context, timeout time.Duration, ping func(context.Context) error) error {
	pingContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return ping(pingContext)
}
