package database

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ConnectWithRetry calls Connect up to maxAttempts times (sleeping 2 seconds
// between each try) and returns the first successful pool.  Use this at
// process startup where the database container may not yet be ready.
func ConnectWithRetry(ctx context.Context, databaseURL string, maxAttempts int, log zerolog.Logger) (*pgxpool.Pool, error) {
	var lastErr error
	for i := range maxAttempts {
		if i > 0 {
			log.Warn().Int("attempt", i+1).Err(lastErr).Msg("database connect failed, retrying")
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

// Connect opens a pgxpool.Pool to databaseURL with an OTel pgx tracer
// registered so every query emits a trace span.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	// Register OTel query tracer — uses the global TracerProvider which is
	// configured by internal/otel before Connect is called.
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

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
