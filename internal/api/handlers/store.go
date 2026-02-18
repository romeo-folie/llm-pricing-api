// Package handlers provides HTTP handler functions for the LLM pricing REST API.
// All handlers rely on the Store interface for database access; no handler
// function touches the DB pool directly. This enables lightweight mock-based
// testing via Fiber's app.Test() without a live database.
package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/api"
)

// Store is the data-access contract used by all handler functions.
// All methods accept a context.Context for OTel span propagation and
// cancellation; handlers pass c.Context() from the Fiber request context.
//
// Every Store method returns only plain Go types — no pgx-specific types
// escape the implementation — so callers and tests remain decoupled from pgx.
type Store interface {
	// ListModels returns a paginated list of models with optional filters.
	// The second return value is the total number of models matching the
	// filter (used to set X-Total-Count). Page is 1-based.
	ListModels(ctx context.Context, filter ListModelsFilter) ([]ModelRow, int, error)

	// GetModel returns a single model by its integer primary key.
	// Returns ErrNotFound when no row matches.
	GetModel(ctx context.Context, id int) (ModelRow, error)

	// ListProviders returns all distinct providers with their model counts.
	ListProviders(ctx context.Context) ([]ProviderRow, error)

	// CompareModels returns pricing rows for the given model IDs (max 5).
	// Returns ErrNotFound if any requested ID does not exist.
	CompareModels(ctx context.Context, ids []int) ([]ModelRow, error)

	// ListChanges returns recent price changes with optional filters.
	ListChanges(ctx context.Context, filter ChangesFilter) ([]ChangeRow, error)

	// GetPriceHistory returns the full price history for a model, used to
	// compute TrustMeta. The rows are ordered by confirmed_at ascending.
	GetPriceHistory(ctx context.Context, modelID int) ([]api.PriceHistoryRow, error)

	// GetPriceHistoryBatch returns price history for multiple models in a single
	// query. Returns a map from model_id to []api.PriceHistoryRow ordered by
	// confirmed_at ascending for each model. Use this in list endpoints to avoid
	// N+1 query patterns.
	GetPriceHistoryBatch(ctx context.Context, modelIDs []int) (map[int][]api.PriceHistoryRow, error)

	// GetModelHistory returns paginated price history for the
	// /v1/models/:id/history endpoint (Developer+ tier, issue #20).
	GetModelHistory(ctx context.Context, modelID int, filter HistoryFilter) ([]HistoryRow, error)

	// ModelExists returns true if a model with the given id exists.
	// It is cheaper than GetModel because it avoids the price JOIN.
	ModelExists(ctx context.Context, id int) (bool, error)

	// ListModelsForContext returns the top N models for the /v1/context
	// endpoint (Developer+ tier, issue #20).
	ListModelsForContext(ctx context.Context, limit int) ([]ContextModelRow, error)

	// RecommendModels returns models ranked by price for the /v1/recommend
	// endpoint (Developer+ tier, issue #20).
	RecommendModels(ctx context.Context, filter RecommendFilter) ([]ModelRow, error)
}

// ErrNotFound is returned by Store methods when a requested resource does not
// exist in the database. Handlers convert it to a 404 ProblemDetail.
var ErrNotFound = fmt.Errorf("not found")

// --- filter / row types -------------------------------------------------

// ListModelsFilter carries query-string parameters for the GET /v1/models
// endpoint. Zero values mean "no filter applied".
type ListModelsFilter struct {
	// Provider filters by models.provider (case-insensitive ILIKE match).
	Provider string
	// Modality filters by models.modality exact match.
	Modality string
	// MinContext filters to models with context_window >= MinContext.
	// nil means no lower bound.
	MinContext *int
	// Page is 1-based page number; 0 and negative values are treated as 1.
	Page int
	// PerPage is the maximum number of results per page; 0 is treated as 50.
	PerPage int
}

// ChangesFilter carries query-string parameters for the GET /v1/changes
// endpoint.
type ChangesFilter struct {
	// Since restricts results to changes confirmed after this time.
	// nil defaults to the last 24 hours.
	Since *time.Time
	// Provider filters by provider name (exact match).
	Provider string
}

