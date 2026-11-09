package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct{ client *redis.Client }

func New(addr string) *Redis                    { return &Redis{client: redis.NewClient(&redis.Options{Addr: addr})} }
func (r *Redis) Close() error                   { return r.client.Close() }
func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }
func (r *Redis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}
func (r *Redis) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}
