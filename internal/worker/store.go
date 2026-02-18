package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/models"
)

// WorkerStore abstracts the database reads required by handler functions.
// It is scoped to the worker package: scrapers read stored data to feed
// the diff engine, which is separate from the reconciler's own Store interface.
type WorkerStore interface {
	// FetchModels returns all rows from the models table.
	// The slice is used by the diff engine to resolve model IDs to slugs.
	FetchModels(ctx context.Context) ([]models.Model, error)

	// FetchPricesBySource returns all prices rows for the given source name.
	// sourceName must match a name in the sources table (e.g. "openrouter",
	// "litellm", "openai-docs", "anthropic-docs", "google-docs",
	// "mistral-docs", "amazon-docs").
	FetchPricesBySource(ctx context.Context, sourceName string) ([]models.Price, error)
}

// pgxWorkerStore is the PostgreSQL-backed implementation of WorkerStore.
type pgxWorkerStore struct {
	db *pgxpool.Pool
}

// NewPgxStore returns a WorkerStore backed by the provided connection pool.
func NewPgxStore(db *pgxpool.Pool) WorkerStore {
	return &pgxWorkerStore{db: db}
}

// FetchModels retrieves all rows from the models table ordered by id.
func (s *pgxWorkerStore) FetchModels(ctx context.Context) ([]models.Model, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, provider, name, slug, modality, context_window, created_at, updated_at
		FROM models ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("worker store: fetch models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		var modality string
		var contextWindow *int32

		if err := rows.Scan(
			&m.ID,
			&m.Provider,
			&m.Name,
			&m.Slug,
			&modality,
			&contextWindow,
			&m.CreatedAt,
			&m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("worker store: scan model row: %w", err)
		}
		m.Modality = models.Modality(modality)
		if contextWindow != nil {
			v := int(*contextWindow)
			m.ContextWindow = &v
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("worker store: iterate model rows: %w", err)
	}
	return result, nil
}

// FetchPricesBySource retrieves all prices rows for the named source via a
// JOIN on the sources table.
func (s *pgxWorkerStore) FetchPricesBySource(ctx context.Context, sourceName string) ([]models.Price, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.model_id, p.input_cost_per_token, p.output_cost_per_token,
		       p.source_id, p.confirmed_at, p.confidence, p.created_at
		FROM prices p
		JOIN sources s ON s.id = p.source_id
		WHERE s.name = $1
	`, sourceName)
	if err != nil {
		return nil, fmt.Errorf("worker store: fetch prices for source %q: %w", sourceName, err)
	}
	defer rows.Close()

	var result []models.Price
	for rows.Next() {
		var p models.Price
		var confidence string

		if err := rows.Scan(
			&p.ID,
			&p.ModelID,
			&p.InputCostPerToken,
			&p.OutputCostPerToken,
			&p.SourceID,
			&p.ConfirmedAt,
			&confidence,
			&p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("worker store: scan price row: %w", err)
		}
		p.Confidence = models.Confidence(confidence)
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("worker store: iterate price rows: %w", err)
	}
	return result, nil
}