// HistoryFilter carries query-string parameters for the
// GET /v1/models/:id/history endpoint (used by issue #20).
type HistoryFilter struct {
	// From restricts to records confirmed at or after this time.
	From *time.Time
	// To restricts to records confirmed at or before this time.
	To *time.Time
}

// RecommendFilter carries parameters for the GET /v1/recommend endpoint
// (used by issue #20).
type RecommendFilter struct {
	Task          string
	ContextSize   *int
	MaxPriceInput *float64
}

// ModelRow is a denormalised view of a model joined with its latest price and
// trust metadata, used by list/detail/compare handlers.
type ModelRow struct {
	ID            int
	Provider      string
	Name          string
	Slug          string
	Modality      string
	ContextWindow *int
	// PriceInput is input_cost_per_token from the prices table (latest row).
	PriceInput float64
	// PriceOutput is output_cost_per_token from the prices table (latest row).
	PriceOutput float64
	// ConfirmedAt is when the price was most recently confirmed.
	ConfirmedAt time.Time
	// Source is the human-readable source name for the latest price.
	Source string
	// Meta holds the pre-computed TrustMeta for this model.
	Meta api.TrustMeta
}

// ProviderRow represents a provider with its model count.
type ProviderRow struct {
	Name       string
	ModelCount int
}

// ChangeRow represents a single price-change event detected from price_history.
type ChangeRow struct {
	ModelID     int
	ModelSlug   string
	Provider    string
	OldInput    float64
	OldOutput   float64
	NewInput    float64
	NewOutput   float64
	ConfirmedAt time.Time
	Source      string
}

// HistoryRow is one price_history record returned by GetModelHistory.
type HistoryRow struct {
	InputCostPerToken  float64
	OutputCostPerToken float64
	Source             string
	ConfirmedAt        time.Time
	RecordedAt         time.Time
}

// ContextModelRow is a lightweight model record for the /v1/context endpoint.
type ContextModelRow struct {
	ID          int
	Provider    string
	Slug        string
	PriceInput  float64
	PriceOutput float64
	Confidence  string
	ConfirmedAt time.Time
}

// --- pgxStore implementation --------------------------------------------

// pgxStore is the production Store backed by a pgxpool.Pool.
type pgxStore struct {
	db *pgxpool.Pool
}

// NewPgxStore creates a Store backed by the supplied pgx connection pool.
func NewPgxStore(db *pgxpool.Pool) Store {
	return &pgxStore{db: db}
}

// page returns safe offset/limit values from a ListModelsFilter.
func (f ListModelsFilter) page() (offset, limit int) {
	limit = f.PerPage
	if limit <= 0 {
		limit = 50
	}
	p := f.Page
	if p <= 0 {
		p = 1
	}
	offset = (p - 1) * limit
	return
}

// ListModels returns paginated models with optional filters.
func (s *pgxStore) ListModels(ctx context.Context, filter ListModelsFilter) ([]ModelRow, int, error) {
	offset, limit := filter.page()

	// Build WHERE clause dynamically.
	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if filter.Provider != "" {
		where += fmt.Sprintf(" AND m.provider ILIKE $%d", argIdx)
		args = append(args, filter.Provider)
		argIdx++
	}
	if filter.Modality != "" {
		where += fmt.Sprintf(" AND m.modality = $%d", argIdx)
		args = append(args, filter.Modality)
		argIdx++
	}
	if filter.MinContext != nil {
		where += fmt.Sprintf(" AND m.context_window >= $%d", argIdx)
		args = append(args, *filter.MinContext)
		argIdx++
	}

	// Count query — runs with the same filter args, without pagination.
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM models m
		%s
	`, where)

	var total int
	if err := s.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("list models count: %w", err)
	}

	// Main query: join with latest price and source name.
	args = append(args, limit, offset)
	mainSQL := fmt.Sprintf(`
		SELECT
			m.id,
			m.provider,
			m.name,
			m.slug,
			m.modality,
			m.context_window,
			COALESCE(p.input_cost_per_token, 0)::float8  AS price_input,
			COALESCE(p.output_cost_per_token, 0)::float8 AS price_output,
			COALESCE(p.confirmed_at, m.created_at)       AS confirmed_at,
			COALESCE(src.name, '')                        AS source
		FROM models m
		LEFT JOIN LATERAL (
			SELECT input_cost_per_token, output_cost_per_token, confirmed_at, source_id
			FROM prices
			WHERE model_id = m.id
			ORDER BY confirmed_at DESC
			LIMIT 1
		) p ON true
		LEFT JOIN sources src ON src.id = p.source_id
		%s
		ORDER BY m.provider, m.name
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)

	rows, err := s.db.Query(ctx, mainSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list models query: %w", err)
	}
	defer rows.Close()

	var models []ModelRow
	for rows.Next() {
		var r ModelRow
		if err := rows.Scan(
			&r.ID, &r.Provider, &r.Name, &r.Slug, &r.Modality,
			&r.ContextWindow, &r.PriceInput, &r.PriceOutput,
			&r.ConfirmedAt, &r.Source,
		); err != nil {
			return nil, 0, fmt.Errorf("list models scan: %w", err)
		}
		models = append(models, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list models rows: %w", err)
	}

	// Batch-load price history for all returned models in a single query.
	if len(models) > 0 {
		ids := make([]int, len(models))
		for i, m := range models {
			ids[i] = m.ID
		}
		historyBatch, err := s.GetPriceHistoryBatch(ctx, ids)
		if err != nil {
			return nil, 0, fmt.Errorf("list models history batch: %w", err)
		}
		for i := range models {
			models[i].Meta = api.ComputeTrustMeta(historyBatch[models[i].ID])
		}
	}

	return models, total, nil
}

