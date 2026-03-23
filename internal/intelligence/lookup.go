package intelligence

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LookupBenchmarkID returns the UUID primary key for a benchmark by its name
// (case-insensitive). Returns an error if the benchmark is not found.
func LookupBenchmarkID(ctx context.Context, db *pgxpool.Pool, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT id FROM benchmarks WHERE LOWER(name) = LOWER($1)`, name,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup benchmark %q: %w", name, err)
	}
	return id, nil
}

// LookupModelIDBySlug returns the integer primary key for a model by its slug.
// Returns an error if the model is not found.
func LookupModelIDBySlug(ctx context.Context, db *pgxpool.Pool, slug string) (int, error) {
	var id int
	err := db.QueryRow(ctx,
		`SELECT id FROM models WHERE slug = $1`, slug,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("lookup model %q: %w", slug, err)
	}
	return id, nil
}

// LookupBenchmarkByName returns the benchmark name exactly as stored in the DB
// (case-insensitive match). Useful for validating CLI input.
func LookupBenchmarkByName(ctx context.Context, db *pgxpool.Pool, name string) (uuid.UUID, string, error) {
	var id uuid.UUID
	var dbName string
	err := db.QueryRow(ctx,
		`SELECT id, name FROM benchmarks WHERE LOWER(name) = $1`, strings.ToLower(name),
	).Scan(&id, &dbName)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("lookup benchmark %q: %w", name, err)
	}
	return id, dbName, nil
}
