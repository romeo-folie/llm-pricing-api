package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectWithRetry calls Connect up to maxAttempts times (sleeping 2 seconds
// between each try) and returns the first successful pool. Use this at process
// startup where the database container may not yet be ready.
func ConnectWithRetry(ctx context.Context, databaseURL string, maxAttempts int) (*pgxpool.Pool, error) {
	var lastErr error
	for i := range maxAttempts {
		if i > 0 {
			slog.Warn("database connect failed, retrying", "attempt", i+1, "err", lastErr)
			time.Sleep(2 * time.Second)
		}
		p, err := Connect(ctx, databaseURL)
		if err == nil {
			return p, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("could not connect to database after %d attempts: %w", maxAttempts, lastErr)
}

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