// GetModel returns a single model by ID.
func (s *pgxStore) GetModel(ctx context.Context, id int) (ModelRow, error) {
	sql := `
		SELECT
			m.id,
			m.provider,
			m.name,
			m.slug,
			m.modality,
			m.context_window,
			COALESCE(p.input_cost_per_token, 0)::float8  AS price_input,
			COALESCE(p.output_cost_per_token, 0)::float8 AS price_output,
			COALESCE(p.confirmed_at, m.created_at)       AS confirmed_at,
			COALESCE(src.name, '')                        AS source
		FROM models m
		LEFT JOIN LATERAL (
			SELECT input_cost_per_token, output_cost_per_token, confirmed_at, source_id
			FROM prices
			WHERE model_id = m.id
			ORDER BY confirmed_at DESC
			LIMIT 1
		) p ON true
		LEFT JOIN sources src ON src.id = p.source_id
		WHERE m.id = $1
	`
	var r ModelRow
	err := s.db.QueryRow(ctx, sql, id).Scan(
		&r.ID, &r.Provider, &r.Name, &r.Slug, &r.Modality,
		&r.ContextWindow, &r.PriceInput, &r.PriceOutput,
		&r.ConfirmedAt, &r.Source,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ModelRow{}, ErrNotFound
		}
		return ModelRow{}, fmt.Errorf("get model %d: %w", id, err)
	}

	history, err := s.GetPriceHistory(ctx, r.ID)
	if err != nil {
		return ModelRow{}, fmt.Errorf("get model history %d: %w", id, err)
	}
	r.Meta = api.ComputeTrustMeta(history)
	return r, nil
}

// ListProviders returns all distinct providers and their model counts.
func (s *pgxStore) ListProviders(ctx context.Context) ([]ProviderRow, error) {
	sql := `
		SELECT provider, COUNT(*) AS model_count
		FROM models
		GROUP BY provider
		ORDER BY provider
	`
	rows, err := s.db.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("list providers query: %w", err)
	}
	defer rows.Close()

	var providers []ProviderRow
	for rows.Next() {
		var r ProviderRow
		if err := rows.Scan(&r.Name, &r.ModelCount); err != nil {
			return nil, fmt.Errorf("list providers scan: %w", err)
		}
		providers = append(providers, r)
	}
	return providers, rows.Err()
}

