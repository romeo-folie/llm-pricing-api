package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/models"
)

// ErrNotPending is returned by Approve and Reject when the review_queue entry
// does not exist or has already been actioned (status != 'pending'). Callers
// should map this to HTTP 409 Conflict rather than 500.
var ErrNotPending = errors.New("entry not found or already resolved")

// ReviewItem is a fully-joined view of a review_queue row, enriched with
// model name, provider, and source names for display in the operator UI.
type ReviewItem struct {
	ID          int
	ModelID     int
	ModelName   string
	Provider    string
	Field       models.PriceField
	SourceAID   int
	SourceAName string
	SourceBID   int
	SourceBName string
	ValueA      float64
	ValueB      float64
	DeltaPct    float64
	Status      models.ReviewStatus
	CreatedAt   time.Time
}

// Store abstracts all database operations required by the review Handler.
// Implementations must be safe for concurrent use.
type Store interface {
	// ListPending returns all review_queue entries with status = 'pending',
	// ordered by creation time descending (newest first).
	ListPending(ctx context.Context) ([]ReviewItem, error)

	// Approve resolves the given review_queue entry by publishing the source-A
	// value as the confirmed price and marking the entry as 'resolved'.
	// All writes occur within a single database transaction.
	Approve(ctx context.Context, id int) error

	// Reject marks the given review_queue entry as 'overridden' without writing
	// any price data.
	Reject(ctx context.Context, id int) error
}

// pgxReviewStore is the PostgreSQL-backed implementation of Store.
type pgxReviewStore struct {
	db *pgxpool.Pool
}

// NewPgxStore returns a Store backed by the given connection pool.
func NewPgxStore(db *pgxpool.Pool) Store {
	return &pgxReviewStore{db: db}
}

// ListPending returns all pending review_queue rows joined with model and source
// tables so the handler does not need to perform additional lookups.
func (s *pgxReviewStore) ListPending(ctx context.Context) ([]ReviewItem, error) {
	const q = `
		SELECT rq.id, rq.model_id, m.name AS model_name, m.provider,
		       rq.field, rq.source_a, sa.name AS source_a_name,
		       rq.source_b, sb.name AS source_b_name,
		       rq.value_a, rq.value_b, rq.delta_pct, rq.status, rq.created_at
		FROM review_queue rq
		JOIN models  m  ON rq.model_id = m.id
		JOIN sources sa ON rq.source_a  = sa.id
		JOIN sources sb ON rq.source_b  = sb.id
		WHERE rq.status = 'pending'
		ORDER BY rq.created_at DESC`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("review store: list pending: %w", err)
	}
	defer rows.Close()

	var items []ReviewItem
	for rows.Next() {
		var item ReviewItem
		var fieldStr, statusStr string

		if err := rows.Scan(
			&item.ID,
			&item.ModelID,
			&item.ModelName,
			&item.Provider,
			&fieldStr,
			&item.SourceAID,
			&item.SourceAName,
			&item.SourceBID,
			&item.SourceBName,
			&item.ValueA,
			&item.ValueB,
			&item.DeltaPct,
			&statusStr,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("review store: scan pending row: %w", err)
		}

		item.Field = models.PriceField(fieldStr)
		item.Status = models.ReviewStatus(statusStr)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review store: iterate pending rows: %w", err)
	}

	return items, nil
}

