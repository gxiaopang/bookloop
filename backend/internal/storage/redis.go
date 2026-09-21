package storage

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/gxiaopang/bookloop/backend/internal/config"
)

type Redis struct {
	Client *redis.Client
}

func NewRedis(cfg config.RedisConfig) *Redis {
	return &Redis{
		Client: redis.NewClient(&redis.Options{
			Addr:     cfg.Address,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
	}
}

func (r *Redis) Ping(ctx context.Context) error {
	return r.Client.Ping(ctx).Err()
}

func (r *Redis) Close() error {
	return r.Client.Close()
}
