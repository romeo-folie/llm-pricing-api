package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/intelligence"
)

// ErrDuplicateFeedback is returned by InsertFeedback when the unique constraint
// (session_id, model_slug, use_case) is violated at the DB level.
var ErrDuplicateFeedback = errors.New("duplicate feedback")

// FeedbackRequest is the JSON body for POST /v1/feedback.
type FeedbackRequest struct {
	UseCase   string `json:"use_case"`
	ModelSlug string `json:"model_slug"`
	Signal    int    `json:"signal"`
	Context   string `json:"context"`
	SessionID string `json:"session_id"`
}

// FeedbackStore abstracts DB operations for recommendation feedback.
type FeedbackStore interface {
	// ModelSlugExists returns true if the given slug exists in the models table.
	ModelSlugExists(ctx context.Context, slug string) (bool, error)

	// IsDuplicateFeedback returns true if the same (session_id, model_slug, use_case)
	// was submitted within the last hour.
	IsDuplicateFeedback(ctx context.Context, sessionID, modelSlug, useCase string) (bool, error)

	// InsertFeedback persists a recommendation feedback row.
	InsertFeedback(ctx context.Context, f FeedbackRow) error

	// TopFeedbackModels returns the top N models by average signal for a use case
	// within the given number of days. If positive is true, returns top upvoted;
	// otherwise top downvoted.
	TopFeedbackModels(ctx context.Context, useCase string, days, limit int, positive bool) ([]FeedbackSummary, error)

	// GetEffectiveWeights returns the latest weight presets for the given use case,
	// keyed by priority. Returns nil map if no rows exist.
	GetEffectiveWeights(ctx context.Context, useCase string) (map[string]map[string]float64, error)
}

// FeedbackRow represents a row in the recommendation_feedback table.
type FeedbackRow struct {
	SessionID string
	APIKeyID  string
	UseCase   string
	ModelSlug string
	Signal    int
	Context   string
}

// FeedbackSummary is a summary of feedback signals for a model.
type FeedbackSummary struct {
	ModelSlug string  `json:"model_slug"`
	AvgSignal float64 `json:"avg_signal"`
	Count     int     `json:"count"`
}

// pgxFeedbackStore is the PostgreSQL-backed FeedbackStore.
type pgxFeedbackStore struct {
	db *pgxpool.Pool
}

// NewFeedbackStore returns a FeedbackStore backed by the given pool.
func NewFeedbackStore(db *pgxpool.Pool) FeedbackStore {
	return &pgxFeedbackStore{db: db}
}

func (s *pgxFeedbackStore) ModelSlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM models WHERE slug = $1)`, slug).Scan(&exists)
	return exists, err
}

func (s *pgxFeedbackStore) IsDuplicateFeedback(ctx context.Context, sessionID, modelSlug, useCase string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM recommendation_feedback
			WHERE session_id = $1 AND model_slug = $2 AND use_case = $3
			  AND created_at > NOW() - INTERVAL '1 hour'
		)
	`, sessionID, modelSlug, useCase).Scan(&exists)
	return exists, err
}