// Approve resolves the given review_queue entry in a single database
// transaction. It publishes the source-A value from the review row as a
// confirmed price by inserting into price_history and upserting prices, then
// marks the review entry as 'resolved'.
func (s *pgxReviewStore) Approve(ctx context.Context, id int) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("review store: approve %d: begin tx: %w", id, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Step 1: fetch the review_queue row, asserting it is still pending.
	// Filtering on status = 'pending' prevents double-actioning a row that was
	// already approved or rejected by a concurrent operator session.
	var modelID, sourceAID int
	var valueA float64
	var fieldStr string

	// FOR UPDATE acquires a row-level lock, preventing a concurrent Approve or
	// Reject from reading and acting on the same row simultaneously.
	err = tx.QueryRow(ctx,
		`SELECT model_id, source_a, value_a, field
		 FROM review_queue
		 WHERE id = $1 AND status = 'pending'
		 FOR UPDATE`,
		id,
	).Scan(&modelID, &sourceAID, &valueA, &fieldStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("review store: approve %d: %w", id, ErrNotPending)
		}
		return fmt.Errorf("review store: approve %d: fetch review row: %w", id, err)
	}
	field := models.PriceField(fieldStr)

	// Step 2: fetch the current confirmed price for (model_id, source_a) so
	// the unchanged column is preserved in the price_history snapshot.
	var currInput, currOutput float64
	err = tx.QueryRow(ctx,
		`SELECT input_cost_per_token, output_cost_per_token
		 FROM prices
		 WHERE model_id = $1 AND source_id = $2`,
		modelID, sourceAID,
	).Scan(&currInput, &currOutput)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("review store: approve %d: fetch current price: %w", id, err)
	}
	// If no row exists, currInput and currOutput remain 0.

	// Step 3: determine the two column values for the new snapshot.
	var newInput, newOutput float64
	switch field {
	case models.PriceFieldInput:
		newInput = valueA
		newOutput = currOutput
	case models.PriceFieldOutput:
		newInput = currInput
		newOutput = valueA
	default:
		return fmt.Errorf("review store: approve %d: unknown field %q", id, field)
	}

	// Step 4: insert immutable price_history record.
	// ON CONFLICT DO NOTHING guards against duplicate calls with the same
	// dedup key (model_id, source_id, confirmed_at). We compute `now` in Go
	// so the timestamp is fixed before SQL execution — using NOW() would be
	// evaluated at statement time and could differ between retries, making
	// the conflict guard ineffective. Matches the reconciler's PublishPrice
	// pattern. A suppressed insert is logged as a warning for observability.
	now := time.Now().UTC()
	ct4, err := tx.Exec(ctx, `
		INSERT INTO price_history
			(model_id, input_cost_per_token, output_cost_per_token, source_id, confirmed_at, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`,
		modelID, newInput, newOutput, sourceAID, now, now,
	)
	if err != nil {
		return fmt.Errorf("review store: approve %d: insert price_history: %w", id, err)
	}
	if ct4.RowsAffected() == 0 {
		slog.Warn("review store: price_history insert suppressed by duplicate constraint",
			"review_id", id, "model_id", modelID, "source_id", sourceAID)
	}

	// Step 5: upsert the current price.
	_, err = tx.Exec(ctx, `
		INSERT INTO prices
			(model_id, input_cost_per_token, output_cost_per_token, source_id, confirmed_at, confidence)
		VALUES ($1, $2, $3, $4, NOW(), 'high')
		ON CONFLICT (model_id, source_id) DO UPDATE SET
			input_cost_per_token  = EXCLUDED.input_cost_per_token,
			output_cost_per_token = EXCLUDED.output_cost_per_token,
			confirmed_at          = EXCLUDED.confirmed_at,
			confidence            = EXCLUDED.confidence`,
		modelID, newInput, newOutput, sourceAID,
	)
	if err != nil {
		return fmt.Errorf("review store: approve %d: upsert prices: %w", id, err)
	}

	// Step 6: mark the review entry as resolved.
	// AND status = 'pending' is belt-and-suspenders (the FOR UPDATE lock in
	// step 1 prevents any concurrent session from changing the status, but
	// checking RowsAffected here makes the invariant explicit and testable).
	ct6, err := tx.Exec(ctx,
		`UPDATE review_queue SET status = 'resolved', resolved_at = NOW()
		 WHERE id = $1 AND status = 'pending'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("review store: approve %d: update review_queue: %w", id, err)
	}
	if ct6.RowsAffected() == 0 {
		return fmt.Errorf("review store: approve %d: %w", id, ErrNotPending)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("review store: approve %d: commit: %w", id, err)
	}
	return nil
}

// Reject marks the given review_queue entry as 'overridden' without publishing
// any price data. The WHERE clause includes status = 'pending' to prevent
// double-actioning an already-resolved entry.
//
// Unlike Approve, Reject does not need an explicit transaction or FOR UPDATE
// lock: a single UPDATE statement acquires a row-level lock implicitly during
// execution, making it atomically safe against concurrent Approve/Reject calls.
// Approve requires FOR UPDATE because it separates the SELECT (step 1) from
// subsequent writes (steps 4-6) across multiple SQL statements in a
// transaction — without the lock a concurrent session could act on the same
// row between those statements.
func (s *pgxReviewStore) Reject(ctx context.Context, id int) error {
	ct, err := s.db.Exec(ctx,
		`UPDATE review_queue SET status = 'overridden', resolved_at = NOW()
		 WHERE id = $1 AND status = 'pending'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("review store: reject %d: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("review store: reject %d: %w", id, ErrNotPending)
	}
	return nil
}
