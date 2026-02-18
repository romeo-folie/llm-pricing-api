# internal/cache

Redis client setup.

## Purpose

Configures and returns a `redis.Client` connected to the Redis instance. Used for response caching, rate limiting, and as the asynq job queue backend. This is the single point of Redis connection management.

## Structure

```
internal/cache/
  redis.go     # Connect() function — client creation with URL parsing
  README.md    # This file
```

## Key Components

- **`Connect(ctx, redisURL) (*redis.Client, error)`** — Attempts to parse the URL with `redis.ParseURL` (supports full `redis://` URIs). Falls back to treating the value as a bare `host:port` address. Verifies connectivity with a ping and closes the client on failure.

## Dependencies

| Dependency | Role |
|---|---|
| `github.com/redis/go-redis/v9` | Redis client library |

## Usage

```go
// With a full URL
client, err := cache.Connect(ctx, "redis://localhost:6380/0")

// With a bare address (common for local dev)
client, err := cache.Connect(ctx, "localhost:6380")

if err != nil {
    log.Fatal(err)
}
defer client.Close()
```
