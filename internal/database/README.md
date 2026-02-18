# internal/database

PostgreSQL connection pool setup.

## Purpose

Configures and returns a `pgxpool.Pool` connected to TimescaleDB/PostgreSQL. This is the single point of database connection management — all other modules receive the pool as a dependency.

## Structure

```
internal/database/
  db.go        # Connect() function — pool creation with tuned settings
  README.md    # This file
```

## Key Components

- **`Connect(ctx, databaseURL) (*pgxpool.Pool, error)`** — Parses the connection URL, applies pool limits (max 20 conns, min 2, 5-minute idle timeout), creates the pool, and verifies connectivity with a ping. Closes the pool and returns an error if the ping fails.

### Pool Configuration

| Setting | Value | Rationale |
|---|---|---|
| `MaxConns` | 20 | Sufficient for API + worker under expected load |
| `MinConns` | 2 | Keeps warm connections ready for low-traffic periods |
| `MaxConnIdleTime` | 5 min | Reclaims idle connections to limit resource usage |

## Dependencies

| Dependency | Role |
|---|---|
| `github.com/jackc/pgx/v5/pgxpool` | PostgreSQL connection pooling |

## Usage

```go
pool, err := database.Connect(ctx, "postgres://user:pass@localhost:5434/db?sslmode=disable")
if err != nil {
    log.Fatal(err)
}
defer pool.Close()
```
