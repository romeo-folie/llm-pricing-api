package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/models"
)

// WebhookRow holds the essential fields of a webhooks table row returned by
// ListActiveWebhooks.  The secret field contains the plaintext secret so the
// delivery worker can compute HMAC-SHA256 signatures without a round-trip.
type WebhookRow struct {
	ID         string
	APIKeyHash string
	URL        string
	Secret     string
}

// Store abstracts all database operations required by the Reconciler.
// Implementations must be safe for concurrent use.
type Store interface {
	// LookupSourceID returns the primary key for a source by name.
	// Returns an error if the source is not found.
	LookupSourceID(ctx context.Context, name string) (int, error)

	// LookupSourceName returns the name for a source by its primary key.
	// Returns an error if the source is not found.
	LookupSourceName(ctx context.Context, id int) (string, error)

	// LookupModelID returns the primary key for a model by slug.
	// Returns an error if the model is not found.
	LookupModelID(ctx context.Context, slug string) (int, error)

	// LookupModelProvider returns the provider name for a model by its primary key.
	// Returns an error if the model is not found.
	LookupModelProvider(ctx context.Context, id int) (string, error)

	// LookupCurrentPrice returns the stored input and output costs for the
	// given (modelID, sourceID) pair.  found is false when no row exists yet.
	LookupCurrentPrice(ctx context.Context, modelID, sourceID int) (input, output float64, found bool, err error)

	// PublishPrice inserts a confirmed snapshot into price_history and upserts
	// the current price in the prices table, within a single transaction.
	PublishPrice(ctx context.Context, modelID, sourceID int, input, output float64, confidence models.Confidence) error

	// FlagDiscrepancy inserts a review_queue entry for a multi-source
	// disagreement.  Uses ON CONFLICT DO NOTHING so repeated calls while a
	// pending entry already exists are silently ignored.
	FlagDiscrepancy(ctx context.Context, modelID, sourceAID, sourceBID int, field models.PriceField, valueA, valueB float64, deltaPct float64) error

	// ListActiveWebhooks returns all non-deleted webhook registrations.
	// The reconciler calls this after every successful price publish to fan
	// out delivery tasks.
	ListActiveWebhooks(ctx context.Context) ([]WebhookRow, error)
}

// pgxStore is the PostgreSQL-backed implementation of Store.
type pgxStore struct {
	db *pgxpool.Pool
}

func (s *pgxStore) LookupSourceID(ctx context.Context, name string) (int, error) {
	var id int
	err := s.db.QueryRow(ctx, `SELECT id FROM sources WHERE name = $1`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("lookup source %q: %w", name, err)
	}
	return id, nil
}

func (s *pgxStore) LookupSourceName(ctx context.Context, id int) (string, error) {
	var name string
	err := s.db.QueryRow(ctx, `SELECT name FROM sources WHERE id = $1`, id).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("lookup source name for id=%d: %w", id, err)
	}
	return name, nil
}

func (s *pgxStore) LookupModelID(ctx context.Context, slug string) (int, error) {
	var id int
	err := s.db.QueryRow(ctx, `SELECT id FROM models WHERE slug = $1`, slug).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("lookup model %q: %w", slug, err)
	}
	return id, nil
}

func (s *pgxStore) LookupModelProvider(ctx context.Context, id int) (string, error) {
	var provider string
	err := s.db.QueryRow(ctx, `SELECT provider FROM models WHERE id = $1`, id).Scan(&provider)
	if err != nil {
		return "", fmt.Errorf("lookup model provider for id=%d: %w", id, err)
	}
	return provider, nil
}

func (s *pgxStore) LookupCurrentPrice(ctx context.Context, modelID, sourceID int) (float64, float64, bool, error) {
	var input, output float64
	err := s.db.QueryRow(ctx,
		`SELECT input_cost_per_token, output_cost_per_token FROM prices WHERE model_id = $1 AND source_id = $2`,
		modelID, sourceID,
	).Scan(&input, &output)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, false, nil
		}
		return 0, 0, false, fmt.Errorf("lookup current price (model=%d, source=%d): %w", modelID, sourceID, err)
	}
	return input, output, true, nil
}

func (s *pgxStore) PublishPrice(ctx context.Context, modelID, sourceID int, input, output float64, confidence models.Confidence) error {
	now := time.Now().UTC()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Supply recorded_at explicitly so the dedup index (model_id, source_id,
	// confirmed_at, recorded_at) fires for same-timestamp duplicate calls.
	// TimescaleDB requires the partition key (recorded_at) in every unique index,
	// so we cannot use a simpler (model_id, source_id, confirmed_at)-only key.
	ct, err := tx.Exec(ctx, `
		INSERT INTO price_history
			(model_id, input_cost_per_token, output_cost_per_token, source_id, confirmed_at, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
	`, modelID, input, output, sourceID, now, now)
	if err != nil {
		return fmt.Errorf("insert price_history (model=%d, source=%d): %w", modelID, sourceID, err)
	}
	if ct.RowsAffected() == 0 {
		// The dedup index was hit: a row with identical (model_id, source_id,
		// confirmed_at, recorded_at) already exists. This indicates a double-publish
		// within the same nanosecond — effectively impossible in normal operation but
		// warrants a warning since price_history is intended to be append-only.
		slog.Warn("store: price_history insert suppressed by duplicate constraint — possible double-publish",
			"model_id", modelID, "source_id", sourceID)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO prices
			(model_id, input_cost_per_token, output_cost_per_token, source_id, confirmed_at, confidence)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (model_id, source_id) DO UPDATE SET
			input_cost_per_token  = EXCLUDED.input_cost_per_token,
			output_cost_per_token = EXCLUDED.output_cost_per_token,
			confirmed_at          = EXCLUDED.confirmed_at,
			confidence            = EXCLUDED.confidence
	`, modelID, input, output, sourceID, now, string(confidence))
	if err != nil {
		return fmt.Errorf("upsert prices (model=%d, source=%d): %w", modelID, sourceID, err)
	}

	return tx.Commit(ctx)
}

func (s *pgxStore) FlagDiscrepancy(ctx context.Context, modelID, sourceAID, sourceBID int, field models.PriceField, valueA, valueB float64, deltaPct float64) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO review_queue (model_id, source_a, source_b, field, value_a, value_b, delta_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (model_id, field) WHERE status = 'pending' DO NOTHING
	`, modelID, sourceAID, sourceBID, string(field), valueA, valueB, math.Abs(deltaPct))
	if err != nil {
		return fmt.Errorf("flag discrepancy (model=%d, field=%s): %w", modelID, field, err)
	}
	return nil
}

func (s *pgxStore) ListActiveWebhooks(ctx context.Context) ([]WebhookRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, api_key_hash, url, secret
		FROM webhooks
		WHERE deleted_at IS NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("list active webhooks: %w", err)
	}
	defer rows.Close()

	var result []WebhookRow
	for rows.Next() {
		var w WebhookRow
		if err := rows.Scan(&w.ID, &w.APIKeyHash, &w.URL, &w.Secret); err != nil {
			return nil, fmt.Errorf("scan webhook row: %w", err)
		}
		result = append(result, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook rows: %w", err)
	}
	return result, nil
}
