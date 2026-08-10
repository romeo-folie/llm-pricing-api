// TODO: replace with a working Chatbot Arena data source when one becomes available.

// Package chatbot_arena implements a benchmark scraper for the Chatbot Arena
// (LMSYS) leaderboard. The upstream API (lmarena.ai/api/leaderboard) returned
// 403 as of 2025 and there is no public replacement. This scraper is a no-op
// stub that logs clearly and returns nil so the cron job does not error-loop.
package chatbot_arena

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/scraper"
)

const (
	benchmarkName = "Chatbot Arena"
)

// Scraper is a no-op stub for the Chatbot Arena leaderboard.
// db and client are retained to satisfy the constructor signature used by
// handlers.go (swebench.New(h.db, nil)) and are unused by the no-op Scrape method.
type Scraper struct {
	db     *pgxpool.Pool //nolint:unused // retained for constructor API consistency
	client *http.Client  //nolint:unused // retained for constructor API consistency
	logger zerolog.Logger
}

// Ensure Scraper implements BenchmarkScraper at compile time.
var _ scraper.BenchmarkScraper = (*Scraper)(nil)

// New returns a Chatbot Arena scraper backed by the given DB pool.
func New(db *pgxpool.Pool, client *http.Client) *Scraper {
	if client == nil {
		client = &http.Client{
			Timeout:   60 * time.Second,
			Transport: scraper.NewSSRFSafeTransport(),
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				return scraper.CheckRedirectHost(req.Context(), req.URL.Hostname())
			},
		}
	}
	return &Scraper{db: db, client: client, logger: zerolog.Nop()}
}

// SetLogger configures the structured logger.
func (s *Scraper) SetLogger(l zerolog.Logger) { s.logger = l }

// Scrape is a no-op. The lmarena.ai API is no longer publicly accessible.
func (s *Scraper) Scrape(_ context.Context) error {
	s.logger.Warn().
		Str("benchmark", benchmarkName).
		Str("reason", "lmarena.ai/api/leaderboard returned 403 — API is no longer publicly accessible").
		Msg("chatbot_arena: skipping scrape — no public endpoint available")
	return nil
}
