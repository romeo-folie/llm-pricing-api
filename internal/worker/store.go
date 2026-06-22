package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/scraper"
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
	// "litellm", "openai", "anthropic", "huggingface_inference_providers").
	FetchPricesBySource(ctx context.Context, sourceName string) ([]models.Price, error)

	// EnsureModels upserts model rows from scraped data so that the
	// reconciler's LookupModelID succeeds for newly discovered models.
	// Existing rows are updated with the latest metadata (context_window,
	// modality) but the original created_at is preserved.
	EnsureModels(ctx context.Context, scraped []scraper.ScrapedModel) error

	// MarkVerified sets prices.last_verified_at = now() for every price row
	// belonging to the named source whose model slug is in slugs. It is called
	// once per scrape cycle to record that these prices were re-confirmed as
	// current, independent of whether their value changed. A no-op when slugs
	// is empty.
	MarkVerified(ctx context.Context, sourceName string, slugs []string) error
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

// EnsureModels upserts model rows from scraped data. New slugs are inserted;
// existing slugs get their metadata refreshed (context_window, modality).
// Individual row failures are counted but do not abort the batch — the pipeline
// continues with whichever models succeeded. An error is only returned if
// every single row failed (catastrophic DB issue).
func (s *pgxWorkerStore) EnsureModels(ctx context.Context, scraped []scraper.ScrapedModel) error {
	var failures int
	for _, m := range scraped {
		modality := normalizeModality(m.Modality)
		name := nameFromSlug(m.Slug)
		_, err := s.db.Exec(ctx, `
			INSERT INTO models (provider, name, slug, modality, context_window)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (slug) DO UPDATE SET
				context_window = COALESCE(EXCLUDED.context_window, models.context_window),
				modality       = EXCLUDED.modality,
				provider       = EXCLUDED.provider
		`, m.Provider, name, m.Slug, modality, m.ContextWindow)
		if err != nil {
			failures++
		}
	}
	if failures == len(scraped) {
		return fmt.Errorf("worker store: ensure models: all %d rows failed", failures)
	}
	return nil
}

// MarkVerified bumps prices.last_verified_at to now() for every price row from
// the named source whose model slug appears in slugs. It resolves slug→model_id
// and sourceName→source_id inside the query (a single round-trip) so the caller
// only passes the scraped slugs. Rows that have no price for this source yet
// (e.g. a brand-new model still pending its first publish) are simply not matched.
func (s *pgxWorkerStore) MarkVerified(ctx context.Context, sourceName string, slugs []string) error {
	if len(slugs) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		UPDATE prices p
		SET last_verified_at = NOW()
		FROM models m, sources s
		WHERE p.model_id = m.id
		  AND p.source_id = s.id
		  AND s.name = $1
		  AND m.slug = ANY($2::text[])
	`, sourceName, slugs)
	if err != nil {
		return fmt.Errorf("worker store: mark verified for source %q: %w", sourceName, err)
	}
	return nil
}

// nameFromSlug extracts a human-readable name from a model slug.
//
// Simple case: "openai/gpt-4" → "gpt-4".
//
// Some providers (e.g. Fireworks AI) use deep path slugs like
// "fireworks_ai/accounts/fireworks/models/stable-diffusion-xl-1024-v1-0".
// Stripping only the first segment would leave "accounts/fireworks/models/…"
// as the name, which is unreadable. For these slugs we return the last
// path segment instead, e.g. "stable-diffusion-xl-1024-v1-0".
func nameFromSlug(slug string) string {
	// No "/" at all — slug is already a bare model name.
	firstSlash := strings.IndexByte(slug, '/')
	if firstSlash < 0 || firstSlash == len(slug)-1 {
		return slug
	}

	// If the remainder after the first segment itself contains a "/", the slug
	// has a deep path (e.g. "fireworks_ai/accounts/…/model-name"). In that
	// case return the last segment only.
	remainder := slug[firstSlash+1:]
	if strings.ContainsRune(remainder, '/') {
		if lastSlash := strings.LastIndexByte(slug, '/'); lastSlash >= 0 && lastSlash < len(slug)-1 {
			return slug[lastSlash+1:]
		}
	}

	return remainder
}

// normalizeModality maps raw modality strings from external APIs to the
// values allowed by the models.modality CHECK constraint.
func normalizeModality(raw string) string {
	switch strings.ToLower(raw) {
	case "text", "text->text":
		return "text"
	case "multimodal", "text+image->text":
		return "multimodal"
	case "image", "text->image", "image_generation":
		return "image"
	case "audio", "audio_transcription", "audio_speech", "text->audio":
		return "audio"
	case "embedding":
		return "embedding"
	default:
		return "text"
	}
}