// CompareModels returns model rows for the given IDs. Returns ErrNotFound if
// any requested ID is absent from the database.
func (s *pgxStore) CompareModels(ctx context.Context, ids []int) ([]ModelRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build $1, $2, ... placeholder list.
	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.provider,
			m.name,
			m.slug,
			m.modality,
			m.context_window,
			COALESCE(p.input_cost_per_token, 0)::float8  AS price_input,
			COALESCE(p.output_cost_per_token, 0)::float8 AS price_output,
			COALESCE(p.confirmed_at, m.created_at)       AS confirmed_at,
			COALESCE(src.name, '')                        AS source
		FROM models m
		LEFT JOIN LATERAL (
			SELECT input_cost_per_token, output_cost_per_token, confirmed_at, source_id
			FROM prices
			WHERE model_id = m.id
			ORDER BY confirmed_at DESC
			LIMIT 1
		) p ON true
		LEFT JOIN sources src ON src.id = p.source_id
		WHERE m.id IN (%s)
	`, placeholders)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("compare models query: %w", err)
	}
	defer rows.Close()

	var models []ModelRow
	for rows.Next() {
		var r ModelRow
		if err := rows.Scan(
			&r.ID, &r.Provider, &r.Name, &r.Slug, &r.Modality,
			&r.ContextWindow, &r.PriceInput, &r.PriceOutput,
			&r.ConfirmedAt, &r.Source,
		); err != nil {
			return nil, fmt.Errorf("compare models scan: %w", err)
		}
		models = append(models, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compare models rows: %w", err)
	}

	// Verify all requested IDs were found.
	found := make(map[int]struct{}, len(models))
	for _, m := range models {
		found[m.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return nil, ErrNotFound
		}
	}

	// Batch-load price history for all returned models in a single query.
	if len(models) > 0 {
		ids := make([]int, len(models))
		for i, m := range models {
			ids[i] = m.ID
		}
		historyBatch, err := s.GetPriceHistoryBatch(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("compare models history batch: %w", err)
		}
		for i := range models {
			models[i].Meta = api.ComputeTrustMeta(historyBatch[models[i].ID])
		}
	}

	return models, nil
}

// ListChanges returns price-change events detected from price_history.
// A change event is a record whose (input_cost_per_token, output_cost_per_token)
// differs from the immediately preceding record for the same model.
func (s *pgxStore) ListChanges(ctx context.Context, filter ChangesFilter) ([]ChangeRow, error) {
	since := filter.Since
	if since == nil {
		t := time.Now().UTC().Add(-24 * time.Hour)
		since = &t
	}

	args := []any{*since}
	argIdx := 2
	providerFilter := ""
	if filter.Provider != "" {
		providerFilter = fmt.Sprintf("AND m.provider = $%d", argIdx)
		args = append(args, filter.Provider)
		argIdx++
	}

	// Use LAG window function to detect rows where price changed vs prior row.
	sql := fmt.Sprintf(`
		WITH ranked AS (
			SELECT
				ph.model_id,
				ph.input_cost_per_token::float8  AS new_input,
				ph.output_cost_per_token::float8 AS new_output,
				ph.confirmed_at,
				src.name                          AS source,
				LAG(ph.input_cost_per_token::float8)  OVER w AS old_input,
				LAG(ph.output_cost_per_token::float8) OVER w AS old_output
			FROM price_history ph
			JOIN models m ON m.id = ph.model_id
			JOIN sources src ON src.id = ph.source_id
			WHERE ph.confirmed_at > $1
			%s
			WINDOW w AS (PARTITION BY ph.model_id ORDER BY ph.confirmed_at)
		)
		SELECT
			r.model_id,
			m.slug,
			m.provider,
			COALESCE(r.old_input, 0)  AS old_input,
			COALESCE(r.old_output, 0) AS old_output,
			r.new_input,
			r.new_output,
			r.confirmed_at,
			r.source
		FROM ranked r
		JOIN models m ON m.id = r.model_id
		WHERE r.old_input IS NOT NULL
		  AND (r.new_input <> r.old_input OR r.new_output <> r.old_output)
		ORDER BY r.confirmed_at DESC
	`, providerFilter)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list changes query: %w", err)
	}
	defer rows.Close()

	var changes []ChangeRow
	for rows.Next() {
		var c ChangeRow
		if err := rows.Scan(
			&c.ModelID, &c.ModelSlug, &c.Provider,
			&c.OldInput, &c.OldOutput,
			&c.NewInput, &c.NewOutput,
			&c.ConfirmedAt, &c.Source,
		); err != nil {
			return nil, fmt.Errorf("list changes scan: %w", err)
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

// GetPriceHistory returns all price_history rows for a model, ordered by
// confirmed_at ascending. Used to compute TrustMeta.
func (s *pgxStore) GetPriceHistory(ctx context.Context, modelID int) ([]api.PriceHistoryRow, error) {
	sql := `
		SELECT
			ph.input_cost_per_token::float8,
			ph.output_cost_per_token::float8,
			COALESCE(src.name, ''),
			ph.confirmed_at,
			ph.recorded_at
		FROM price_history ph
		LEFT JOIN sources src ON src.id = ph.source_id
		WHERE ph.model_id = $1
		ORDER BY ph.confirmed_at ASC
	`
	rows, err := s.db.Query(ctx, sql, modelID)
	if err != nil {
		return nil, fmt.Errorf("get price history %d: %w", modelID, err)
	}
	defer rows.Close()

	var history []api.PriceHistoryRow
	for rows.Next() {
		var r api.PriceHistoryRow
		if err := rows.Scan(
			&r.InputCostPerToken, &r.OutputCostPerToken,
			&r.Source, &r.ConfirmedAt, &r.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("get price history scan %d: %w", modelID, err)
		}
		history = append(history, r)
	}
	return history, rows.Err()
}

// GetPriceHistoryBatch returns price history for multiple models in a single
// query. The returned map is keyed by model_id; rows within each slice are
// ordered by confirmed_at ascending.
func (s *pgxStore) GetPriceHistoryBatch(ctx context.Context, modelIDs []int) (map[int][]api.PriceHistoryRow, error) {
	if len(modelIDs) == 0 {
		return map[int][]api.PriceHistoryRow{}, nil
	}

	// Build the $1,$2,... placeholder list and args slice.
	args := make([]any, len(modelIDs))
	placeholders := make([]string, len(modelIDs))
	for i, id := range modelIDs {
		args[i] = id
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	sql := fmt.Sprintf(`
		SELECT
			ph.model_id,
			ph.input_cost_per_token::float8,
			ph.output_cost_per_token::float8,
			COALESCE(src.name, ''),
			ph.confirmed_at,
			ph.recorded_at
		FROM price_history ph
		LEFT JOIN sources src ON src.id = ph.source_id
		WHERE ph.model_id = ANY(ARRAY[%s]::int[])
		ORDER BY ph.model_id, ph.confirmed_at ASC
	`, joinStrings(placeholders))

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("get price history batch: %w", err)
	}
	defer rows.Close()

	result := make(map[int][]api.PriceHistoryRow, len(modelIDs))
	for rows.Next() {
		var modelID int
		var r api.PriceHistoryRow
		if err := rows.Scan(
			&modelID,
			&r.InputCostPerToken, &r.OutputCostPerToken,
			&r.Source, &r.ConfirmedAt, &r.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("get price history batch scan: %w", err)
		}
		result[modelID] = append(result[modelID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get price history batch rows: %w", err)
	}
	return result, nil
}

// joinStrings concatenates a slice of strings with ", " between each element.
func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// ModelExists returns true if a model row with the given id exists.
func (s *pgxStore) ModelExists(ctx context.Context, id int) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM models WHERE id = $1)`, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("model exists %d: %w", id, err)
	}
	return exists, nil
}

