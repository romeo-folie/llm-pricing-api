package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisAddr parses a Redis URL and returns the bare host:port address
// suitable for use with asynq.RedisClientOpt{Addr: ...}.
// It accepts both full Redis URLs (redis://[user:pass@]host:port/db) and
// bare host:port strings, matching the fallback behaviour of Connect.
func RedisAddr(redisURL string) string {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return redisURL // assume bare host:port
	}
	return opts.Addr
}

func Connect(ctx context.Context, redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		// Fall back to treating the value as a bare host:port
		opts = &redis.Options{Addr: redisURL}
	}

	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
