// Package bfcl implements a benchmark scraper for the Berkeley Function Calling
// Leaderboard (BFCL V4). It fetches overall accuracy scores and upserts them
// into the model_benchmark_scores table via the intelligence package.
package bfcl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/intelligence"
	"llm-pricing-api/internal/scraper"
	"llm-pricing-api/internal/scraper/slugmap"
)

const (
	// jsonURL is the primary data source — a JSON dump of the leaderboard.
	jsonURL = "https://gorilla.cs.berkeley.edu/leaderboard_bigquery_legacy_data/BFCL_v3_combined.json"
	// sourceURL is the human-readable leaderboard page used for source attribution.
	sourceURL    = "https://gorilla.cs.berkeley.edu/leaderboard.html"
	benchmarkName = "BFCL V4"
)

// Scraper fetches BFCL leaderboard data and upserts benchmark scores.
type Scraper struct {
	db     *pgxpool.Pool
	client *http.Client
	logger zerolog.Logger
}

// Ensure Scraper implements BenchmarkScraper at compile time.
var _ scraper.BenchmarkScraper = (*Scraper)(nil)

// New returns a BFCL scraper backed by the given DB pool.
// If client is nil, a default client with a 60s timeout and SSRF-safe transport
// is used.
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

// leaderboardEntry represents one row in the BFCL JSON dataset.
type leaderboardEntry struct {
	Model          string  `json:"model"`
	OverallAcc     float64 `json:"overall_acc"`
	Rank           int     `json:"rank"`
}

// Scrape fetches the BFCL leaderboard, resolves model names to canonical slugs,
// and upserts benchmark scores for each matched model.
func (s *Scraper) Scrape(ctx context.Context) error {
	benchmarkID, err := intelligence.LookupBenchmarkID(ctx, s.db, benchmarkName)
	if err != nil {
		return fmt.Errorf("bfcl: %w", err)
	}

	entries, err := s.fetchJSON(ctx)
	if err != nil {
		return fmt.Errorf("bfcl: %w", err)
	}

	now := time.Now().UTC()
	version := fmt.Sprintf("v4-%s", now.Format("2006-01"))

	var matched, skipped int
	for _, e := range entries {
		slug, ok := slugmap.Resolve(e.Model)
		if !ok {
			s.logger.Debug().Str("model", e.Model).Msg("bfcl: no slug mapping — skipping")
			skipped++
			continue
		}

		modelID, err := intelligence.LookupModelIDBySlug(ctx, s.db, slug)
		if err != nil {
			s.logger.Debug().Str("slug", slug).Err(err).Msg("bfcl: model not in DB — skipping")
			skipped++
			continue
		}

		// Overall accuracy is already 0–100.
		raw := e.OverallAcc
		norm := e.OverallAcc
		if err := intelligence.UpsertBenchmarkScore(ctx, s.db, intelligence.BenchmarkScore{
			ModelID:          modelID,
			BenchmarkID:      benchmarkID,
			RawScore:         &raw,
			NormalizedScore:  &norm,
			BenchmarkVersion: version,
			SourceURL:        sourceURL,
			Confidence:       "high",
			EvaluatedAt:      now,
		}); err != nil {
			return fmt.Errorf("bfcl: upsert score for %s: %w", slug, err)
		}
		matched++
	}

	s.logger.Info().Int("matched", matched).Int("skipped", skipped).Msg("bfcl: scrape complete")
	return nil
}

// fetchJSON retrieves the leaderboard JSON data.
func (s *Scraper) fetchJSON(ctx context.Context) ([]leaderboardEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "llm-pricing-api/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var entries []leaderboardEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return entries, nil
}