// GetModelHistory returns paginated price_history for the
// /v1/models/:id/history endpoint (Developer+ tier, issue #20).
func (s *pgxStore) GetModelHistory(ctx context.Context, modelID int, filter HistoryFilter) ([]HistoryRow, error) {
	args := []any{modelID}
	argIdx := 2
	where := "WHERE ph.model_id = $1"

	if filter.From != nil {
		where += fmt.Sprintf(" AND ph.confirmed_at >= $%d", argIdx)
		args = append(args, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		where += fmt.Sprintf(" AND ph.confirmed_at <= $%d", argIdx)
		args = append(args, *filter.To)
		argIdx++
	}
	_ = argIdx

	sql := fmt.Sprintf(`
		SELECT
			ph.input_cost_per_token::float8,
			ph.output_cost_per_token::float8,
			COALESCE(src.name, ''),
			ph.confirmed_at,
			ph.recorded_at
		FROM price_history ph
		LEFT JOIN sources src ON src.id = ph.source_id
		%s
		ORDER BY ph.confirmed_at DESC
	`, where)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("get model history %d: %w", modelID, err)
	}
	defer rows.Close()

	var history []HistoryRow
	for rows.Next() {
		var r HistoryRow
		if err := rows.Scan(
			&r.InputCostPerToken, &r.OutputCostPerToken,
			&r.Source, &r.ConfirmedAt, &r.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("get model history scan %d: %w", modelID, err)
		}
		history = append(history, r)
	}
	return history, rows.Err()
}