func (s *pgxFeedbackStore) InsertFeedback(ctx context.Context, f FeedbackRow) error {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO recommendation_feedback (session_id, api_key_id, use_case, model_slug, signal, context)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (session_id, model_slug, use_case) DO NOTHING
	`, f.SessionID, nilIfEmpty(f.APIKeyID), f.UseCase, f.ModelSlug, f.Signal, nilIfEmpty(f.Context))
	if err != nil {
		// Translate unique_violation (23505) to the typed sentinel in case the
		// constraint is enforced via a path that doesn't use ON CONFLICT.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateFeedback
		}
		return err
	}
	// ON CONFLICT DO NOTHING: zero rows affected means a duplicate was suppressed.
	if tag.RowsAffected() == 0 {
		return ErrDuplicateFeedback
	}
	return nil
}

func (s *pgxFeedbackStore) TopFeedbackModels(ctx context.Context, useCase string, days, limit int, positive bool) ([]FeedbackSummary, error) {
	order := "ASC"
	if positive {
		order = "DESC"
	}
	// order is a compile-time constant ("ASC" or "DESC"), safe to interpolate.
	query := `
		SELECT model_slug, AVG(signal)::float8 AS avg_signal, COUNT(*)::int AS cnt
		FROM recommendation_feedback
		WHERE use_case = $1 AND created_at > NOW() - ($2 || ' days')::interval
		GROUP BY model_slug
		HAVING COUNT(*) >= 3
		ORDER BY avg_signal ` + order + `
		LIMIT $3
	`
	rows, err := s.db.Query(ctx, query, useCase, days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FeedbackSummary
	for rows.Next() {
		var fs FeedbackSummary
		if err := rows.Scan(&fs.ModelSlug, &fs.AvgSignal, &fs.Count); err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}

func (s *pgxFeedbackStore) GetEffectiveWeights(ctx context.Context, useCase string) (map[string]map[string]float64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (priority) priority, weights
		FROM weight_preset_history
		WHERE use_case = $1
		ORDER BY priority, effective_at DESC
	`, useCase)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]map[string]float64)
	for rows.Next() {
		var priority string
		var weights map[string]float64
		if err := rows.Scan(&priority, &weights); err != nil {
			return nil, err
		}
		result[priority] = weights
	}
	if len(result) == 0 {
		return nil, rows.Err()
	}
	return result, rows.Err()
}

// nilIfEmpty returns nil for empty strings to store NULL in the DB.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// FeedbackHandler handles POST /v1/feedback.
type FeedbackHandler struct {
	store FeedbackStore
}

// NewFeedbackHandler creates a FeedbackHandler.
func NewFeedbackHandler(store FeedbackStore) *FeedbackHandler {
	return &FeedbackHandler{store: store}
}

// Create handles POST /v1/feedback.
func (h *FeedbackHandler) Create(c *fiber.Ctx) error {
	var body FeedbackRequest
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid JSON body")
	}

	// Validate required fields.
	if body.SessionID == "" {
		return api.NewBadRequest("session_id is required")
	}
	if body.ModelSlug == "" {
		return api.NewBadRequest("model_slug is required")
	}
	if body.UseCase == "" {
		return api.NewBadRequest("use_case is required")
	}
	if body.Signal != 1 && body.Signal != -1 {
		return api.NewBadRequest("signal must be 1 or -1")
	}

	// Validate use_case against known values.
	if !intelligence.ValidUseCases[intelligence.UseCase(body.UseCase)] {
		return api.NewBadRequest("invalid use_case: " + body.UseCase)
	}

	// Check model slug exists.
	exists, err := h.store.ModelSlugExists(c.Context(), body.ModelSlug)
	if err != nil {
		return api.NewInternalError("failed to validate model slug")
	}
	if !exists {
		return api.NewBadRequest("unknown model_slug: " + body.ModelSlug)
	}

	// Check for duplicate within last hour.
	dup, err := h.store.IsDuplicateFeedback(c.Context(), body.SessionID, body.ModelSlug, body.UseCase)
	if err != nil {
		return api.NewInternalError("failed to check for duplicate feedback")
	}
	if dup {
		return api.NewConflict("duplicate feedback: same session_id + model_slug + use_case within 1 hour")
	}

	// Extract API key ID if present (optional auth).
	apiKeyID, _ := c.Locals("key_hash").(string)

	row := FeedbackRow{
		SessionID: body.SessionID,
		APIKeyID:  apiKeyID,
		UseCase:   body.UseCase,
		ModelSlug: body.ModelSlug,
		Signal:    body.Signal,
		Context:   body.Context,
	}

	if err := h.store.InsertFeedback(c.Context(), row); err != nil {
		if errors.Is(err, ErrDuplicateFeedback) {
			return api.NewConflict("duplicate feedback: same session_id + model_slug + use_case")
		}
		return api.NewInternalError("failed to record feedback")
	}

	return api.Created(c, fiber.Map{"status": "recorded"}, api.TrustMeta{
		ConfirmedAt: time.Now().UTC(),
		Source:      "user_feedback",
		Confidence:  "high",
	})
}