// ListModelsForContext returns the top N models for the /v1/context endpoint.
func (s *pgxStore) ListModelsForContext(ctx context.Context, limit int) ([]ContextModelRow, error) {
	sql := `
		SELECT
			m.id,
			m.provider,
			m.slug,
			COALESCE(p.input_cost_per_token, 0)::float8  AS price_input,
			COALESCE(p.output_cost_per_token, 0)::float8 AS price_output,
			COALESCE(p.confidence, 'low'),
			COALESCE(p.confirmed_at, m.created_at)
		FROM models m
		LEFT JOIN LATERAL (
			SELECT input_cost_per_token, output_cost_per_token, confidence, confirmed_at
			FROM prices
			WHERE model_id = m.id
			ORDER BY confirmed_at DESC
			LIMIT 1
		) p ON true
		ORDER BY m.provider, m.name
		LIMIT $1
	`
	rows, err := s.db.Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("list models for context: %w", err)
	}
	defer rows.Close()

	var models []ContextModelRow
	for rows.Next() {
		var r ContextModelRow
		if err := rows.Scan(
			&r.ID, &r.Provider, &r.Slug,
			&r.PriceInput, &r.PriceOutput,
			&r.Confidence, &r.ConfirmedAt,
		); err != nil {
			return nil, fmt.Errorf("list models for context scan: %w", err)
		}
		models = append(models, r)
	}
	return models, rows.Err()
}

// RecommendModels returns models ranked by input price for the
// /v1/recommend endpoint.
func (s *pgxStore) RecommendModels(ctx context.Context, filter RecommendFilter) ([]ModelRow, error) {
	args := []any{}
	argIdx := 1
	where := "WHERE 1=1"

	if filter.ContextSize != nil {
		where += fmt.Sprintf(" AND m.context_window >= $%d", argIdx)
		args = append(args, *filter.ContextSize)
		argIdx++
	}
	if filter.MaxPriceInput != nil {
		where += fmt.Sprintf(" AND COALESCE(p.input_cost_per_token, 0)::float8 <= $%d", argIdx)
		args = append(args, *filter.MaxPriceInput)
		argIdx++
	}
	_ = argIdx

	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.provider,
			m.name,
			m.slug,
			m.modality,
			m.context_window,
			COALESCE(p.input_cost_per_token, 0)::float8  AS price_input,
			COALESCE(p.output_cost_per_token, 0)::float8 AS price_output,
			COALESCE(p.confirmed_at, m.created_at)       AS confirmed_at,
			COALESCE(src.name, '')                        AS source
		FROM models m
		LEFT JOIN LATERAL (
			SELECT input_cost_per_token, output_cost_per_token, confirmed_at, source_id
			FROM prices
			WHERE model_id = m.id
			ORDER BY confirmed_at DESC
			LIMIT 1
		) p ON true
		LEFT JOIN sources src ON src.id = p.source_id
		%s
		ORDER BY price_input ASC
		LIMIT 50
	`, where)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("recommend models query: %w", err)
	}
	defer rows.Close()

	var models []ModelRow
	for rows.Next() {
		var r ModelRow
		if err := rows.Scan(
			&r.ID, &r.Provider, &r.Name, &r.Slug, &r.Modality,
			&r.ContextWindow, &r.PriceInput, &r.PriceOutput,
			&r.ConfirmedAt, &r.Source,
		); err != nil {
			return nil, fmt.Errorf("recommend models scan: %w", err)
		}
		models = append(models, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recommend models rows: %w", err)
	}

	// Batch-load price history for all returned models in a single query.
	if len(models) > 0 {
		ids := make([]int, len(models))
		for i, m := range models {
			ids[i] = m.ID
		}
		historyBatch, err := s.GetPriceHistoryBatch(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("recommend models history batch: %w", err)
		}
		for i := range models {
			models[i].Meta = api.ComputeTrustMeta(historyBatch[models[i].ID])
		}
	}

	return models, nil
}
